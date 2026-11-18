#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/common.sh
source "${SCRIPT_DIR}/scripts/lib/common.sh"

usage() {
  cat <<'EOF'
Usage: save-images-from-lock.sh --release-tag TAG [--group GROUPS] [--output DIR]

Exports locally available images selected by images/images.lock. This command
never pulls images. GROUPS is all (default), clawmanager, runtime,
platform-base, or a comma-separated combination such as clawmanager,runtime.

Outputs:
  clawmanager-images-TAG.tar
  runtime-images-TAG.tar
  platform-base-images-TAG.tar
EOF
}

release_tag=
groups_argument=all
output_dir=${SCRIPT_DIR}/images/local
while (($# > 0)); do
  case "$1" in
    --release-tag)
      (($# >= 2)) || die "--release-tag requires a value"
      release_tag=$2
      shift 2
      ;;
    --group)
      (($# >= 2)) || die "--group requires a value"
      groups_argument=$2
      shift 2
      ;;
    --output)
      (($# >= 2)) || die "--output requires a directory"
      output_dir=$2
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

[[ ${release_tag} =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || die \
  "--release-tag is required and may contain only letters, numbers, dot, underscore, and dash"
[[ -f ${IMAGE_LOCK_FILE} ]] || die "missing image lock: ${IMAGE_LOCK_FILE}"

require_command docker
require_command sha256sum
python_command=python3
if ! python3 --version >/dev/null 2>&1; then
  python_command=python
fi
require_command "${python_command}"
docker info >/dev/null 2>&1 || die "Docker daemon is unavailable"

declare -A selected_groups=()
IFS=',' read -r -a requested_groups <<<"${groups_argument}"
((${#requested_groups[@]} > 0)) || die "--group cannot be empty"
for group in "${requested_groups[@]}"; do
  case "${group}" in
    all)
      ((${#requested_groups[@]} == 1)) || die "--group all cannot be combined with another group"
      selected_groups[clawmanager]=1
      selected_groups[runtime]=1
      selected_groups[platform-base]=1
      ;;
    clawmanager|runtime|platform-base)
      selected_groups[${group}]=1
      ;;
    *)
      die "unknown image group: ${group}"
      ;;
  esac
done

declare -a clawmanager_images=()
declare -a runtime_images=()
declare -a platform_base_images=()
declare -A seen_lock_keys=()
while IFS='|' read -r key image extra; do
  key=${key%$'\r'}
  image=${image%$'\r'}
  [[ -z ${key} || ${key:0:1} == '#' ]] && continue
  [[ -z ${extra:-} ]] || die "invalid images.lock line for ${key}: expected key|image"
  [[ ${key} =~ ^[a-z0-9][a-z0-9._-]*$ ]] || die "invalid image key: ${key}"
  [[ -z ${seen_lock_keys[${key}]+_} ]] || die "duplicate image key: ${key}"
  seen_lock_keys[${key}]=1
  if [[ -z ${image} ]]; then
    [[ ${key} == runtime.* ]] || die "only runtime.* image values may be empty: ${key}"
    continue
  fi
  [[ ${image} =~ ^[^[:space:]]+:[^[:space:]/]+$ ]] || die \
    "image must have an explicit tag: ${image}"
  case "${key}" in
    platform.clawmanager) clawmanager_images+=("${image}") ;;
    runtime.*) runtime_images+=("${image}") ;;
    *) platform_base_images+=("${image}") ;;
  esac
done <"${IMAGE_LOCK_FILE}"

deduplicate_array() {
  local -n source_array=$1
  local -A seen=()
  local -a unique=()
  local image
  for image in "${source_array[@]}"; do
    [[ -n ${seen[${image}]+_} ]] && continue
    seen[${image}]=1
    unique+=("${image}")
  done
  source_array=("${unique[@]}")
}

deduplicate_array clawmanager_images
deduplicate_array runtime_images
deduplicate_array platform_base_images

((${#clawmanager_images[@]} == 1)) || die \
  "platform.clawmanager must select exactly one image"
((${#platform_base_images[@]} > 0)) || die "platform-base group is empty"

declare -a temporary_archives=()
cleanup() {
  local archive
  for archive in "${temporary_archives[@]}"; do
    if [[ -n ${archive} && -f ${archive} ]]; then
      rm -f -- "${archive}"
    fi
  done
}
trap cleanup EXIT

mkdir -p "${output_dir}"

save_group() {
  local group=$1
  local array_name=$2
  local -n images=$array_name
  local output_name=${group}-images-${release_tag}.tar
  local output_path=${output_dir}/${output_name}
  local temporary_path=${output_path}.partial.$$
  local image image_platform

  ((${#images[@]} > 0)) || die \
    "${group} group has no active images in images.lock"
  for image in "${images[@]}"; do
    if ! docker image inspect "${image}" >/dev/null 2>&1; then
      die "local image is missing: ${image}; pull or build this exact tag before exporting"
    fi
    image_platform=$(docker image inspect "${image}" --format '{{.Os}}/{{.Architecture}}')
    [[ ${image_platform} == linux/amd64 ]] || die \
      "image ${image} is ${image_platform}, expected linux/amd64"
  done

  temporary_archives+=("${temporary_path}")
  log "saving ${#images[@]} ${group} image(s) to ${output_path}"
  docker save --output "${temporary_path}" "${images[@]}"
  "${python_command}" "${SCRIPT_DIR}/scripts/verify-image-archive.py" \
    "${temporary_path}" "${images[@]}"
  mv -f -- "${temporary_path}" "${output_path}"
  printf '%s  %s\n' "$(sha256sum "${output_path}" | awk '{print $1}')" \
    "${output_name}" >"${output_path}.sha256"
  log "created ${output_path}"
}

[[ -n ${selected_groups[clawmanager]:-} ]] && \
  save_group clawmanager clawmanager_images
[[ -n ${selected_groups[runtime]:-} ]] && save_group runtime runtime_images
[[ -n ${selected_groups[platform-base]:-} ]] && \
  save_group platform-base platform_base_images

log "selected image groups exported successfully"
