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
	"encoding/json"
	"fmt"
	"hpc-toolkit/pkg/imagebuilder"
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

// JobSetTemplate remains the same...
const JobSetTemplate = `
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: {{.WorkloadName}}
  labels:
    gcluster.google.com/workload: {{.WorkloadName}}
    kueue.x-k8s.io/queue-name: {{.KueueQueueName}}
spec:
  ttlSecondsAfterFinished: {{.TtlSecondsAfterFinished}}
  failurePolicy:
    maxRestarts: {{.MaxRestarts}}
  replicatedJobs:
    - name: main-job
      replicas: {{.NumSlices}}
      template:
        spec:
          parallelism: {{.VmsPerSlice}}
          completions: {{.VmsPerSlice}}
          backoffLimit: 0
          template:
            metadata:
              labels:
                gcluster.google.com/workload: {{.WorkloadName}}
            spec:
              restartPolicy: Never
              containers:
              - name: workload-container
                image: {{.FullImageName}}
{{.CommandToRun}}
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

type ManifestOptions struct {
	WorkloadName            string
	FullImageName           string
	CommandToRun            string
	AcceleratorType         string
	ResourcesString         string
	ProjectID               string
	ClusterName             string
	ClusterLocation         string
	KueueQueueName          string
	NumSlices               int
	VmsPerSlice             int
	MaxRestarts             int
	TtlSecondsAfterFinished int
}

type GKEOrchestrator struct{}

func NewGKEOrchestrator() (*GKEOrchestrator, error) {
	return &GKEOrchestrator{}, nil
}

func (g *GKEOrchestrator) SubmitJob(job orchestrator.JobDefinition) error {
	logging.Info("Starting gcluster run workflow...")

	projectID, err := g.getProjectID(job.ProjectID)
	if err != nil {
		return err
	}
	job.ProjectID = projectID

	logging.Info("Configuring kubectl for GKE cluster '%s'...", job.ClusterName)
	err = g.configureKubectl(job.ClusterName, job.ClusterLocation, job.ProjectID)
	if err != nil {
		return err
	}

	localQueue, err := g.resolveKueueQueue(job.KueueQueueName)
	if err != nil {
		logging.Info("Warning: Failed to auto-discover Kueue Queue Name: %v. Falling back to default-queue.", err)
		localQueue = "default-queue"
	}
	job.KueueQueueName = localQueue

	// --- NEW: AUTO-UPDATE CLUSTER QUEUE RESOURCES ---
	logging.Info("Ensuring Kueue ClusterQueue covers all requested resources...")
	if err := g.ensureClusterQueueCoverage(job.KueueQueueName); err != nil {
		logging.Info("Warning: Could not automatically update ClusterQueue: %v. Workload might remain suspended.", err)
	}

	accelType, err := g.resolveAcceleratorType(job.AcceleratorType)
	if err != nil {
		logging.Info("Warning: Failed to auto-discover Accelerator Type: %v. Assuming CPU-only.", err)
		accelType = ""
	}
	job.AcceleratorType = accelType

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

// ensureClusterQueueCoverage checks the ClusterQueue and patches it to include cpu/memory if missing
func (g *GKEOrchestrator) ensureClusterQueueCoverage(localQueueName string) error {
	// 1. Get the ClusterQueue name associated with the LocalQueue
	res := shell.ExecuteCommand("kubectl", "get", "localqueue", localQueueName, "-n", "default", "-o", "jsonpath={.spec.clusterQueue}")
	if res.ExitCode != 0 {
		return fmt.Errorf("failed to find clusterqueue for %s: %s", localQueueName, res.Stderr)
	}
	cqName := strings.TrimSpace(res.Stdout)
	if cqName == "" {
		cqName = localQueueName // Fallback to same name if not specified
	}

	// 2. Check if CPU/Memory are already covered
	res = shell.ExecuteCommand("kubectl", "get", "clusterqueue", cqName, "-o", "json")
	if res.ExitCode != 0 {
		return fmt.Errorf("failed to get clusterqueue %s: %s", cqName, res.Stderr)
	}

	var cq map[string]interface{}
	if err := json.Unmarshal([]byte(res.Stdout), &cq); err != nil {
		return err
	}

	// Navigate the JSON to find coveredResources
	spec := cq["spec"].(map[string]interface{})
	rgList := spec["resourceGroups"].([]interface{})
	if len(rgList) == 0 {
		return nil
	}

	rg := rgList[0].(map[string]interface{})
	covered := rg["coveredResources"].([]interface{})

	hasCPU := false
	hasMem := false
	for _, r := range covered {
		if r.(string) == "cpu" {
			hasCPU = true
		}
		if r.(string) == "memory" {
			hasMem = true
		}
	}

	if hasCPU && hasMem {
		logging.Info("Kueue ClusterQueue '%s' already covers CPU and Memory.", cqName)
		return nil
	}

	// 3. Patch the ClusterQueue to add CPU and Memory with high quotas
	logging.Info("Patching ClusterQueue '%s' to include CPU and Memory quotas...", cqName)

	patch := fmt.Sprintf(`[
		{"op": "add", "path": "/spec/resourceGroups/0/coveredResources/-", "value": "cpu"},
		{"op": "add", "path": "/spec/resourceGroups/0/coveredResources/-", "value": "memory"},
		{"op": "add", "path": "/spec/resourceGroups/0/flavors/0/resources/-", "value": {"name": "cpu", "nominalQuota": "500"}},
		{"op": "add", "path": "/spec/resourceGroups/0/flavors/0/resources/-", "value": {"name": "memory", "nominalQuota": "2000Gi"}}
	]`)

	res = shell.ExecuteCommand("kubectl", "patch", "clusterqueue", cqName, "--type", "json", "-p", patch)
	if res.ExitCode != 0 {
		// If JSON patch fails because resources array doesn't exist, we skip or use a merge patch
		return fmt.Errorf("failed to patch clusterqueue: %s", res.Stderr)
	}

	logging.Info("ClusterQueue successfully updated.")
	return nil
}

// All other functions (getProjectID, resolveKueueQueue, resolveAcceleratorType, buildDockerImage, etc.) remain identical to your previous version...

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

func (g *GKEOrchestrator) resolveKueueQueue(requested string) (string, error) {
	if requested != "" {
		logging.Info("Using provided Kueue LocalQueue: %s", requested)
		return requested, nil
	}

	logging.Info("Auto-discovering Kueue LocalQueue...")
	res := shell.ExecuteCommand("kubectl", "get", "localqueue", "-n", "default", "-o", "jsonpath={.items[*].metadata.name}")
	if res.ExitCode != 0 {
		return "", fmt.Errorf("failed to query LocalQueues: %s", res.Stderr)
	}

	output := strings.TrimSpace(res.Stdout)
	if output == "" {
		logging.Info("No LocalQueues found. Defaulting to 'default-queue'.")
		return "default-queue", nil
	}

	queues := strings.Fields(output)
	if len(queues) == 1 {
		logging.Info("Auto-discovered Kueue LocalQueue: %s", queues[0])
		return queues[0], nil
	}

	logging.Info("Warning: Multiple LocalQueues found (%v). Defaulting to the first one: %s", queues, queues[0])
	return queues[0], nil
}

func (g *GKEOrchestrator) resolveAcceleratorType(requested string) (string, error) {
	if requested != "" {
		logging.Info("Using provided Accelerator Type: %s", requested)
		return requested, nil
	}

	logging.Info("Auto-discovering Accelerator Type...")

	res := shell.ExecuteCommand("kubectl", "get", "resourceflavors.kueue.x-k8s.io", "-o", "jsonpath={range .items[*]}{.spec.nodeLabels.cloud\\.google\\.com/gke-accelerator}{\"\\n\"}{end}")
	output := strings.TrimSpace(res.Stdout)

	if res.ExitCode != 0 || output == "" {
		res = shell.ExecuteCommand("kubectl", "get", "nodes", "-o", "jsonpath={range .items[*]}{.metadata.labels.cloud\\.google\\.com/gke-accelerator}{\"\\n\"}{end}")
		if res.ExitCode != 0 {
			return "", fmt.Errorf("failed to query Nodes for accelerators: %s", res.Stderr)
		}
		output = strings.TrimSpace(res.Stdout)
	}

	if output == "" {
		logging.Info("No accelerators found. Defaulting to CPU-only workload.")
		return "", nil
	}

	accelerators := make(map[string]bool)
	for _, acc := range strings.Split(output, "\n") {
		acc = strings.TrimSpace(acc)
		if acc != "" {
			accelerators[acc] = true
		}
	}

	if len(accelerators) == 0 {
		logging.Info("No hardware accelerators found. Defaulting to CPU-only workload.")
		return "", nil
	}

	uniqueAccels := make([]string, 0, len(accelerators))
	for acc := range accelerators {
		uniqueAccels = append(uniqueAccels, acc)
	}

	if len(uniqueAccels) == 1 {
		logging.Info("Auto-discovered Accelerator Type: %s", uniqueAccels[0])
		return uniqueAccels[0], nil
	}

	logging.Info("Warning: Multiple Accelerator Types found (%v). Defaulting to the first one: %s", uniqueAccels, uniqueAccels[0])
	return uniqueAccels[0], nil
}

func (g *GKEOrchestrator) buildDockerImage(project, baseDockerImage, buildContext, platformStr, dockerImage string) (string, error) {
	if baseDockerImage != "" {
		logging.Info("Building Docker image using Crane (Go implementation) on top of %s...", baseDockerImage)

		ignorePatterns := []string{
			".git", ".terraform", ".ghpc", ".ansible", "vendor", "bin", "pkg", "node_modules", "*.log", "tmp/", ".DS_Store", "__pycache__",
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
		logging.Info("Cleaning up any existing JobSet with name '%s'...", opts.WorkloadName)
		shell.ExecuteCommand("kubectl", "delete", "jobset", opts.WorkloadName, "--ignore-not-found=true")

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
		logging.Info("JobSet CRD found. Verifying Webhook health...")
		cmdEndpoints := shell.ExecuteCommand("kubectl", "get", "endpoints", "jobset-webhook-service", "-n", "jobset-system", "-o", "jsonpath={.subsets[*].addresses[*].ip}")
		if cmdEndpoints.ExitCode == 0 && strings.TrimSpace(cmdEndpoints.Stdout) != "" {
			logging.Info("JobSet Webhook is healthy.")
			return nil
		}
		logging.Info("JobSet Webhook endpoints not found. Proceeding with re-installation/fix...")
	}

	jobSetManifestsURL := "https://github.com/kubernetes-sigs/jobset/releases/download/v0.10.1/manifests.yaml"
	return g.installJobSetCRD(jobSetManifestsURL)
}

func (g *GKEOrchestrator) installJobSetCRD(jobSetManifestsURL string) error {
	logging.Info("Installing/Fixing JobSet CRD and Webhook...")

	manifestBytes, err := g.downloadJobSetManifests(jobSetManifestsURL)
	if err != nil {
		return err
	}

	cleanedManifests, err := g.cleanJobSetManifests(manifestBytes)
	if err != nil {
		return err
	}

	logging.Info("Force-recreating JobSet Controller Manager...")
	shell.ExecuteCommand("kubectl", "delete", "deployment", "jobset-controller-manager", "-n", "jobset-system", "--ignore-not-found=true")

	if err := g.applyJobSetManifests(cleanedManifests); err != nil {
		return err
	}

	logging.Info("JobSet components applied successfully.")

	return g.waitForJobSetWebhook()
}

func (g *GKEOrchestrator) waitForJobSetWebhook() error {
	logging.Info("Waiting for JobSet webhook service to be ready...")
	cmd := shell.NewCommand("kubectl", "rollout", "status", "deployment/jobset-controller-manager", "-n", "jobset-system", "--timeout=300s")
	res := cmd.Execute()
	if res.ExitCode != 0 {
		return fmt.Errorf("jobset controller manager failed to become ready: %s\n%s", res.Stderr, res.Stdout)
	}

	logging.Info("Verifying JobSet webhook service endpoints...")
	for i := 0; i < 100; i++ {
		cmdEndpoints := shell.ExecuteCommand("kubectl", "get", "endpoints", "jobset-webhook-service", "-n", "jobset-system", "-o", "jsonpath={.subsets[*].addresses[*].ip}")
		if cmdEndpoints.ExitCode == 0 && strings.TrimSpace(cmdEndpoints.Stdout) != "" {
			logging.Info("JobSet webhook service endpoints are available.")
			return nil
		}
		shell.ExecuteCommand("sleep", "3")
	}

	return fmt.Errorf("timed out waiting for jobset-webhook-service endpoints to be available")
}

func (g *GKEOrchestrator) isJobSetCRDInstalled() (bool, error) {
	logging.Info("Checking for JobSet CRD installation...")
	res := shell.ExecuteCommand("kubectl", "get", "crd", "jobsets.jobset.x-k8s.io")
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
			g.injectTolerationsAndLabels(data)
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

func (g *GKEOrchestrator) injectTolerationsAndLabels(data map[interface{}]interface{}) {
	kind, ok := data["kind"].(string)
	if !ok || kind != "Deployment" {
		return
	}

	meta, ok := data["metadata"].(map[interface{}]interface{})
	if !ok {
		return
	}
	name, ok := meta["name"].(string)
	if !ok || (name != "jobset-controller-manager" && name != "jobset-controller") {
		return
	}

	spec, ok := data["spec"].(map[interface{}]interface{})
	if !ok {
		return
	}
	template, ok := spec["template"].(map[interface{}]interface{})
	if !ok {
		return
	}
	podSpec, ok := template["spec"].(map[interface{}]interface{})
	if !ok {
		return
	}

	tolerations := []interface{}{
		map[interface{}]interface{}{
			"key":      "nvidia.com/gpu",
			"operator": "Exists",
			"effect":   "NoSchedule",
		},
		map[interface{}]interface{}{
			"key":      "components.gke.io/gke-managed-components",
			"operator": "Exists",
			"effect":   "NoSchedule",
		},
	}

	if existingTolerations, ok := podSpec["tolerations"].([]interface{}); ok {
		podSpec["tolerations"] = append(existingTolerations, tolerations...)
	} else {
		podSpec["tolerations"] = tolerations
	}

	if podMeta, ok := template["metadata"].(map[interface{}]interface{}); ok {
		labels, ok := podMeta["labels"].(map[interface{}]interface{})
		if !ok {
			labels = make(map[interface{}]interface{})
			podMeta["labels"] = labels
		}
		labels["app.kubernetes.io/instance"] = "jobset"
		labels["app.kubernetes.io/name"] = "jobset"
		labels["control-plane"] = "controller-manager"
		labels["app.kubernetes.io/component"] = "controller-manager"
	}
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
	var gpuLimit, tpuLimit, cpuLimit, memoryLimit string

	switch opts.AcceleratorType {
	case "nvidia-h100-mega-80gb", "nvidia-h100-80gb", "nvidia-b200":
		gpuLimit = "8"
		cpuLimit = "1"
		memoryLimit = "4Gi"
	case "nvidia-gb200":
		gpuLimit = "4"
		cpuLimit = "1"
		memoryLimit = "4Gi"
	case "nvidia-a100-80gb", "nvidia-tesla-a100":
		gpuLimit = "1"
		cpuLimit = "1"
		memoryLimit = "4Gi"
	case "nvidia-l4":
		gpuLimit = "1"
		cpuLimit = "1"
		memoryLimit = "4Gi"
	case "tpu-v4-podslice", "tpu-v5p-slice", "tpu-v5-lite-podslice", "tpu-v5-lite-device", "tpu-v6e-slice":
		tpuLimit = "4"
		cpuLimit = "1"
		memoryLimit = "4Gi"
	case "":
		cpuLimit = "0.5"
		memoryLimit = "512Mi"
	default:
		if strings.Contains(strings.ToLower(opts.AcceleratorType), "nvidia") {
			gpuLimit = "1"
			cpuLimit = "1"
			memoryLimit = "4Gi"
		} else if strings.Contains(strings.ToLower(opts.AcceleratorType), "tpu") {
			tpuLimit = "4"
			cpuLimit = "1"
			memoryLimit = "4Gi"
		} else {
			cpuLimit = "0.5"
			memoryLimit = "512Mi"
		}
	}

	resourcesString := fmt.Sprintf("                resources:\n                  limits:\n                    cpu: %s\n                    memory: %s", cpuLimit, memoryLimit)
	if gpuLimit != "" {
		resourcesString += fmt.Sprintf("\n                    nvidia.com/gpu: %s", gpuLimit)
	}
	if tpuLimit != "" {
		resourcesString += fmt.Sprintf("\n                    google.com/tpu: %s", tpuLimit)
	}

	escapedCommand := strings.ReplaceAll(opts.CommandToRun, "\"", "\\\"")
	updatedCommand := fmt.Sprintf("                command: [\"/bin/bash\", \"-c\", \"%s\"]\n%s", escapedCommand, resourcesString)

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
	}{
		WorkloadName:            workloadName,
		KueueQueueName:          kueueQueueName,
		TtlSecondsAfterFinished: ttlSecondsAfterFinished,
		MaxRestarts:             maxRestarts,
		NumSlices:               numSlices,
		VmsPerSlice:             vmsPerSlice,
		FullImageName:           opts.FullImageName,
		CommandToRun:            updatedCommand,
		AcceleratorTypeLabel:    acceleratorTypeLabel,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute jobset template: %w", err)
	}
	return buf.String(), nil
}
