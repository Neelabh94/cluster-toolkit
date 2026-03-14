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
	"context"
	"encoding/json"
	"fmt"
	"hpc-toolkit/pkg/imagebuilder"
	"hpc-toolkit/pkg/logging"
	"hpc-toolkit/pkg/orchestrator"
	"hpc-toolkit/pkg/scheduling"
	"hpc-toolkit/pkg/shell"
	"io"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"gopkg.in/yaml.v2"
	k8syaml "sigs.k8s.io/yaml"
)

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
{{- if .PodFailurePolicy }}
          podFailurePolicy:
{{.PodFailurePolicy}}
{{- end }}
          template:
            metadata:
              labels:
                gcluster.google.com/workload: {{.WorkloadName}}
{{- if .TopologyAnnotation }}
              annotations:
{{.TopologyAnnotation}}
{{- end }}
            spec:
{{- if .SchedulerName }}
              schedulerName: {{.SchedulerName}}
{{- end }}
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
{{- if .NodeSelector }}
              nodeSelector:
{{.NodeSelector}}
{{- end }}
{{- if .Affinity }}
              affinity:
{{.Affinity}}
{{- end }}
{{- if .Tolerations }}
              tolerations:
{{.Tolerations}}
{{- end }}
{{- if .ImagePullSecrets }}
              imagePullSecrets:
{{.ImagePullSecrets}}
{{- end }}
{{- if .ServiceAccountName }}
              serviceAccountName: {{.ServiceAccountName}}
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
	NodeSelector            string
	Affinity                string
	PodFailurePolicy        string
	ImagePullSecrets        string
	ServiceAccountName      string
	TopologyAnnotation      string
	SchedulerName           string
	Tolerations             string
	AwaitJobCompletion      bool
}

type Executor interface {
	ExecuteCommand(name string, args ...string) shell.CommandResult
}

type DefaultExecutor struct{}

func (d *DefaultExecutor) ExecuteCommand(name string, args ...string) shell.CommandResult {
	return shell.ExecuteCommand(name, args...)
}

type GKEOrchestrator struct {
	executor Executor
}

func NewGKEOrchestrator() (*GKEOrchestrator, error) {
	return &GKEOrchestrator{
		executor: &DefaultExecutor{},
	}, nil
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

	manifestOpts, err := g.prepareManifestOptions(job, fullImageName)
	if err != nil {
		return err
	}

	err = g.generateAndApplyManifest(manifestOpts, job.OutputManifest)
	if err != nil {
		return err
	}

	if job.AwaitJobCompletion && job.OutputManifest == "" {
		err = g.waitForJobCompletion(job.WorkloadName, job.ClusterName, job.ClusterLocation, job.ProjectID)
		if err != nil {
			return err
		}
	}

	logging.Info("gcluster run workflow completed.")
	return nil
}

func (g *GKEOrchestrator) ensureClusterQueueCoverage(localQueueName string) error {
	res := g.executor.ExecuteCommand("kubectl", "get", "localqueue", localQueueName, "-n", "default", "-o", "jsonpath={.spec.clusterQueue}")
	if res.ExitCode != 0 {
		return fmt.Errorf("failed to find clusterqueue for %s: %s", localQueueName, res.Stderr)
	}
	cqName := strings.TrimSpace(res.Stdout)
	if cqName == "" {
		cqName = localQueueName
	}

	res = g.executor.ExecuteCommand("kubectl", "get", "clusterqueue", cqName, "-o", "json")
	if res.ExitCode != 0 {
		return fmt.Errorf("failed to get clusterqueue %s: %s", cqName, res.Stderr)
	}

	var cq map[string]interface{}
	if err := json.Unmarshal([]byte(res.Stdout), &cq); err != nil {
		return err
	}

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

	logging.Info("Patching ClusterQueue '%s' to include CPU and Memory quotas...", cqName)
	patch := `[
		{"op": "add", "path": "/spec/resourceGroups/0/coveredResources/-", "value": "cpu"},
		{"op": "add", "path": "/spec/resourceGroups/0/coveredResources/-", "value": "memory"},
		{"op": "add", "path": "/spec/resourceGroups/0/flavors/0/resources/-", "value": {"name": "cpu", "nominalQuota": "500"}},
		{"op": "add", "path": "/spec/resourceGroups/0/flavors/0/resources/-", "value": {"name": "memory", "nominalQuota": "2000Gi"}}
	]`

	res = g.executor.ExecuteCommand("kubectl", "patch", "clusterqueue", cqName, "--type", "json", "-p", patch)
	if res.ExitCode != 0 {
		return fmt.Errorf("failed to patch clusterqueue: %s", res.Stderr)
	}

	logging.Info("ClusterQueue successfully updated.")
	return nil
}

func (g *GKEOrchestrator) getProjectID(initialProjectID string) (string, error) {
	if initialProjectID == "" {
		res := g.executor.ExecuteCommand("gcloud", "config", "get-value", "project")
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
	res := g.executor.ExecuteCommand("kubectl", "get", "localqueue", "-n", "default", "-o", "jsonpath={.items[*].metadata.name}")
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

	res := g.executor.ExecuteCommand("kubectl", "get", "resourceflavors.kueue.x-k8s.io", "-o", "jsonpath={range .items[*]}{.spec.nodeLabels.cloud\\.google\\.com/gke-accelerator}{\"\\n\"}{end}")
	output := strings.TrimSpace(res.Stdout)

	if res.ExitCode != 0 || output == "" {
		res = g.executor.ExecuteCommand("kubectl", "get", "nodes", "-o", "jsonpath={range .items[*]}{.metadata.labels.cloud\\.google\\.com/gke-accelerator}{\"\\n\"}{end}")
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
	credsRes := g.executor.ExecuteCommand("gcloud", "container", "clusters", "get-credentials", clusterName, "--zone", clusterLocation, "--project", projectID)
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
		g.executor.ExecuteCommand("kubectl", "delete", "jobset", opts.WorkloadName, "--ignore-not-found=true")

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
		cmdEndpoints := g.executor.ExecuteCommand("kubectl", "get", "endpoints", "jobset-webhook-service", "-n", "jobset-system", "-o", "jsonpath={.subsets[*].addresses[*].ip}")
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
	g.executor.ExecuteCommand("kubectl", "delete", "deployment", "jobset-controller-manager", "-n", "jobset-system", "--ignore-not-found=true")

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
		cmdEndpoints := g.executor.ExecuteCommand("kubectl", "get", "endpoints", "jobset-webhook-service", "-n", "jobset-system", "-o", "jsonpath={.subsets[*].addresses[*].ip}")
		if cmdEndpoints.ExitCode == 0 && strings.TrimSpace(cmdEndpoints.Stdout) != "" {
			logging.Info("JobSet webhook service endpoints are available.")
			return nil
		}
		g.executor.ExecuteCommand("sleep", "3")
	}

	return fmt.Errorf("timed out waiting for jobset-webhook-service endpoints to be available")
}

func (g *GKEOrchestrator) isJobSetCRDInstalled() (bool, error) {
	logging.Info("Checking for JobSet CRD installation...")
	res := g.executor.ExecuteCommand("kubectl", "get", "crd", "jobsets.jobset.x-k8s.io")
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
	g.setManifestDefaults(&opts)

	cpuLimit, memoryLimit, gpuLimit, tpuLimit := g.calculateResourceLimits(opts.AcceleratorType)

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

	data := g.prepareJobSetTemplateData(opts, updatedCommand)

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute jobset template: %w", err)
	}
	return buf.String(), nil
}

func (g *GKEOrchestrator) setManifestDefaults(opts *ManifestOptions) {
	if opts.WorkloadName == "" {
		opts.WorkloadName = "gcluster-workload-" + shell.RandomString(8)
	}
	if opts.KueueQueueName == "" {
		opts.KueueQueueName = "default-queue"
	}
	if opts.NumSlices == 0 {
		opts.NumSlices = 1
	}
	if opts.VmsPerSlice == 0 {
		opts.VmsPerSlice = 1
	}
	if opts.MaxRestarts == 0 {
		opts.MaxRestarts = 1
	}
	if opts.TtlSecondsAfterFinished == 0 {
		opts.TtlSecondsAfterFinished = 3600
	}
}

func (g *GKEOrchestrator) calculateResourceLimits(acceleratorType string) (cpu, mem, gpu, tpu string) {
	switch acceleratorType {
	case "nvidia-h100-mega-80gb", "nvidia-h100-80gb":
		return "208", "1000Gi", "8", ""
	case "nvidia-gb200":
		return "208", "1000Gi", "4", ""
	case "nvidia-a100-80gb", "nvidia-tesla-a100":
		return "12", "85Gi", "1", "" // Assuming 1/8th of A100 node approx? Or keep 1/4Gi if not specified in test.
	case "nvidia-l4":
		return "4", "24Gi", "1", ""
	case "tpu-v4-podslice", "tpu-v5p-slice", "tpu-v5-lite-podslice", "tpu-v5-lite-device":
		return "1", "4Gi", "", "4"
	case "tpu-v6e-slice":
		return "16", "100Gi", "", "4"
	case "":
		return "0.5", "512Mi", "", ""
	default:
		if strings.Contains(strings.ToLower(acceleratorType), "nvidia") {
			return "1", "4Gi", "1", ""
		} else if strings.Contains(strings.ToLower(acceleratorType), "tpu") {
			return "1", "4Gi", "", "4"
		} else {
			return "0.5", "512Mi", "", ""
		}
	}
}

func (g *GKEOrchestrator) prepareJobSetTemplateData(opts ManifestOptions, updatedCommand string) interface{} {
	return struct {
		WorkloadName            string
		KueueQueueName          string
		TtlSecondsAfterFinished int
		MaxRestarts             int
		NumSlices               int
		VmsPerSlice             int
		FullImageName           string
		CommandToRun            string
		AcceleratorTypeLabel    string
		NodeSelector            string
		Affinity                string
		PodFailurePolicy        string
		ImagePullSecrets        string
		ServiceAccountName      string
		TopologyAnnotation      string
		SchedulerName           string
		Tolerations             string
	}{
		WorkloadName:            opts.WorkloadName,
		KueueQueueName:          opts.KueueQueueName,
		TtlSecondsAfterFinished: opts.TtlSecondsAfterFinished,
		MaxRestarts:             opts.MaxRestarts,
		NumSlices:               opts.NumSlices,
		VmsPerSlice:             opts.VmsPerSlice,
		FullImageName:           opts.FullImageName,
		CommandToRun:            updatedCommand,
		AcceleratorTypeLabel:    g.GenerateGKENodeSelectorLabel(opts.AcceleratorType),
		NodeSelector:            opts.NodeSelector,
		Affinity:                opts.Affinity,
		PodFailurePolicy:        opts.PodFailurePolicy,
		ImagePullSecrets:        opts.ImagePullSecrets,
		ServiceAccountName:      opts.ServiceAccountName,
		TopologyAnnotation:      opts.TopologyAnnotation,
		SchedulerName:           opts.SchedulerName,
		Tolerations:             opts.Tolerations,
	}
}

func (g *GKEOrchestrator) indentYaml(s string, indent int) string {
	lines := strings.Split(s, "\n")
	padding := strings.Repeat(" ", indent)
	var result []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, padding+line)
		}
	}
	return strings.Join(result, "\n")
}

func (g *GKEOrchestrator) prepareManifestOptions(job orchestrator.JobDefinition, fullImageName string) (ManifestOptions, error) {
	schedOpts := scheduling.SchedulingOptions{
		PlacementPolicy:    job.PlacementPolicy,
		NodeAffinityLabels: job.NodeSelector,
		Topology:           job.Topology,
		Scheduler:          job.Scheduler,
	}

	nodeSelectorStr, err := g.buildNodeSelector(schedOpts, job)
	if err != nil {
		return ManifestOptions{}, err
	}

	affinityStr, err := g.buildAffinity(schedOpts)
	if err != nil {
		return ManifestOptions{}, err
	}

	podFailurePolicyStr, err := g.generatePodFailurePolicy(job.RestartOnExitCodes)
	if err != nil {
		return ManifestOptions{}, err
	}
	podFailurePolicyStr = g.indentYaml(podFailurePolicyStr, 12)

	imagePullSecretsStr := g.generateImagePullSecrets(job.ImagePullSecrets)
	if imagePullSecretsStr != "" {
		imagePullSecretsStr = g.indentYaml(imagePullSecretsStr, 16)
	}

	topologyAnnotationStr := g.buildTopologyAnnotation(job.Topology)

	tolerations := scheduling.GetTolerations(job.AcceleratorType)
	var tolerationsStr string
	if len(tolerations) > 0 {
		b, err := k8syaml.Marshal(tolerations)
		if err != nil {
			return ManifestOptions{}, fmt.Errorf("failed to marshal tolerations: %w", err)
		}
		tolerationsStr = g.indentYaml(string(b), 16)
	}

	return ManifestOptions{
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
		NodeSelector:            nodeSelectorStr,
		Affinity:                affinityStr,
		PodFailurePolicy:        podFailurePolicyStr,
		ImagePullSecrets:        imagePullSecretsStr,
		ServiceAccountName:      job.ServiceAccountName,
		TopologyAnnotation:      topologyAnnotationStr,
		SchedulerName:           job.Scheduler,
		Tolerations:             tolerationsStr,
		AwaitJobCompletion:      job.AwaitJobCompletion,
	}, nil
}

func (g *GKEOrchestrator) parseJobStatus(obj map[string]interface{}) (statusStr, completionTime string) {
	statusStr = "Unknown"
	completionTime = ""

	statusMap, ok := obj["status"].(map[string]interface{})
	if !ok {
		return
	}

	if conditions, ok := statusMap["conditions"].([]interface{}); ok {
		for _, c := range conditions {
			cond := c.(map[string]interface{})
			condType, _ := cond["type"].(string)
			condStatus, _ := cond["status"].(string)
			if condStatus == "True" {
				switch condType {
				case "Completed", "Succeeded":
					statusStr = "Succeeded"
				case "Failed":
					statusStr = "Failed"
				case "Suspended":
					statusStr = "Suspended"
				}
			}
		}
	}

	return
}

func (g *GKEOrchestrator) generatePodFailurePolicy(exitCodes []int) (string, error) {
	if len(exitCodes) == 0 {
		return "", nil
	}

	var validCodes []int
	for _, code := range exitCodes {
		if code == 0 {
			logging.Info("Warning: Exit code 0 (success) cannot be used in PodFailurePolicy. Ignoring it.")
			continue
		}
		validCodes = append(validCodes, code)
	}

	if len(validCodes) == 0 {
		return "", nil
	}

	policy := map[string]interface{}{
		"rules": []map[string]interface{}{
			{
				"action": "Ignore",
				"onExitCodes": map[string]interface{}{
					"operator": "In",
					"values":   validCodes,
				},
			},
		},
	}
	b, err := yaml.Marshal(policy)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (g *GKEOrchestrator) generateImagePullSecrets(secrets string) string {
	if secrets == "" {
		return ""
	}
	parts := strings.Split(secrets, ",")
	var secretList []map[string]string
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s != "" {
			secretList = append(secretList, map[string]string{"name": s})
		}
	}
	if len(secretList) == 0 {
		return ""
	}
	b, _ := yaml.Marshal(secretList)
	return string(b)
}

func (g *GKEOrchestrator) ListJobs(opts orchestrator.ListOptions) ([]orchestrator.JobStatus, error) {
	logging.Info("Listing jobs in cluster '%s'...", opts.ClusterName)
	if err := g.configureKubectl(opts.ClusterName, opts.ClusterLocation, opts.ProjectID); err != nil {
		return nil, err
	}

	client, err := g.getDynamicClient()
	if err != nil {
		return nil, err
	}

	gvr := schema.GroupVersionResource{Group: "jobset.x-k8s.io", Version: "v1alpha2", Resource: "jobsets"}
	list, err := client.Resource(gvr).Namespace("default").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list jobsets: %w", err)
	}

	var jobs []orchestrator.JobStatus
	for _, item := range list.Items {
		name := item.GetName()
		if opts.NameContains != "" && !strings.Contains(name, opts.NameContains) {
			continue
		}

		creationParams := item.GetCreationTimestamp()
		creationTime := creationParams.Time.Format(time.RFC3339)

		statusStr, completionTime := g.parseJobStatus(item.Object)

		if opts.Status != "" && !strings.EqualFold(statusStr, opts.Status) {
			continue
		}

		jobs = append(jobs, orchestrator.JobStatus{
			Name:           name,
			Status:         statusStr,
			CreationTime:   creationTime,
			CompletionTime: completionTime,
		})
	}

	return jobs, nil
}

func (g *GKEOrchestrator) DeleteJob(name string, opts orchestrator.DeleteOptions) error {
	logging.Info("Deleting job '%s' in cluster '%s'...", name, opts.ClusterName)
	if err := g.configureKubectl(opts.ClusterName, opts.ClusterLocation, opts.ProjectID); err != nil {
		return err
	}

	client, err := g.getDynamicClient()
	if err != nil {
		return err
	}

	gvr := schema.GroupVersionResource{Group: "jobset.x-k8s.io", Version: "v1alpha2", Resource: "jobsets"}
	err = client.Resource(gvr).Namespace("default").Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete jobset %s: %w", name, err)
	}

	logging.Info("Job '%s' deleted successfully.", name)
	return nil
}

func (g *GKEOrchestrator) getDynamicClient() (dynamic.Interface, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig: %w", err)
	}
	return dynamic.NewForConfig(config)
}

func (g *GKEOrchestrator) waitForJobCompletion(workloadName, clusterName, clusterLocation, projectID string) error {
	logging.Info("Waiting for job '%s' to complete...", workloadName)

	// kubectl wait --for jsonpath='.status.conditions[-1].type'=Finished jobset <workloadName> --timeout=1h
	waitRes := g.executor.ExecuteCommand("kubectl", "wait", "--for", "jsonpath={.status.conditions[-1].type}=Finished",
		"jobset", workloadName, "--timeout=1h")

	jobConsoleLink := fmt.Sprintf("https://console.cloud.google.com/kubernetes/workload/gke/%s/%s/details/%s?project=%s",
		clusterLocation, clusterName, workloadName, projectID)

	if waitRes.ExitCode != 0 {
		if strings.Contains(waitRes.Stderr, "timed out waiting") || strings.Contains(waitRes.Stdout, "timed out waiting") {
			logging.Error("Timed out waiting for job '%s' to finish. Check its status in the Cloud Console: %s", workloadName, jobConsoleLink)
			return fmt.Errorf("job timed out")
		}
		return fmt.Errorf("error waiting for job completion: %s\n%s", waitRes.Stderr, waitRes.Stdout)
	}

	logging.Info("Job '%s' has finished. Checking final status...", workloadName)

	// kubectl get jobset <workloadName> -o jsonpath='{.status.conditions[-1].type}'
	statusRes := g.executor.ExecuteCommand("kubectl", "get", "jobset", workloadName, "-o", "jsonpath={.status.conditions[-1].type}")

	if statusRes.ExitCode != 0 {
		return fmt.Errorf("failed to get final job status: %s\n%s", statusRes.Stderr, statusRes.Stdout)
	}

	finalStatus := strings.TrimSpace(statusRes.Stdout)
	if finalStatus != "Completed" {
		logging.Error("Job '%s' finished with status '%s'. Check details in the Cloud Console: %s", workloadName, finalStatus, jobConsoleLink)
		return fmt.Errorf("job completed unsuccessfully with status: %s", finalStatus)
	}

	logging.Info("Job '%s' completed successfully. View details in the Cloud Console: %s", workloadName, jobConsoleLink)
	return nil
}

func (g *GKEOrchestrator) buildNodeSelector(schedOpts scheduling.SchedulingOptions, job orchestrator.JobDefinition) (string, error) {
	nodeSelector := scheduling.GetNodeSelector(schedOpts)
	accelLabel := g.GenerateGKENodeSelectorLabel(job.AcceleratorType)
	if accelLabel != "" {
		if nodeSelector == nil {
			nodeSelector = make(map[string]string)
		}
		if strings.Contains(accelLabel, "tpu-v6e") {
			nodeSelector["cloud.google.com/gke-tpu-accelerator"] = accelLabel
		} else {
			nodeSelector["cloud.google.com/gke-accelerator"] = accelLabel
		}
	}

	if len(nodeSelector) > 0 {
		b, err := yaml.Marshal(nodeSelector)
		if err != nil {
			return "", fmt.Errorf("failed to marshal nodeSelector: %w", err)
		}
		return g.indentYaml(string(b), 16), nil
	}
	return "", nil
}

func (g *GKEOrchestrator) buildAffinity(schedOpts scheduling.SchedulingOptions) (string, error) {
	if affinity := scheduling.GetAffinity(schedOpts); affinity != nil {
		b, err := k8syaml.Marshal(affinity)
		if err != nil {
			return "", fmt.Errorf("failed to marshal affinity: %w", err)
		}
		return g.indentYaml(string(b), 16), nil
	}
	return "", nil
}

func (g *GKEOrchestrator) buildTopologyAnnotation(topology string) string {
	topologyAnnotation := scheduling.GetTopologyAnnotation(topology)
	if len(topologyAnnotation) > 0 {
		b, err := yaml.Marshal(topologyAnnotation)
		if err == nil {
			return g.indentYaml(string(b), 16)
		}
	}
	return ""
}
