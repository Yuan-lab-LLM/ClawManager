#!/usr/bin/env bash

# Build-host only. Never source this library from the target installer.
ensure_bundle_image_local() {
  local image=$1
  local policy image_platform attempt pulled=false
  if ! docker image inspect "${image}" >/dev/null 2>&1; then
    policy=$(awk -F'|' -v wanted="${image}" '
      /^[[:space:]]*#/ { next }
      { gsub(/^[[:space:]]+|[[:space:]]+$/, "", $1)
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", $2) }
      $2 == wanted {
        if ($1 ~ /^(kubernetes|calico|longhorn)\./) infra=1
        else application=1
      }
      END { print (infra && !application ? "infrastructure" : "application") }
    ' "${IMAGE_LOCK_FILE}")
    [[ ${policy} == infrastructure ]] || die \
      "local application image is missing: ${image}; prepare it locally or run the optional pull-github-image.sh"
    [[ ${image} != *:latest && ${image} == *:* ]] || die \
      "infrastructure image must use a fixed tag: ${image}"
    for attempt in 1 2 3; do
      log "pulling locked infrastructure image ${image} (${attempt}/3)"
      if docker pull --platform "${TARGET_OS}/${TARGET_ARCH}" "${image}"; then
        pulled=true
        break
      fi
    done
    [[ ${pulled} == true ]] || die \
      "cannot download infrastructure image ${image}; check registry access or docker load an archive containing this exact tag, then rerun"
  else
    log "using cached local image ${image}"
  fi
  image_platform=$(docker image inspect "${image}" --format '{{.Os}}/{{.Architecture}}')
  [[ ${image_platform} == "${TARGET_OS}/${TARGET_ARCH}" ]] || die \
    "image ${image} is ${image_platform}, expected ${TARGET_OS}/${TARGET_ARCH}"
}
