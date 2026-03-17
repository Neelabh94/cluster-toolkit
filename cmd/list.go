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
	"os"
	"text/tabwriter"

	"hpc-toolkit/pkg/logging"
	"hpc-toolkit/pkg/orchestrator"
	"hpc-toolkit/pkg/orchestrator/gke"

	"github.com/spf13/cobra"
)

var (
	filterStatus string
	filterName   string
)

var listWorkloadsCmd = &cobra.Command{
	Use:          "list",
	Short:        "List workloads (jobs) in the cluster.",
	Run:          runListWorkloads,
	SilenceUsage: true,
}

func init() {
	jobCmd.AddCommand(listWorkloadsCmd)

	listWorkloadsCmd.Flags().StringVar(&clusterName, "cluster", "", "Name of the GKE cluster. Required.")
	listWorkloadsCmd.Flags().StringVar(&clusterLocation, "cluster-region", "", "Region of the GKE cluster. Required.")
	listWorkloadsCmd.Flags().StringVarP(&projectID, "project", "p", "", "Google Cloud Project ID.")

	listWorkloadsCmd.Flags().StringVar(&filterStatus, "status", "", "Filter jobs by status (e.g. Running, Failed, Succeeded).")
	listWorkloadsCmd.Flags().StringVar(&filterName, "name-contains", "", "Filter jobs by name.")

	_ = listWorkloadsCmd.MarkFlagRequired("cluster")
	_ = listWorkloadsCmd.MarkFlagRequired("cluster-region")
}

func runListWorkloads(cmd *cobra.Command, args []string) {
	logging.Info("Listing jobs...")

	orc, err := gke.NewGKEOrchestrator()
	if err != nil {
		logging.Fatal("Failed to create orchestrator: %v", err)
	}

	opts := orchestrator.ListOptions{
		ClusterName:     clusterName,
		ClusterLocation: clusterLocation,
		ProjectID:       projectID,
		Status:          filterStatus,
		NameContains:    filterName,
	}

	jobs, err := orc.ListJobs(opts)
	if err != nil {
		logging.Fatal("Failed to list jobs: %v", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tCREATION_TIME\tCOMPLETION_TIME")
	for _, job := range jobs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", job.Name, job.Status, job.CreationTime, job.CompletionTime)
	}
	w.Flush()
}
