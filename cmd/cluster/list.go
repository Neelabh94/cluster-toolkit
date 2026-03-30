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
