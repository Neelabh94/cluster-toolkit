# Replication Guide: `gcluster run` Proof-of-Concept

This guide provides a step-by-step process to replicate the `gcluster run` Proof-of-Concept (POC), which demonstrates building a Docker image using Google Cloud Build and deploying it as a Kubernetes Job to a GKE cluster using the `cluster-toolkit`.

## 1. Prerequisites

Before you begin, ensure you have the following installed and configured:

*   **Go (1.20 or later):** Required for building the `gcluster` binary.
*   **Google Cloud SDK:** Includes `gcloud` and `kubectl`.
    *   Ensure `gcloud` is authenticated and configured with a default project (`gcloud auth login` and `gcloud config set project <YOUR_GCP_PROJECT_ID>`).
    *   Ensure `kubectl` is installed (`gcloud components install kubectl`).
*   **A GCP Project:** You will need a GCP project with billing enabled and necessary APIs enabled (e.g., Kubernetes Engine API, Cloud Build API, Artifact Registry API).
*   **`make`:** (Usually pre-installed on Linux/macOS, or install via package manager).

## 2. Clone the Repository

Clone the `cluster-toolkit` repository to your local machine:

```bash
git clone https://github.com/GoogleCloudPlatform/hpc-toolkit cluster-toolkit
cd cluster-toolkit
```

## 3. Apply Code Changes

This POC involves creating several new files and modifying a few existing ones. For each file, click the provided link to view its content and ensure you create/modify it exactly as shown.

### 3.1. Create New Files

#### `cluster-toolkit/cmd/run.go`
This file defines the `gcluster run` command and its CLI flags.

*   **Location:** `cluster-toolkit/cmd/run.go`
*   **Content:** [Link to cluster-toolkit/cmd/run.go content on Google Docs]

#### `cluster-toolkit/pkg/run/run.go`
This file orchestrates the entire `gcluster run` workflow.

*   **Location:** `cluster-toolkit/pkg/run/run.go`
*   **Content:** [Link to cluster-toolkit/pkg/run/run.go content on Google Docs]

#### `cluster-toolkit/pkg/run/cloudbuild/cloudbuild.go`
This file handles Cloud Build YAML generation and submission.

*   **Location:** `cluster-toolkit/pkg/run/cloudbuild/cloudbuild.go`
*   **Content:** [Link to cluster-toolkit/pkg/run/cloudbuild/cloudbuild.go content on Google Docs]

#### `cluster-toolkit/pkg/run/gkemanifest/gkemanifest.go`
This file manages Kubernetes Job manifest generation and application.

*   **Location:** `cluster-toolkit/pkg/run/gkemanifest/gkemanifest.go`
*   **Content:** [Link to cluster-toolkit/pkg/run/gkemanifest/gkemanifest.go content on Google Docs]

#### `cluster-toolkit/docs/design/gcluster_run_design_doc.md`
The design document for `gcluster run` (already created).

*   **Location:** `cluster-toolkit/docs/design/gcluster_run_design_doc.md`
*   **Content:** [Link to cluster-toolkit/docs/design/gcluster_run_design_doc.md content on Google Docs]

#### `cluster-toolkit/docs/technical_details_gcluster_run.md`
The detailed technical document for `gcluster run` (already created).

*   **Location:** `cluster-toolkit/docs/technical_details_gcluster_run.md`
*   **Content:** [Link to cluster-toolkit/docs/technical_details_gcluster_run.md content on Google Docs]

### 3.2. Modify Existing Files

#### `cluster-toolkit/pkg/shell/common.go`
Modified to include generic shell command execution and random string generation.

*   **Location:** `cluster-toolkit/pkg/shell/common.go`
*   **Content:** [Link to cluster-toolkit/pkg/shell/common.go content on Google Docs]

#### `cluster-toolkit/go.mod`
(No direct modification by the agent, but ensures the module path is `hpc-toolkit`)
Confirm that your `go.mod` file contains `module hpc-toolkit` at the top.

*   **Location:** `cluster-toolkit/go.mod`
*   **Content:** [Link to cluster-toolkit/go.mod content on Google Docs]

## 4. Build the `gcluster` Binary

Navigate to the `cluster-toolkit` directory (if not already there) and build the `gcluster` binary:

```bash
cd cluster-toolkit
make
```

This command compiles the Go source code, including your new `gcluster run` command, and creates an executable named `gcluster` in the current directory.

## 5. Prepare Application Code

Create the following placeholder files in your `cluster-toolkit` directory. These will be used to build your Docker image.

#### `cluster-toolkit/Dockerfile`

*   **Location:** `cluster-toolkit/Dockerfile`
*   **Content:** [Link to cluster-toolkit/Dockerfile content on Google Docs]

#### `cluster-toolkit/requirements.txt`

*   **Location:** `cluster-toolkit/requirements.txt`
*   **Content:** [Link to cluster-toolkit/requirements.txt content on Google Docs]

#### `cluster-toolkit/app.py`

*   **Location:** `cluster-toolkit/app.py`
*   **Content:** [Link to cluster-toolkit/app.py content on Google Docs]

## 6. Deploy a GKE Cluster

For this POC, we'll deploy a basic GKE cluster using an example blueprint. This will create a CPU-only cluster, which we will target with our job.

*   **Ensure `gcloud` is configured with your project ID and a region/zone where GKE is available.**
    ```bash
    gcloud config set project <YOUR_GCP_PROJECT_ID>
    gcloud config set compute/region us-central1 # Or your preferred region
    ```

*   **Create the deployment directory:**

    ```bash
    ./gcluster create examples/hpc-gke.yaml --vars="project_id=<YOUR_GCP_PROJECT_ID>,deployment_name=cluster-01,region=us-central1,gcp_public_cidrs_access_enabled=false,authorized_cidr=0.0.0.0/0"
    ```

    *Replace `<YOUR_GCP_PROJECT_ID>` with your actual GCP Project ID.*

*   **Deploy the GKE cluster:**

    ```bash
    ./gcluster deploy cluster-01
    ```
    *This command will show a Terraform plan. You will be prompted to confirm the changes (type `a` and press Enter).*

    *This deployment process can take a significant amount of time (e.g., 10-20 minutes or more) as it provisions cloud resources.* Wait for the command to complete successfully.

## 7. Submit the Job with `gcluster run`

Now that the cluster is deployed, you can submit your sample job using the `gcluster run` command. Note that we are *not* specifying an `--accelerator-type` here to ensure it schedules on the default CPU nodes.

```bash
./gcluster run \
  --image-name=<YOUR_GCP_PROJECT_ID>/my-test-app:latest \
  --dockerfile=./Dockerfile \
  --build-context=.
  --command="python app.py" \
  --cluster-name=cluster-01 \
  --cluster-location=us-central1
```

*Replace `<YOUR_GCP_PROJECT_ID>` with your actual GCP Project ID.*

## 8. Verify the Job

Verify that the image was built by Cloud Build and that the Kubernetes Job ran successfully on your GKE cluster.

*   **Check Cloud Build Status:**

    ```bash
    gcloud builds list --project <YOUR_GCP_PROJECT_ID>
    ```
    Look for a build with `STATUS: SUCCESS` and `IMAGES` pointing to `gcr.io/<YOUR_GCP_PROJECT_ID>/my-test-app`.

*   **Check Kubernetes Job Status:**

    ```bash
    kubectl get jobs --namespace default
    ```
    Look for a job named similar to `gcluster-workload-XXXXXXXX` with `COMPLETIONS: 1/1` and `STATUS: Completed`.

*   **Get Pod Logs:**
    First, get the exact pod name for your completed job:
    ```bash
    kubectl get pods --namespace default --selector=gcluster.google.com/workload=<JOB_NAME>
    ```
    (Replace `<JOB_NAME>` with the name of your completed job from `kubectl get jobs` output, e.g., `gcluster-workload-5f1619aa`).

    Then, get the logs from the pod:
    ```bash
    kubectl logs <POD_NAME> --namespace default
    ```
    You should see the output:
    ```
    Hello from the gcluster run application!
    This is a placeholder application.
    ```

## 9. Cleanup (Optional)

To avoid incurring unnecessary costs, you can destroy the deployed GKE cluster:

```bash
./gcluster destroy cluster-01
```
*You will be prompted to confirm the destruction (type `a` and press Enter).*
