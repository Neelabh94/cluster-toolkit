
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

var volumeCmd = &cobra.Command{
	Use:   "volume",
	Short: "Manage volume resources on the cluster.",
}

var listVolumesCmd = &cobra.Command{
	Use:          "list",
	Short:        "List volume resources in the cluster.",
	Run:          runListVolumes,
	SilenceUsage: true,
}

func init() {
	rootCmd.AddCommand(volumeCmd)
	volumeCmd.AddCommand(listVolumesCmd)

	listVolumesCmd.Flags().StringVar(&clusterName, "cluster-name", "", "Name of the GKE cluster. If omitted, all clusters in the region will be queried.")
	listVolumesCmd.Flags().StringVar(&clusterLocation, "cluster-region", "", "Region of the GKE cluster. Required.")
	listVolumesCmd.Flags().StringVarP(&projectID, "project", "p", "", "Google Cloud Project ID.")

	_ = listVolumesCmd.MarkFlagRequired("cluster-region")
}

func runListVolumes(cmd *cobra.Command, args []string) {
	logging.Info("Listing volume resources...")

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

	if len(volumes) == 0 {
		logging.Info("No volume resources found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tCLUSTER\tMOUNT_POINT")
	for _, vol := range volumes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", vol.Name, vol.Type, vol.Cluster, vol.MountPoint)
	}
	w.Flush()
}
