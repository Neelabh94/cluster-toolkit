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
	"hpc-toolkit/pkg/logging"
	"hpc-toolkit/pkg/orchestrator"
	"hpc-toolkit/pkg/orchestrator/gke"

	"github.com/spf13/cobra"
)

var DescribeCmd = &cobra.Command{
	Use:          "describe",
	Short:        "Details the specific environment exhaustively (hardware, exact configs, networking).",
	Run:          runClusterDescribe,
	SilenceUsage: true,
}

func runClusterDescribe(cmd *cobra.Command, args []string) {
	if clusterName == "" || clusterLocation == "" {
		logging.Fatal("--cluster and --cluster-region are required for describe")
	}

	logging.Info("Describing cluster %s...", clusterName)

	orc, err := gke.NewGKEOrchestrator()
	if err != nil {
		logging.Fatal("Failed to create orchestrator: %v", err)
	}

	opts := orchestrator.ListOptions{
		ProjectID:       projectID,
		ClusterLocation: clusterLocation,
	}

	description, err := orc.DescribeEnvironment(clusterName, opts)
	if err != nil {
		logging.Fatal("Failed to describe cluster: %v", err)
	}

	fmt.Println(description)
}
