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
	rootCmd.AddCommand(deleteCmd)
	deleteCmd.AddCommand(deleteJobCmd)

	deleteJobCmd.Flags().StringVar(&clusterName, "cluster-name", "", "Name of the GKE cluster. Required.")
	deleteJobCmd.Flags().StringVar(&clusterLocation, "cluster-location", "", "Location (zone or region) of the GKE cluster. Required.")
	deleteJobCmd.Flags().StringVarP(&projectID, "project", "p", "", "Google Cloud Project ID.")

	_ = deleteJobCmd.MarkFlagRequired("cluster-name")
	_ = deleteJobCmd.MarkFlagRequired("cluster-location")
}

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete resources (jobs).",
}

var deleteJobCmd = &cobra.Command{
	Use:   "job [job-name]",
	Short: "Delete a job from the cluster.",
	Args:  cobra.ExactArgs(1),
	Run:   runDeleteJob,
	SilenceUsage: true,
}

func runDeleteJob(cmd *cobra.Command, args []string) {
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
