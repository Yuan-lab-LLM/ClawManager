#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"

output_dir=${1:?usage: build-offline-packages.sh OUTPUT_DIR}
load_versions
require_command docker
mkdir -p "${output_dir}"
docker_volume_source=${output_dir}
case "$(uname -s)" in
  MINGW*|MSYS*)
    docker_volume_source=$(cd "${output_dir}" && pwd -W)
    ;;
esac

log "building the Ubuntu ${UBUNTU_VERSION_ID} (${UBUNTU_CODENAME}) offline apt repository"
MSYS_NO_PATHCONV=1 docker run --rm --interactive --platform "linux/${TARGET_ARCH}" \
  -e KUBERNETES_MINOR="${KUBERNETES_MINOR}" \
  -e KUBERNETES_DEB_VERSION="${KUBERNETES_DEB_VERSION}" \
  -e TARGET_ARCH="${TARGET_ARCH}" \
  -e UBUNTU_CODENAME="${UBUNTU_CODENAME}" \
  -v "${docker_volume_source}:/out" \
  "ubuntu:${UBUNTU_VERSION_ID}" bash -se <<'CONTAINER'
export DEBIAN_FRONTEND=noninteractive
# Ubuntu's container image deletes downloaded debs after apt-get install. Keep
# bootstrap dependencies so the resulting repository also works on a fresh,
# disconnected Ubuntu host.
rm -f /etc/apt/apt.conf.d/docker-clean
apt-get update
apt-get --download-only --reinstall install -y ca-certificates curl gpg dpkg-dev
apt-get install -y ca-certificates curl gpg dpkg-dev
install -m 0755 -d /etc/apt/keyrings
curl --fail --silent --show-error --location --retry 5 --retry-all-errors \
  --connect-timeout 15 --max-time 180 https://download.docker.com/linux/ubuntu/gpg \
  | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
chmod a+r /etc/apt/keyrings/docker.gpg
printf '%s\n' \
  "deb [arch=${TARGET_ARCH} signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu ${UBUNTU_CODENAME} stable" \
  >/etc/apt/sources.list.d/docker.list
curl --fail --silent --show-error --location --retry 5 --retry-all-errors \
  --connect-timeout 15 --max-time 180 \
  "https://pkgs.k8s.io/core:/stable:/${KUBERNETES_MINOR}/deb/Release.key" \
  | gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
printf '%s\n' \
  "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/${KUBERNETES_MINOR}/deb/ /" \
  >/etc/apt/sources.list.d/kubernetes.list
apt-get update
apt-get --download-only --reinstall install -y \
  containerd.io \
  "kubelet=${KUBERNETES_DEB_VERSION}" \
  "kubeadm=${KUBERNETES_DEB_VERSION}" \
  "kubectl=${KUBERNETES_DEB_VERSION}" \
  conntrack socat ebtables ipset iproute2 iptables \
  open-iscsi nfs-common cryptsetup dmsetup \
  ca-certificates curl gpg openssl openssh-client rsync jq python3-minimal
cp /var/cache/apt/archives/*.deb /out/
cd /out
dpkg-scanpackages . /dev/null | gzip -9 >Packages.gz
CONTAINER

printf 'UBUNTU_VERSION_ID=%s\nUBUNTU_CODENAME=%s\nTARGET_ARCH=%s\n' \
  "${UBUNTU_VERSION_ID}" "${UBUNTU_CODENAME}" "${TARGET_ARCH}" \
  >"${output_dir}/target-release.env"

log "offline apt repository is ready: ${output_dir}"
