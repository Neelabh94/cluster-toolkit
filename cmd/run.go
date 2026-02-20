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
	"hpc-toolkit/pkg/logging"
	"hpc-toolkit/pkg/orchestrator"
	"hpc-toolkit/pkg/orchestrator/gke"

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

	// JobSet and Kueue related options
	workloadName            string
	kueueQueueName          string
	numSlices               int
	vmsPerSlice             int
	maxRestarts             int
	ttlSecondsAfterFinished int
	platform                string // Added for platform flag
)

func init() {
	rootCmd.AddCommand(runCmd)

	runCmd.Flags().StringVarP(&dockerImage, "docker-image", "i", "", "Name of the pre-built Docker image to run (e.g., my-project/my-image:tag).")
	runCmd.Flags().StringVar(&baseDockerImage, "base-docker-image", "", "Name of the base Docker image for Crane to build upon (e.g., python:3.9-slim). Requires --build-context.")
	runCmd.Flags().StringVarP(&buildContext, "build-context", "c", "", "Path to the build context directory for Crane (e.g., .). Required with --base-docker-image.")
	runCmd.Flags().StringVarP(&commandToRun, "command", "e", "", "Command to execute in the container (e.g., 'python train.py'). Required.")
	runCmd.Flags().StringVarP(&acceleratorType, "accelerator-type", "a", "", "Type of accelerator to request (e.g., 'nvidia-tesla-a100'). If empty, it will be auto-discovered.")
	runCmd.Flags().StringVarP(&outputManifest, "output-manifest", "o", "", "Path to output the generated Kubernetes manifest instead of applying it.")
	runCmd.Flags().StringVar(&clusterName, "cluster-name", "", "Name of the GKE cluster to deploy the workload to. Required.")
	runCmd.Flags().StringVar(&clusterLocation, "cluster-location", "", "Location (zone or region) of the GKE cluster. Required.")
	runCmd.Flags().StringVarP(&projectID, "project", "p", "", "Google Cloud Project ID. If not provided, it will be inferred from your gcloud configuration.")
	runCmd.Flags().StringVarP(&platform, "platform", "f", "linux/amd64", "Target platform for the Docker image build (e.g., 'linux/amd64', 'linux/arm64'). Used with --base-docker-image.")

	// JobSet and Kueue flags
	runCmd.Flags().StringVarP(&workloadName, "workload-name", "w", "", "Name of the workload (JobSet) to create. Required.")
	runCmd.Flags().StringVar(&kueueQueueName, "kueue-queue", "", "Name of the Kueue LocalQueue to submit the workload to. If empty, it will be auto-discovered.")
	runCmd.Flags().IntVar(&numSlices, "num-slices", 1, "Number of JobSet replicas (slices).")
	runCmd.Flags().IntVar(&vmsPerSlice, "vms-per-slice", 1, "Number of VMs (pods) per slice.")
	runCmd.Flags().IntVar(&maxRestarts, "max-restarts", 1, "Maximum number of restarts for the JobSet before failing.")
	runCmd.Flags().IntVar(&ttlSecondsAfterFinished, "ttl-seconds-after-finished", 3600, "Time (in seconds) to retain the JobSet after it finishes.")

	// Mark required flags
	_ = runCmd.MarkFlagRequired("command")
	_ = runCmd.MarkFlagRequired("cluster-name")
	_ = runCmd.MarkFlagRequired("cluster-location")
	_ = runCmd.MarkFlagRequired("workload-name") // Mark new workload-name as required

	// Mutually exclusive flags and conditional requirements will be validated in runRunCmd
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Runs a Docker image workload on a GKE cluster using JobSet.",
	Long: `The 'run' command deploys a Docker image as a workload (Kubernetes JobSet)
on a GKE cluster, integrated with Kueue. Image can be pre-built (--docker-image)
or built on-the-fly using Crane (--base-docker-image with --build-context).

It accepts parameters for the Docker image, command to execute, accelerator type,
and JobSet/Kueue specific configurations like workload name, queue, slices, and restarts.`,
	Run:          runRunCmd,
	SilenceUsage: true,
}

func runRunCmd(cmd *cobra.Command, args []string) {
	logging.Info("Executing gcluster run command...")

	// Validation logic for image flags
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
	}

	gkeOrchestrator, err := gke.NewGKEOrchestrator()
	if err != nil {
		logging.Fatal("Failed to create GKE orchestrator: %v", err)
	}

	if err := gkeOrchestrator.SubmitJob(jobDef); err != nil {
		logging.Fatal("gcluster run failed: %v", err)
	}
}
