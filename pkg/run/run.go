// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package run

import (
	"bytes"
	"fmt"
	"hpc-toolkit/pkg/logging"
	"hpc-toolkit/pkg/run/gkemanifest"
	"hpc-toolkit/pkg/shell"
	"hpc-toolkit/pkg/workload_image"
	"io"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v2" // Using yaml.v2 for programmatic YAML manipulation
)

// RunOptions holds all the necessary parameters for the 'run' command logic
type RunOptions struct {
	DockerImage     string // Renamed from ImageName, for pre-built image
	BaseDockerImage string // For Crane builds, to specify base image
	BuildContext    string
	Platform        string // Added for platform flag
	CommandToRun    string
	AcceleratorType string
	OutputManifest  string // If set, manifest is saved here instead of applied
	ProjectID       string
	ClusterName     string
	ClusterLocation string

	// JobSet and Kueue related options
	WorkloadName            string
	KueueQueueName          string
	NumSlices               int
	VmsPerSlice             int
	MaxRestarts             int
	TtlSecondsAfterFinished int
}

// ExecuteRun orchestrates the Cloud Build and GKE manifest deployment
func ExecuteRun(opts RunOptions) error {
	logging.Info("Starting gcluster run workflow...")

	projectID, err := getProjectID(opts.ProjectID)
	if err != nil {
		return err
	}
	opts.ProjectID = projectID

	// Configure kubectl for GKE cluster *before* checking for CRDs
	logging.Info("Configuring kubectl for GKE cluster '%s'...", opts.ClusterName)
	err = configureKubectl(opts.ClusterName, opts.ClusterLocation, opts.ProjectID)
	if err != nil {
		return err
	}
	logging.Info("kubectl configured successfully.")

	// Check and install JobSet CRD if not present
	if err := checkAndInstallJobSetCRD(); err != nil {
		return fmt.Errorf("failed to check or install JobSet CRD: %w", err)
	}

	fullImageName, err := buildDockerImage(opts)
	if err != nil {
		return err
	}

	err = generateAndApplyManifest(generateManifestOptions{
		WorkloadName:    opts.WorkloadName, // Use WorkloadName
		FullImageName:   fullImageName,
		CommandToRun:    opts.CommandToRun,
		AcceleratorType: opts.AcceleratorType,
		ProjectID:       opts.ProjectID,
		ClusterName:     opts.ClusterName,
		ClusterLocation: opts.ClusterLocation,
		// Pass JobSet and Kueue specific options
		KueueQueueName:          opts.KueueQueueName,
		NumSlices:               opts.NumSlices,
		VmsPerSlice:             opts.VmsPerSlice,
		MaxRestarts:             opts.MaxRestarts,
		TtlSecondsAfterFinished: opts.TtlSecondsAfterFinished,
	}, opts.OutputManifest)
	if err != nil {
		return err
	}

	logging.Info("gcluster run workflow completed.")
	return nil
}

func getProjectID(initialProjectID string) (string, error) {
	if initialProjectID == "" {
		res := shell.ExecuteCommand("gcloud", "config", "get-value", "project")
		if res.ExitCode != 0 {
			return "", fmt.Errorf("failed to get GCP project ID from gcloud config: %s", res.Stderr)
		}
		projectID := strings.TrimSpace(res.Stdout)
		if projectID == "" {
			return "", fmt.Errorf("GCP project ID is empty. Please provide it via --project flag or configure gcloud CLI.")
		}
		logging.Info("Using GCP Project ID inferred from gcloud config: %s", projectID)
		return projectID, nil
	}
	logging.Info("Using provided GCP Project ID: %s", initialProjectID)
	return initialProjectID, nil
}

func buildDockerImage(opts RunOptions) (string, error) {
	if opts.BaseDockerImage != "" {
		// Scenario: Crane build requested
		// The --build-context validation is now done in cmd/run.go
		logging.Info("Building Docker image using Crane (Go implementation) on top of %s...", opts.BaseDockerImage)

		ignorePatterns := []string{
			".git",
			".terraform",
			".ghpc",    // Added to ignore patterns
			".ansible", // Explicitly ignore .ansible directories
			"vendor",
			"bin",
			"pkg",
			"node_modules",
			"*.log",
			"tmp/",
			".DS_Store",
			"__pycache__",
		}

		dockerignorePatterns, err := workload_image.ReadDockerignorePatterns(opts.BuildContext)
		if err != nil {
			return "", fmt.Errorf("failed to read .dockerignore patterns: %w", err)
		}
		ignorePatterns = append(ignorePatterns, dockerignorePatterns...)

		fullImageName, err := workload_image.BuildContainerImageFromBaseImage(
			opts.ProjectID,
			opts.BaseDockerImage,
			opts.BuildContext,
			opts.Platform,
			ignorePatterns, // Pass the defined ignorePatterns argument
		)
		if err != nil {
			return "", fmt.Errorf("crane-based image build failed: %w", err)
		}
		logging.Info("Built image will be available at: %s", fullImageName)
		return fullImageName, nil
	} else if opts.DockerImage != "" {
		// Scenario: Use pre-existing image
		// The --build-context validation is now done in cmd/run.go
		logging.Info("Using pre-existing Docker image: %s", opts.DockerImage)
		return opts.DockerImage, nil
	} else {
		// This case should ideally be caught by CLI validation in cmd/run.go
		return "", fmt.Errorf("internal error: neither --docker-image nor --base-docker-image was provided, but CLI validation should have caught this")
	}
}

func configureKubectl(clusterName, clusterLocation, projectID string) error {
	credsRes := shell.ExecuteCommand("gcloud", "container", "clusters", "get-credentials", clusterName, "--zone", clusterLocation, "--project", projectID)
	if credsRes.ExitCode != 0 {
		return fmt.Errorf("failed to get GKE cluster credentials: %s\n%s", credsRes.Stderr, credsRes.Stdout)
	}
	return nil
}

type generateManifestOptions struct {
	WorkloadName    string
	FullImageName   string
	CommandToRun    string
	AcceleratorType string
	ProjectID       string
	ClusterName     string
	ClusterLocation string

	// JobSet and Kueue related options
	KueueQueueName          string
	NumSlices               int
	VmsPerSlice             int
	MaxRestarts             int
	TtlSecondsAfterFinished int
}

func generateAndApplyManifest(opts generateManifestOptions, outputManifestPath string) error {
	logging.Info("Generating GKE manifest...")
	gkeManifestContent, err := gkemanifest.GenerateGKEManifest(gkemanifest.ManifestOptions{
		WorkloadName:            opts.WorkloadName, // Use WorkloadName
		FullImageName:           opts.FullImageName,
		CommandToRun:            opts.CommandToRun,
		AcceleratorType:         opts.AcceleratorType,
		ProjectID:               opts.ProjectID,
		ClusterName:             opts.ClusterName,
		ClusterLocation:         opts.ClusterLocation,
		KueueQueueName:          opts.KueueQueueName,
		NumSlices:               opts.NumSlices,
		VmsPerSlice:             opts.VmsPerSlice,
		MaxRestarts:             opts.MaxRestarts,
		TtlSecondsAfterFinished: opts.TtlSecondsAfterFinished,
	})
	if err != nil {
		return fmt.Errorf("failed to generate GKE manifest: %w", err)
	}

	if outputManifestPath != "" {
		logging.Info("Saving GKE manifest to %s", outputManifestPath)
		if err := os.WriteFile(outputManifestPath, []byte(gkeManifestContent), 0644); err != nil {
			return fmt.Errorf("failed to write GKE manifest to file %s: %w", outputManifestPath, err)
		}
		logging.Info("GKE manifest saved successfully.")
	} else {
		logging.Info("Applying GKE manifest to cluster...")
		err = gkemanifest.ApplyGKEManifest(gkeManifestContent, opts.ProjectID, opts.ClusterName, opts.ClusterLocation)
		if err != nil {
			return fmt.Errorf("failed to apply GKE manifest: %w", err)
		}
		logging.Info("GKE workload deployed successfully.")
	}
	return nil
}

// checkAndInstallJobSetCRD orchestrates the checking and installation of the JobSet CRD.
func checkAndInstallJobSetCRD() error {
	if installed, err := isJobSetCRDInstalled(); err != nil {
		return err
	} else if installed {
		return nil
	}

	jobSetManifestsURL := "https://github.com/kubernetes-sigs/jobset/releases/download/v0.10.1/manifests.yaml"
	return installJobSetCRD(jobSetManifestsURL)
}

// installJobSetCRD downloads, cleans, and applies the JobSet manifests.
func installJobSetCRD(jobSetManifestsURL string) error {
	logging.Info("JobSet CRD not found. Installing now...")

	manifestBytes, err := downloadJobSetManifests(jobSetManifestsURL)
	if err != nil {
		return err
	}

	cleanedManifests, err := cleanJobSetManifests(manifestBytes)
	if err != nil {
		return err
	}

	if err := applyJobSetManifests(cleanedManifests); err != nil {
		return err
	}

	logging.Info("JobSet CRD installed successfully.")
	return nil
}

// isJobSetCRDInstalled checks if the JobSet CRD is already installed.
func isJobSetCRDInstalled() (bool, error) {
	logging.Info("Checking for JobSet CRD installation...")
	res := shell.ExecuteCommand("kubectl", "get", "crd", "jobsets.kueue.x-k8s.io")
	if res.ExitCode == 0 {
		logging.Info("JobSet CRD already installed.")
		return true, nil
	}
	if strings.Contains(res.Stderr, "not found") || strings.Contains(res.Stdout, "NotFound") {
		logging.Info("JobSet CRD not found.")
		return false, nil
	}
	return false, fmt.Errorf("failed to check for JobSet CRD: %s\n%s", res.Stderr, res.Stdout)
}

// downloadJobSetManifests downloads the JobSet manifests from the given URL.
func downloadJobSetManifests(url string) ([]byte, error) {
	logging.Info("Downloading JobSet manifests from %s", url)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download JobSet manifests: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download JobSet manifests: received status code %d", resp.StatusCode)
	}

	manifestBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read JobSet manifests: %w", err)
	}
	return manifestBytes, nil
}

// cleanJobSetManifests removes 'description' fields from the YAML manifests.
func cleanJobSetManifests(manifestBytes []byte) ([]byte, error) {
	logging.Info("Cleaning JobSet manifests (removing description fields)...")
	// Split the multi-document YAML
	decoder := yaml.NewDecoder(bytes.NewReader(manifestBytes))
	var cleanedManifests bytes.Buffer

	for {
		var doc interface{}
		if err := decoder.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to decode YAML document: %w", err)
		}

		if doc == nil { // Skip empty documents
			continue
		}

		// Convert to map and remove description fields
		if data, ok := doc.(map[interface{}]interface{}); ok {
			removeDescriptionFields(data)
			cleanedBytes, err := yaml.Marshal(data)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal cleaned YAML: %w", err)
			}
			cleanedManifests.Write(cleanedBytes)
			cleanedManifests.WriteString("---\n") // Separator for multi-document YAML
		} else {
			// If it's not a map, just re-marshal it as is
			cleanedBytes, err := yaml.Marshal(doc)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal YAML document: %w", err)
			}
			cleanedManifests.Write(cleanedBytes)
			cleanedManifests.WriteString("---\n")
		}
	}
	return cleanedManifests.Bytes(), nil
}

// applyJobSetManifests applies the cleaned JobSet manifests to the cluster.
func applyJobSetManifests(manifests []byte) error {
	logging.Info("Applying JobSet manifests...")
	cmd := shell.NewCommand("kubectl", "apply", "-f", "-")
	cmd.SetInput(string(manifests))
	res := cmd.Execute()
	if res.ExitCode != 0 {
		return fmt.Errorf("failed to apply JobSet manifests: %s\n%s", res.Stderr, res.Stdout)
	}
	logging.Info("JobSet manifests applied successfully.")
	return nil
}

// removeDescriptionFields recursively removes 'description' keys from a map.
func removeDescriptionFields(data map[interface{}]interface{}) {
	for key, value := range data {
		if key == "description" {
			delete(data, key)
			continue
		}
		if subMap, ok := value.(map[interface{}]interface{}); ok {
			removeDescriptionFields(subMap)
		} else if subList, ok := value.([]interface{}); ok {
			for _, item := range subList {
				if itemMap, ok := item.(map[interface{}]interface{}); ok {
					removeDescriptionFields(itemMap)
				}
			}
		}
	}
}
