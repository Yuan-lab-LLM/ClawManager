#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/common.sh
source "${SCRIPT_DIR}/scripts/lib/common.sh"

allow_missing_archive=false
if [[ ${1:-} == --allow-missing-archive ]]; then
  allow_missing_archive=true
elif [[ $# -gt 0 ]]; then
  die "usage: $0 [--allow-missing-archive]"
fi

load_versions
load_install_config
load_nodes
require_command awk
require_command grep
require_command sed
require_command sort
require_command tar

normalize_image() {
  # YAML files may be maintained on Windows. Do not let a CRLF line ending
  # become part of the image reference when validation runs on Linux.
  local image=${1%$'\r'}
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

[[ -f ${IMAGE_LOCK_FILE} ]] || die "missing image lock: ${IMAGE_LOCK_FILE}"

declare -A lock_keys=()
declare -A lock_images=()
while IFS='|' read -r key image extra; do
  [[ -z ${key} || ${key:0:1} == '#' ]] && continue
  [[ -z ${extra:-} ]] || die "invalid images.lock line for ${key}: expected key|image"
  [[ ${key} =~ ^[a-z0-9][a-z0-9._-]*$ ]] || die "invalid image key: ${key}"
  [[ -z ${lock_keys[${key}]+_} ]] || die "duplicate image key: ${key}"
  lock_keys[${key}]=${image}
  if [[ -z ${image} ]]; then
    [[ ${key} == runtime.* ]] || die "only optional runtime.* image values may be empty: ${key}"
    continue
  fi
  [[ ${image} =~ ^[^[:space:]]+:[^[:space:]/]+$ ]] || die \
    "image must have an explicit tag: ${image}"
  normalized_image=$(normalize_image "${image}")
  lock_images[${normalized_image}]=1
  [[ ${image} != *:latest ]] || warn "mutable :latest tag in image lock: ${image}"
done <"${IMAGE_LOCK_FILE}"

required_keys=(
  kubernetes.kube-apiserver kubernetes.kube-controller-manager
  kubernetes.kube-scheduler kubernetes.kube-proxy kubernetes.coredns
  kubernetes.pause kubernetes.etcd calico.operator calico.apiserver
  calico.cni calico.csi calico.key-cert-provisioner
  calico.kube-controllers calico.node calico.node-driver-registrar
  calico.pod2daemon-flexvol calico.typha
  platform.mysql platform.minio platform.redis platform.skill-scanner
  platform.clawmanager
)
required_keys+=(longhorn.manager longhorn.engine longhorn.instance-manager longhorn.share-manager)
for key in "${required_keys[@]}"; do
  [[ -n ${lock_keys[${key}]:-} ]] || die "missing required image key: ${key}"
done

runtime_key_count=0
for key in "${!lock_keys[@]}"; do
  [[ ${key} == runtime.* && -n ${lock_keys[${key}]} ]] || continue
  ((runtime_key_count += 1))
done
((runtime_key_count > 0)) || die "images.lock must select at least one non-empty runtime.* image"

[[ ${lock_keys[kubernetes.kube-apiserver]} == *:${KUBERNETES_VERSION} ]] || die \
  "kube-apiserver image does not match KUBERNETES_VERSION=${KUBERNETES_VERSION}"
[[ ${lock_keys[calico.operator]} == *:${CALICO_OPERATOR_VERSION} ]] || die \
  "Calico operator image does not match CALICO_OPERATOR_VERSION=${CALICO_OPERATOR_VERSION}"
[[ ${lock_keys[longhorn.manager]} == *:${LONGHORN_VERSION} ]] || die \
  "Longhorn manager image does not match LONGHORN_VERSION=${LONGHORN_VERSION}"

single_manifest=${MANIFEST_DIR}/clawmanager-single-node.yaml
cluster_manifest=${MANIFEST_DIR}/clawmanager-cluster.yaml
calico_operator_manifest=${MANIFEST_DIR}/calico/tigera-operator.yaml
calico_custom_manifest=${MANIFEST_DIR}/calico/custom-resources.yaml
for path in "${single_manifest}" "${cluster_manifest}" \
  "${calico_operator_manifest}" "${calico_custom_manifest}"; do
  [[ -f ${path} ]] || die "missing bundled manifest: ${path}"
done

[[ -f ${PACKAGE_DIR}/Packages.gz ]] || die "offline apt repository has no Packages.gz"
target_release_file=${PACKAGE_DIR}/target-release.env
[[ -f ${target_release_file} ]] || die "offline apt repository has no target-release.env"
grep -Fx "UBUNTU_VERSION_ID=${UBUNTU_VERSION_ID}" "${target_release_file}" >/dev/null || die \
  "offline apt repository does not target Ubuntu ${UBUNTU_VERSION_ID}"
grep -Fx "UBUNTU_CODENAME=${UBUNTU_CODENAME}" "${target_release_file}" >/dev/null || die \
  "offline apt repository does not target ${UBUNTU_CODENAME}"
grep -Fx "TARGET_ARCH=${TARGET_ARCH}" "${target_release_file}" >/dev/null || die \
  "offline apt repository architecture does not match ${TARGET_ARCH}"
for package_name in kubeadm kubelet kubectl containerd.io open-iscsi nfs-common \
  openssh-client python3-minimal rsync; do
  compgen -G "${PACKAGE_DIR}/${package_name}_*.deb" >/dev/null || die \
    "offline apt repository is missing ${package_name}"
done

if grep -nE '^[[:space:]]*imagePullPolicy:[[:space:]]*Always[[:space:]]*$' \
  "${single_manifest}" "${cluster_manifest}"; then
  die "offline manifest cannot use imagePullPolicy: Always"
fi

declare -A manifest_images=()
while IFS= read -r raw_image; do
  [[ -n ${raw_image} ]] || continue
  image="$(normalize_image "${raw_image}")"
  manifest_images[${image}]=1
done < <(
  awk '$1 == "image:" { gsub(/["\047]/, "", $2); print $2 }' \
    "${single_manifest}" "${cluster_manifest}" "${calico_operator_manifest}"
)

while IFS= read -r image; do
  [[ -n ${lock_images[${image}]:-} ]] || die "manifest image missing from images.lock: ${image}"
done < <(printf '%s\n' "${!manifest_images[@]}" | sort)

while IFS= read -r raw_image; do
  image="$(normalize_image "${raw_image}")"
  [[ -n ${lock_images[${image}]:-} ]] || die "runtime image ref missing from images.lock: ${image}"
done < <(
  awk '
    $1 == "-" && $2 == "name:" && $3 == "CLAWMANAGER_RUNTIME_IMAGE_REF" { wanted=1; next }
    wanted && $1 == "value:" { gsub(/["\047]/, "", $2); print $2; wanted=0 }
  ' "${single_manifest}" "${cluster_manifest}"
)

for key in "${!lock_keys[@]}"; do
  [[ ${key} == runtime.* ]] || continue
  image=${lock_keys[${key}]}
  [[ -n ${image} ]] || continue
  grep -Fq "${image}" "${single_manifest}" || die \
    "single-node YAML does not contain ${key} image: ${image}"
  grep -Fq "${image}" "${cluster_manifest}" || die \
    "cluster YAML does not contain ${key} image: ${image}"
done

if [[ ! -f ${IMAGE_ARCHIVE} ]]; then
  ${allow_missing_archive} && {
    warn "image archive not present yet: ${IMAGE_ARCHIVE}"
    log "manifest and image lock validation passed"
    exit 0
  }
  die "missing image archive: ${IMAGE_ARCHIVE}"
fi

tar -tf "${IMAGE_ARCHIVE}" >/dev/null || die "invalid image tar: ${IMAGE_ARCHIVE}"
manifest_json="$(tar -xOf "${IMAGE_ARCHIVE}" manifest.json 2>/dev/null || true)"
[[ -n ${manifest_json} ]] || die "image archive has no Docker-compatible manifest.json"

while IFS= read -r image; do
  normalized_image=$(normalize_image "${image}")
  archive_tag=${normalized_image}
  if [[ ${archive_tag} == docker.io/library/* ]]; then
    archive_tag=${archive_tag#docker.io/library/}
  elif [[ ${archive_tag} == docker.io/* ]]; then
    archive_tag=${archive_tag#docker.io/}
  fi
  grep -Fq "\"${image}\"" <<<"${manifest_json}" \
    || grep -Fq "\"${normalized_image}\"" <<<"${manifest_json}" \
    || grep -Fq "\"${archive_tag}\"" <<<"${manifest_json}" \
    || die "image tar missing RepoTag: ${image}"
done < <(read_image_lock)

archive_hash_file=${IMAGE_ARCHIVE}.sha256
if [[ -f ${archive_hash_file} ]]; then
  expected_hash=$(awk '{print $1}' "${archive_hash_file}")
  actual_hash=$(sha256_file "${IMAGE_ARCHIVE}")
  [[ ${actual_hash} == "${expected_hash}" ]] || die "image archive SHA256 mismatch"
else
  warn "missing image archive checksum: ${archive_hash_file}"
fi

log "bundle validation passed for profile=${PROFILE}, bundle=${BUNDLE_ID}"
