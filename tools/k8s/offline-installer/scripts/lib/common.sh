#!/usr/bin/env bash

set -Eeuo pipefail

INSTALLER_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSIONS_FILE="${VERSIONS_FILE:-${INSTALLER_ROOT}/versions.env}"
INSTALL_CONFIG_FILE="${INSTALL_CONFIG_FILE:-${INSTALLER_ROOT}/config/install.env}"
NODES_CONFIG_FILE="${NODES_CONFIG_FILE:-${INSTALLER_ROOT}/config/nodes.conf}"
IMAGE_LOCK_FILE="${IMAGE_LOCK_FILE:-${INSTALLER_ROOT}/images/images.lock}"
IMAGE_ARCHIVE="${IMAGE_ARCHIVE:-${INSTALLER_ROOT}/images/clawmanager-images-linux-amd64.tar}"
PACKAGE_DIR="${PACKAGE_DIR:-${INSTALLER_ROOT}/packages/amd64}"
MANIFEST_DIR="${MANIFEST_DIR:-${INSTALLER_ROOT}/manifests}"

OWNER_DIR=/etc/clawmanager-installer
OWNER_FILE=${OWNER_DIR}/owner
STATE_DIR=/var/lib/clawmanager-installer/state
# shellcheck disable=SC2034 # Used by scripts sourcing this shared library.
REMOTE_ROOT=/opt/clawmanager-offline

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"
}

warn() {
  printf '[%s] WARNING: %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >&2
}

die() {
  printf '[%s] ERROR: %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >&2
  exit 1
}

on_error() {
  local exit_code=$?
  local line=${BASH_LINENO[0]:-unknown}
  printf '[%s] ERROR: command failed at line %s (exit=%s)\n' \
    "$(date '+%Y-%m-%d %H:%M:%S')" "${line}" "${exit_code}" >&2
  exit "${exit_code}"
}

trap on_error ERR

require_command() {
  local command_name=$1
  command -v "${command_name}" >/dev/null 2>&1 || die "missing command: ${command_name}"
}

require_root() {
  [[ ${EUID} -eq 0 ]] || die "run this command with sudo or as root"
}

shell_quote() {
  printf '%q' "$1"
}

load_versions() {
  [[ -f ${VERSIONS_FILE} ]] || die "missing ${VERSIONS_FILE}"
  # shellcheck disable=SC1090
  source "${VERSIONS_FILE}"

  local required=(
    BUNDLE_SCHEMA_VERSION TARGET_OS TARGET_ARCH UBUNTU_VERSION_ID UBUNTU_CODENAME
    KUBERNETES_MINOR KUBERNETES_VERSION KUBERNETES_DEB_VERSION
    CALICO_VERSION CALICO_OPERATOR_VERSION LONGHORN_VERSION BUNDLE_REVISION BUNDLE_ID
  )
  local name
  for name in "${required[@]}"; do
    [[ -n ${!name:-} ]] || die "${name} is missing from versions.env"
  done
  [[ ${BUNDLE_ID} == *-r${BUNDLE_REVISION} ]] || die \
    "BUNDLE_ID must end with -r${BUNDLE_REVISION}: ${BUNDLE_ID}"
}

load_install_config() {
  [[ -f ${INSTALL_CONFIG_FILE} ]] || die \
    "missing ${INSTALL_CONFIG_FILE}; copy config/install.env.example to config/install.env"
  # shellcheck disable=SC1090
  source "${INSTALL_CONFIG_FILE}"

  : "${PROFILE:?PROFILE is required}"
  : "${SSH_USER:?SSH_USER is required}"
  : "${SSH_PORT:?SSH_PORT is required}"
  : "${SSH_PRIVATE_KEY:?SSH_PRIVATE_KEY is required}"
  : "${POD_CIDR:?POD_CIDR is required}"
  : "${SERVICE_CIDR:?SERVICE_CIDR is required}"
  : "${LONGHORN_DATA_PATH:?LONGHORN_DATA_PATH is required}"
  : "${LONGHORN_REPLICAS:?LONGHORN_REPLICAS is required}"
  : "${CLAWMANAGER_NODE_PORT:=30443}"
  : "${NODE_READY_TIMEOUT:=15m}"
  : "${CLAWMANAGER_READY_TIMEOUT:=20m}"

  case "${PROFILE}" in
    single-node|cluster) ;;
    *) die "PROFILE must be single-node or cluster" ;;
  esac

  [[ ${SSH_PORT} =~ ^[0-9]+$ ]] || die "SSH_PORT must be a number"
  [[ ${SSH_USER} =~ ^[a-z_][a-z0-9_-]*$ ]] || die "SSH_USER is not a valid Linux user name"
  [[ ${LONGHORN_REPLICAS} =~ ^[1-9][0-9]*$ ]] || die "LONGHORN_REPLICAS must be a positive integer"
  [[ ${LONGHORN_DATA_PATH} == /* && ${LONGHORN_DATA_PATH} != *[[:space:]]* ]] || die \
    "LONGHORN_DATA_PATH must be an absolute path without spaces"
  [[ ${CLAWMANAGER_NODE_PORT:-30443} =~ ^[0-9]+$ ]] || die "CLAWMANAGER_NODE_PORT must be a number"
  ((CLAWMANAGER_NODE_PORT >= 30000 && CLAWMANAGER_NODE_PORT <= 32767)) || die \
    "CLAWMANAGER_NODE_PORT must be in the Kubernetes NodePort range 30000-32767"
}

declare -a NODE_ROLES=()
declare -a NODE_HOSTNAMES=()
declare -a NODE_TARGETS=()
declare -a NODE_IPS=()
declare -a NODE_STORAGE=()

trim() {
  local value=$1
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "${value}"
}

load_nodes() {
  [[ -f ${NODES_CONFIG_FILE} ]] || die \
    "missing ${NODES_CONFIG_FILE}; copy config/nodes.conf.example to config/nodes.conf"

  NODE_ROLES=()
  NODE_HOSTNAMES=()
  NODE_TARGETS=()
  NODE_IPS=()
  NODE_STORAGE=()

  local raw role hostname target node_ip storage extra
  while IFS= read -r raw || [[ -n ${raw} ]]; do
    raw="$(trim "${raw}")"
    [[ -z ${raw} || ${raw:0:1} == '#' ]] && continue
    IFS='|' read -r role hostname target node_ip storage extra <<<"${raw}"
    role="$(trim "${role:-}")"
    hostname="$(trim "${hostname:-}")"
    target="$(trim "${target:-}")"
    node_ip="$(trim "${node_ip:-}")"
    storage="$(trim "${storage:-}")"
    extra="$(trim "${extra:-}")"

    [[ -z ${extra} ]] || die "too many fields in nodes.conf line: ${raw}"
    [[ ${role} == control-plane || ${role} == worker ]] || die "invalid role in nodes.conf: ${role}"
    [[ ${hostname} =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || die "invalid hostname: ${hostname}"
    [[ -n ${target} ]] || die "empty SSH target for ${hostname}"
    [[ ${target} == local || ${target} =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$ ]] || die \
      "SSH target must be an IPv4 address or DNS name for ${hostname}"
    [[ ${node_ip} == auto || ${node_ip} =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || die \
      "node_ip must be auto or IPv4 for ${hostname}"
    [[ ${storage} == true || ${storage} == false ]] || die "storage must be true or false for ${hostname}"

    NODE_ROLES+=("${role}")
    NODE_HOSTNAMES+=("${hostname}")
    NODE_TARGETS+=("${target}")
    NODE_IPS+=("${node_ip}")
    NODE_STORAGE+=("${storage}")
  done <"${NODES_CONFIG_FILE}"

  ((${#NODE_ROLES[@]} > 0)) || die "nodes.conf contains no nodes"

  local control_plane_count=0 worker_count=0 index other
  for index in "${!NODE_ROLES[@]}"; do
    if [[ ${NODE_ROLES[index]} == control-plane ]]; then
      ((control_plane_count += 1))
      [[ ${NODE_TARGETS[index]} == local ]] || die "control-plane SSH target must be local"
    else
      ((worker_count += 1))
      [[ ${NODE_TARGETS[index]} != local ]] || die "worker ${NODE_HOSTNAMES[index]} requires an SSH target"
    fi
    for other in "${!NODE_ROLES[@]}"; do
      ((other >= index)) && continue
      [[ ${NODE_HOSTNAMES[index]} != "${NODE_HOSTNAMES[other]}" ]] || die \
        "duplicate hostname: ${NODE_HOSTNAMES[index]}"
      if [[ ${NODE_TARGETS[index]} != local && ${NODE_TARGETS[index]} == "${NODE_TARGETS[other]}" ]]; then
        die "duplicate SSH target: ${NODE_TARGETS[index]}"
      fi
    done
  done

  ((control_plane_count == 1)) || die "nodes.conf must contain exactly one control-plane"
  if [[ ${PROFILE} == single-node ]]; then
    ((worker_count == 0)) || die "single-node profile cannot contain worker nodes"
  else
    ((worker_count > 0)) || die "cluster profile requires at least one worker"
    local storage_count=0
    for index in "${!NODE_STORAGE[@]}"; do
      [[ ${NODE_STORAGE[index]} == true ]] && ((storage_count += 1))
    done
    ((storage_count >= LONGHORN_REPLICAS)) || die \
      "LONGHORN_REPLICAS=${LONGHORN_REPLICAS}, but only ${storage_count} storage nodes are enabled; lower the replica count or add storage workers"
  fi
}

control_plane_index() {
  local index
  for index in "${!NODE_ROLES[@]}"; do
    [[ ${NODE_ROLES[index]} == control-plane ]] && { printf '%s' "${index}"; return 0; }
  done
  return 1
}

first_worker_target() {
  local index
  for index in "${!NODE_ROLES[@]}"; do
    [[ ${NODE_ROLES[index]} == worker ]] && { printf '%s' "${NODE_TARGETS[index]}"; return 0; }
  done
  return 1
}

ssh_options() {
  printf '%s\n' \
    -i "${SSH_PRIVATE_KEY}" \
    -p "${SSH_PORT}" \
    -o BatchMode=yes \
    -o ConnectTimeout=10 \
    -o StrictHostKeyChecking=accept-new
}

node_bash() {
  local target=$1
  local script=$2
  if [[ ${target} == local ]]; then
    bash -se <<<"${script}"
    return
  fi

  local -a options=()
  mapfile -t options < <(ssh_options)
  printf '%s\n' "${script}" | ssh "${options[@]}" "${SSH_USER}@${target}" 'sudo -n bash -se'
}

node_capture() {
  local target=$1
  local command=$2
  if [[ ${target} == local ]]; then
    bash -c "${command}"
    return
  fi

  local -a options=()
  mapfile -t options < <(ssh_options)
  local quoted
  quoted="$(shell_quote "${command}")"
  ssh "${options[@]}" "${SSH_USER}@${target}" "sudo -n bash -c ${quoted}"
}

node_file_exists() {
  local target=$1
  local path=$2
  node_capture "${target}" "test -f $(shell_quote "${path}")"
}

mark_node_step() {
  local target=$1
  local step=$2
  node_bash "${target}" "mkdir -p '${STATE_DIR}'; chmod 700 '${STATE_DIR}'; touch '${STATE_DIR}/${step}'"
}

node_step_marked() {
  local target=$1
  local step=$2
  node_file_exists "${target}" "${STATE_DIR}/${step}"
}

assert_or_claim_node() {
  local target=$1
  local hostname=$2
  local bundle_id_quoted
  bundle_id_quoted="$(shell_quote "${BUNDLE_ID}")"
  node_bash "${target}" "
set -Eeuo pipefail
owner_file='${OWNER_FILE}'
bundle_id=${bundle_id_quoted}
if [[ -f \"\${owner_file}\" ]]; then
  actual=\$(sed -n 's/^bundle_id=//p' \"\${owner_file}\")
  [[ \"\${actual}\" == \"\${bundle_id}\" ]] || {
    echo \"node is owned by another installer bundle: \${actual}\" >&2
    exit 42
  }
  exit 0
fi
if [[ -e /etc/kubernetes/admin.conf || -e /etc/kubernetes/kubelet.conf ]] \
  || [[ -e /etc/rancher/k3s/k3s.yaml || -e /var/snap/microk8s || -e /etc/k0s ]] \
  || command -v kubeadm >/dev/null 2>&1 \
  || command -v kubelet >/dev/null 2>&1 \
  || command -v k3s >/dev/null 2>&1 \
  || command -v microk8s >/dev/null 2>&1 \
  || command -v k0s >/dev/null 2>&1; then
  echo 'existing Kubernetes installation detected without ClawManager installer ownership' >&2
  exit 43
fi
mkdir -p '${OWNER_DIR}' '${STATE_DIR}'
chmod 700 '${OWNER_DIR}' '${STATE_DIR}'
printf 'bundle_id=%s\nhostname=%s\n' \"\${bundle_id}\" '$(shell_quote "${hostname}")' >\"\${owner_file}\"
chmod 600 \"\${owner_file}\"
"
}

assert_target_ubuntu_amd64() {
  local target=$1
  node_bash "${target}" "
set -Eeuo pipefail
source /etc/os-release
[[ \"\${ID}\" == ubuntu && \"\${VERSION_ID}\" == '${UBUNTU_VERSION_ID}' ]] || {
  echo \"Ubuntu ${UBUNTU_VERSION_ID} is required; found \${ID:-unknown} \${VERSION_ID:-unknown}\" >&2
  exit 44
}
[[ \$(dpkg --print-architecture) == '${TARGET_ARCH}' ]] || {
  echo 'architecture mismatch: expected ${TARGET_ARCH}' >&2
  exit 45
}
"
}

sha256_file() {
  sha256sum "$1" | awk '{print $1}'
}

read_image_lock() {
  awk -F'|' '
    /^[[:space:]]*#/ || /^[[:space:]]*$/ { next }
    NF == 2 {
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", $2)
      if ($2 != "") print $2
    }
  ' "${IMAGE_LOCK_FILE}"
}

normalize_image_reference() {
  local image=$1
  local name=${image%@*}
  if [[ ${name} != */* ]]; then
    printf 'docker.io/library/%s' "${image}"
    return
  fi
  local first=${name%%/*}
  if [[ ${first} != *.* && ${first} != *:* && ${first} != localhost ]]; then
    printf 'docker.io/%s' "${image}"
    return
  fi
  printf '%s' "${image}"
}

image_for_key() {
  local wanted_key=$1
  awk -F'|' -v wanted_key="${wanted_key}" '
    /^[[:space:]]*#/ || /^[[:space:]]*$/ { next }
    $1 == wanted_key && $2 != "" { print $2; found=1; exit }
    END { if (!found) exit 1 }
  ' "${IMAGE_LOCK_FILE}"
}
