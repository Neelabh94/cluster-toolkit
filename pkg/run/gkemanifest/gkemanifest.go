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

package gkemanifest

import (
	"bytes"
	"fmt"
	"hpc-toolkit/pkg/logging"
	"hpc-toolkit/pkg/shell"
	"os"
	"strings"
	"text/template"
)

// KubernetesJobTemplate is the Go template for generating a basic Kubernetes Job manifest.
// This is a simplified version inspired by xpk's JobSet, focusing on basic functionality for POC.
const KubernetesJobTemplate = `
apiVersion: batch/v1
kind: Job
metadata:
  name: {{.JobName}}
  labels:
    gcluster.google.com/workload: {{.JobName}}
spec:
  template:
    metadata:
      labels:
        gcluster.google.com/workload: {{.JobName}}
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
	JobName         string
	FullImageName   string
	CommandToRun    string
	AcceleratorType string // Original value, e.g., "nvidia-tesla-a100"
	GpuLimit        string
	CPULimit        string
	MemoryLimit     string
	ProjectID       string
	ClusterName     string
	ClusterLocation string
}

// GenerateGKENodeSelectorLabel generates the node selector label based on accelerator type.
// This is a simplified mapping for POC.
func GenerateGKENodeSelectorLabel(acceleratorType string) string {
	switch acceleratorType {
	case "nvidia-tesla-a100":
		return "nvidia-tesla-a100"
	case "tpu-v4-podslice":
		return "tpu-v4-podslice"
	// Add more mappings as needed
	default:
		return acceleratorType // Fallback
	}
}

// GenerateGKEManifest generates the Kubernetes Job manifest content
func GenerateGKEManifest(opts ManifestOptions) (string, error) {
	jobName := opts.JobName
	if jobName == "" {
		jobName = "gcluster-workload" // Default job name if not provided
	}

	acceleratorTypeLabel := GenerateGKENodeSelectorLabel(opts.AcceleratorType)
	gpuLimit := "0"
	cpuLimit := "1"
	memoryLimit := "2Gi"

	switch opts.AcceleratorType {
	case "nvidia-tesla-a100":
		gpuLimit = "1"
		cpuLimit = "8"
		memoryLimit = "64Gi"
	case "tpu-v4-podslice":
		// TPU Pod Slices require specific configuration, and 'gpuLimit' might not be applicable directly.
		// For POC, we'll keep it simple, but this would need proper TPU specific manifests.
		gpuLimit = "0" // No GPUs for TPU
		cpuLimit = "16"
		memoryLimit = "128Gi"
	default: // Default CPU-only workload
		gpuLimit = "0"
		cpuLimit = "0.5"      // Reduced CPU
		memoryLimit = "512Mi" // Reduced Memory
	}

	tmpl, err := template.New("kubernetesJob").Parse(KubernetesJobTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse kubernetes job template: %w", err)
	}

	data := struct {
		JobName              string
		FullImageName        string
		CommandToRun         string
		AcceleratorTypeLabel string
		GpuLimit             string
		CPULimit             string
		MemoryLimit          string
	}{
		JobName:              jobName,
		FullImageName:        opts.FullImageName,
		CommandToRun:         opts.CommandToRun,
		AcceleratorTypeLabel: acceleratorTypeLabel,
		GpuLimit:             gpuLimit,
		CPULimit:             cpuLimit,
		MemoryLimit:          memoryLimit,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute kubernetes job template: %w", err)
	}
	return buf.String(), nil
}

// ApplyGKEManifest applies the generated Kubernetes manifest to the GKE cluster
func ApplyGKEManifest(manifestContent string, projectID, clusterName, clusterLocation string) error {
	tmpFile, err := os.CreateTemp("", "gke-manifest-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temporary GKE manifest file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(manifestContent); err != nil {
		return fmt.Errorf("failed to write GKE manifest content to temporary file: %w", err)
	}

	logging.Info("Applying GKE manifest to cluster '%s' in '%s' for project '%s'...", clusterName, clusterLocation, projectID)
	logging.Info("GKE Manifest YAML content:\n%s", manifestContent)

	// Ensure kubectl is configured for the target cluster (POC simplified)
	// In a real scenario, this might involve 'gcloud container clusters get-credentials'
	// For now, assume kubectl context is already set up correctly by the user.

	cmdArgs := []string{"kubectl", "apply", "-f", tmpFile.Name()}
	logging.Info("Executing: %s", strings.Join(cmdArgs, " "))
	result := shell.ExecuteCommand(cmdArgs[0], cmdArgs[1:]...)

	if result.ExitCode != 0 {
		return fmt.Errorf("kubectl apply failed with exit code %d: %s\n%s", result.ExitCode, result.Stderr, result.Stdout)
	}

	logging.Info("GKE manifest applied successfully.")
	return nil
}
