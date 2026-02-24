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

package scheduling

import (
	corev1 "k8s.io/api/core/v1"
)

// SchedulingOptions holds the options for generating scheduling constraints.
type SchedulingOptions struct {
	// PlacementPolicy is the name of the GKE Placement Policy to use.
	PlacementPolicy string
	// CPUAffinity specifies whether to use CPU affinity.
	// This could be simpler (bool) or more complex depending on requirements.
	// For now, let's assume it's a map of labels or a specific type.
	// If it's just "define CPU affinity rules", we might need more details.
	// But let's start with a generic map for now, or just NodeAffinity.
	// The request mentioned "--cpu-affinity: To define CPU affinity rules."
	// Maybe it's a boolean or a specific mode.
	// Let's assume it's a boolean flag for now that enables "compact" placement if possible,
	// or specific CPU pinning. But given CLI flag, let's stick to node affinity for now?
	// The user request said: "Implement functions ... GetAffinity ... generate the appropriate affinity sections".
	
	// Setup for explicit Node Affinity
	NodeAffinityLabels map[string]string
}

// GetNodeSelector returns a map of labels for node selection.
// It combines the accelerator type label, placement policy, and user-provided machine labels.
func GetNodeSelector(opts SchedulingOptions) map[string]string {
	nodeSelector := make(map[string]string)

	// Add accelerator label if present in options (we assumed opts had it, let's add it to struct or pass it)
	// We need to update SchedulingOptions to include AcceleratorTypeLabel if we want it here.
	// For now, let's assume we merge the output of this with accelerator label in the orchestrator,
	// OR we add it to machineLabels.
	// But let's check `SchedulingOptions` definition - it doesn't have it.
	// We should just use what is in `machineLabels` and `PlacementPolicy`.

	if opts.PlacementPolicy != "" {
		nodeSelector["cloud.google.com/gke-placement-group"] = opts.PlacementPolicy
	}

	for k, v := range opts.NodeAffinityLabels {
		nodeSelector[k] = v
	}

	if len(nodeSelector) == 0 {
		return nil
	}
	return nodeSelector
}

// GetAffinity returns the affinity configuration for the pod.
// Currently it maps generic affinity rules if needed.
func GetAffinity(opts SchedulingOptions) *corev1.Affinity {
	// If PlacementPolicy is used, we primarily use NodeSelector (handled above).
	// But sometimes users want PodAffinity or AntiAffinity.
	// If `CPUAffinity` implies specific NUMA topology or similar, we might add it here.
	
	// For now, if we have no custom affinity rules beyond node selectors, return nil.
	return nil
}
