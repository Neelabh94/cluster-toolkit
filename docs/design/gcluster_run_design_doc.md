# Differences between cluster-toolkit and xpk and bridging actions for `gcluster run`

This document outlines the key differences observed between the `cluster-toolkit` and `xpk` repositories, specifically in the context of creating a `gcluster run` command in `cluster-toolkit` that mirrors the functionality of `xpk create workload` but leverages Cloud Build for image generation.

## Observed Differences

### 1. Core Language and Framework
*   **cluster-toolkit:** Primarily Go, using `spf13/cobra` for CLI command parsing and structuring.
*   **xpk:** Primarily Python, using its own custom argument parsing and execution flow.

### 2. Workload Definition and Manifest Generation
*   **cluster-toolkit:** Existing commands (`deploy`, `create`) focus on managing infrastructure via Terraform and Packer, often orchestrating these tools. It does not have a direct concept of a "workload" in the Kubernetes sense.
*   **xpk:** Has a well-defined concept of "workload" and generates Kubernetes `JobSet` (or `PathwaysJob`) YAML manifests dynamically based on various inputs (accelerator type, storage, etc.). It uses Python string formatting and Jinja2 for templating.

### 3. Image Building and Management
*   **cluster-toolkit:** No built-in image building capabilities. Relies on external tools like Packer for VM image creation, not Docker container images.
*   **xpk:** Performs local Docker image builds using `docker buildx build` and pushes images to a specified registry using `docker push`. This relies on a local Docker daemon.

### 4. Kubernetes Interaction
*   **cluster-toolkit:** Orchestrates external tools (Terraform, Packer) that interact with cloud resources. Direct Kubernetes API interaction is not a primary pattern for existing commands.
*   **xpk:** Directly interacts with Kubernetes clusters, primarily by generating YAML manifests and applying them using `kubectl apply`.

## Bridging Actions for `gcluster run`

To implement `gcluster run` in `cluster-toolkit` with Cloud Build integration, the following actions are necessary to bridge the identified gaps:

### 1. CLI Command Structure (Go / Cobra)
*   **Action:** Create a new `run.go` file in `cluster-toolkit/cmd/`. This file will define the `gcluster run` Cobra command, its short/long descriptions, and register it with the `rootCmd`. It will also define and parse the necessary CLI flags (`--image`, `--command`, `--accelerator-type`, `--dockerfile`, `--build-context`, etc.).

### 2. Cloud Build Integration (Go)
*   **Gap:** `xpk` uses local Docker. `gcluster run` needs to use Cloud Build to avoid local Docker dependency.
*   **Action:**
    *   Create a new Go package (e.g., `cluster-toolkit/pkg/run/cloudbuild/`).
    *   Develop Go functions to generate a `cloudbuild.yaml` file dynamically. This `cloudbuild.yaml` will specify steps to build a Docker image from a given Dockerfile and context, and push it to a Google Artifact Registry or Container Registry.
    *   Implement a Go function to execute `gcloud builds submit` using `hpc-toolkit/pkg/shell` to submit the generated `cloudbuild.yaml` to GCP.
    *   This component will also be responsible for constructing the final image registry path (`gcr.io/project/image:tag`).

### 3. GKE Manifest Generation (Go / `text/template`)
*   **Gap:** `xpk` has rich Python-based YAML templating logic for `JobSet`s. This needs to be replicated in Go.
*   **Action:**
    *   Create a new Go package (e.g., `cluster-toolkit/pkg/run/gkemanifest/`).
    *   Develop Go functions to generate Kubernetes `JobSet` (or `Job`) YAML manifests. This will involve using Go's `text/template` package to create templates similar in structure to `xpk`'s `WORKLOAD_CREATE_YAML`, customized with placeholders for the image name, command, accelerator type, etc.
    *   The logic for adding node selectors, tolerations, and resource requests based on accelerator type will need to be translated from `xpk`'s Python to Go.

### 4. Kubernetes Workload Application (Go / `kubectl apply`)
*   **Gap:** `xpk` uses `kubectl apply`. `cluster-toolkit` needs to do the same.
*   **Action:**
    *   Within the `cluster-toolkit/pkg/run/gkemanifest/` package, implement a Go function to take the generated GKE manifest YAML, write it to a temporary file, and then execute `kubectl apply -f <temp_file>` using `hpc-toolkit/pkg/shell`.
    *   Consider adding an optional flag to simply output the manifest to stdout or a file instead of applying it, for debugging/inspection.

### 5. Orchestration (Go)
*   **Action:** A central `Run` function in a package like `cluster-toolkit/pkg/run/run/` will orchestrate the entire process:
    1.  Parse CLI arguments.
    2.  Call the `cloudbuild` component to build and push the Docker image.
    3.  Receive the full image path from the `cloudbuild` component.
    4.  Call the `gkemanifest` component to generate the Kubernetes manifest using the image path and other CLI inputs.
    5.  Call the `gkemanifest` component to apply the generated manifest to the GKE cluster.

This structured approach allows for a modular implementation that adheres to `cluster-toolkit`'s Go conventions while leveraging the insights from `xpk`'s workload management.

## Deviations and Learnings During Implementation

During the implementation of the `gcluster run` command, several challenges and deviations from the initial plan were encountered. These provided valuable insights and led to adjustments in the implementation strategy:

### 1. `shell` Package Utilities

*   **Initial Assumption:** It was assumed that a generic `ExecuteCommand` and `RandomString` function would be readily available within `hpc-toolkit/pkg/shell` (or `cluster-toolkit/pkg/shell`).
*   **Deviation:** Neither `hpc-toolkit/pkg/shell/common.go`, `packer.go`, nor `terraform.go` contained a generic command execution utility returning stdout/stderr/exit code.
*   **Resolution:** A `Result` struct, `ExecuteCommand` function, and `RandomString` function were implemented directly within `cluster-toolkit/pkg/shell/common.go` to fulfill the POC's requirements. This made the `cluster-toolkit` more self-contained for basic shell operations.

### 2. Go Module Import Paths

*   **Challenge:** Initial import statements within the newly created `pkg/run` sub-packages incorrectly referenced `cluster-toolkit/pkg/shell`.
*   **Deviation:** The `go.mod` file for `cluster-toolkit` defined its module path as `hpc-toolkit`. Consequently, all internal imports to `cluster-toolkit/pkg/shell` needed to be corrected to `hpc-toolkit/pkg/shell`.
*   **Resolution:** Import statements in `cluster-toolkit/pkg/run/cloudbuild/cloudbuild.go`, `cluster-toolkit/pkg/run/gkemanifest/gkemanifest.go`, and `cluster-toolkit/pkg/run/run.go` were updated to reflect the `hpc-toolkit` module path.

### 3. Compilation Errors and Debugging

A series of compilation errors highlighted the complexities of Go development and strict compiler checks:

*   **`undefined: logging.Debug`**:
    *   **Deviation:** The `hpc-toolkit/pkg/logging` package does not expose a `Debug` level function.
    *   **Resolution:** `logging.Debug` calls were replaced with `logging.Info`.
*   **Unused Imports (`"path/filepath"`, `"fmt"`)**:
    *   **Deviation:** Compiler flagged unused `path/filepath` and `fmt` imports.
    *   **Resolution:** Redundant import statements were removed.
*   **Missing Import (`"strings"`)**:
    *   **Deviation:** `strings.TrimSpace` was used without importing the `strings` package.
    *   **Resolution:** `import "strings"` was added to the relevant files.
*   **Syntax Errors in `GenerateGKEManifest`**:
    *   **Deviation:** Repeated syntax errors ("unexpected name tmpl", "non-declaration statement outside function body") arose due to incorrect brace matching and structure within the `switch` statement and template parsing logic.
    *   **Resolution:** The `GenerateGKEManifest` function's body was carefully re-structured to ensure all braces were correctly matched and all statements were within their proper function and code block scopes.

### 4. Project ID String Trimming

*   **Challenge:** The `gcloud builds submit` command failed with an `INVALID_ARGUMENT: invalid image name` error, showing an unexpected space: `gcr.io/hpc-toolkit-gsc /hpc-toolkit-gsc/my-test-app:latest`.
*   **Deviation:** The output of `gcloud config get-value project` (used to get `ProjectID`) contained a trailing newline character.
*   **Resolution:** `strings.TrimSpace(res.Stdout)` was applied to `ProjectID` in `cluster-toolkit/pkg/run/run.go` and defensively to the `imageName` and `projectID` parameters at the start of `GetFullImageName` in `cluster-toolkit/pkg/run/cloudbuild/cloudbuild.go`.

### 5. Intelligent Image Name Construction

*   **Challenge:** The `GetFullImageName` function redundantly prefixed the `imageName` with the `ProjectID` even when the `imageName` parameter already included the project ID (e.g., `hpc-toolkit-gsc/my-test-app:latest` became `gcr.io/hpc-toolkit-gsc/hpc-toolkit-gsc/my-test-app:latest`).
*   **Deviation:** The logic in `GetFullImageName` needed to be more discerning.
*   **Resolution:** The `GetFullImageName` function in `cluster-toolkit/pkg/run/cloudbuild/cloudbuild.go` was refined to check if the `imageName` already contained the `projectID` as a prefix and only prepend `gcr.io/` appropriately, preventing redundant project ID inclusion.

### 6. Kubernetes Context Configuration

*   **Challenge:** `kubectl apply` failed with a "connection refused" error to `http://localhost:8080` because `kubectl` was not configured to interact with the newly deployed GKE cluster.
*   **Deviation:** The `ExecuteRun` function in `cluster-toolkit/pkg/run/run.go` initially assumed `kubectl` was pre-configured.
*   **Resolution:** A call to `gcloud container clusters get-credentials` was added to `cluster-toolkit/pkg/run/run.go` before GKE manifest application, ensuring `kubectl` is properly set up.

### 7. Dynamic Cluster Information

*   **Challenge:** The initial `gcluster run` command used hardcoded `ClusterName` (`gke-a3-mega-cluster`) and `ClusterLocation` (`us-central1`), which did not match the actual deployed cluster name (`cluster-01`).
*   **Deviation:** The `gcluster create` output and `gcloud container clusters list` were used to identify the actual deployed cluster's name and location.
*   **Resolution:** `cluster-toolkit/cmd/run.go` was modified to accept `--cluster-name` and `--cluster-location` flags, which are then passed to `run.RunOptions`, making the command flexible and dynamic.

### 8. Pod Resource Scheduling

*   **Challenge:** The first attempt to run a job failed with "0/3 nodes available: 3 node(s) didn't match Pod's node affinity/selector" because a GPU (`nvidia-tesla-a100`) was requested on a CPU-only cluster. The subsequent job (without GPU request) still failed with "0/3 nodes are available: 2 Insufficient memory, 3 Insufficient cpu".
*   **Deviation:** The default CPU and memory requests for a CPU-only job (`cpu: 1`, `memory: 2Gi`) were too high for the `e2-medium` nodes in the deployed cluster.
*   **Resolution:**
    *   The `--accelerator-type` flag was made optional in `cluster-toolkit/cmd/run.go`.
    *   The `GenerateGKEManifest` function in `cluster-toolkit/pkg/run/gkemanifest/gkemanifest.go` was updated to conditionally include the `nodeSelector` block only if `AcceleratorTypeLabel` is present.
    *   The default CPU and memory limits for CPU-only workloads were reduced to `cpu: 0.5` and `memory: 512Mi` to fit the `e2-medium` nodes.

These deviations demonstrate the iterative nature of software development, especially when integrating new features into an existing codebase and interacting with complex external systems like GCP and Kubernetes. Each challenge presented an opportunity to refine the design and implementation, leading to a more robust and functional POC.
