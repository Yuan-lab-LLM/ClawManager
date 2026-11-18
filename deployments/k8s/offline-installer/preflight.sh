#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/common.sh
source "${SCRIPT_DIR}/scripts/lib/common.sh"

require_root
load_versions
load_install_config
load_nodes
require_command awk
require_command ssh

if [[ ${PROFILE} == cluster ]]; then
  [[ -f ${SSH_PRIVATE_KEY} ]] || die \
    "SSH private key is missing: ${SSH_PRIVATE_KEY}; run prepare-ssh.sh first"
fi

log "running read-only host preflight for Ubuntu ${UBUNTU_VERSION_ID} ${TARGET_ARCH}"
for index in "${!NODE_ROLES[@]}"; do
  role=${NODE_ROLES[index]}
  hostname=${NODE_HOSTNAMES[index]}
  target=${NODE_TARGETS[index]}
  bundle_id_quoted=$(shell_quote "${BUNDLE_ID}")

  log "checking ${role} ${hostname} (${target})"
  node_bash "${target}" "
set -Eeuo pipefail
source /etc/os-release
[[ \"\${ID}\" == ubuntu && \"\${VERSION_ID}\" == '${UBUNTU_VERSION_ID}' ]] || {
  echo '[FAIL] Ubuntu ${UBUNTU_VERSION_ID} is required' >&2
  exit 44
}
[[ \$(dpkg --print-architecture) == '${TARGET_ARCH}' ]] || {
  echo '[FAIL] architecture must be ${TARGET_ARCH}' >&2
  exit 45
}

bundle_id=${bundle_id_quoted}
owned=false
if [[ -f '${OWNER_FILE}' ]]; then
  actual=\$(sed -n 's/^bundle_id=//p' '${OWNER_FILE}')
  [[ \"\${actual}\" == \"\${bundle_id}\" ]] || {
    echo \"[FAIL] node is owned by another installer bundle: \${actual}\" >&2
    exit 42
  }
  owned=true
fi

if [[ \"\${owned}\" == false ]] && {
  [[ -e /etc/kubernetes/admin.conf || -e /etc/kubernetes/kubelet.conf ]] \
    || [[ -e /etc/rancher/k3s/k3s.yaml || -e /var/snap/microk8s || -e /etc/k0s ]] \
    || command -v kubeadm >/dev/null 2>&1 \
    || command -v kubelet >/dev/null 2>&1 \
    || command -v k3s >/dev/null 2>&1 \
    || command -v microk8s >/dev/null 2>&1 \
    || command -v k0s >/dev/null 2>&1;
}; then
  echo '[FAIL] unmanaged Kubernetes installation detected' >&2
  exit 43
fi

cpu=\$(nproc)
memory_kib=\$(awk '/^MemTotal:/ {print \$2}' /proc/meminfo)
disk_kib=\$(df -Pk / | awk 'NR == 2 {print \$4}')
printf '[INFO] os=%s arch=%s kernel=%s cpu=%s memoryGiB=%s rootFreeGiB=%s owner=%s\\n' \
  \"\${PRETTY_NAME}\" \"\$(dpkg --print-architecture)\" \"\$(uname -r)\" \
  \"\${cpu}\" \"\$((memory_kib / 1024 / 1024))\" \"\$((disk_kib / 1024 / 1024))\" \"\${owned}\"

((cpu >= 2)) || echo '[WARN] fewer than 2 CPU cores'
((memory_kib >= 4 * 1024 * 1024)) || echo '[WARN] less than 4 GiB memory'
((disk_kib >= 40 * 1024 * 1024)) || echo '[WARN] less than 40 GiB free on root filesystem'
[[ -f /sys/fs/cgroup/cgroup.controllers ]] || echo '[WARN] cgroup v2 is not active'
modprobe -n overlay >/dev/null 2>&1 || echo '[WARN] overlay kernel module is unavailable'
modprobe -n br_netfilter >/dev/null 2>&1 || echo '[WARN] br_netfilter kernel module is unavailable'
modprobe -n iscsi_tcp >/dev/null 2>&1 || echo '[WARN] iscsi_tcp kernel module is unavailable'

if command -v timedatectl >/dev/null 2>&1; then
  synchronized=\$(timedatectl show -p NTPSynchronized --value 2>/dev/null || true)
  [[ \"\${synchronized}\" == yes ]] || echo '[WARN] system clock is not reported as synchronized'
fi
if swapon --noheadings --show 2>/dev/null | grep -q .; then
  echo '[INFO] swap is active and will be disabled by install.sh'
fi

if [[ \"\${owned}\" == false ]] && command -v ss >/dev/null 2>&1; then
  if ss -H -lnt 2>/dev/null | awk '{print \$4}' | grep -Eq '(^|:|\\])6443\$|(^|:|\\])10250\$'; then
    echo '[FAIL] Kubernetes port 6443 or 10250 is already in use' >&2
    exit 46
  fi
fi
"
done

log "host preflight passed; review any WARN lines before installation"
