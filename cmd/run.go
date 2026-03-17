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

package cmd

import (
	"fmt"
	"hpc-toolkit/pkg/logging"
	"hpc-toolkit/pkg/orchestrator"
	"hpc-toolkit/pkg/orchestrator/gke"
	"hpc-toolkit/pkg/prereq"

	"github.com/spf13/cobra"
)

var (
	dockerImage     string
	baseDockerImage string
	buildContext    string
	commandToRun    string
	acceleratorType string
	outputManifest  string
	clusterName     string
	clusterLocation string
	projectID       string

	workloadName            string
	kueueQueueName          string
	numSlices               int
	vmsPerSlice             int
	maxRestarts             int
	ttlSecondsAfterFinished int

	placementPolicy string
	nodeSelector    map[string]string

	cpuAffinityStr     string
	restartOnExitCodes []int
	imagePullSecrets   string
	serviceAccountName string
	topology           string
	scheduler          string
	platform           string

	awaitJobCompletion bool
)

func init() {
	jobCmd.AddCommand(submitCmd)

	submitCmd.Flags().StringVarP(&dockerImage, "image", "i", "", "Name of the pre-built Docker image to run (e.g., my-project/my-image:tag).")
	submitCmd.Flags().StringVar(&baseDockerImage, "base-docker-image", "", "Name of the base Docker image for Crane to build upon (e.g., python:3.9-slim). Requires --build-context.")
	submitCmd.Flags().StringVarP(&buildContext, "build-context", "c", "", "Path to the build context directory for Crane (e.g., .). Required with --base-docker-image.")
	submitCmd.Flags().StringVarP(&commandToRun, "command", "e", "", "Command to execute in the container (e.g., 'python train.py'). Required.")
	submitCmd.Flags().StringVarP(&acceleratorType, "accelerator", "a", "", "Type of accelerator to request (e.g., 'nvidia-tesla-a100'). If empty, it will be auto-discovered.")
	submitCmd.Flags().StringVarP(&outputManifest, "dry-run-out", "o", "", "Path to output the generated Kubernetes manifest instead of applying it.")
	submitCmd.Flags().StringVar(&clusterName, "cluster", "", "Name of the GKE cluster to deploy the workload to. Required.")
	submitCmd.Flags().StringVar(&clusterLocation, "cluster-region", "", "Region of the GKE cluster. Required.")
	submitCmd.Flags().StringVarP(&projectID, "project", "p", "", "Google Cloud Project ID. If not provided, it will be inferred from your gcloud configuration.")
	submitCmd.Flags().StringVarP(&platform, "platform", "f", "linux/amd64", "Target platform for the Docker image build (e.g., 'linux/amd64', 'linux/arm64'). Used with --base-docker-image.")

	submitCmd.Flags().StringVarP(&workloadName, "name", "n", "", "Name of the workload to create. Required.")
	submitCmd.Flags().StringVar(&kueueQueueName, "kueue-queue", "", "Name of the Kueue LocalQueue to submit the workload to. If empty, it will be auto-discovered.")
	submitCmd.Flags().IntVar(&numSlices, "nodes", 1, "Number of JobSet replicas (nodes).")
	submitCmd.Flags().IntVar(&vmsPerSlice, "vms-per-slice", 1, "Number of VMs (pods) per slice.")
	submitCmd.Flags().IntVar(&maxRestarts, "max-restarts", 1, "Maximum number of restarts for the JobSet before failing.")
	submitCmd.Flags().IntVar(&ttlSecondsAfterFinished, "ttl-seconds-after-finished", 3600, "Time (in seconds) to retain the JobSet after it finishes.")

	submitCmd.Flags().StringVar(&placementPolicy, "placement-policy", "", "Name of the GKE placement policy to use.")
	submitCmd.Flags().StringToStringVar(&nodeSelector, "machine-label", nil, "Key=value pairs for node labels to target specific machine types.")
	submitCmd.Flags().StringVar(&cpuAffinityStr, "cpu-affinity", "", "CPU affinity rules (e.g., 'numa').")
	submitCmd.Flags().IntSliceVar(&restartOnExitCodes, "restart-on-exit-codes", nil, "List of exit codes that should not trigger a job failure.")
	submitCmd.Flags().StringVar(&imagePullSecrets, "image-pull-secret", "", "Comma-separated list of secrets for pulling images.")
	submitCmd.Flags().StringVar(&serviceAccountName, "service-account", "", "Service account name for the pods.")
	submitCmd.Flags().StringVar(&topology, "topology", "", "TPU slice topology (e.g., 2x2x1).")
	submitCmd.Flags().StringVar(&scheduler, "scheduler", "", "Kubernetes Scheduler name (e.g., gke.io/topology-aware-auto).")
	submitCmd.Flags().BoolVar(&awaitJobCompletion, "await-job-completion", false, "If true, gcluster will wait for the submitted job to complete.")

	_ = submitCmd.MarkFlagRequired("command")
	_ = submitCmd.MarkFlagRequired("cluster")
	_ = submitCmd.MarkFlagRequired("cluster-region")
}

var submitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Submits a Docker image workload on a Gke cluster using JobSet.",
	Long: `The 'submit' command deploys a Docker image as a workload (Kubernetes JobSet)
on a GKE cluster, integrated with Kueue. Image can be pre-built (--image)
or built on-the-fly using Crane (--base-docker-image with --build-context).

It accepts parameters for the Docker image, command to execute, accelerator type,
and JobSet/Kueue specific configurations like workload name, queue, nodes, and restarts.`,
	Run: runRunCmd,

	PreRunE: func(cmd *cobra.Command, args []string) error {
		logging.Info("Running prerequisite checks for 'gcluster job submit'...")
		err := prereq.EnsurePrerequisites(&projectID)
		if err != nil {
			return fmt.Errorf("prerequisite checks failed: %w", err)
		}
		logging.Info("Prerequisite checks completed successfully.")
		return nil
	},
	SilenceUsage: true,
}

func runRunCmd(cmd *cobra.Command, args []string) {
	logging.Info("Executing gcluster job submit command...")

	if dockerImage == "" && baseDockerImage == "" {
		logging.Fatal("Either --docker-image or --base-docker-image must be provided.")
	}
	if dockerImage != "" && baseDockerImage != "" {
		logging.Fatal("Cannot provide both --docker-image and --base-docker-image.")
	}
	if dockerImage != "" && buildContext != "" {
		logging.Fatal("--build-context cannot be provided when --docker-image is used as no build is performed.")
	}
	if baseDockerImage != "" && buildContext == "" {
		logging.Fatal("A --build-context must be provided when --base-docker-image is used for a Crane build.")
	}

	jobDef := orchestrator.JobDefinition{
		DockerImage:             dockerImage,
		BaseDockerImage:         baseDockerImage,
		BuildContext:            buildContext,
		Platform:                platform,
		CommandToRun:            commandToRun,
		AcceleratorType:         acceleratorType,
		OutputManifest:          outputManifest,
		ProjectID:               projectID,
		ClusterName:             clusterName,
		ClusterLocation:         clusterLocation,
		WorkloadName:            workloadName,
		KueueQueueName:          kueueQueueName,
		NumSlices:               numSlices,
		VmsPerSlice:             vmsPerSlice,
		MaxRestarts:             maxRestarts,
		TtlSecondsAfterFinished: ttlSecondsAfterFinished,
		PlacementPolicy:         placementPolicy,
		NodeSelector:            nodeSelector,
		Affinity:                map[string]string{"cpu-affinity": cpuAffinityStr},
		RestartOnExitCodes:      restartOnExitCodes,
		ImagePullSecrets:        imagePullSecrets,
		ServiceAccountName:      serviceAccountName,
		Topology:                topology,
		Scheduler:               scheduler,
		AwaitJobCompletion:      awaitJobCompletion,
	}

	gkeOrchestrator, err := gke.NewGKEOrchestrator()
	if err != nil {
		logging.Fatal("Failed to create GKE orchestrator: %v", err)
	}

	if err := gkeOrchestrator.SubmitJob(jobDef); err != nil {
		logging.Fatal("gcluster job submit failed: %v", err)
	}
}
