
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
	clusterName     string
	clusterLocation string
	projectID       string
)

var storageCmd = &cobra.Command{
	Use:   "storage",
	Short: "Manage storage resources on the cluster.",
}

var listStoragesCmd = &cobra.Command{
	Use:          "list",
	Short:        "List storage resources in the cluster.",
	Run:          runListStorages,
	SilenceUsage: true,
}

func init() {
	rootCmd.AddCommand(storageCmd)
	storageCmd.AddCommand(listStoragesCmd)

	listStoragesCmd.Flags().StringVar(&clusterName, "cluster-name", "", "Name of the GKE cluster. If omitted, all clusters in the region will be queried.")
	listStoragesCmd.Flags().StringVar(&clusterLocation, "cluster-region", "", "Region of the GKE cluster. Required.")
	listStoragesCmd.Flags().StringVarP(&projectID, "project", "p", "", "Google Cloud Project ID.")

	_ = listStoragesCmd.MarkFlagRequired("cluster-region")
}

func runListStorages(cmd *cobra.Command, args []string) {
	logging.Info("Listing storage resources...")

	orc, err := gke.NewGKEOrchestrator()
	if err != nil {
		logging.Fatal("Failed to create orchestrator: %v", err)
	}

	opts := orchestrator.ListOptions{
		ClusterName:     clusterName,
		ClusterLocation: clusterLocation,
		ProjectID:       projectID,
	}

	storages, err := orc.ListStorages(opts)
	if err != nil {
		logging.Fatal("Failed to list storages: %v", err)
	}

	if len(storages) == 0 {
		logging.Info("No storage resources found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tCLUSTER\tMOUNT_POINT")
	for _, stg := range storages {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", stg.Name, stg.Type, stg.Cluster, stg.MountPoint)
	}
	w.Flush()
}
