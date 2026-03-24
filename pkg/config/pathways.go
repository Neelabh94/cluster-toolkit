/*
Copyright 2024 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

// PathwaysConfig contains Pathways-specific configuration parameters.
type PathwaysConfig struct {
	ProxyServerImage            string `json:"proxyServerImage,omitempty"`
	ServerImage                 string `json:"serverImage,omitempty"`
	WorkerImage                 string `json:"workerImage,omitempty"`
	Headless                    bool   `json:"headless,omitempty"`
	GCSLocation                 string `json:"gcsLocation,omitempty"`
	ElasticSlices               int    `json:"elasticSlices,omitempty"`
	MaxSliceRestarts            int    `json:"maxSliceRestarts,omitempty"`
	ProxyArgs                   string `json:"proxyArgs,omitempty"`
	ServerArgs                  string `json:"serverArgs,omitempty"`
	WorkerArgs                  string `json:"workerArgs,omitempty"`
	ColocatedPythonSidecarImage string `json:"colocatedPythonSidecarImage,omitempty"`
}
