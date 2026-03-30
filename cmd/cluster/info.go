package cluster

import (
	"fmt"
	"hpc-toolkit/pkg/logging"
	"hpc-toolkit/pkg/orchestrator"
	"hpc-toolkit/pkg/orchestrator/gke"

	"github.com/spf13/cobra"
)

var InfoCmd = &cobra.Command{
	Use:          "info",
	Short:        "Show summarized status of the current target cluster's resources.",
	Run:          runClusterInfo,
	SilenceUsage: true,
}

func runClusterInfo(cmd *cobra.Command, args []string) {
	if clusterName == "" || clusterLocation == "" {
		logging.Fatal("--cluster and --cluster-region are required for info")
	}

	logging.Info("Fetching cluster info for %s...", clusterName)

	orc, err := gke.NewGKEOrchestrator()
	if err != nil {
		logging.Fatal("Failed to create orchestrator: %v", err)
	}

	opts := orchestrator.ListOptions{
		ProjectID:       projectID,
		ClusterLocation: clusterLocation,
	}

	info, err := orc.GetClusterInfo(clusterName, opts)
	if err != nil {
		logging.Fatal("Failed to get cluster info: %v", err)
	}

	fmt.Println(info)
}
