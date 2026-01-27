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

package cloudbuild

import (
	"bytes"
	"fmt"
	"hpc-toolkit/pkg/logging"
	"hpc-toolkit/pkg/shell"
	"os"
	"strings"
	"text/template"
)

// CloudBuildTemplate is the Go template for generating cloudbuild.yaml
const CloudBuildTemplate = `
steps:
- name: 'gcr.io/cloud-builders/docker'
  args: ['build', '-f', '{{.Dockerfile}}', '-t', '{{.FullImageName}}', '.']
images:
- '{{.FullImageName}}'
`

// BuildOptions holds parameters for the Cloud Build process
type BuildOptions struct {
	ImageName    string
	Dockerfile   string
	BuildContext string
	ProjectID    string
	Region       string // GCR/Artifact Registry region
}

// GenerateCloudBuildYaml generates the cloudbuild.yaml content
func GenerateCloudBuildYaml(opts BuildOptions) (string, error) {
	fullImageName, err := GetFullImageName(opts.ImageName, opts.ProjectID, opts.Region)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New("cloudbuild").Parse(CloudBuildTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse cloudbuild template: %w", err)
	}

	data := struct {
		Dockerfile    string
		FullImageName string
	}{
		Dockerfile:    opts.Dockerfile,
		FullImageName: fullImageName,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute cloudbuild template: %w", err)
	}
	return buf.String(), nil
}

// SubmitCloudBuild submits the cloudbuild.yaml to GCP Cloud Build
func SubmitCloudBuild(cloudBuildYamlContent string, buildContextPath string, projectID string) (string, error) {
	tmpFile, err := os.CreateTemp("", "cloudbuild-*.yaml")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary cloudbuild.yaml file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(cloudBuildYamlContent); err != nil {
		return "", fmt.Errorf("failed to write cloudbuild.yaml content to temporary file: %w", err)
	}

	logging.Info("Submitting Cloud Build with context: %s", buildContextPath)
	logging.Info("CloudBuild YAML content:\n%s", cloudBuildYamlContent)

	cmd := fmt.Sprintf("gcloud builds submit %s --config=%s --project=%s", buildContextPath, tmpFile.Name(), projectID)

	// Execute the gcloud command
	result := shell.ExecuteCommand(cmd)

	if result.ExitCode != 0 {
		return "", fmt.Errorf("gcloud builds submit failed with exit code %d: %s\n%s", result.ExitCode, result.Stderr, result.Stdout)
	}

	// Attempt to extract build URL from stdout
	buildURL := extractBuildURL(result.Stdout)
	if buildURL != "" {
		logging.Info("Cloud Build submitted successfully: %s", buildURL)
	} else {
		logging.Info("Cloud Build submitted successfully. Check 'gcloud builds list' for status.")
	}
	return buildURL, nil
}

// GetFullImageName constructs the full image name for GCR/Artifact Registry
func GetFullImageName(imageName, projectID, region string) (string, error) {
	imageName = strings.TrimSpace(imageName) // Trim input imageName
	projectID = strings.TrimSpace(projectID) // Safeguard trim projectID
	fmt.Printf("DEBUG: GetFullImageName: Input imageName='%s', projectID='%s'\n", imageName, projectID)

	if imageName == "" {
		return "", fmt.Errorf("image name cannot be empty")
	}

	// Split tag from image name
	parts := strings.SplitN(imageName, ":", 2)
	baseImage := parts[0]
	tag := "latest"
	if len(parts) == 2 {
		tag = parts[1]
	}

	fmt.Printf("DEBUG: GetFullImageName: baseImage='%s', tag='%s'\n", baseImage, tag)

	// If imageName already contains a registry host like gcr.io/ or -docker.pkg.dev/
	if strings.HasPrefix(imageName, "gcr.io/") || strings.Contains(imageName, "-docker.pkg.dev/") {
		fmt.Printf("DEBUG: GetFullImageName: Image name already fully qualified: %s\n", imageName)
		return imageName, nil
	}

	// If imageName starts with the projectID (e.g., "hpc-toolkit-gsc/my-app:latest")
	// and the provided projectID matches, then just prepend "gcr.io/"
	if strings.HasPrefix(baseImage, projectID+"/") {
		fullImage := fmt.Sprintf("gcr.io/%s:%s", baseImage, tag)
		fmt.Printf("DEBUG: GetFullImageName: Constructed full image name (with projectID prefix): %s\n", fullImage)
		return fullImage, nil
	}

	// Otherwise, prepend gcr.io/<projectID>/
	fullImage := fmt.Sprintf("gcr.io/%s/%s:%s", projectID, baseImage, tag)
	fmt.Printf("DEBUG: GetFullImageName: Constructed full image name (prepended projectID): %s\n", fullImage)
	return fullImage, nil
}

// extractBuildURL attempts to parse the Cloud Build URL from gcloud's stdout
func extractBuildURL(stdout string) string {
	lines := strings.Split(stdout, "\n")
	for _, line := range lines {
		if strings.Contains(line, "builds/") && strings.Contains(line, "console.cloud.google.com") {
			// This is a heuristic, may need refinement
			if idx := strings.Index(line, "https://console.cloud.google.com"); idx != -1 {
				return strings.TrimSpace(line[idx:])
			}
		}
	}
	return ""
}
