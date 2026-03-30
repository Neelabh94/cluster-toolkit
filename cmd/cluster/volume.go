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

var VolumeCmd = &cobra.Command{
	Use:          "volume",
	Short:        "Discovers and lists available storage options accessible to the user.",
	Run:          runListVolumes,
	SilenceUsage: true,
}

func runListVolumes(cmd *cobra.Command, args []string) {
	logging.Info("Listing managed volumes...")

	orc, err := gke.NewGKEOrchestrator()
	if err != nil {
		logging.Fatal("Failed to create orchestrator: %v", err)
	}

	opts := orchestrator.ListOptions{
		ClusterName:     clusterName,
		ClusterLocation: clusterLocation,
		ProjectID:       projectID,
	}

	volumes, err := orc.ListVolumes(opts)
	if err != nil {
		logging.Fatal("Failed to list volumes: %v", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tMOUNT_PATH\tCLUSTER")
	for _, v := range volumes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", v.Name, v.Type, v.MountPath, v.Cluster)
	}
	w.Flush()
}
