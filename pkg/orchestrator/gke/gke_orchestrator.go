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

package gke

import (
	"bytes"
	"fmt"
	"hpc-toolkit/pkg/imagebuilder" // New import for the image builder
	"hpc-toolkit/pkg/logging"
	"hpc-toolkit/pkg/orchestrator"
	"hpc-toolkit/pkg/shell"
	"io"
	"net/http"
	"os"
	"strings"
	"text/template"

	"gopkg.in/yaml.v2"
)

// JobSetTemplate is the Go template for generating a Kubernetes JobSet manifest.
const JobSetTemplate = `
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: {{.WorkloadName}}
  labels:
    gcluster.google.com/workload: {{.WorkloadName}}
    kueue.x-k8s.io/queue-name: {{.KueueQueueName}} # Name of the LocalQueue
spec:
  ttlSecondsAfterFinished: {{.TtlSecondsAfterFinished}}
  failurePolicy:
    maxRestarts: {{.MaxRestarts}}
  replicatedJobs:
    - name: main-job # ReplicatedJob name, for now a single one for the main workload
      replicas: {{.NumSlices}}
      template:
        spec:
          parallelism: {{.VmsPerSlice}}    # Equal to the number of VMs per slice (or sub-slice).
          completions: {{.VmsPerSlice}}    # Same as the above.
          backoffLimit: 0   # When any pod fails, the job is failed
          template:
            metadata:
              labels:
                gcluster.google.com/workload: {{.WorkloadName}}
            spec:
              restartPolicy: Never
              containers:
              - name: workload-container
                image: {{.FullImageName}}
                command: ["/bin/bash", "-c", "{{.CommandToRun}}"]
                resources:
                  limits:
                    nvidia.com/gpu: {{.GpuLimit}}
                    cpu: {{.CPULimit}}
                    memory: {{.MemoryLimit}}
                # Add a placeholder volume mount for potential future storage integration
                volumeMounts:
                - name: temp-storage
                  mountPath: /mnt/data
              volumes:
              - name: temp-storage
                emptyDir: {}
{{- if .AcceleratorTypeLabel }}
              nodeSelector:
                cloud.google.com/gke-accelerator: {{.AcceleratorTypeLabel}}
{{- end }}
`

// ManifestOptions holds parameters for GKE manifest generation
type ManifestOptions struct {
	WorkloadName            string // Renamed from JobName
	FullImageName           string
	CommandToRun            string
	AcceleratorType         string // Original value, e.g., "nvidia-tesla-a100"
	GpuLimit                string // These limits will eventually come from RunOptions
	CPULimit                string
	MemoryLimit             string
	ProjectID               string
	ClusterName             string
	ClusterLocation         string
	KueueQueueName          string
	NumSlices               int
	VmsPerSlice             int
	MaxRestarts             int
	TtlSecondsAfterFinished int
}

// GKEOrchestrator implements the Orchestrator interface for GKE.
type GKEOrchestrator struct {
	// Add any GKE-specific clients or configurations here if needed.
}

// NewGKEOrchestrator creates and returns a new GKEOrchestrator instance.
func NewGKEOrchestrator() (*GKEOrchestrator, error) {
	return &GKEOrchestrator{}, nil
}

// SubmitJob orchestrates the Cloud Build and GKE manifest deployment for a GKE cluster.
func (g *GKEOrchestrator) SubmitJob(job orchestrator.JobDefinition) error {
	logging.Info("Starting gcluster run workflow...")

	projectID, err := g.getProjectID(job.ProjectID)
	if err != nil {
		return err
	}
	job.ProjectID = projectID

	// Configure kubectl for GKE cluster *before* checking for CRDs
	logging.Info("Configuring kubectl for GKE cluster '%s'...", job.ClusterName)
	err = g.configureKubectl(job.ClusterName, job.ClusterLocation, job.ProjectID)
	if err != nil {
		return err
	}
	logging.Info("kubectl configured successfully.")

	// Check and install JobSet CRD if not present
	if err := g.checkAndInstallJobSetCRD(); err != nil {
		return fmt.Errorf("failed to check or install JobSet CRD: %w", err)
	}

	fullImageName, err := g.buildDockerImage(job.ProjectID, job.BaseDockerImage, job.BuildContext, job.Platform, job.DockerImage)
	if err != nil {
		return err
	}

	err = g.generateAndApplyManifest(ManifestOptions{
		WorkloadName:            job.WorkloadName,
		FullImageName:           fullImageName,
		CommandToRun:            job.CommandToRun,
		AcceleratorType:         job.AcceleratorType,
		ProjectID:               job.ProjectID,
		ClusterName:             job.ClusterName,
		ClusterLocation:         job.ClusterLocation,
		KueueQueueName:          job.KueueQueueName,
		NumSlices:               job.NumSlices,
		VmsPerSlice:             job.VmsPerSlice,
		MaxRestarts:             job.MaxRestarts,
		TtlSecondsAfterFinished: job.TtlSecondsAfterFinished,
	}, job.OutputManifest)
	if err != nil {
		return err
	}

	logging.Info("gcluster run workflow completed.")
	return nil
}

func (g *GKEOrchestrator) getProjectID(initialProjectID string) (string, error) {
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

func (g *GKEOrchestrator) buildDockerImage(project, baseDockerImage, buildContext, platformStr, dockerImage string) (string, error) {
	if baseDockerImage != "" {
		logging.Info("Building Docker image using Crane (Go implementation) on top of %s...", baseDockerImage)

		ignorePatterns := []string{
			".git",
			".terraform",
			".ghpc",
			".ansible",
			"vendor",
			"bin",
			"pkg",
			"node_modules",
			"*.log",
			"tmp/",
			".DS_Store",
			"__pycache__",
		}

		ignoreMatcher, err := imagebuilder.ReadDockerignorePatterns(buildContext, ignorePatterns)
		if err != nil {
			return "", fmt.Errorf("failed to read .dockerignore patterns: %w", err)
		}

		fullImageName, err := imagebuilder.BuildContainerImageFromBaseImage(
			project,
			baseDockerImage,
			buildContext,
			platformStr,
			ignoreMatcher,
		)
		if err != nil {
			return "", fmt.Errorf("crane-based image build failed: %w", err)
		}
		logging.Info("Built image will be available at: %s", fullImageName)
		return fullImageName, nil
	} else if dockerImage != "" {
		logging.Info("Using pre-existing Docker image: %s", dockerImage)
		return dockerImage, nil
	} else {
		return "", fmt.Errorf("internal error: neither --docker-image nor --base-docker-image was provided, but CLI validation should have caught this")
	}
}

func (g *GKEOrchestrator) configureKubectl(clusterName, clusterLocation, projectID string) error {
	credsRes := shell.ExecuteCommand("gcloud", "container", "clusters", "get-credentials", clusterName, "--zone", clusterLocation, "--project", projectID)
	if credsRes.ExitCode != 0 {
		return fmt.Errorf("failed to get GKE cluster credentials: %s\n%s", credsRes.Stderr, credsRes.Stdout)
	}
	return nil
}

func (g *GKEOrchestrator) generateAndApplyManifest(opts ManifestOptions, outputManifestPath string) error {
	logging.Info("Generating GKE manifest...")
	gkeManifestContent, err := g.GenerateGKEManifest(opts)
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
		err = g.applyJobSetManifests([]byte(gkeManifestContent))
		if err != nil {
			return fmt.Errorf("failed to apply GKE manifest: %w", err)
		}
		logging.Info("GKE workload deployed successfully.")
	}
	return nil
}

func (g *GKEOrchestrator) checkAndInstallJobSetCRD() error {
	if installed, err := g.isJobSetCRDInstalled(); err != nil {
		return err
	} else if installed {
		return nil
	}

	jobSetManifestsURL := "https://github.com/kubernetes-sigs/jobset/releases/download/v0.10.1/manifests.yaml"
	return g.installJobSetCRD(jobSetManifestsURL)
}

func (g *GKEOrchestrator) installJobSetCRD(jobSetManifestsURL string) error {
	logging.Info("JobSet CRD not found. Installing now...")

	manifestBytes, err := g.downloadJobSetManifests(jobSetManifestsURL)
	if err != nil {
		return err
	}

	cleanedManifests, err := g.cleanJobSetManifests(manifestBytes)
	if err != nil {
		return err
	}

	if err := g.applyJobSetManifests(cleanedManifests); err != nil {
		return err
	}

	logging.Info("JobSet CRD installed successfully.")

	return g.waitForJobSetWebhook()
}

// waitForJobSetWebhook waits for the JobSet controller manager deployment to be available.
func (g *GKEOrchestrator) waitForJobSetWebhook() error {
	logging.Info("Waiting for JobSet webhook service to be ready...")
	// Wait for the deployment to be available.
	// We use a generous timeout because pulling images might take time on some clusters.
	cmd := shell.NewCommand("kubectl", "rollout", "status", "deployment/jobset-controller-manager", "-n", "jobset-system", "--timeout=300s")
	res := cmd.Execute()
	if res.ExitCode != 0 {
		return fmt.Errorf("jobset controller manager failed to become ready: %s\n%s", res.Stderr, res.Stdout)
	}
	logging.Info("JobSet webhook service is ready.")
	return nil
}

func (g *GKEOrchestrator) isJobSetCRDInstalled() (bool, error) {
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

func (g *GKEOrchestrator) downloadJobSetManifests(url string) ([]byte, error) {
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

func (g *GKEOrchestrator) cleanJobSetManifests(manifestBytes []byte) ([]byte, error) {
	logging.Info("Cleaning JobSet manifests (removing description fields)...")
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

		if doc == nil {
			continue
		}

		if data, ok := doc.(map[interface{}]interface{}); ok {
			g.removeDescriptionFields(data)
			cleanedBytes, err := yaml.Marshal(data)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal cleaned YAML: %w", err)
			}
			cleanedManifests.Write(cleanedBytes)
			cleanedManifests.WriteString("---\n")
		} else {
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

func (g *GKEOrchestrator) applyJobSetManifests(manifests []byte) error {
	logging.Info("Applying JobSet manifests...")
	cmd := shell.NewCommand("kubectl", "apply", "-f", "-")
	cmd.SetInput(string(manifests))
	res := cmd.Execute()
	if res.ExitCode != 0 {
		return fmt.Errorf("kubectl apply failed with exit code %d: %s\n%s", res.ExitCode, res.Stderr, res.Stdout)
	}
	logging.Info("JobSet manifests applied successfully.")
	return nil
}

func (g *GKEOrchestrator) removeDescriptionFields(data map[interface{}]interface{}) {
	for key, value := range data {
		if key == "description" {
			delete(data, key)
			continue
		}
		if subMap, ok := value.(map[interface{}]interface{}); ok {
			g.removeDescriptionFields(subMap)
		} else if subList, ok := value.([]interface{}); ok {
			for _, item := range subList {
				if itemMap, ok := item.(map[interface{}]interface{}); ok {
					g.removeDescriptionFields(itemMap)
				}
			}
		}
	}
}

// GenerateGKENodeSelectorLabel generates the node selector label based on accelerator type.
func (g *GKEOrchestrator) GenerateGKENodeSelectorLabel(acceleratorType string) string {
	switch acceleratorType {
	case "nvidia-tesla-a100":
		return "nvidia-tesla-a100"
	case "tpu-v4-podslice":
		return "tpu-v4-podslice"
	default:
		return acceleratorType
	}
}

// GenerateGKEManifest generates the Kubernetes JobSet manifest content
func (g *GKEOrchestrator) GenerateGKEManifest(opts ManifestOptions) (string, error) {
	workloadName := opts.WorkloadName
	if workloadName == "" {
		workloadName = "gcluster-workload-" + shell.RandomString(8)
	}

	kueueQueueName := opts.KueueQueueName
	if kueueQueueName == "" {
		kueueQueueName = "default-queue"
	}

	numSlices := opts.NumSlices
	if numSlices == 0 {
		numSlices = 1
	}

	vmsPerSlice := opts.VmsPerSlice
	if vmsPerSlice == 0 {
		vmsPerSlice = 1
	}

	maxRestarts := opts.MaxRestarts
	if maxRestarts == 0 {
		maxRestarts = 1
	}

	ttlSecondsAfterFinished := opts.TtlSecondsAfterFinished
	if ttlSecondsAfterFinished == 0 {
		ttlSecondsAfterFinished = 3600
	}

	acceleratorTypeLabel := g.GenerateGKENodeSelectorLabel(opts.AcceleratorType)
	var gpuLimit, cpuLimit, memoryLimit string

	switch opts.AcceleratorType {
	case "nvidia-tesla-a100":
		gpuLimit = "1"
		cpuLimit = "8"
		memoryLimit = "64Gi"
	case "tpu-v4-podslice":
		gpuLimit = "0"
		cpuLimit = "16"
		memoryLimit = "128Gi"
	default: // Default CPU-only workload
		gpuLimit = "0"
		cpuLimit = "0.5"
		memoryLimit = "512Mi"
	}

	tmpl, err := template.New("jobSet").Parse(JobSetTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse jobset template: %w", err)
	}

	data := struct {
		WorkloadName            string
		KueueQueueName          string
		TtlSecondsAfterFinished int
		MaxRestarts             int
		NumSlices               int
		VmsPerSlice             int
		FullImageName           string
		CommandToRun            string
		AcceleratorTypeLabel    string
		GpuLimit                string
		CPULimit                string
		MemoryLimit             string
	}{
		WorkloadName:            workloadName,
		KueueQueueName:          kueueQueueName,
		TtlSecondsAfterFinished: ttlSecondsAfterFinished,
		MaxRestarts:             maxRestarts,
		NumSlices:               numSlices,
		VmsPerSlice:             vmsPerSlice,
		FullImageName:           opts.FullImageName,
		CommandToRun:            opts.CommandToRun,
		AcceleratorTypeLabel:    acceleratorTypeLabel,
		GpuLimit:                gpuLimit,
		CPULimit:                cpuLimit,
		MemoryLimit:             memoryLimit,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute jobset template: %w", err)
	}
	return buf.String(), nil
}
