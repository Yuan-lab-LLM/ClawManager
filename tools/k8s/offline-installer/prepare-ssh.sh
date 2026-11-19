#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/common.sh
source "${SCRIPT_DIR}/scripts/lib/common.sh"

require_root
require_command ssh
require_command ssh-keygen
require_command ssh-copy-id
load_versions
load_install_config
load_nodes

if [[ ${PROFILE} == single-node ]]; then
  log "single-node profile has no worker SSH setup"
  exit 0
fi

key_dir="$(dirname "${SSH_PRIVATE_KEY}")"
mkdir -p "${key_dir}"
chmod 700 "${key_dir}"

if [[ ! -f ${SSH_PRIVATE_KEY} ]]; then
  log "generating installer SSH key: ${SSH_PRIVATE_KEY}"
  ssh-keygen -q -t ed25519 -N '' -f "${SSH_PRIVATE_KEY}" -C "${BUNDLE_ID}"
fi

[[ -f ${SSH_PRIVATE_KEY}.pub ]] || die "missing public key: ${SSH_PRIVATE_KEY}.pub"
chmod 600 "${SSH_PRIVATE_KEY}"
chmod 644 "${SSH_PRIVATE_KEY}.pub"

for index in "${!NODE_ROLES[@]}"; do
  [[ ${NODE_ROLES[index]} == worker ]] || continue
  target=${NODE_TARGETS[index]}
  hostname=${NODE_HOSTNAMES[index]}

  log "configuring SSH key for ${hostname} (${SSH_USER}@${target})"
  ssh-copy-id \
    -i "${SSH_PRIVATE_KEY}.pub" \
    -p "${SSH_PORT}" \
    -o StrictHostKeyChecking=accept-new \
    "${SSH_USER}@${target}"

  local_options=(
    -i "${SSH_PRIVATE_KEY}"
    -p "${SSH_PORT}"
    -o BatchMode=yes
    -o ConnectTimeout=10
    -o StrictHostKeyChecking=accept-new
  )
  if ! ssh "${local_options[@]}" "${SSH_USER}@${target}" 'sudo -n true'; then
    log "passwordless sudo is not configured on ${hostname}; enter its sudo password once"
    ssh -tt \
      -i "${SSH_PRIVATE_KEY}" \
      -p "${SSH_PORT}" \
      -o StrictHostKeyChecking=accept-new \
      "${SSH_USER}@${target}" \
      "tmp_file=\$(mktemp); printf '%s ALL=(ALL) NOPASSWD:ALL\n' '${SSH_USER}' >\"\${tmp_file}\"; sudo visudo -cf \"\${tmp_file}\" && sudo install -m 0440 \"\${tmp_file}\" /etc/sudoers.d/clawmanager-installer; rm -f \"\${tmp_file}\""
    ssh "${local_options[@]}" "${SSH_USER}@${target}" 'sudo -n true' || die \
      "could not configure passwordless sudo on ${SSH_USER}@${target}"
  fi
  ssh "${local_options[@]}" "${SSH_USER}@${target}" 'hostname; sudo -n true'
done

log "SSH passwordless access is ready for all workers"
