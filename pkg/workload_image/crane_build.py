# Copyright 2026 "Google LLC"
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import tarfile
import fnmatch
import random
import string
import tempfile
import sys
import os
import datetime
import logging
import subprocess
import argparse # Added for command-line argument parsing
from enum import Enum
from typing import List

# Configure logging to go to stderr so stdout is clean for the image name
logging.basicConfig(level=logging.INFO, format='%(levelname)s: %(message)s', stream=sys.stderr)
log = logging.getLogger()

class DockerPlatform(Enum):
    LINUX_AMD64 = "linux/amd64"
    LINUX_ARM64 = "linux/arm64"

# Removed log_and_print and log_and_exit as their functionality is replaced by direct sys.stdout/stderr and sys.exit

def write_tmp_file(content: str) -> str:
    with tempfile.NamedTemporaryFile(delete=False, mode='w') as f:
        f.write(content)
    return f.name

def create_filtered_tar(source_dir, output_filename, ignore_patterns):
    log.info(f"Creating filtered tar from {source_dir} to {output_filename}")
    with tarfile.open(output_filename, "w:gz") as tar:
        def exclude_files(tarinfo):
            for pattern in ignore_patterns:
                if fnmatch.fnmatch(tarinfo.name, pattern):
                    return None
            return tarinfo

        # Add all files and subdirectories from source_dir, but rename the root to "."
        # This makes the tarball relative to its own root
        try:
            tar.add(source_dir, arcname=".", filter=exclude_files)
        except Exception as e:
            log.error(f"Error creating tar archive: {e}")
            raise

def run_command_with_updates(command: str, update_message: str) -> int:
    log.info(f"{update_message}: {command}")
    try:
        result = subprocess.run(command, shell=True, check=False, text=True, capture_output=True)
        if result.returncode != 0:
            log.error(f"Command failed with exit code {result.returncode}: {command}")
            log.error(f"Stdout: {result.stdout.strip()}")
            log.error(f"Stderr: {result.stderr.strip()}")
        return result.returncode
    except Exception as e:
        log.error(f"Exception during command execution: {e}")
        return 1

def build_container_image_from_base_image(
    project: str,
    base_docker_image: str,
    script_dir: str,
    docker_platform_str: str,
    gitignore_patterns: List[str]
) -> str: # Returns the full image name on success
    try:
        docker_platform = DockerPlatform(docker_platform_str)
    except ValueError:
        log.error(f"Invalid Docker platform: {docker_platform_str}. Supported: {[p.value for p in DockerPlatform]}")
        sys.exit(1)

    docker_name = f'{os.getenv("USER", "unknown")}-runner'
    tag_length = 4
    tag_random_prefix = ''.join(random.choices(string.ascii_lowercase, k=tag_length))
    tag_datetime = datetime.datetime.now().strftime('%Y-%m-%d-%H-%M-%S')
    tag_name = f'{tag_random_prefix}-{tag_datetime}'
    cloud_docker_image = f'gcr.io/{project}/{docker_name}:{tag_name}'

    log.info(f"Starting image build process for {cloud_docker_image}")
    log.info(f"Base Docker Image: {base_docker_image}")
    log.info(f"Script Directory: {script_dir}")
    log.info(f"Target Platform: {docker_platform.value}")

    image_archive_path = write_tmp_file('')
    log.info(f"Temporary image archive path: {image_archive_path}")
    try:
        log.info("Creating filtered tarball...")
        create_filtered_tar(script_dir, image_archive_path, gitignore_patterns)
        log.info("Filtered tarball created successfully.")
    except Exception as e:
        log.error(f"Failed to create filtered tarball: {e}")
        os.remove(image_archive_path) # Clean up on failure
        sys.exit(1)

    crane_command = (
        f'crane mutate {base_docker_image} --append {image_archive_path}'
        f' --platform {docker_platform.value} --tag {cloud_docker_image}'
    )

    log.info(f"Executing crane mutate command: {crane_command}")
    return_code = run_command_with_updates(crane_command, f'Uploading Container Image to {cloud_docker_image}')
    os.remove(image_archive_path)

    if return_code != 0:
        log.error("Crane command failed.")
        sys.exit(1)
    
    log.info(f"Image {cloud_docker_image} built and uploaded successfully.")
    return cloud_docker_image

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Build a container image using crane.")
    parser.add_argument("--project", required=True, help="Google Cloud Project ID.")
    parser.add_argument("--image-name", required=True, help="Base Docker image name (e.g., python:3.10).")
    parser.add_argument("--script-dir", required=True, help="Path to the directory containing scripts/build context.")
    parser.add_argument("--dockerfile", default="Dockerfile", help="Path to the Dockerfile (for reference, not directly used by crane mutate).")
    parser.add_argument("--platform", default="linux/amd64", help="Target Docker platform (e.g., linux/amd64).")
    # gitignore_patterns could be passed as a comma-separated string, or read from a file
    # For simplicity, let's assume default common patterns or an empty list for now.
    parser.add_argument("--ignore-patterns", nargs='*', default=[], help="List of glob patterns to ignore during tar creation.")

    args = parser.parse_args()

    # The project_id is no longer inferred here, it must be provided.
    # The base_docker_image is now explicitly passed via --image-name.
    # script_dir is passed via --script-dir.

    try:
        final_image_name = build_container_image_from_base_image(
            project=args.project,
            base_docker_image=args.image_name, # Renamed to base_docker_image
            script_dir=args.script_dir,
            docker_platform_str=args.platform,
            gitignore_patterns=args.ignore_patterns
        )
        print(final_image_name) # Print the final image name to stdout for Go to capture
    except Exception as e:
        log.error(f"An unexpected error occurred: {e}")
        sys.exit(1)
