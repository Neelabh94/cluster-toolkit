#!/bin/bash
set -e
set -o pipefail

export PROJECT_ID="cloud-tpu-multipod-dev"
export CLUSTER_NAME="bodaborg-super-tpu7x-y6k"
export ZONE="us-central1"
export BASE_OUTPUT_DIR="gs://ubench-logs"
export WORKLOAD_NAME="neelgoyal-ctk-512chip-8x8x8"
export ARTIFACT_DIR="gs://ubench-logs/${WORKLOAD_NAME}"
export WORKLOAD_IMAGE="gcr.io/tpu-prod-env-multipod/maxtext_jax_nightly:maxtext_58741fa_20260731"

# XLA Flags tuned for TPU 7x 512-chip mesh performance
XLA_FLAGS=" \
  --xla_tpu_scoped_vmem_limit_kib=98304 \
  --xla_tpu_use_minor_sharding_for_major_trivial_input=true \
  --xla_tpu_relayout_group_size_threshold_for_reduce_scatter=1 \
  --xla_tpu_assign_all_reduce_scatter_layout=true \
  --xla_tpu_enable_data_parallel_all_reduce_opt=true \
  --xla_tpu_data_parallel_opt_different_sized_ops=true \
  --xla_tpu_enable_async_collective_fusion=false \
  --xla_tpu_enable_async_collective_fusion_fuse_all_gather=true \
  --xla_tpu_enable_async_collective_fusion_multiple_steps=true \
  --xla_tpu_overlap_compute_collective_tc=true \
  --xla_enable_async_all_gather=true \
  --xla_tpu_enable_async_collective_fusion_fuse_all_reduce=false \
  --xla_tpu_enable_sparse_core_collective_offload_all_reduce=true \
  --xla_tpu_enable_all_reduce_offload_tracing=true \
  --xla_tpu_use_tc_device_shape_on_sc=true \
  --xla_sc_enable_instruction_fusion=false \
  --xla_sc_disjoint_spmem=false \
  --xla_sc_disable_megacore_partitioning=true \
  --xla_tpu_enable_all_experimental_scheduler_features=true \
  --xla_tpu_enable_scheduler_memory_pressure_tracking=true \
  --xla_tpu_host_transfer_overlap_limit=24 \
  --xla_tpu_aggressive_opt_barrier_removal=ENABLED \
  --xla_lhs_prioritize_async_depth_over_stall=ENABLED \
  --xla_tpu_enable_ag_backward_pipelining=true \
  --xla_should_allow_loop_variant_parameter_in_chain=ENABLED \
  --xla_should_add_loop_invariant_op_in_chain=ENABLED \
  --xla_max_concurrent_host_send_recv=100 \
  --xla_tpu_scheduler_percent_shared_memory_limit=100 \
  --xla_latency_hiding_scheduler_rerun=2 \
  --xla_jf_spmd_threshold_for_windowed_einsum_mib=1000000 "

MAXTEXT_ARGS="\
model_name=llama3.1-8b \
dataset_type=synthetic \
dataset_path=gs://max-datasets-rogue \
enable_checkpointing=True \
async_checkpointing=True \
checkpoint_period=100 \
steps=2000 \
base_output_directory=${BASE_OUTPUT_DIR} \
run_name=${WORKLOAD_NAME}"

echo "=== Submitting 512-chip (128-node, 8x8x8 topology) Dynamic Slicing Workload: $WORKLOAD_NAME ==="
./gcluster job submit \
  --queue multislice-queue \
  --cluster $CLUSTER_NAME \
  --project $PROJECT_ID \
  --location $ZONE \
  --priority very-low \
  --restarts 3 \
  --compute-type tpu7x \
  --topology 8x8x8 \
  --num-slices 1 \
  --image "${WORKLOAD_IMAGE}" \
  --name "${WORKLOAD_NAME}" \
  --command "set -e && set -o pipefail && export ENABLE_PATHWAYS_PERSISTENCE='1' && \
export LIBTPU_INIT_ARGS='${XLA_FLAGS}' && \
export ARTIFACT_DIR='${ARTIFACT_DIR}' && \
export JAX_PLATFORMS='tpu,cpu' && export ENABLE_PJRT_COMPATIBILITY='true' && \
python3 -m maxtext.trainers.pre_train.train maxtext/configs/base.yml ${MAXTEXT_ARGS} | tee train.log && \
(gcloud storage cp train.log ${ARTIFACT_DIR}/logs/train-\${TPU_WORKER_ID}.log || true)"

