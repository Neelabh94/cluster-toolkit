// Copyright 2026 "Google LLC"
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gkemanifest

import (
	"fmt" // Re-added
	"testing"

	"sigs.k8s.io/yaml" // For parsing YAML for assertions
)

// assertJobSetMetadata checks the API version, kind, metadata name, and labels of the JobSet manifest.
func assertJobSetMetadata(t *testing.T, result map[string]interface{}, expectedAPIVersion, expectedKind, expectedWorkloadName, expectedKueueQueueName string) {
	t.Helper()

	if apiVersion := result["apiVersion"]; apiVersion != expectedAPIVersion {
		t.Errorf("Expected apiVersion %q, got %q", expectedAPIVersion, apiVersion)
	}
	if kind := result["kind"]; kind != expectedKind {
		t.Errorf("Expected kind %q, got %q", expectedKind, kind)
	}

	metadata, ok := result["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata not found or not a map")
	}
	if name := metadata["name"]; name != expectedWorkloadName {
		t.Errorf("Expected metadata.name %q, got %q", expectedWorkloadName, name)
	}
	labels, ok := metadata["labels"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata.labels not found or not a map")
	}
	if workloadLabel := labels["gcluster.google.com/workload"]; workloadLabel != expectedWorkloadName {
		t.Errorf("Expected gcluster.google.com/workload label %q, got %q", expectedWorkloadName, workloadLabel)
	}
	if kueueLabel := labels["kueue.x-k8s.io/queue-name"]; kueueLabel != expectedKueueQueueName {
		t.Errorf("Expected kueue.x-k8s.io/queue-name label %q, got %q", expectedKueueQueueName, kueueLabel)
	}
}

// assertJobSetSpec checks the spec fields of the JobSet manifest.
func assertJobSetSpec(t *testing.T, result map[string]interface{}, expectedTtlSecondsAfterFinished, expectedMaxRestarts, expectedNumSlices, expectedVmsPerSlice int) {
	t.Helper()

	components := getJobSetSpecComponents(t, result)

	if ttl := components.spec["ttlSecondsAfterFinished"]; int(ttl.(float64)) != expectedTtlSecondsAfterFinished {
		t.Errorf("Expected spec.ttlSecondsAfterFinished %d, got %v", expectedTtlSecondsAfterFinished, ttl)
	}
	failurePolicy, ok := components.spec["failurePolicy"].(map[string]interface{})
	if !ok {
		t.Fatalf("spec.failurePolicy not found or not a map")
	}
	if maxRestarts := failurePolicy["maxRestarts"]; int(maxRestarts.(float64)) != expectedMaxRestarts {
		t.Errorf("Expected spec.failurePolicy.maxRestarts %d, got %v", expectedMaxRestarts, maxRestarts)
	}

	if replicas := components.mainJob["replicas"]; int(replicas.(float64)) != expectedNumSlices {
		t.Errorf("Expected replicatedJobs[0].replicas %d, got %v", expectedNumSlices, replicas)
	}

	if parallelism := components.jobTemplateSpec["parallelism"]; int(parallelism.(float64)) != expectedVmsPerSlice {
		t.Errorf("Expected replicatedJobs[0].template.spec.parallelism %d, got %v", expectedVmsPerSlice, parallelism)
	}
	if completions := components.jobTemplateSpec["completions"]; int(completions.(float64)) != expectedVmsPerSlice {
		t.Errorf("Expected replicatedJobs[0].template.spec.completions %d, got %v", expectedVmsPerSlice, completions)
	}
	if backoffLimit := components.jobTemplateSpec["backoffLimit"]; int(backoffLimit.(float64)) != 0 {
		t.Errorf("Expected replicatedJobs[0].template.spec.backoffLimit %d, got %v", 0, backoffLimit)
	}
}

type jobSetSpecComponents struct {
	spec            map[string]interface{}
	replicatedJobs  []interface{}
	mainJob         map[string]interface{}
	jobTemplate     map[string]interface{}
	jobTemplateSpec map[string]interface{}
}

func getJobSetSpecComponents(t *testing.T, result map[string]interface{}) jobSetSpecComponents {
	t.Helper()

	spec, ok := result["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("spec not found or not a map")
	}

	replicatedJobs, ok := spec["replicatedJobs"].([]interface{})
	if !ok || len(replicatedJobs) == 0 {
		t.Fatalf("replicatedJobs not found or empty")
	}
	mainJob, ok := replicatedJobs[0].(map[string]interface{})
	if !ok {
		t.Fatalf("mainJob not found or not a map")
	}

	jobTemplate, ok := mainJob["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("replicatedJobs[0].template not found or not a map")
	}
	jobTemplateSpec, ok := jobTemplate["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("replicatedJobs[0].template.spec not found or not a map")
	}

	return jobSetSpecComponents{
		spec:            spec,
		replicatedJobs:  replicatedJobs,
		mainJob:         mainJob,
		jobTemplate:     jobTemplate,
		jobTemplateSpec: jobTemplateSpec,
	}
}

type jobSetContainerComponents struct {
	spec            map[string]interface{}
	replicatedJobs  []interface{}
	mainJob         map[string]interface{}
	jobTemplate     map[string]interface{}
	jobTemplateSpec map[string]interface{}
	podTemplate     map[string]interface{}
	podSpec         map[string]interface{}
	containers      []interface{}
	container       map[string]interface{}
}

func getJobSetContainerComponents(t *testing.T, result map[string]interface{}) jobSetContainerComponents {
	t.Helper()

	spec, ok := result["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("spec not found or not a map")
	}
	replicatedJobs, ok := spec["replicatedJobs"].([]interface{})
	if !ok || len(replicatedJobs) == 0 {
		t.Fatalf("replicatedJobs not found or empty")
	}
	mainJob, ok := replicatedJobs[0].(map[string]interface{})
	if !ok {
		t.Fatalf("mainJob not found or not a map")
	}
	jobTemplate, ok := mainJob["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("replicatedJobs[0].template not found or not a map")
	}
	jobTemplateSpec, ok := jobTemplate["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("replicatedJobs[0].template.spec not found or not a map")
	}
	podTemplate, ok := jobTemplateSpec["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("replicatedJobs[0].template.spec.template not found or not a map")
	}
	podSpec, ok := podTemplate["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("replicatedJobs[0].template.spec.template.spec not found or not a map")
	}

	containers, ok := podSpec["containers"].([]interface{})
	if !ok || len(containers) == 0 {
		t.Fatalf("containers not found or empty")
	}
	container, ok := containers[0].(map[string]interface{})
	if !ok {
		t.Fatalf("container not found or not a map")
	}

	return jobSetContainerComponents{
		spec:            spec,
		replicatedJobs:  replicatedJobs,
		mainJob:         mainJob,
		jobTemplate:     jobTemplate,
		jobTemplateSpec: jobTemplateSpec,
		podTemplate:     podTemplate,
		podSpec:         podSpec,
		containers:      containers,
		container:       container,
	}
}

// assertJobSetContainer checks the container image and command fields of the JobSet manifest.
func assertJobSetContainer(t *testing.T, result map[string]interface{}, expectedFullImageName, expectedCommandToRun string) {
	t.Helper()

	components := getJobSetContainerComponents(t, result)

	if restartPolicy := components.podSpec["restartPolicy"]; restartPolicy != "Never" {
		t.Errorf("Expected restartPolicy %q, got %q", "Never", restartPolicy)
	}

	if image := components.container["image"]; image != expectedFullImageName {
		t.Errorf("Expected container image %q, got %q", expectedFullImageName, image)
	}
	commandArgs, ok := components.container["command"].([]interface{})
	if !ok || len(commandArgs) < 3 {
		t.Fatalf("container command not found or invalid format")
	}
	if cmdStr := commandArgs[2]; cmdStr != expectedCommandToRun {
		t.Errorf("Expected container command %q, got %q", expectedCommandToRun, cmdStr)
	}
}

// assertJobSetResources checks the container resource limits of the JobSet manifest.
func assertJobSetResources(t *testing.T, result map[string]interface{}, expectedGpuLimit, expectedCPULimit, expectedMemoryLimit string) {
	t.Helper()

	components := getJobSetContainerComponents(t, result)

	resources, ok := components.container["resources"].(map[string]interface{})
	if !ok {
		t.Fatalf("container resources not found or not a map")
	}
	limits, ok := resources["limits"].(map[string]interface{})
	if !ok {
		t.Fatalf("container resources.limits not found or not a map")
	}
	// Convert float64 to string for comparison, as limits are expected as strings
	if gpuLimit := fmt.Sprintf("%v", limits["nvidia.com/gpu"]); gpuLimit != expectedGpuLimit {
		t.Errorf("Expected GPU limit %q, got %q", expectedGpuLimit, gpuLimit)
	}
	if cpuLimit := fmt.Sprintf("%v", limits["cpu"]); cpuLimit != expectedCPULimit {
		t.Errorf("Expected CPU limit %q, got %q", expectedCPULimit, cpuLimit)
	}
	if memoryLimit := limits["memory"]; memoryLimit != expectedMemoryLimit {
		t.Errorf("Expected Memory limit %q, got %q", expectedMemoryLimit, memoryLimit)
	}
}

// assertJobSetNodeSelector checks the node selector fields of the JobSet manifest.
func assertJobSetNodeSelector(t *testing.T, result map[string]interface{}, expectedAcceleratorTypeLabel string) {
	t.Helper()

	components := getJobSetContainerComponents(t, result)

	// NodeSelector assertions
	if expectedAcceleratorTypeLabel != "" {
		nodeSelector, ok := components.podSpec["nodeSelector"].(map[string]interface{})
		if !ok {
			t.Fatalf("nodeSelector not found or not a map")
		}
		if acceleratorLabel := nodeSelector["cloud.google.com/gke-accelerator"]; acceleratorLabel != expectedAcceleratorTypeLabel {
			t.Errorf("Expected nodeSelector['cloud.google.com/gke-accelerator'] %q, got %q", expectedAcceleratorTypeLabel, acceleratorLabel)
		}
	} else {
		if _, ok := components.podSpec["nodeSelector"]; ok {
			t.Errorf("nodeSelector found for CPU-only workload, but not expected")
		}
	}
}

func TestGenerateGKEManifest(t *testing.T) {
	tests := []struct {
		name string
		opts ManifestOptions
		// Expected values for key fields in the generated JobSet manifest
		expectedAPIVersion              string
		expectedKind                    string
		expectedWorkloadName            string
		expectedKueueQueueName          string
		expectedTtlSecondsAfterFinished int
		expectedMaxRestarts             int
		expectedNumSlices               int
		expectedVmsPerSlice             int
		expectedFullImageName           string
		expectedCommandToRun            string
		expectedAcceleratorTypeLabel    string
		expectedGpuLimit                string
		expectedCPULimit                string
		expectedMemoryLimit             string
	}{
		{
			name: "Basic JobSet with TPU",
			opts: ManifestOptions{
				WorkloadName:            "test-tpu-workload",
				FullImageName:           "gcr.io/my-project/my-tpu-image:latest",
				CommandToRun:            "python tpu_train.py",
				AcceleratorType:         "tpu-v4-podslice",
				KueueQueueName:          "tpu-queue",
				NumSlices:               2,
				VmsPerSlice:             4,
				MaxRestarts:             3,
				TtlSecondsAfterFinished: 3600,
			},
			expectedAPIVersion:              "jobset.x-k8s.io/v1alpha2",
			expectedKind:                    "JobSet",
			expectedWorkloadName:            "test-tpu-workload",
			expectedKueueQueueName:          "tpu-queue",
			expectedTtlSecondsAfterFinished: 3600,
			expectedMaxRestarts:             3,
			expectedNumSlices:               2,
			expectedVmsPerSlice:             4,
			expectedFullImageName:           "gcr.io/my-project/my-tpu-image:latest",
			expectedCommandToRun:            "python tpu_train.py",
			expectedAcceleratorTypeLabel:    "tpu-v4-podslice",
			expectedGpuLimit:                "0",
			expectedCPULimit:                "16",
			expectedMemoryLimit:             "128Gi",
		},
		{
			name: "Basic JobSet with GPU",
			opts: ManifestOptions{
				WorkloadName:            "test-gpu-workload",
				FullImageName:           "gcr.io/my-project/my-gpu-image:v1",
				CommandToRun:            "bash run.sh",
				AcceleratorType:         "nvidia-tesla-a100",
				KueueQueueName:          "gpu-queue",
				NumSlices:               1,
				VmsPerSlice:             1,
				MaxRestarts:             0, // This will default to 1 in GenerateGKEManifest
				TtlSecondsAfterFinished: 600,
			},
			expectedAPIVersion:              "jobset.x-k8s.io/v1alpha2",
			expectedKind:                    "JobSet",
			expectedWorkloadName:            "test-gpu-workload",
			expectedKueueQueueName:          "gpu-queue",
			expectedTtlSecondsAfterFinished: 600,
			expectedMaxRestarts:             1, // Changed to 1 to match default logic
			expectedNumSlices:               1,
			expectedVmsPerSlice:             1,
			expectedFullImageName:           "gcr.io/my-project/my-gpu-image:v1",
			expectedCommandToRun:            "bash run.sh",
			expectedAcceleratorTypeLabel:    "nvidia-tesla-a100",
			expectedGpuLimit:                "1",
			expectedCPULimit:                "8",
			expectedMemoryLimit:             "64Gi",
		},
		{
			name: "Basic JobSet with CPU (defaults)",
			opts: ManifestOptions{
				WorkloadName:            "test-cpu-workload",
				FullImageName:           "ubuntu:latest",
				CommandToRun:            "echo 'hello world'",
				AcceleratorType:         "", // CPU default
				KueueQueueName:          "", // Default to "default-queue"
				NumSlices:               0,  // Default to 1
				VmsPerSlice:             0,  // Default to 1
				MaxRestarts:             0,  // Default to 1
				TtlSecondsAfterFinished: 0,  // Default to 3600
			},
			expectedAPIVersion:              "jobset.x-k8s.io/v1alpha2",
			expectedKind:                    "JobSet",
			expectedWorkloadName:            "test-cpu-workload",
			expectedKueueQueueName:          "default-queue",
			expectedTtlSecondsAfterFinished: 3600,
			expectedMaxRestarts:             1,
			expectedNumSlices:               1,
			expectedVmsPerSlice:             1,
			expectedFullImageName:           "ubuntu:latest",
			expectedCommandToRun:            "echo 'hello world'",
			expectedAcceleratorTypeLabel:    "", // No specific label for default CPU
			expectedGpuLimit:                "0",
			expectedCPULimit:                "0.5",
			expectedMemoryLimit:             "512Mi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest, err := GenerateGKEManifest(tt.opts)
			if err != nil {
				t.Fatalf("GenerateGKEManifest failed: %v", err)
			}

			// Parse the generated YAML to a map for easier assertion
			var result map[string]interface{}
			err = yaml.Unmarshal([]byte(manifest), &result)
			if err != nil {
				t.Fatalf("Failed to unmarshal generated YAML: %v", err)
			}

			assertJobSetMetadata(t, result, tt.expectedAPIVersion, tt.expectedKind, tt.expectedWorkloadName, tt.expectedKueueQueueName)
			assertJobSetSpec(t, result, tt.expectedTtlSecondsAfterFinished, tt.expectedMaxRestarts, tt.expectedNumSlices, tt.expectedVmsPerSlice)
			assertJobSetContainer(t, result, tt.expectedFullImageName, tt.expectedCommandToRun)
			assertJobSetResources(t, result, tt.expectedGpuLimit, tt.expectedCPULimit, tt.expectedMemoryLimit)
			assertJobSetNodeSelector(t, result, tt.expectedAcceleratorTypeLabel)
		})
	}
}
