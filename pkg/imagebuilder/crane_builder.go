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

package imagebuilder

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/compression"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/sirupsen/logrus"
)

// DockerPlatform represents the target platform for a Docker image.
type DockerPlatform string

const (
	LinuxAMD64 DockerPlatform = "linux/amd64"
	LinuxARM64 DockerPlatform = "linux/arm64"
)

// BuildContainerImageFromBaseImage builds and pushes a container image.
// It appends a new layer created from the scriptDir, filtered by ignorePatterns,
// to a base Docker image.
func BuildContainerImageFromBaseImage(
	project string,
	baseDockerImage string,
	scriptDir string,
	platformStr string,
	ignorePatterns []string,
) (string, error) {
	platform, err := parsePlatform(platformStr)
	if err != nil {
		return "", err
	}

	userName := os.Getenv("USER")
	if userName == "" {
		userName = "unknown"
	}

	tagRandomPrefix := generateRandomString(4)
	tagDatetime := time.Now().Format("2006-01-02-15-04-05") // YYYY-MM-DD-HH-MM-SS
	imageName := fmt.Sprintf("gcr.io/%s/%s-runner:%s-%s", project, userName, tagRandomPrefix, tagDatetime)

	logrus.Infof("Starting image build process for %s", imageName)
	logrus.Infof("Base Docker Image: %s", baseDockerImage)
	logrus.Infof("Script Directory: %s", scriptDir)
	logrus.Infof("Target Platform: %s/%s", platform.OS, platform.Architecture) // Corrected

	// 1. Create a tarball in a temporary file from the scriptDir, applying ignore patterns.
	tempTarballPath, err := createFilteredTar(scriptDir, ignorePatterns)
	if err != nil {
		return "", fmt.Errorf("failed to create filtered tarball: %w", err)
	}
	// Ensure the temporary file is cleaned up after use.
	defer func() {
		if tempTarballPath != "" {
			os.Remove(tempTarballPath)
			logrus.Debugf("Cleaned up temporary tarball file: %s", tempTarballPath)
		}
	}()

	// 2. Create a v1.Layer from the tarball.
	tarLayer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		file, openErr := os.Open(tempTarballPath)
		if openErr != nil {
			return nil, fmt.Errorf("failed to open temporary tarball %q: %w", tempTarballPath, openErr)
		}
		return file, nil
	}, tarball.WithCompression(compression.GZip))
	if err != nil {
		return "", fmt.Errorf("failed to create layer from tarball: %w", err)
	}

	// 3. Pull the base image.
	baseRef, err := name.ParseReference(baseDockerImage)
	if err != nil {
		return "", fmt.Errorf("failed to parse base image reference %q: %w", baseDockerImage, err)
	}

	// Correctly pass a pointer to v1.Platform
	baseImg, err := crane.Pull(baseRef.String(), crane.WithPlatform(&platform)) // Corrected, pass platform directly
	if err != nil {
		return "", fmt.Errorf("failed to pull base image %q: %w", baseDockerImage, err)
	}

	// 4. Append the new layer to the base image.
	newImg, err := mutate.AppendLayers(baseImg, tarLayer)
	if err != nil {
		return "", fmt.Errorf("failed to append layer: %w", err)
	}

	// 5. Optionally, set the image config (e.g., entrypoint, cmd) if needed.
	// For crane mutate --append, typically only new layers are added.
	// If the python script had specific config changes, they would need to be replicated here.

	// 6. Push the new image.
	imageRef, err := name.ParseReference(imageName)
	if err != nil {
		return "", fmt.Errorf("failed to parse new image reference %q: %w", imageName, err)
	}

	logrus.Infof("Uploading Container Image to %s", imageName)
	// Correctly pass a pointer to v1.Platform
	err = crane.Push(newImg, imageRef.String(), crane.WithPlatform(&platform)) // Corrected, pass platform directly
	if err != nil {
		return "", fmt.Errorf("failed to push image %q: %w", imageName, err)
	}

	logrus.Infof("Image %s built and uploaded successfully.", imageName)
	return imageName, nil
}

// parsePlatform converts a platform string (e.g., "linux/amd64") into a v1.Platform struct.
func parsePlatform(platformStr string) (v1.Platform, error) {
	parts := strings.Split(platformStr, "/")
	if len(parts) != 2 {
		return v1.Platform{}, fmt.Errorf("invalid platform format: %q, expected \"os/arch\"", platformStr)
	}
	return v1.Platform{
		OS:           parts[0],
		Architecture: parts[1],
	}, nil
}

// generateRandomString generates a random string of specified length.
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz"
	// Seed the random number generator using the current time
	var seededRand *rand.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func ReadDockerignorePatterns(dir string) ([]string, error) {
	dockerignorePath := filepath.Join(dir, ".dockerignore")
	_, err := os.Stat(dockerignorePath)
	if os.IsNotExist(err) {
		return []string{}, nil // No .dockerignore file, return empty patterns
	}
	if err != nil {
		return nil, fmt.Errorf("failed to stat .dockerignore file %q: %w", dockerignorePath, err)
	}

	content, err := os.ReadFile(dockerignorePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read .dockerignore file %q: %w", dockerignorePath, err)
	}

	var patterns []string
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue // Skip empty lines and comments
		}
		patterns = append(patterns, line)
	}
	logrus.Infof("Found %d patterns in .dockerignore at %q", len(patterns), dockerignorePath)
	return patterns, nil
}

// shouldIgnore checks if a file or directory should be ignored based on glob patterns.
func shouldIgnore(path string, info fs.FileInfo, relPath string, ignorePatterns []string) (bool, error) {
	logrus.Debugf("shouldIgnore: Checking %q (IsDir: %t)", relPath, info.IsDir())
	if relPath == "." { // Never ignore the root directory itself
		return false, nil
	}

	// Split relPath into segments
	segments := strings.Split(relPath, string(os.PathSeparator))

	for _, pattern := range ignorePatterns {
		logrus.Debugf("shouldIgnore: Matching %q against pattern %q", relPath, pattern)
		// 1. Check if the full relPath matches the pattern (for files like "*.log")
		matched, err := filepath.Match(pattern, relPath)
		if err != nil {
			logrus.Warnf("Invalid ignore pattern %q: %v", pattern, err)
			continue
		}
		if matched {
			logrus.Debugf("Ignoring %q due to full path pattern match %q", relPath, pattern)
			if info.IsDir() {
				return true, filepath.SkipDir // Skip directory and its contents
			}
			return true, nil // Skip this file
		}

		// 2. Check if any segment of the relPath matches the pattern (for directory names like ".terraform")
		for _, segment := range segments {
			if segment == pattern {
				logrus.Debugf("Ignoring %q due to segment match %q", relPath, pattern)
				if info.IsDir() {
					return true, filepath.SkipDir
				}
				return true, nil
			}
		}

		// 3. Check for directory prefixes (e.g., if pattern is "foo" and relPath is "foo/bar")
		if strings.HasPrefix(relPath, pattern+string(os.PathSeparator)) {
			logrus.Debugf("Ignoring %q due to directory prefix match %q", relPath, pattern)
			if info.IsDir() {
				return true, filepath.SkipDir
			}
			return true, nil
		}
	}
	return false, nil
}

// processTarEntry processes a single file or directory for tarball creation.
func processTarEntry(tarWriter *tar.Writer, sourceDir string, ignorePatterns []string, path string, info fs.FileInfo, errFromWalk error) error {
	if errFromWalk != nil {
		return errFromWalk
	}

	relPath, err := filepath.Rel(sourceDir, path)
	if err != nil {
		return fmt.Errorf("failed to get relative path for %q: %w", path, err)
	}

	logrus.Debugf("createFilteredTar: Processing path %q (IsDir: %t, IsRegular: %t)", path, info.IsDir(), info.Mode().IsRegular())

	ignored, skipDirResult := shouldIgnore(path, info, relPath, ignorePatterns)
	if ignored {
		return skipDirResult
	}

	header, err := tar.FileInfoHeader(info, relPath)
	if err != nil {
		return fmt.Errorf("failed to create tar header for %q: %w", path, err)
	}
	header.Name = relPath

	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header for %q: %w", path, err)
	}

	if info.Mode().IsRegular() {
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open file %q: %w", path, err)
		}
		defer file.Close()

		if _, err := io.Copy(tarWriter, file); err != nil {
			return fmt.Errorf("failed to write file content for %q: %w", path, err)
		}
	}

	return nil
}

func createFilteredTar(sourceDir string, ignorePatterns []string) (string, error) { // Changed return type
	tmpFile, err := os.CreateTemp("", "gcluster-build-context-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file for tarball: %w", err)
	}
	defer tmpFile.Close() // Close tmpFile immediately after writing

	gzipWriter := gzip.NewWriter(tmpFile)
	tarWriter := tar.NewWriter(gzipWriter)

	logrus.Infof("Creating filtered tar from %s to temporary file %s", sourceDir, tmpFile.Name())

	var walkErr error
	defer func() {
		// Ensure tar and gzip writers are closed to flush any buffered data
		if closeErr := tarWriter.Close(); closeErr != nil && walkErr == nil {
			walkErr = fmt.Errorf("failed to close tar writer: %w", closeErr)
		}
		if closeErr := gzipWriter.Close(); closeErr != nil && walkErr == nil {
			walkErr = fmt.Errorf("failed to close gzip writer: %w", closeErr)
		}
	}()

	walkErr = filepath.Walk(sourceDir, func(path string, info fs.FileInfo, err error) error {
		return processTarEntry(tarWriter, sourceDir, ignorePatterns, path, info, err)
	})

	if walkErr != nil {
		os.Remove(tmpFile.Name()) // Clean up temp file on error
		return "", walkErr        // Return empty string and error
	}

	return tmpFile.Name(), nil // Return the path to the temporary file
}
