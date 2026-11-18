#!/usr/bin/env bash

set -Eeuo pipefail

render_platform_manifest() {
  local source_manifest
  if [[ ${PROFILE} == single-node ]]; then
    source_manifest=${MANIFEST_DIR}/clawmanager-single-node.yaml
  else
    source_manifest=${MANIFEST_DIR}/clawmanager-cluster.yaml
  fi
  local rendered_manifest=${STATE_DIR}/clawmanager-rendered.yaml
  [[ -f ${source_manifest} ]] || die "missing ClawManager manifest: ${source_manifest}"
  mkdir -p "${STATE_DIR}"

  python3 - "${source_manifest}" "${rendered_manifest}" "${PROFILE}" \
    "${LONGHORN_DATA_PATH}" "${LONGHORN_REPLICAS}" "${CLAWMANAGER_NODE_PORT:-30443}" <<'PY'
from pathlib import Path
import re
import sys

source, destination, profile, data_path, replicas, node_port = sys.argv[1:]
text = Path(source).read_text(encoding="utf-8")
text = re.sub(r"(?m)^(\s*imagePullPolicy:)\s*Always\s*$", r"\1 IfNotPresent", text)
text = re.sub(r"(?m)^(\s*nodePort:)\s*30443\s*$", rf"\1 {node_port}", text)

if profile == "cluster":
    text = text.replace('/var/lib/longhorn/', data_path.rstrip('/') + '/')
    text = re.sub(
        r'(?m)^(\s*numberOfReplicas:)\s*"3"\s*$',
        rf'\1 "{replicas}"',
        text,
    )
    marker = '    priority-class: "longhorn-critical"\n'
    additions = (
        f'    default-data-path: "{data_path}"\n'
        f'    default-replica-count: "{replicas}"\n'
        '    create-default-disk-labeled-nodes: "true"\n'
    )
    if additions not in text:
        if marker not in text:
            raise SystemExit("Longhorn default-setting marker is missing")
        text = text.replace(marker, marker + additions, 1)

if re.search(r"(?m)^\s*imagePullPolicy:\s*Always\s*$", text):
    raise SystemExit("rendered offline manifest still contains imagePullPolicy: Always")
Path(destination).write_text(text, encoding="utf-8")
PY
  printf '%s' "${rendered_manifest}"
}

prepare_longhorn_storage_paths() {
  [[ ${PROFILE} == cluster ]] || return 0
  local index
  for index in "${!NODE_ROLES[@]}"; do
    [[ ${NODE_STORAGE[index]} == true ]] || continue
    log "preparing Longhorn data path on ${NODE_HOSTNAMES[index]}: ${LONGHORN_DATA_PATH}"
    node_bash "${NODE_TARGETS[index]}" "
set -Eeuo pipefail
mkdir -p '${LONGHORN_DATA_PATH}'
chmod 700 '${LONGHORN_DATA_PATH}'
"
  done
}

apply_clawmanager() {
  local rendered_manifest
  rendered_manifest=$(render_platform_manifest)
  prepare_longhorn_storage_paths

  log "applying ClawManager ${PROFILE} manifest"
  KUBECONFIG=/etc/kubernetes/admin.conf kubectl apply -f "${rendered_manifest}"
  mark_node_step local clawmanager_applied

  if [[ ${PROFILE} == cluster ]]; then
    log "waiting for Longhorn control plane"
    KUBECONFIG=/etc/kubernetes/admin.conf kubectl -n longhorn-system rollout status \
      daemonset/longhorn-manager --timeout="${CLAWMANAGER_READY_TIMEOUT}"
    KUBECONFIG=/etc/kubernetes/admin.conf kubectl -n longhorn-system rollout status \
      deployment/longhorn-driver-deployer --timeout="${CLAWMANAGER_READY_TIMEOUT}"
  fi

  log "waiting for ClawManager workloads"
  KUBECONFIG=/etc/kubernetes/admin.conf kubectl -n clawmanager-system rollout status \
    deployment/mysql --timeout="${CLAWMANAGER_READY_TIMEOUT}"
  KUBECONFIG=/etc/kubernetes/admin.conf kubectl -n clawmanager-system rollout status \
    deployment/minio --timeout="${CLAWMANAGER_READY_TIMEOUT}"
  KUBECONFIG=/etc/kubernetes/admin.conf kubectl -n clawmanager-system rollout status \
    deployment/clawmanager-team-redis --timeout="${CLAWMANAGER_READY_TIMEOUT}"
  KUBECONFIG=/etc/kubernetes/admin.conf kubectl -n clawmanager-system rollout status \
    deployment/clawmanager-app --timeout="${CLAWMANAGER_READY_TIMEOUT}"

  local pvc_phase
  pvc_phase=$(KUBECONFIG=/etc/kubernetes/admin.conf kubectl -n clawmanager-system \
    get pvc clawmanager-workspaces -o jsonpath='{.status.phase}')
  [[ ${pvc_phase} == Bound ]] || die "clawmanager-workspaces PVC is ${pvc_phase}, expected Bound"
  mark_node_step local clawmanager_ready
}

print_install_summary() {
  local cp_index
  cp_index=$(control_plane_index)
  local control_plane_ip=${RESOLVED_NODE_IPS[cp_index]}
  local node_port=${CLAWMANAGER_NODE_PORT:-30443}
  log "installation completed successfully"
  printf '\nClawManager: https://%s:%s\n' "${control_plane_ip}" "${node_port}"
  printf 'Profile: %s\n' "${PROFILE}"
  printf 'Kubernetes: %s\n' "${KUBERNETES_VERSION}"
  printf 'Resolved nodes: %s\n' "${STATE_DIR}/resolved-nodes.conf"
  printf 'Re-run this installer to verify or resume the same bundle.\n'
}
