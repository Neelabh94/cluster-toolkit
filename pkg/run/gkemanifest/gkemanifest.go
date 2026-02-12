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
	WorkloadName    string // Renamed from JobName
	FullImageName   string
	CommandToRun    string
	AcceleratorType string // Original value, e.g., "nvidia-tesla-a100"
	GpuLimit        string // These limits will eventually come from RunOptions
	CPULimit        string
	MemoryLimit     string
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

// GenerateGKEManifest generates the Kubernetes JobSet manifest content
func GenerateGKEManifest(opts ManifestOptions) (string, error) {
	workloadName := opts.WorkloadName
	if workloadName == "" {
		workloadName = "gcluster-workload-" + shell.RandomString(8) // Default workload name if not provided
	}

	kueueQueueName := opts.KueueQueueName
	if kueueQueueName == "" {
		kueueQueueName = "default-queue" // Default Kueue LocalQueue name
	}

	// Default values for JobSet fields, if not provided
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
		maxRestarts = 1 // Default to 1 restart
	}

	ttlSecondsAfterFinished := opts.TtlSecondsAfterFinished
	if ttlSecondsAfterFinished == 0 {
		ttlSecondsAfterFinished = 3600 // Default to 1 hour
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
		gpuLimit = "0" // No GPUs for TPU
		cpuLimit = "16"
		memoryLimit = "128Gi"
	default: // Default CPU-only workload
		gpuLimit = "0"
		cpuLimit = "0.5"      // Reduced CPU
		memoryLimit = "512Mi" // Reduced Memory
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
