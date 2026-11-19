#!/usr/bin/env bash

set -Eeuo pipefail

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALLER_ROOT="$(cd "${TEST_DIR}/.." && pwd)"
repo_dir=${1:?usage: offline-package-install-check.sh PACKAGE_DIR}
repo_dir="$(cd "${repo_dir}" && pwd)"
target_release_file=${repo_dir}/target-release.env
[[ -f ${target_release_file} ]] || {
  echo "missing ${target_release_file}" >&2
  exit 1
}
ubuntu_version=$(sed -n 's/^UBUNTU_VERSION_ID=//p' "${target_release_file}")
VERSIONS_FILE="${INSTALLER_ROOT}/ubuntu${ubuntu_version}/versions.env"
# shellcheck source=../scripts/lib/common.sh
source "${INSTALLER_ROOT}/scripts/lib/common.sh"

docker_repo_dir=${repo_dir}
case "$(uname -s)" in
  MINGW*|MSYS*)
    docker_repo_dir=$(cd "${repo_dir}" && pwd -W)
    ;;
esac
load_versions
require_command docker

grep -Fx "UBUNTU_VERSION_ID=${UBUNTU_VERSION_ID}" \
  "${repo_dir}/target-release.env" >/dev/null
grep -Fx "UBUNTU_CODENAME=${UBUNTU_CODENAME}" \
  "${repo_dir}/target-release.env" >/dev/null
grep -Fx "TARGET_ARCH=${TARGET_ARCH}" \
  "${repo_dir}/target-release.env" >/dev/null

MSYS_NO_PATHCONV=1 docker run --rm --interactive --network none \
  --platform "linux/${TARGET_ARCH}" \
  -e KUBERNETES_VERSION="${KUBERNETES_VERSION}" \
  -e KUBERNETES_DEB_VERSION="${KUBERNETES_DEB_VERSION}" \
  -v "${docker_repo_dir}:/repo:ro" \
  "ubuntu:${UBUNTU_VERSION_ID}" bash -se <<'CONTAINER'
export DEBIAN_FRONTEND=noninteractive
printf '#!/bin/sh\nexit 101\n' >/usr/sbin/policy-rc.d
chmod +x /usr/sbin/policy-rc.d
printf 'deb [trusted=yes] file:/repo ./\n' >/tmp/offline.list
apt_opts=(
  -o 'Dir::Etc::sourcelist=/tmp/offline.list'
  -o 'Dir::Etc::sourceparts=-'
  -o 'APT::Get::List-Cleanup=0'
  -o 'Acquire::Languages=none'
  -o 'APT::Sandbox::User=root'
)
apt-get "${apt_opts[@]}" update
apt-get "${apt_opts[@]}" install -y \
  containerd.io \
  "kubelet=${KUBERNETES_DEB_VERSION}" \
  "kubeadm=${KUBERNETES_DEB_VERSION}" \
  "kubectl=${KUBERNETES_DEB_VERSION}" \
  conntrack socat ebtables ipset iproute2 iptables \
  open-iscsi nfs-common cryptsetup dmsetup \
  ca-certificates curl gpg openssl openssh-client rsync jq python3-minimal
kubeadm version -o short | grep -Fx "${KUBERNETES_VERSION}"
dpkg-query -W -f='${Package} ${Version} ${Architecture}\n' \
  containerd.io kubelet kubeadm kubectl open-iscsi nfs-common
CONTAINER

log "offline package installation check passed"
