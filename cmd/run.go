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
	"hpc-toolkit/pkg/run" // Import the new run package

	"github.com/spf13/cobra"
)

var (
	imageName       string
	dockerfilePath  string
	buildContext    string
	commandToRun    string
	acceleratorType string
	outputManifest  string
	clusterName     string
	clusterLocation string
)

func init() {
	rootCmd.AddCommand(runCmd)

	runCmd.Flags().StringVarP(&imageName, "image-name", "i", "", "Name of the Docker image to build and run (e.g., my-project/my-image:tag). Required.")
	runCmd.Flags().StringVarP(&dockerfilePath, "dockerfile", "f", "Dockerfile", "Path to the Dockerfile.")
	runCmd.Flags().StringVarP(&buildContext, "build-context", "c", ".", "Path to the build context directory.")
	runCmd.Flags().StringVarP(&commandToRun, "command", "e", "", "Command to execute in the container (e.g., 'python train.py'). Required.")
	runCmd.Flags().StringVarP(&acceleratorType, "accelerator-type", "a", "", "Type of accelerator to request (e.g., 'nvidia-tesla-a100', 'tpu-v4-podslice'). Required.")
	runCmd.Flags().StringVarP(&outputManifest, "output-manifest", "o", "", "Path to output the generated Kubernetes manifest instead of applying it.")
	runCmd.Flags().StringVar(&clusterName, "cluster-name", "", "Name of the GKE cluster to deploy the workload to. Required.")
	runCmd.Flags().StringVar(&clusterLocation, "cluster-location", "", "Location (zone or region) of the GKE cluster. Required.")

	// Mark required flags
	runCmd.MarkFlagRequired("image-name")
	runCmd.MarkFlagRequired("command")
	runCmd.MarkFlagRequired("cluster-name")
	runCmd.MarkFlagRequired("cluster-location")
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Builds a Docker image using Cloud Build and runs a workload on a GKE cluster.",
	Long: `The 'run' command automates the process of building a Docker image via Google Cloud Build
and deploying it as a workload (Kubernetes Job) on a GKE cluster.

It accepts parameters for the Docker image, command to execute, and desired accelerator type.`,
	Run:          runRunCmd,
	SilenceUsage: true,
}

func runRunCmd(cmd *cobra.Command, args []string) {
	logging.Info("Executing gcluster run command...")
	runOpts := run.RunOptions{
		ImageName:       imageName,
		Dockerfile:      dockerfilePath,
		BuildContext:    buildContext,
		CommandToRun:    commandToRun,
		AcceleratorType: acceleratorType,
		OutputManifest:  outputManifest,
		ProjectID:       "", // Will be populated by ExecuteRun if empty
		ClusterName:     clusterName,
		ClusterLocation: clusterLocation,
	}

	if err := run.ExecuteRun(runOpts); err != nil {
		logging.Fatal("gcluster run failed: %v", err)
	}
}
