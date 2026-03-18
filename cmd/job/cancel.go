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

package job

import (
	"hpc-toolkit/pkg/logging"
	"hpc-toolkit/pkg/orchestrator"
	"hpc-toolkit/pkg/orchestrator/gke"

	"github.com/spf13/cobra"
)

var CancelJobCmd = &cobra.Command{
	Use:          "cancel [job-name]",
	Short:        "Cancel a job from the cluster.",
	Args:         cobra.ExactArgs(1),
	Run:          runCancelJob,
	SilenceUsage: true,
}

func init() {
	CancelJobCmd.Flags().StringVar(&clusterName, "cluster", "", "Name of the GKE cluster. Required.")
	CancelJobCmd.Flags().StringVar(&clusterLocation, "cluster-region", "", "Region of the GKE cluster. Required.")
	CancelJobCmd.Flags().StringVarP(&projectID, "project", "p", "", "Google Cloud Project ID.")

	_ = CancelJobCmd.MarkFlagRequired("cluster")
	_ = CancelJobCmd.MarkFlagRequired("cluster-region")
}

func runCancelJob(cmd *cobra.Command, args []string) {
	jobName := args[0]
	logging.Info("Cancelling job %s...", jobName)

	orc, err := gke.NewGKEOrchestrator()
	if err != nil {
		logging.Fatal("Failed to create orchestrator: %v", err)
	}

	opts := orchestrator.CancelOptions{
		ClusterName:     clusterName,
		ClusterLocation: clusterLocation,
		ProjectID:       projectID,
	}

	if err := orc.CancelJob(jobName, opts); err != nil {
		logging.Fatal("Failed to delete job: %v", err)
	}
}
