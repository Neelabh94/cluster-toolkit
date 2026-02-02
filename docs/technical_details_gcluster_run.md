# Technical Details: `gcluster run` Command

This document provides a detailed technical overview of the `gcluster run` command implemented within the `cluster-toolkit` repository. This command facilitates abstracted job submissions to a GKE cluster, leveraging Google Cloud Build for container image management.

## 1. New Files and Their Relationships

The `gcluster run` functionality is distributed across several new files and modifications to existing ones, structured to maintain modularity and adherence to Go project conventions.

### `cluster-toolkit/cmd/run.go`

*   **Purpose:** Defines the top-level `gcluster run` Cobra command. It handles parsing command-line arguments and flags, and acts as the entry point for the `gcluster run` workflow.
*   **Contents:**
    * `runCmd`: A `*cobra.Command` instance defining the command's `Use`, `Short`, `Long` descriptions, and its `Run` function (`runRunCmd`).
    * `init()` function: Registers `runCmd` with the `rootCmd` (from `cluster-toolkit/cmd/root.go`) and declares command-specific flags using `StringVarP` and `StringVar`. It also marks required flags using `MarkFlagRequired`.
    * `runRunCmd(cmd *cobra.Command, args []string)`: The core execution function for the `run` command. It collects flag values into a `run.RunOptions` struct and then delegates the entire workflow orchestration to `run.ExecuteRun()`.*   **Relationship:** Imports `hpc-toolkit/pkg/run` (the orchestration package) and `github.com/spf13/cobra`. It's the CLI interface to the `gcluster run` logic.

### `cluster-toolkit/pkg/run/run.go`

*   **Purpose:** Orchestrates the end-to-end `gcluster run` workflow. It ties together the Cloud Build and GKE manifest generation/application steps.
*   **Contents:**
    * `RunOptions` struct: Holds all parameters gathered from the CLI for the `run` command, including a `UseCraneBuilder` boolean flag to choose the image building method.
    * `ExecuteRun(opts RunOptions) error`: The main orchestration function.
        * Retrieves the GCP `ProjectID`. If provided via the `--project` flag, it uses that value. Otherwise, it attempts to infer it from the `gcloud` configuration.
        * Conditionally calls either Cloud Build (using `cloudbuild.GenerateCloudBuildYaml` and `cloudbuild.SubmitCloudBuild`) or the Python-based `crane` builder (via `executeCraneBuild`) to build and push the Docker image.
        * Constructs the full image name from the chosen builder.
        * Calls `gcloud container clusters get-credentials` to configure `kubectl` for the target cluster.
        * Constructs `gkemanifest.ManifestOptions` and calls `gkemanifest.GenerateGKEManifest` to create the Kubernetes Job YAML.
        * Either saves the manifest to a file (if `--output-manifest` is provided) or calls `gkemanifest.ApplyGKEManifest` to deploy the job to GKE.*   **Relationship:** Imports and coordinates `hpc-toolkit/pkg/run/cloudbuild` and `hpc-toolkit/pkg/run/gkemanifest`. Also uses `hpc-toolkit/pkg/shell` for executing `gcloud` commands.

### `cluster-toolkit/pkg/run/cloudbuild/cloudbuild.go`

*   **Purpose:** Manages the generation and submission of Cloud Build configurations for container image creation.
*   **Contents:**
    * `CloudBuildTemplate`: A Go `text/template` string defining the structure of a `cloudbuild.yaml` file for Docker image building.
    * `BuildOptions` struct: Parameters for the Cloud Build process.
    * `GenerateCloudBuildYaml(opts BuildOptions) (string, error)`: Generates the `cloudbuild.yaml` content by executing `CloudBuildTemplate` with provided options.
    * `SubmitCloudBuild(cloudBuildYamlContent string, buildContextPath string, projectID string) (string, error)`: Writes the generated `cloudBuildYamlContent` to a temporary file and then executes `gcloud builds submit` using `hpc-toolkit/pkg/shell.ExecuteCommand`. It attempts to extract the Cloud Build URL from the output.
    * `GetFullImageName(imageName, projectID, region string) (string, error)`: A utility function to construct the fully qualified Docker image name (e.g., `gcr.io/<PROJECT_ID>/<IMAGE_NAME>:<TAG>`), handling cases where the input `imageName` may or may not include the project ID.*   **Relationship:** Used by `hpc-toolkit/pkg/run/run.go` to handle image building. Relies on `hpc-toolkit/pkg/shell` for command execution.

### `cluster-toolkit/pkg/run/gkemanifest/gkemanifest.go`

*   **Purpose:** Handles the generation and application of Kubernetes Job manifests for deployment to GKE.
*   **Contents:**
    * `KubernetesJobTemplate`: A Go `text/template` string defining the basic structure of a Kubernetes `Job` manifest. It includes placeholders for job name, image, command, resources (CPU, memory, GPU), and conditional `nodeSelector`.
    * `ManifestOptions` struct: Parameters for GKE manifest generation.
    * `GenerateGKENodeSelectorLabel(acceleratorType string) string`: A helper function to map accelerator types (e.g., `nvidia-tesla-a100`) to appropriate GKE node labels.
    * `GenerateGKEManifest(opts ManifestOptions) (string, error)`: Generates the Kubernetes Job YAML content by executing `KubernetesJobTemplate`. It dynamically sets resource limits (CPU, memory, GPU) and the `AcceleratorTypeLabel` based on the `AcceleratorType` in `opts`.
    * `ApplyGKEManifest(manifestContent string, projectID, clusterName, clusterLocation string) error`: Writes the generated manifest to a temporary file and then executes `kubectl apply -f <temp_file>` using `hpc-toolkit/pkg/shell.ExecuteCommand`.*   **Relationship:** Used by `hpc-toolkit/pkg/run/run.go` to handle GKE workload deployment. Relies on `hpc-toolkit/pkg/shell` for command execution.

### `cluster-toolkit/pkg/shell/common.go` (Additions/Modifications)

*   **Purpose:** Provides generic shell command execution and utility functions.
*   **Contents (Additions):**
    * `Result` struct: Stores `Stdout`, `Stderr`, and `ExitCode` from a shell command execution.
    * `ExecuteCommand(name string, args ...string) Result`: Executes a command (`name`) with its arguments (`args...`) and captures its output and exit code. This new signature provides more robust argument passing by bypassing shell interpretation.
    * `RandomString(length int) string`: Generates a random string of specified length, used for unique job names.*   **Relationship:** These additions serve as a foundational utility layer used by `hpc-toolkit/pkg/run/cloudbuild`, `hpc-toolkit/pkg/run/gkemanifest`, and `hpc-toolkit/pkg/run/run.go` for all shell interactions.

## 2. CLI Options for Workflow Submissions

The `gcluster run` command accepts the following flags:

*   **`--image-name`** (short: `-i`)

    *   **Type:** String

    *   **Required:** Yes

    *   **Description:** Name of the Docker image to build and run (e.g., `my-project/my-image:tag`). This will be used to tag and push the image to a container registry.

*   **`--dockerfile`** (short: `-f`)

    *   **Type:** String

    *   **Default:** `Dockerfile`

    *   **Description:** Path to the Dockerfile to use for Cloud Build. Relative to `--build-context`.

*   **`--build-context`** (short: `-c`)

    *   **Type:** String

    *   **Default:** `.`

    *   **Description:** Path to the directory containing the Dockerfile and application code, used as the Cloud Build context.

*   **`--command`** (short: `-e`)

    *   **Type:** String

    *   **Required:** Yes

    *   **Description:** The command to execute within the container (e.g., `"python train.py --epochs 10"`).

*   **`--accelerator-type`** (short: `-a`)

    *   **Type:** String

    *   **Required:** No (Optional)

    *   **Description:** Type of accelerator to request for the GKE workload (e.g., `nvidia-tesla-a100`, `tpu-v4-podslice`). If omitted, a CPU-only workload with minimal resources is assumed.

*   **`--output-manifest`** (short: `-o`)

    *   **Type:** String

    *   **Required:** No

    *   **Description:** If provided, the generated Kubernetes manifest will be saved to this file instead of being applied directly to the GKE cluster.

*   **`--cluster-name`**

    *   **Type:** String

    *   **Required:** Yes

    *   **Description:** Name of the GKE cluster to deploy the workload to.

*   **`--cluster-location`**

    *   **Type:** String

    *   **Required:** Yes

    *   **Description:** Location (zone or region) of the GKE cluster.

*   **`--project`** (short: `-p`)

    *   **Type:** String

    *   **Required:** No

    *   **Description:** Google Cloud Project ID. If not provided, it will be inferred from your `gcloud` configuration.

*   **`--use-crane-builder`**

    *   **Type:** Boolean

    *   **Default:** `false`

    *   **Description:** If set to `true`, the `crane`-based Python image builder will be used instead of Google Cloud Build.

## 3. Workflow After Command Execution

When `gcluster run` is invoked, the following high-level steps occur:

1. **Parameter Collection & Validation:** CLI flags are parsed and collected into a `RunOptions` struct.
2. **GCP Project ID Resolution:** The GCP Project ID is resolved. If provided via the `--project` flag, that value is used. Otherwise, the command attempts to infer the Project ID from the active `gcloud` configuration.
3. **Cloud Build / Crane Image Building:** Image building is performed based on the `--use-crane-builder` flag. If `true`, the `crane`-based approach (implemented in the Python SLURM orchestrator) is used for direct registry pushing. Otherwise, a `cloudbuild.yaml` file is dynamically generated and submitted to Google Cloud Build. This flexibility allows users to choose their preferred image building method.
4. **Kubernetes Context Configuration:** The `kubectl` command-line tool is configured to interact with the specified GKE cluster using `gcloud container clusters get-credentials`.
5. **GKE Manifest Generation:**
    * A Kubernetes `Job` manifest is dynamically generated based on the collected parameters (`--image-name`, `--command`, `--accelerator-type`, etc.).
    * Resource requests (CPU, memory, GPU) are set based on the `accelerator-type`. If no accelerator is specified, a CPU-only workload with conservative resource requests is generated.
    * Node selectors for accelerators are conditionally included in the manifest.
6. **Workload Deployment:**
    * If `--output-manifest` is specified, the generated Kubernetes Job YAML is saved to the provided file path.
    * Otherwise, the manifest is applied directly to the GKE cluster using `kubectl apply`.
7. **Completion:** The `gcluster run` command exits, indicating the initiation of the job. Monitoring of the Cloud Build and Kubernetes Job status must be done separately using `gcloud builds list` and `kubectl get jobs/pods`.

## 4. Limitations of the Proof-of-Concept (POC)

The current `gcluster run` implementation is a proof-of-concept and has the following limitations:

* **No Cloud Build Monitoring:** The command currently does not wait for the Cloud Build process to complete. It assumes success and proceeds with GKE manifest generation. A robust implementation would poll Cloud Build status.
* **Simplified GKE Manifest:** Only a basic Kubernetes `Job` manifest is generated. Advanced features like `JobSet` (used by XPK), `PodSet`, custom scheduling (e.g., Kueue), advanced storage configurations beyond `emptyDir`, or specific GPU/TPU topology handling are not yet implemented.
* **Limited Accelerator Type Mapping:** The mapping from `--accelerator-type` to GKE node selectors and resource requests is simplified. A production-ready solution would require a more comprehensive and configurable mapping.
* **Hardcoded Region for Cloud Build:** The Cloud Build region is currently hardcoded to `us-central1`.
* **Basic Error Handling:** Error handling is present but could be more granular and user-friendly.
* **kubectl Context Assumption:** Relies on `gcloud` and `kubectl` being installed and authenticated in the user's environment.
* **No Automatic Cluster Provisioning:** Assumes a GKE cluster is already deployed and accessible.
* **GKE Node Auto-Provisioning (NAP) Integration:** The `gcluster run` command currently does not provide direct support for configuring or interacting with GKE's Node Auto-Provisioning (NAP). Future enhancements could allow `gcluster run` to leverage NAP to dynamically scale GKE node pools based on workload resource requests. This would involve adding new CLI flags to enable NAP, define resource bounds, and specify allowed machine types for auto-provisioned nodes.

## 5. Comparison with XPK's `xpk create workload`

| Feature                      | `gcluster run` (POC)                                 | `xpk create workload` (Reference)                       |
| :--------------------------- | :--------------------------------------------------- | :------------------------------------------------------ |
| **Language**                 | Go                                                   | Python                                                  |
| **CLI Framework**            | Cobra                                                | Custom (Python-based)                                   |
| **Image Building**           | Cloud Build (via `gcloud builds submit`) or `crane`-based direct registry push (configurable via `--use-crane-builder` flag).             | Local Docker build (`docker buildx build`) + `docker push` |
| **GKE Manifest Type**        | Basic Kubernetes `Job`                               | Kubernetes `JobSet` (advanced scheduling capabilities)  |
| **Resource/Accelerator Mapping** | Simplified (conditional `nodeSelector`, basic limits) | Comprehensive (TPU topology, GPU schedulers, placement policies) |
| **Storage Integration**      | Basic `emptyDir` placeholder                         | Advanced (GCS FUSE, Filestore, Parallelstore, PD, Lustre) |
| **Scheduling Features**      | Basic Kubernetes `Job` scheduling                    | Advanced `JobSet` (Kueue integration, sub/super-slicing) |
| **Monitoring Integration**   | Initiates job, user monitors via `kubectl`           | Provides links to GCP monitoring dashboards (Outlier, Debugging) |
| **External Dependencies**    | `gcloud CLI`, `kubectl CLI`                          | `gcloud CLI`, `kubectl CLI`, `docker CLI`               |
| **Primary Goal**             | POC for Cloud Build-based job submission             | Production-ready advanced AI/HPC workload management    |

## 6. Example Workflow: Submitting a Job to `cluster-01`

This section details the actual steps performed to execute and verify the `gcluster run` command on the `cluster-01` GKE cluster.

1. **Create Deployment Directory:**

```bash

./gcluster create examples/hpc-gke.yaml --vars="project_id=hpc-toolkit-gsc,deployment_name=cluster-01,region=us-central1,gcp_public_cidrs_access_enabled=false,authorized_cidr=0.0.0.0/0"

# Output: Creating deployment folder "cluster-01" ...

```

2. **Deploy GKE Cluster:**

```bash

./gcluster deploy cluster-01

# Output includes Terraform plan and apply output, eventually showing:

# ... GKE cluster created successfully.

# (User provides 'a' to apply changes interactively during deployment)

```

3. **Build `gcluster` Binary (after initial `run` command implementation):**

```bash

make

# Output: **************** building gcluster ************************

# (No errors after multiple fixes)

```

4. **First `gcluster run` attempt (with `--accelerator-type` hardcoded to `nvidia-tesla-a100`):**

```bash

./gcluster run \

  --image-name=hpc-toolkit-gsc/my-test-app:latest \

  --dockerfile=./Dockerfile \

  --build-context=.

  --command="python app.py" \

  --accelerator-type=nvidia-tesla-a100 \

  --cluster-name=cluster-01 \

  --cluster-location=us-central1

# Output showed: `gcloud.container.clusters.get-credentials` failed with 404 (hardcoded name)

```

5. **Identify Actual Cluster Name & Location:**

```bash

gcloud container clusters list --project hpc-toolkit-gsc --zone us-central1 --format="value(name)"

# Output: cluster-01

# Location was confirmed as us-central1 from blueprint vars.

```

6. **Modify `gcluster run` flags to accept dynamic cluster name/location:**

* Updated `cluster-toolkit/cmd/run.go` to add `--cluster-name` and `--cluster-location` flags, and removed hardcoded values.

* Rebuilt `gcluster` via `make`.

7. **Second `gcluster run` attempt (with `nvidia-tesla-a100` and correct cluster name/location):**

```bash

./gcluster run \

  --image-name=hpc-toolkit-gsc/my-test-app:latest \

  --dockerfile=./Dockerfile \

  --build-context=.

  --command="python app.py" \

  --accelerator-type=nvidia-tesla-a100 \

  --cluster-name=cluster-01 \

  --cluster-location=us-central1

# Output showed `kubectl apply` failed due to `connect: connection refused` (kubectl context not configured).

```

8. **Add `gcloud container clusters get-credentials` call:**

* Updated `cluster-toolkit/pkg/run/run.go` to include `gcloud container clusters get-credentials` before `kubectl apply`.

* Rebuilt `gcluster` via `make`.

9. **Third `gcluster run` attempt (with `nvidia-tesla-a100` and `kubectl` context):**

```bash

./gcluster run \

  --image-name=hpc-toolkit-gsc/my-test-app:latest \

  --dockerfile=./Dockerfile \

  --build-context=.

  --command="python app.py" \

  --accelerator-type=nvidia-tesla-a100 \

  --cluster-name=cluster-01 \

  --cluster-location=us-central1

# Output showed `kubectl get pods` for the job was `Pending` with "didn't match Pod's node affinity/selector" (no GPU nodes).

```

10. **Inspect Nodes and Adjust `gcluster run` for CPU-only:**

```bash

kubectl get nodes --show-labels

# Output confirmed e2-medium (CPU-only) nodes.

```

* Updated `cluster-toolkit/pkg/run/gkemanifest/gkemanifest.go` to conditionally include node selector for accelerators and set default low CPU/memory limits for CPU-only jobs.

* Updated `cluster-toolkit/cmd/run.go` to make `--accelerator-type` optional.

* Rebuilt `gcluster` via `make`.

11. **Fourth `gcluster run` attempt (without `--accelerator-type`):**

```bash

./gcluster run \

  --image-name=hpc-toolkit-gsc/my-test-app:latest \

  --dockerfile=./Dockerfile \

  --build-context=.

  --command="python app.py" \

  --cluster-name=cluster-01 \

  --cluster-location=us-central1

# Output: `gcluster run workflow completed.`

```

12. **Verify Job Execution:**

```bash

kubectl get jobs --namespace default

# Output: gcluster-workload-5f1619aa   Completed   1/1

kubectl get pods --namespace default --selector=gcluster.google.com/workload=gcluster-workload-5f1619aa

# Output: gcluster-workload-5f1619aa-XXXXX   0/1     Completed

kubectl logs gcluster-workload-5f1619aa-XXXXX --namespace default

# Output:

# Hello from the gcluster run application!

# This is a placeholder application.

```




This concludes the detailed technical document for `gcluster run`.
