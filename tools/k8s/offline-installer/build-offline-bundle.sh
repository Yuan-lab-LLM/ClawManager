#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/common.sh
source "${SCRIPT_DIR}/scripts/lib/common.sh"
# shellcheck source=scripts/lib/build-images.sh
source "${SCRIPT_DIR}/scripts/lib/build-images.sh"

usage() {
  cat <<'EOF'
Usage: build-offline-bundle.sh [--release-tag TAG] [--output DIR]
                               [--reuse-image-bundle FILE]

Builds one Ubuntu amd64 offline bundle containing both the single-node and
cluster profiles. Ubuntu 24.04 is the default; use --ubuntu-version 22.04 to
build the separately validated Jammy bundle. Update images/images.lock before
running it; staged YAML and bootstrap SQL are rendered from that file.
Missing Kubernetes/Calico/Longhorn images are pulled at locked versions on
the build host. Application images must be supplied locally (for example
using save-images-from-lock.sh or the optional pull-github-image.sh).

--release-tag changes the distributable filename to:
clawmanager-k8s-offline-TAG-ubuntu<version>-r<revision>-linux-<arch>.tar.gz
If omitted, the existing BUNDLE_ID filename is retained for compatibility.

--reuse-image-bundle reuses the verified combined image archive from an
existing offline bundle whose images.lock is identical. This avoids Docker
load/save and is useful when only the Ubuntu package repository changes.
EOF
}

output_dir=${SCRIPT_DIR}/dist
ubuntu_version=24.04
reuse_image_bundle=
release_tag=
while (($# > 0)); do
  case "$1" in
    --release-tag)
      (($# >= 2)) || die "--release-tag requires a value"
      release_tag=$2
      shift 2
      ;;
    --output)
      (($# >= 2)) || die "--output requires a directory"
      output_dir=$2
      shift 2
      ;;
    --ubuntu-version)
      (($# >= 2)) || die "--ubuntu-version requires 22.04 or 24.04"
      ubuntu_version=$2
      shift 2
      ;;
    --reuse-image-bundle)
      (($# >= 2)) || die "--reuse-image-bundle requires a .tar.gz bundle"
      reuse_image_bundle=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

case "${ubuntu_version}" in
  22.04|24.04) ;;
  *) die "--ubuntu-version must be 22.04 or 24.04" ;;
esac
if [[ -n ${release_tag} && ! ${release_tag} =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
  die "--release-tag may contain only letters, numbers, dot, underscore, and dash"
fi
VERSIONS_FILE="${SCRIPT_DIR}/ubuntu${ubuntu_version}/versions.env"
load_versions
require_command curl
require_command sha256sum
require_command tar
require_command cmp
python_command=python3
if ! python3 --version >/dev/null 2>&1; then
  python_command=python
fi
require_command "${python_command}"
if [[ -z ${reuse_image_bundle} ]]; then
  require_command docker
  docker info >/dev/null 2>&1 || die "Docker daemon is unavailable"
else
  [[ -f ${reuse_image_bundle} ]] || die \
    "reuse image bundle does not exist: ${reuse_image_bundle}"
  reuse_image_bundle=$(cd "$(dirname "${reuse_image_bundle}")" && pwd)/$(basename "${reuse_image_bundle}")
  [[ ${reuse_image_bundle} == *.tar.gz ]] || die \
    "--reuse-image-bundle must point to a .tar.gz offline bundle"
fi

single_source=${SCRIPT_DIR}/manifests/clawmanager-single-node.yaml
cluster_source=${SCRIPT_DIR}/manifests/clawmanager-cluster.yaml
[[ -f ${single_source} && -f ${cluster_source} ]] || die "offline ClawManager manifests are missing"

work_parent=${SCRIPT_DIR}/ubuntu${ubuntu_version}
work_dir=$(mktemp -d "${work_parent}/.bundle-work.XXXXXX")
cleanup() {
  if [[ ${work_dir} == "${work_parent}/.bundle-work."* ]]; then
    rm -rf -- "${work_dir}"
  fi
}
trap cleanup EXIT

if [[ -n ${release_tag} ]]; then
  bundle_name=clawmanager-k8s-offline-${release_tag}-ubuntu${UBUNTU_VERSION_ID}-r${BUNDLE_REVISION}-linux-${TARGET_ARCH}
else
  bundle_name=clawmanager-k8s-offline-${BUNDLE_ID}-linux-${TARGET_ARCH}
fi
stage_root=${work_dir}/${bundle_name}
mkdir -p "${stage_root}/config" "${stage_root}/images/local" \
  "${stage_root}/manifests/calico" "${stage_root}/packages/${TARGET_ARCH}" \
  "${stage_root}/scripts/lib" "${output_dir}"

cp "${VERSIONS_FILE}" "${stage_root}/versions.env"
cp "${SCRIPT_DIR}/README.md" "${stage_root}/"
cp "${SCRIPT_DIR}/install.sh" "${SCRIPT_DIR}/prepare-ssh.sh" \
  "${SCRIPT_DIR}/preflight.sh" \
  "${SCRIPT_DIR}/verify-bundle.sh" "${stage_root}/"
cp "${SCRIPT_DIR}/scripts/lib/"*.sh "${stage_root}/scripts/lib/"
cp "${SCRIPT_DIR}/config/"*.example "${stage_root}/config/"
cp "${SCRIPT_DIR}/images/images.lock" "${stage_root}/images/"
cp "${single_source}" "${stage_root}/manifests/clawmanager-single-node.yaml"
cp "${cluster_source}" "${stage_root}/manifests/clawmanager-cluster.yaml"
chmod +x "${stage_root}/"*.sh "${stage_root}/scripts/lib/"*.sh

calico_operator_cache=${SCRIPT_DIR}/manifests/calico/tigera-operator.yaml
if [[ -f ${calico_operator_cache} ]]; then
  log "using bundled Calico ${CALICO_VERSION} operator manifest"
  cp "${calico_operator_cache}" \
    "${stage_root}/manifests/calico/tigera-operator.yaml"
else
  log "downloading Calico ${CALICO_VERSION} operator manifest"
  curl --fail --location --retry 3 --connect-timeout 15 --max-time 120 \
    "https://raw.githubusercontent.com/projectcalico/calico/${CALICO_VERSION}/manifests/tigera-operator.yaml" \
    --output "${stage_root}/manifests/calico/tigera-operator.yaml"
fi
cat >"${stage_root}/manifests/calico/custom-resources.yaml" <<EOF
apiVersion: operator.tigera.io/v1
kind: Installation
metadata:
  name: default
spec:
  registry: docker.io
  calicoNetwork:
    ipPools:
      - blockSize: 26
        cidr: 10.244.0.0/16
        encapsulation: VXLANCrossSubnet
        natOutgoing: Enabled
        nodeSelector: all()
    nodeAddressAutodetectionV4:
      kubernetes: NodeInternalIP
---
apiVersion: operator.tigera.io/v1
kind: APIServer
metadata:
  name: default
spec: {}
EOF

log "rendering staged manifests and Runtime catalog from images.lock"
"${python_command}" "${SCRIPT_DIR}/scripts/render-bundle-manifests.py" "${stage_root}"

package_cache=${SCRIPT_DIR}/ubuntu${UBUNTU_VERSION_ID}/packages/${TARGET_ARCH}
if [[ -f ${package_cache}/Packages.gz && -f ${package_cache}/target-release.env ]] \
  && compgen -G "${package_cache}/*.deb" >/dev/null; then
  log "using prebuilt Ubuntu ${UBUNTU_VERSION_ID} (${UBUNTU_CODENAME}) offline apt repository"
  cp -a "${package_cache}/." "${stage_root}/packages/${TARGET_ARCH}/"
else
  VERSIONS_FILE="${VERSIONS_FILE}" bash "${SCRIPT_DIR}/scripts/build-offline-packages.sh" \
    "${stage_root}/packages/${TARGET_ARCH}"
  mkdir -p "${package_cache}"
  cp -a "${stage_root}/packages/${TARGET_ARCH}/." "${package_cache}/"
fi

mapfile -t bundle_images < <(read_image_lock | awk '!seen[$0]++')
((${#bundle_images[@]} > 0)) || die "images.lock is empty"
image_archive=${stage_root}/images/clawmanager-images-linux-${TARGET_ARCH}.tar
if [[ -n ${reuse_image_bundle} ]]; then
  source_bundle_name=$(basename "${reuse_image_bundle}" .tar.gz)
  source_image_dir=${work_dir}/reuse/${source_bundle_name}/images
  source_image_name=clawmanager-images-linux-${TARGET_ARCH}.tar

  source_bundle_checksum=${reuse_image_bundle}.sha256
  if [[ -f ${source_bundle_checksum} ]]; then
    log "verifying source offline bundle checksum"
    (cd "$(dirname "${reuse_image_bundle}")" && \
      sha256sum -c "$(basename "${source_bundle_checksum}")")
  else
    log "warning: source bundle has no adjacent .sha256 file"
  fi

  log "extracting the verified combined image archive without Docker"
  mkdir -p "${work_dir}/reuse"
  tar -xzf "${reuse_image_bundle}" -C "${work_dir}/reuse" \
    "${source_bundle_name}/images/images.lock" \
    "${source_bundle_name}/images/${source_image_name}" \
    "${source_bundle_name}/images/${source_image_name}.sha256"
  cmp -s "${source_image_dir}/images.lock" "${SCRIPT_DIR}/images/images.lock" || die \
    "source bundle images.lock differs from the current images.lock"
  (cd "${source_image_dir}" && sha256sum -c "${source_image_name}.sha256")
  mv "${source_image_dir}/${source_image_name}" "${image_archive}"
  mv "${source_image_dir}/${source_image_name}.sha256" "${image_archive}.sha256"
  log "reused combined image archive from ${source_bundle_name}"
else
  # Input archives are authoritative for each release. Load every matching
  # category before checking the exact image references selected by the lock.
  for local_archive in \
    "${SCRIPT_DIR}/images/local/"clawmanager-images-*.tar \
    "${SCRIPT_DIR}/images/local/"runtime-images-*.tar \
    "${SCRIPT_DIR}/images/local/"platform-base-images*.tar; do
    [[ -e ${local_archive} ]] || continue
    log "loading image input archive: ${local_archive}"
    docker load --input "${local_archive}"
  done

  for image in "${bundle_images[@]}"; do
    ensure_bundle_image_local "${image}"
  done

  log "saving ${#bundle_images[@]} images into one shared-layer archive"
  docker save --output "${image_archive}" "${bundle_images[@]}"
  printf '%s  %s\n' "$(sha256sum "${image_archive}" | awk '{print $1}')" \
    "$(basename "${image_archive}")" >"${image_archive}.sha256"
fi

"${python_command}" "${SCRIPT_DIR}/scripts/verify-image-archive.py" \
  "${image_archive}" "${bundle_images[@]}"

temporary_config=${work_dir}/install.env
temporary_nodes=${work_dir}/nodes.conf
cp "${stage_root}/config/install.env.example" "${temporary_config}"
cp "${stage_root}/config/nodes.conf.example" "${temporary_nodes}"
INSTALL_CONFIG_FILE="${temporary_config}" \
NODES_CONFIG_FILE="${temporary_nodes}" \
  bash "${stage_root}/verify-bundle.sh"

archive_path=${output_dir}/${bundle_name}.tar.gz
log "creating ${archive_path}"
tar -C "${work_dir}" -czf "${archive_path}" "${bundle_name}"
printf '%s  %s\n' "$(sha256sum "${archive_path}" | awk '{print $1}')" \
  "$(basename "${archive_path}")" >"${archive_path}.sha256"
log "offline bundle is ready: ${archive_path}"
