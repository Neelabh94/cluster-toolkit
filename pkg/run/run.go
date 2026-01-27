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

package run

import (
	"fmt"
	"hpc-toolkit/pkg/logging"
	"hpc-toolkit/pkg/run/cloudbuild"
	"hpc-toolkit/pkg/run/gkemanifest"
	"hpc-toolkit/pkg/shell"
	"os"
	"strings"
)

// RunOptions holds all the necessary parameters for the 'run' command logic
type RunOptions struct {
	ImageName       string
	Dockerfile      string
	BuildContext    string
	CommandToRun    string
	AcceleratorType string
	OutputManifest  string // If set, manifest is saved here instead of applied
	ProjectID       string
	ClusterName     string
	ClusterLocation string
}

// ExecuteRun orchestrates the Cloud Build and GKE manifest deployment
func ExecuteRun(opts RunOptions) error {
	logging.Info("Starting gcluster run workflow...")

	// 1. Get Project ID (if not provided, attempt to get from gcloud config)
	if opts.ProjectID == "" {
		res := shell.ExecuteCommand("gcloud config get-value project")
		if res.ExitCode != 0 {
			return fmt.Errorf("failed to get GCP project ID from gcloud config: %s", res.Stderr)
		}
		logging.Info("DEBUG: Raw gcloud project output: '%s'", res.Stdout) // Debug statement
		opts.ProjectID = strings.TrimSpace(res.Stdout)
		logging.Info("DEBUG: Trimmed GCP Project ID: '%s'", opts.ProjectID) // Debug statement
		logging.Info("Using GCP Project ID from gcloud config: %s", opts.ProjectID)
	}

	// 2. Build Docker Image using Cloud Build
	logging.Info("Initiating Cloud Build for Docker image...")
	cbOpts := cloudbuild.BuildOptions{
		ImageName:    opts.ImageName,
		Dockerfile:   opts.Dockerfile,
		BuildContext: opts.BuildContext,
		ProjectID:    opts.ProjectID,
		Region:       "us-central1", // For POC, assume us-central1 or get from gcloud config
	}

	cloudBuildYaml, err := cloudbuild.GenerateCloudBuildYaml(cbOpts)
	if err != nil {
		return fmt.Errorf("failed to generate Cloud Build YAML: %w", err)
	}

	// For POC, we are not waiting for the build to complete, just submitting.
	// In a real scenario, we might want to poll for status.
	buildURL, err := cloudbuild.SubmitCloudBuild(cloudBuildYaml, opts.BuildContext, opts.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to submit Cloud Build: %w", err)
	}
	if buildURL != "" {
		logging.Info("Cloud Build submitted. Monitor its progress at: %s", buildURL)
	} else {
		logging.Info("Cloud Build submitted. Use 'gcloud builds list' to monitor progress.")
	}

	// Construct the full image name assuming the build will be successful.
	// In a robust implementation, this would only happen AFTER the build completes.
	fullImageName, err := cloudbuild.GetFullImageName(opts.ImageName, opts.ProjectID, cbOpts.Region)
	if err != nil {
		return fmt.Errorf("failed to construct full image name: %w", err)
	}
	logging.Info("Assuming image will be available at: %s", fullImageName)

	// 3. Generate and Apply GKE Manifest
	logging.Info("Configuring kubectl for GKE cluster '%s'...", opts.ClusterName)
	getCredsCmd := fmt.Sprintf("gcloud container clusters get-credentials %s --zone %s --project %s", opts.ClusterName, opts.ClusterLocation, opts.ProjectID)
	credsRes := shell.ExecuteCommand(getCredsCmd)
	if credsRes.ExitCode != 0 {
		return fmt.Errorf("failed to get GKE cluster credentials: %s\n%s", credsRes.Stderr, credsRes.Stdout)
	}
	logging.Info("kubectl configured successfully.")

	logging.Info("Generating GKE manifest...")
	gkeOpts := gkemanifest.ManifestOptions{
		JobName:         "gcluster-workload-" + shell.RandomString(8), // Unique job name
		FullImageName:   fullImageName,
		CommandToRun:    opts.CommandToRun,
		AcceleratorType: opts.AcceleratorType,
		ProjectID:       opts.ProjectID,
		ClusterName:     opts.ClusterName,
		ClusterLocation: opts.ClusterLocation,
	}

	gkeManifestContent, err := gkemanifest.GenerateGKEManifest(gkeOpts)
	if err != nil {
		return fmt.Errorf("failed to generate GKE manifest: %w", err)
	}

	if opts.OutputManifest != "" {
		logging.Info("Saving GKE manifest to %s", opts.OutputManifest)
		if err := os.WriteFile(opts.OutputManifest, []byte(gkeManifestContent), 0644); err != nil {
			return fmt.Errorf("failed to write GKE manifest to file %s: %w", opts.OutputManifest, err)
		}
		logging.Info("GKE manifest saved successfully.")
	} else {
		logging.Info("Applying GKE manifest to cluster...")
		// In a real scenario, we'd need to ensure the kubectl context is correctly set for the target cluster
		// For POC, we rely on the user having correctly configured kubectl.
		err = gkemanifest.ApplyGKEManifest(gkeManifestContent, opts.ProjectID, opts.ClusterName, opts.ClusterLocation)
		if err != nil {
			return fmt.Errorf("failed to apply GKE manifest: %w", err)
		}
		logging.Info("GKE workload deployed successfully.")
	}

	logging.Info("gcluster run workflow completed.")
	return nil
}
