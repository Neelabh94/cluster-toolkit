package cluster

import (
	"github.com/spf13/cobra"
)

var (
	clusterName     string
	clusterLocation string
	projectID       string
)

// ClusterCmd represents the base command for cluster-related operations
var ClusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "[EXPERIMENTAL] Manage clusters and environments.",
	Long:  `Discover, list, and introspect target clusters and environments. This feature is under active development.`,
}

func init() {
	ClusterCmd.PersistentFlags().StringVar(&clusterName, "cluster", "", "Name of the GKE cluster. Required for info, describe, volume.")
	ClusterCmd.PersistentFlags().StringVar(&clusterLocation, "cluster-region", "", "Region of the GKE cluster. Required for info, describe, volume.")
	ClusterCmd.PersistentFlags().StringVarP(&projectID, "project", "p", "", "Google Cloud Project ID.")

	ClusterCmd.AddCommand(ListCmd)
	ClusterCmd.AddCommand(InfoCmd)
	ClusterCmd.AddCommand(DescribeCmd)
	ClusterCmd.AddCommand(VolumeCmd)
}
