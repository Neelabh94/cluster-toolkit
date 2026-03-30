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

package cluster

import (
	"fmt"
	"os"
	"text/tabwriter"

	"hpc-toolkit/pkg/logging"
	"hpc-toolkit/pkg/orchestrator"
	"hpc-toolkit/pkg/orchestrator/gke"

	"github.com/spf13/cobra"
)

var ListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List available target clusters and environments.",
	Run:          runListClusters,
	SilenceUsage: true,
}

func runListClusters(cmd *cobra.Command, args []string) {
	logging.Info("Listing clusters...")

	orc, err := gke.NewGKEOrchestrator()
	if err != nil {
		logging.Fatal("Failed to create orchestrator: %v", err)
	}

	opts := orchestrator.ListOptions{
		ProjectID: projectID,
	}

	clusters, err := orc.ListEnvironments(opts)
	if err != nil {
		logging.Fatal("Failed to list clusters: %v", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME	LOCATION	STATUS")
	for _, c := range clusters {
		fmt.Fprintf(w, "%s\t%s\t%s\n", c.Name, c.Location, c.Status)
	}
	w.Flush()
}
