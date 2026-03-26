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

package gke

import (
	"hpc-toolkit/pkg/orchestrator"
	"hpc-toolkit/pkg/orchestrator/gke/mock"
	"hpc-toolkit/pkg/shell"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type MockExecutor struct {
	responses map[string][]shell.CommandResult
	callCount map[string]int
}

func NewMockExecutor(responses map[string][]shell.CommandResult) *MockExecutor {
	return &MockExecutor{
		responses: responses,
		callCount: make(map[string]int),
	}
}

func (m *MockExecutor) ExecuteCommand(name string, args ...string) shell.CommandResult {
	cmdKey := name + " " + strings.Join(args, " ")

	for key, results := range m.responses {
		if strings.HasPrefix(cmdKey, key) {
			count := m.callCount[key]
			if count < len(results) {
				m.callCount[key]++
				return results[count]
			}
		}
	}

	return shell.CommandResult{ExitCode: 1, Stderr: "mock error: unexpected command"}
}

func TestGenerateGKEManifest_Accelerators(t *testing.T) {

	tests := []struct {
		name            string
		acceleratorType string
		cpuLimit        string
		memoryLimit     string
		gpuLimit        string
		tpuLimit        string
		wantLabels      []string // Labels that should be in the output
		wantLimits      []string // Limits that should be in the output,
		dontWantLimits  []string // Limits that should NOT be in the output
	}{
		{
			name:            "A3 Mega (H100)",
			acceleratorType: "nvidia-h100-mega-80gb",
			cpuLimit:        "208",
			memoryLimit:     "1000Gi",
			gpuLimit:        "8",
			wantLabels:      []string{"cloud.google.com/gke-accelerator: nvidia-h100-mega-80gb"},
			wantLimits:      []string{"nvidia.com/gpu: 8", "cpu: 208", "memory: 1000Gi"},
			dontWantLimits:  []string{"google.com/tpu"},
		},
		{
			name:            "A4X Max (GB200)",
			acceleratorType: "nvidia-gb200",
			cpuLimit:        "208",
			memoryLimit:     "1000Gi",
			gpuLimit:        "4",
			wantLabels:      []string{"cloud.google.com/gke-accelerator: nvidia-gb200"},
			wantLimits:      []string{"nvidia.com/gpu: 4", "cpu: 208", "memory: 1000Gi"},
			dontWantLimits:  []string{"google.com/tpu"},
		},
		{
			name:            "G2 (L4)",
			acceleratorType: "nvidia-l4",
			cpuLimit:        "4",
			memoryLimit:     "24Gi",
			gpuLimit:        "1",
			wantLabels:      []string{"cloud.google.com/gke-accelerator: nvidia-l4"},
			wantLimits:      []string{"nvidia.com/gpu: 1", "cpu: 4", "memory: 24Gi"},
			dontWantLimits:  []string{"google.com/tpu"},
		},
		{
			name:            "TPU v6e slice",
			acceleratorType: "tpu-v6e-slice",
			cpuLimit:        "16",
			memoryLimit:     "128Gi",
			tpuLimit:        "4",
			wantLabels:      []string{"cloud.google.com/gke-tpu-accelerator: tpu-v6e-slice"},
			wantLimits:      []string{"google.com/tpu: 4", "cpu: 16", "memory: 100Gi"},
			dontWantLimits:  []string{"nvidia.com/gpu"},
		},
		{
			name:            "CPU Only (Default)",
			acceleratorType: "",
			cpuLimit:        "0.5",
			memoryLimit:     "512Mi",
			wantLabels:      []string{}, // No accelerator label
			wantLimits:      []string{"cpu: 0.5", "memory: 512Mi"},
			dontWantLimits:  []string{"nvidia.com/gpu", "google.com/tpu", "cloud.google.com/gke-accelerator"},
		},
		{
			name:            "Fallback NVIDIA",
			acceleratorType: "nvidia-unknown-new-gpu",
			cpuLimit:        "1",
			memoryLimit:     "4Gi",
			gpuLimit:        "1",
			wantLabels:      []string{"cloud.google.com/gke-accelerator: nvidia-unknown-new-gpu"},
			wantLimits:      []string{"nvidia.com/gpu: 1", "cpu: 1", "memory: 4Gi"},
			dontWantLimits:  []string{"google.com/tpu"},
		},
		{
			name:            "Fallback TPU",
			acceleratorType: "unknown-tpu-version",
			cpuLimit:        "1",
			memoryLimit:     "4Gi",
			tpuLimit:        "4",
			wantLabels:      []string{"cloud.google.com/gke-accelerator: unknown-tpu-version"},
			wantLimits:      []string{"google.com/tpu: 4", "cpu: 1", "memory: 4Gi"},
			dontWantLimits:  []string{"nvidia.com/gpu"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := orchestrator.JobDefinition{
				WorkloadName:    "test-workload",
				CommandToRun:    "echo hello",
				AcceleratorType: tt.acceleratorType,
			}

			mockResponses := map[string][]shell.CommandResult{
				"kubectl get resourceflavors": {{ExitCode: 0, Stdout: ""}},
				"kubectl get nodes":           {{ExitCode: 0, Stdout: ""}},
			}
			orc := &GKEOrchestrator{executor: NewMockExecutor(mockResponses)}

			opts, err := orc.prepareManifestOptions(job, "test-image:latest")
			if err != nil {
				t.Fatalf("prepareManifestOptions failed: %v", err)
			}
			// prepareManifestOptions doesn't set limits in opts (GenerateGKEManifest does),
			// but it sets NodeSelector string which is key for labels.

			manifest, err := orc.GenerateGKEManifest(opts)
			if err != nil {
				t.Fatalf("GenerateGKEManifest failed: %v", err)
			}

			for _, want := range tt.wantLabels {
				if !strings.Contains(manifest, want) {
					t.Errorf("manifest missing expected label %q\nManifest: %s", want, manifest)
				}
			}

			for _, want := range tt.wantLimits {
				if !strings.Contains(manifest, want) {
					t.Errorf("manifest missing expected limit %q\nManifest: %s", want, manifest)
				}
			}

			for _, dontWant := range tt.dontWantLimits {
				if strings.Contains(manifest, dontWant) {
					t.Errorf("manifest contains unexpected limit %q", dontWant)
				}
			}
		})
	}
}

func TestGenerateGKEManifest_CommandEscaping(t *testing.T) {
	orc, _ := NewGKEOrchestrator()
	opts := ManifestOptions{
		WorkloadName:    "test-workload",
		FullImageName:   "test-image:latest",
		CommandToRun:    `python -c "print('hello')"` + " && echo \"world\"",
		AcceleratorType: "nvidia-l4",
	}

	manifest, err := orc.GenerateGKEManifest(opts)
	if err != nil {
		t.Fatalf("GenerateGKEManifest failed: %v", err)
	}

	// We expect the command to be properly escaped in the JSON/YAML array syntax used in the manifest
	expectedSubStr := `command: ["/bin/bash", "-c", "python -c \"print('hello')\" && echo \"world\""]`
	if !strings.Contains(manifest, expectedSubStr) {
		t.Errorf("manifest command string is not properly escaped.\nExpected substring: %s\nActual manifest section:\n%s", expectedSubStr, manifest)
	}
}

func TestInjectTolerations(t *testing.T) {
	orc, _ := NewGKEOrchestrator()

	// Sample Deployment YAML
	inputYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: jobset-controller-manager
  namespace: jobset-system
spec:
  template:
    metadata:
      labels:
        foo: bar
    spec:
      containers:
      - name: manager
        image: jobset:v0.1.0
`
	// Convert to byte array
	manifestBytes := []byte(inputYAML)

	// Clean and inject
	cleanedBytes, err := orc.cleanJobSetManifests(manifestBytes)
	if err != nil {
		t.Fatalf("cleanJobSetManifests failed: %v", err)
	}

	cleanedString := string(cleanedBytes)

	// Check for tolerations
	if !strings.Contains(cleanedString, "key: nvidia.com/gpu") {
		t.Errorf("Resulting manifest should contain nvidia.com/gpu toleration.\nGot:\n%s", cleanedString)
	}
	if !strings.Contains(cleanedString, "key: components.gke.io/gke-managed-components") {
		t.Errorf("Resulting manifest should contain components.gke.io/gke-managed-components toleration.\nGot:\n%s", cleanedString)
	}
	if !strings.Contains(cleanedString, "control-plane: controller-manager") {
		t.Errorf("Resulting manifest should contain control-plane: controller-manager label.\nGot:\n%s", cleanedString)
	}
}

func TestWaitForJobCompletion(t *testing.T) {
	workloadName := "test-workload"
	clusterName := "test-cluster"
	clusterLocation := "us-central1-a"
	projectID := "test-project"

	tests := []struct {
		name          string
		mockResponses map[string][]shell.CommandResult
		expectedError string
	}{
		{
			name: "Successful completion",
			mockResponses: map[string][]shell.CommandResult{
				"kubectl wait --for jsonpath={.status.conditions[-1].type}=Finished jobset test-workload --timeout=1h": {
					{ExitCode: 0, Stdout: "jobset.jobset.x-k8s.io/test-workload condition met"},
				},
				"kubectl get jobset test-workload -o jsonpath={.status.conditions[-1].type}": {
					{ExitCode: 0, Stdout: "Completed"},
				},
			},
			expectedError: "",
		},
		{
			name: "Job timeout",
			mockResponses: map[string][]shell.CommandResult{
				"kubectl wait --for jsonpath={.status.conditions[-1].type}=Finished jobset test-workload --timeout=1h": {
					{ExitCode: 1, Stderr: "timed out waiting for conditions to be met"},
				},
			},
			expectedError: "job timed out",
		},
		{
			name: "Job finished but not completed",
			mockResponses: map[string][]shell.CommandResult{
				"kubectl wait --for jsonpath={.status.conditions[-1].type}=Finished jobset test-workload --timeout=1h": {
					{ExitCode: 0, Stdout: "jobset.jobset.x-k8s.io/test-workload condition met"},
				},
				"kubectl get jobset test-workload -o jsonpath={.status.conditions[-1].type}": {
					{ExitCode: 0, Stdout: "Failed"},
				},
			},
			expectedError: "job completed unsuccessfully with status: Failed",
		},
		{
			name: "Error during kubectl wait",
			mockResponses: map[string][]shell.CommandResult{
				"kubectl wait --for jsonpath={.status.conditions[-1].type}=Finished jobset test-workload --timeout=1h": {
					{ExitCode: 1, Stderr: "some kubectl error"},
				},
			},
			expectedError: "error waiting for job completion: some kubectl error\n",
		},
		{
			name: "Error during kubectl get status",
			mockResponses: map[string][]shell.CommandResult{
				"kubectl wait --for jsonpath={.status.conditions[-1].type}=Finished jobset test-workload --timeout=1h": {
					{ExitCode: 0, Stdout: "jobset.jobset.x-k8s.io/test-workload condition met"},
				},
				"kubectl get jobset test-workload -o jsonpath={.status.conditions[-1].type}": {
					{ExitCode: 1, Stderr: "some get status error"},
				},
			},
			expectedError: "failed to get final job status: some get status error\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockExecutor := NewMockExecutor(tt.mockResponses)
			orc := &GKEOrchestrator{executor: mockExecutor}

			err := orc.waitForJobCompletion(workloadName, clusterName, clusterLocation, projectID)

			if tt.expectedError == "" {
				if err != nil {
					t.Errorf("Expected no error, but got: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("Expected error containing %q, but got: %v", tt.expectedError, err)
				}
			}
		})
	}
}

func TestListVolumes(t *testing.T) {
	tests := []struct {
		name          string
		pvc           *unstructured.Unstructured
		expectedType  string
		expectedMount string
	}{
		{
			name: "Volume with ghpc_module label",
			pvc: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "PersistentVolumeClaim",
					"metadata": map[string]interface{}{
						"name":      "test-pvc-1",
						"namespace": "default",
						"labels": map[string]interface{}{
							"ghpc_role":   "file-system",
							"ghpc_module": "gke-storage",
						},
					},
					"spec": map[string]interface{}{
						"storageClassName": "standard-rwo",
					},
				},
			},
			expectedType:  "gke-storage",
			expectedMount: "/mnt/data/test-pvc-1",
		},
		{
			name: "Volume with storageClassName fallback",
			pvc: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "PersistentVolumeClaim",
					"metadata": map[string]interface{}{
						"name":      "test-pvc-2",
						"namespace": "default",
						"labels": map[string]interface{}{
							"ghpc_role": "file-system",
						},
					},
					"spec": map[string]interface{}{
						"storageClassName": "premium-rwo",
					},
				},
			},
			expectedType:  "premium-rwo",
			expectedMount: "/mnt/data/test-pvc-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pvcList := &unstructured.UnstructuredList{
				Items: []unstructured.Unstructured{*tt.pvc},
			}
			fc := &mock.MockDynamicClient{PvcList: pvcList}

			mockResponses := map[string][]shell.CommandResult{
				"gcloud container clusters get-credentials": {{ExitCode: 0}},
				"gcloud config get-value project":           {{ExitCode: 0, Stdout: "test-project"}},
			}
			orc := &GKEOrchestrator{
				executor:      NewMockExecutor(mockResponses),
				dynamicClient: fc,
			}

			opts := orchestrator.ListOptions{
				ClusterName:     "test-cluster",
				ClusterLocation: "us-central1",
				ProjectID:       "test-project",
			}

			volumes, err := orc.ListVolumes(opts)
			if err != nil {
				t.Fatalf("ListVolumes failed: %v", err)
			}

			if len(volumes) != 1 {
				t.Fatalf("expected 1 volume, got %d", len(volumes))
			}

			vol := volumes[0]
			if vol.Name != tt.pvc.GetName() {
				t.Errorf("expected name %s, got %s", tt.pvc.GetName(), vol.Name)
			}
			if vol.Type != tt.expectedType {
				t.Errorf("expected type %s, got %s", tt.expectedType, vol.Type)
			}
		})
	}
}
