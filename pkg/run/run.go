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
	"fmt"
	"hpc-toolkit/pkg/logging"
	"hpc-toolkit/pkg/run/cloudbuild"
	"hpc-toolkit/pkg/run/gkemanifest"
	"hpc-toolkit/pkg/shell"
	"os"
	"path/filepath"
	"strings"
)

// RunOptions holds all the necessary parameters for the 'run' command logic
type RunOptions struct {
	ImageName       string
	Dockerfile      string
	BuildContext    string
	CommandToRun    string
	AcceleratorType string
	OutputManifest  string // If set, manifest is saved here instead of applied
	ProjectID       string
	ClusterName     string
	ClusterLocation string
	UseCraneBuilder bool
}

// ExecuteRun orchestrates the Cloud Build and GKE manifest deployment
func ExecuteRun(opts RunOptions) error {
	logging.Info("Starting gcluster run workflow...")

	projectID, err := getProjectID(opts.ProjectID)
	if err != nil {
		return err
	}
	opts.ProjectID = projectID

	fullImageName, err := buildDockerImage(opts)
	if err != nil {
		return err
	}

	// 3. Generate and Apply GKE Manifest
	logging.Info("Configuring kubectl for GKE cluster '%s'...", opts.ClusterName)
	err = configureKubectl(opts.ClusterName, opts.ClusterLocation, opts.ProjectID)
	if err != nil {
		return err
	}
	logging.Info("kubectl configured successfully.")

	err = generateAndApplyManifest(generateManifestOptions{
		JobName:         "gcluster-workload-" + shell.RandomString(8), // Unique job name
		FullImageName:   fullImageName,
		CommandToRun:    opts.CommandToRun,
		AcceleratorType: opts.AcceleratorType,
		ProjectID:       opts.ProjectID,
		ClusterName:     opts.ClusterName,
		ClusterLocation: opts.ClusterLocation,
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
	var fullImageName string
	var err error

	if opts.UseCraneBuilder {
		logging.Info("Building Docker image using crane (Python implementation)...")
		fullImageName, err = executeCraneBuild(opts)
		if err != nil {
			return "", fmt.Errorf("crane-based image build failed: %w", err)
		}
	} else {
		logging.Info("Initiating Cloud Build for Docker image...")
		cbOpts := cloudbuild.BuildOptions{
			ImageName:    opts.ImageName,
			Dockerfile:   opts.Dockerfile,
			BuildContext: opts.BuildContext,
			ProjectID:    opts.ProjectID,
			Region:       "us-central1", // For POC, assume us-central1 or get from gcloud config
		}

		cloudBuildYaml, err := cloudbuild.GenerateCloudBuildYaml(cbOpts)
		if err != nil {
			return "", fmt.Errorf("failed to generate Cloud Build YAML: %w", err)
		}

		buildURL, err := cloudbuild.SubmitCloudBuild(cloudBuildYaml, opts.BuildContext, opts.ProjectID)
		if err != nil {
			return "", fmt.Errorf("failed to submit Cloud Build: %w", err)
		}
		if buildURL != "" {
			logging.Info("Cloud Build submitted. Monitor its progress at: %s", buildURL)
		} else {
			logging.Info("Cloud Build submitted. Use 'gcloud builds list' to monitor progress.")
		}

		fullImageName, err = cloudbuild.GetFullImageName(opts.ImageName, opts.ProjectID, cbOpts.Region)
		if err != nil {
			return "", fmt.Errorf("failed to construct full image name: %w", err)
		}
	}
	logging.Info("Assuming image will be available at: %s", fullImageName)
	return fullImageName, nil
}

func configureKubectl(clusterName, clusterLocation, projectID string) error {
	credsRes := shell.ExecuteCommand("gcloud", "container", "clusters", "get-credentials", clusterName, "--zone", clusterLocation, "--project", projectID)
	if credsRes.ExitCode != 0 {
		return fmt.Errorf("failed to get GKE cluster credentials: %s\n%s", credsRes.Stderr, credsRes.Stdout)
	}
	return nil
}

type generateManifestOptions struct {
	JobName         string
	FullImageName   string
	CommandToRun    string
	AcceleratorType string
	ProjectID       string
	ClusterName     string
	ClusterLocation string
}

func generateAndApplyManifest(opts generateManifestOptions, outputManifestPath string) error {
	logging.Info("Generating GKE manifest...")
	gkeManifestContent, err := gkemanifest.GenerateGKEManifest(gkemanifest.ManifestOptions{
		JobName:         opts.JobName,
		FullImageName:   opts.FullImageName,
		CommandToRun:    opts.CommandToRun,
		AcceleratorType: opts.AcceleratorType,
		ProjectID:       opts.ProjectID,
		ClusterName:     opts.ClusterName,
		ClusterLocation: opts.ClusterLocation,
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

func executeCraneBuild(opts RunOptions) (string, error) {
	pythonExec := "python3"
	pythonScriptRelPath := "pkg/workload_image/crane_build.py"

	// Get the absolute path to the Python script
	pythonScriptPath, err := filepath.Abs(pythonScriptRelPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for Python script %s: %w", pythonScriptRelPath, err)
	}

	// Arguments to pass to the Python script
	// The Python executable expects the script path as its first argument.
	scriptAndArgs := []string{
		pythonScriptPath,
		"--project", opts.ProjectID,
		"--image-name", opts.ImageName,
		"--dockerfile", opts.Dockerfile,
		"--script-dir", opts.BuildContext,
		"--platform", "linux/amd64", // Hardcoded for now, needs to be dynamic
	}

	// Construct the full command array for logging
	fullCmd := append([]string{pythonExec}, scriptAndArgs...)
	logging.Info("Executing Python crane builder script: %s", strings.Join(fullCmd, " "))

	// Execute the Python script
	res := shell.ExecuteCommand(pythonExec, scriptAndArgs...)

	if res.ExitCode != 0 {
		return "", fmt.Errorf("Python crane builder script failed. Stderr: %s\nStdout: %s", res.Stderr, res.Stdout)
	}

	fullImageName := strings.TrimSpace(res.Stdout)
	if fullImageName == "" {
		return "", fmt.Errorf("Python crane builder script did not return a full image name. Stderr: %s\nStdout: %s", res.Stderr, res.Stdout)
	}

	return fullImageName, nil
}
