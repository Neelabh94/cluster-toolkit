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

package orchestrator

// JobDefinition holds all the necessary parameters to define a job.
// This struct is intended to be general enough to support various orchestrators,
// with specific orchestrator implementations extracting the fields relevant to them.
type JobDefinition struct {
	DockerImage     string
	BaseDockerImage string
	BuildContext    string
	Platform        string
	CommandToRun    string
	AcceleratorType string
	OutputManifest  string
	ProjectID       string
	ClusterName     string
	ClusterLocation string

	// JobSet and Kueue related options
	WorkloadName            string
	KueueQueueName          string
	NumSlices               int
	VmsPerSlice             int
	MaxRestarts             int
	TtlSecondsAfterFinished int
}

// Orchestrator defines the interface for submitting and managing jobs on a cluster.
type Orchestrator interface {
	// SubmitJob takes a JobDefinition and orchestrates its deployment.
	SubmitJob(job JobDefinition) error
}
