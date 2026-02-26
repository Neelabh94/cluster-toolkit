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

func init() {
	workloadCmd.AddCommand(deleteWorkloadCmd)

	deleteWorkloadCmd.Flags().StringVar(&clusterName, "cluster-name", "", "Name of the GKE cluster. Required.")
	deleteWorkloadCmd.Flags().StringVar(&clusterLocation, "cluster-region", "", "Region of the GKE cluster. Required.")
	deleteWorkloadCmd.Flags().StringVarP(&projectID, "project", "p", "", "Google Cloud Project ID.")

	_ = deleteWorkloadCmd.MarkFlagRequired("cluster-name")
	_ = deleteWorkloadCmd.MarkFlagRequired("cluster-region")
}

var deleteWorkloadCmd = &cobra.Command{
	Use:          "delete [workload-name]",
	Short:        "Delete a workload from the cluster.",
	Args:         cobra.ExactArgs(1),
	Run:          runDeleteWorkload,
	SilenceUsage: true,
}

func runDeleteWorkload(cmd *cobra.Command, args []string) {
	jobName := args[0]
	logging.Info("Deleting job %s...", jobName)

	orc, err := gke.NewGKEOrchestrator()
	if err != nil {
		logging.Fatal("Failed to create orchestrator: %v", err)
	}

	opts := orchestrator.DeleteOptions{
		ClusterName:     clusterName,
		ClusterLocation: clusterLocation,
		ProjectID:       projectID,
	}

	if err := orc.DeleteJob(jobName, opts); err != nil {
		logging.Fatal("Failed to delete job: %v", err)
	}
}
