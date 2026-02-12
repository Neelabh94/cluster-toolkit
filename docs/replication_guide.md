# Replication Guide: `gcluster run` Sample Workload

This guide provides a step-by-step process to deploy a GKE cluster, submit a sample Python script as a workload using `gcluster run` with on-the-fly image building via Crane, and then destroy the cluster.

## 1. Prerequisites

Before you begin, ensure you have the following installed and configured:

* **Go (1.20 or later):** Required for building the `gcluster` binary.
* **Google Cloud SDK:** Includes `gcloud` and `kubectl`.
  * Ensure `gcloud` is authenticated and configured with a default project (`gcloud auth login` and `gcloud config set project <YOUR_GCP_PROJECT_ID>`).
  * Ensure `kubectl` is installed (`gcloud components install kubectl`).
  * **Docker Credential Helper:** Configure Docker to authenticate to Google Container Registry and Artifact Registry. This allows `gcluster` to push/pull images.

    ```bash
    gcloud auth configure-docker gcr.io
    gcloud auth configure-docker us-central1-docker.pkg.dev # Or your Artifact Registry host
    ```

  * **Application Default Credentials (ADC):** Authenticate your local environment for Google Cloud client libraries and tools. This is an interactive step, requiring browser interaction.

    ```bash
    gcloud auth application-default login
    ```

  * **Enable Artifact Registry API:** Ensure the Artifact Registry API is enabled in your project.

    ```bash
    gcloud services enable artifactregistry.googleapis.com --project <YOUR_GCP_PROJECT_ID>
    ```

* **A GCP Project:** You will need a GCP project with billing enabled and necessary APIs enabled (e.g., Kubernetes Engine API, Artifact Registry API, Cloud Resource Manager API).
* **Docker:** Required for building images locally if needed for debugging, but `gcluster run` uses Crane internally.
* **`make`:** (Usually pre-installed on Linux/macOS, or install via package manager).

## 2. Clone the Repository

Clone the `cluster-toolkit` repository to your local machine:

```bash
git clone https://github.com/GoogleCloudPlatform/hpc-toolkit cluster-toolkit
cd cluster-toolkit
```

## 3. Build the `gcluster` Binary

Navigate to the `cluster-toolkit` directory (if not already there) and build the `gcluster` binary:

```bash
go build -o gcluster .
```

This command compiles the Go source code, including the `gcluster run` command, and creates an executable named `gcluster` in the current directory.

## 4. Prepare Sample Application Code

Create a directory named `job_details` and place the following files inside it. This will serve as your build context for the workload.

### `cluster-toolkit/job_details/Dockerfile`

```dockerfile
FROM python:3.9-slim

WORKDIR /app

COPY requirements.txt .

RUN pip install --no-cache-dir -r requirements.txt

COPY app.py .

CMD ["python", "app.py"]
```

### `cluster-toolkit/job_details/requirements.txt`

(This file can be empty for this simple example, or list any Python dependencies.)

```text
# No specific requirements for this example
```

### `cluster-toolkit/job_details/app.py`

```python
# app.py
print("Hello from the gcluster run application!")
print("This is a sample application running on GKE.")
```

## 5. Deploy a GKE Cluster

For this example, we'll deploy a basic GKE cluster using the `hpc-gke.yaml` blueprint.

* **Ensure `gcloud` is configured with your project ID and a region/zone where GKE is available.**

    ```bash
        gcloud config set project <YOUR_GCP_PROJECT_ID>
        gcloud config set compute/region us-central1 # Or your preferred region
        ```

    * **Create the deployment directory:**

    ```bash
    ./gcluster create examples/hpc-gke.yaml --vars="project_id=<YOUR_GCP_PROJECT_ID>,deployment_name=my-test-cluster,region=us-central1,gcp_public_cidrs_access_enabled=false,authorized_cidr=$(curl -s ifconfig.me)/32"
    ```

    *Replace `<YOUR_GCP_PROJECT_ID>` with your actual GCP Project ID.*

* **Deploy the GKE cluster:**

    ```bash
    ./gcluster deploy my-test-cluster
    ```

    *This command will show a Terraform plan. You will be prompted to confirm the changes (type `a` and press Enter).*

    *This deployment process can take a significant amount of time (e.g., 10-20 minutes or more) as it provisions cloud resources.* Wait for the command to complete successfully.

## 6. `gcluster run` Command Reference

The `gcluster run` command deploys a Docker image as a workload (Kubernetes JobSet) on a GKE cluster, integrated with Kueue. It can use pre-built images or build images on-the-fly using Crane.

### Supported Flags

Here are the flags currently supported by `gcluster run`:

* `-i, --docker-image string`: Name of a pre-built Docker image to run (e.g., `my-project/my-image:tag`). Use this if your image is already pushed to a registry.
* `--base-docker-image string`: Name of the base Docker image for Crane to build upon (e.g., `python:3.9-slim`). Required when using `--build-context` for an on-the-fly build.
* `-c, --build-context string`: Path to the build context directory for Crane (e.g., `./job_details`). Required with `--base-docker-image`. Crane will automatically look for a `Dockerfile` within this directory.
* `-e, --command string`: Command to execute in the container (e.g., `'python app.py'`). This overrides the `CMD` instruction in your `Dockerfile`. (Required)
* `-a, --accelerator-type string`: Type of accelerator to request (e.g., `'nvidia-tesla-a100'`, `'tpu-v4-podslice'`). Specify this if your workload requires GPUs or TPUs.
* `-o, --output-manifest string`: Path to output the generated Kubernetes manifest instead of applying it directly to the cluster. Useful for inspection.
* `--cluster-name string`: Name of the GKE cluster to deploy the workload to. (Required)
* `--cluster-location string`: Location (region) of the GKE cluster. (Required)
* `-p, --project string`: Google Cloud Project ID. If not provided, it will be inferred from your `gcloud` configuration.
* `-f, --platform string`: Target platform for the Docker image build (e.g., `'linux/amd64'`, `'linux/arm64'`). Used with `--base-docker-image`. (Default: `linux/amd64`)
* `-w, --workload-name string`: Name of the workload (JobSet) to create. This name will be used for Kubernetes resources. (Required)
* `--kueue-queue string`: Name of the Kueue LocalQueue to submit the workload to. (Default: `default-queue`)
* `--num-slices int`: Number of JobSet replicas (slices). (Default: `1`)
* `--vms-per-slice int`: Number of VMs (pods) per slice. (Default: `1`)
* `--max-restarts int`: Maximum number of restarts for the JobSet before failing. (Default: `1`)
* `--ttl-seconds-after-finished int`: Time (in seconds) to retain the JobSet after it finishes. (Default: `3600` seconds / 1 hour)

## 7. Submit the Sample Workload with `gcluster run`

Now that the cluster is deployed and your application code is prepared, you can submit your sample Python script as a JobSet workload. `gcluster run` will automatically build your Docker image using Crane and push it to Artifact Registry (or Container Registry) in your project.

* **Submit the workload:**

    ```bash
    ./gcluster run \
      --project <YOUR_GCP_PROJECT_ID> \
      --cluster-name my-test-cluster \
      --cluster-location us-central1 \
      --base-docker-image python:3.9-slim \
      --build-context job_details \
      --command "python app.py" \
      --workload-name my-python-app-job
    ```

    *Replace `<YOUR_GCP_PROJECT_ID>` with your actual GCP Project ID.*

    This command will:
    1. Verify/install the JobSet CRD on your cluster.
    2. Build a Docker image from `job_details/Dockerfile` using `python:3.9-slim` as the base, and push it to Artifact Registry.
    3. Generate and apply a Kubernetes JobSet manifest to your `my-test-cluster`.

## 8. Verify the Workload

Verify that the Kubernetes JobSet ran successfully on your GKE cluster.

* **Check JobSet Status:**

    ```bash
    kubectl get jobset --namespace default
    ```

    Look for a JobSet named `my-python-app-job` with a `SUCCEEDED` status in the `CONDITIONS` section.

* **Get Pod Logs:**
    First, get the name of the Pod created by your JobSet:

    ```bash
    kubectl get pods --namespace default -l jobset.sigs.k8s.io/jobset-name=my-python-app-job
    ```

    Note the Pod name (e.g., `my-python-app-job-worker-0-xxxxxx`).

    Then, get the logs from the pod:

    ```bash
    kubectl logs <POD_NAME> --namespace default
    ```

    You should see the output:

    ```text
    Hello from the gcluster run application!
    This is a sample application running on GKE.
    ```

## 9. Cleanup

To avoid incurring unnecessary costs, destroy the deployed GKE cluster and its resources:

```bash
./gcluster destroy my-test-cluster
```

*You will be prompted to confirm the destruction (type `a` and press Enter).*
