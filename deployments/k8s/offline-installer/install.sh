#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/common.sh
source "${SCRIPT_DIR}/scripts/lib/common.sh"
# shellcheck source=scripts/lib/node.sh
source "${SCRIPT_DIR}/scripts/lib/node.sh"
# shellcheck source=scripts/lib/kubernetes.sh
source "${SCRIPT_DIR}/scripts/lib/kubernetes.sh"
# shellcheck source=scripts/lib/platform.sh
source "${SCRIPT_DIR}/scripts/lib/platform.sh"

require_root
load_versions
load_install_config
load_nodes
require_command awk
require_command ip
require_command sha256sum
require_command tar

log "validating offline bundle before changing any node"
bash "${SCRIPT_DIR}/verify-bundle.sh"

if [[ ${PROFILE} == cluster ]]; then
  require_command ssh
  [[ -f ${SSH_PRIVATE_KEY} ]] || die \
    "SSH private key is missing: ${SSH_PRIVATE_KEY}; run prepare-ssh.sh first"
  for index in "${!NODE_ROLES[@]}"; do
    [[ ${NODE_ROLES[index]} == worker ]] || continue
    log "checking SSH and passwordless sudo on ${NODE_TARGETS[index]}"
    node_capture "${NODE_TARGETS[index]}" 'true'
  done
fi

log "checking supported operating system"
for index in "${!NODE_ROLES[@]}"; do
  assert_target_ubuntu_amd64 "${NODE_TARGETS[index]}"
done

log "checking that no node contains an unmanaged Kubernetes installation"
for index in "${!NODE_ROLES[@]}"; do
  assert_or_claim_node "${NODE_TARGETS[index]}" "${NODE_HOSTNAMES[index]}"
done

for index in "${!NODE_ROLES[@]}"; do
  [[ ${NODE_ROLES[index]} == worker ]] || continue
  stage_packages_on_worker "${NODE_TARGETS[index]}"
done
for index in "${!NODE_ROLES[@]}"; do
  install_node_packages "${NODE_TARGETS[index]}"
done
require_command python3
require_command rsync
require_command openssl

log "resolving and validating Kubernetes node IPs"
cp_index=$(control_plane_index)
declare -a RESOLVED_NODE_IPS=()
RESOLVED_NODE_IPS[cp_index]=$(detect_control_plane_ip "${cp_index}")
for index in "${!NODE_ROLES[@]}"; do
  [[ ${NODE_ROLES[index]} == worker ]] || continue
  RESOLVED_NODE_IPS[index]=$(detect_worker_ip "${index}" "${RESOLVED_NODE_IPS[cp_index]}")
done
validate_resolved_node_ips "${RESOLVED_NODE_IPS[@]}"
write_resolved_nodes "${RESOLVED_NODE_IPS[@]}"

for index in "${!NODE_ROLES[@]}"; do
  configure_node_system "${NODE_TARGETS[index]}" "${NODE_HOSTNAMES[index]}"
done

for index in "${!NODE_ROLES[@]}"; do
  [[ ${NODE_ROLES[index]} == worker ]] || continue
  stage_image_payload_on_worker "${NODE_TARGETS[index]}"
done
for index in "${!NODE_ROLES[@]}"; do
  import_node_images "${NODE_TARGETS[index]}"
done

initialize_control_plane "${RESOLVED_NODE_IPS[cp_index]}" "${NODE_HOSTNAMES[cp_index]}"

if [[ ${PROFILE} == cluster ]]; then
  create_join_material "${RESOLVED_NODE_IPS[cp_index]}"
  for index in "${!NODE_ROLES[@]}"; do
    [[ ${NODE_ROLES[index]} == worker ]] || continue
    join_worker "${index}" "${RESOLVED_NODE_IPS[cp_index]}"
  done
fi

install_calico
wait_for_all_nodes
configure_node_roles
apply_clawmanager
print_install_summary
