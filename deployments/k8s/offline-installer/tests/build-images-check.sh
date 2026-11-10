#!/usr/bin/env bash
set -Eeuo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${ROOT}/scripts/lib/common.sh"
source "${ROOT}/scripts/lib/build-images.sh"
TARGET_OS=linux
TARGET_ARCH=amd64
TEST_CASE=cached
pull_count=0
docker() {
  if [[ $1 == pull ]]; then
    pull_count=$((pull_count + 1))
    [[ $2 == --platform && $3 == linux/amd64 ]] || return 9
    case ${TEST_CASE} in
      missing) return 0 ;;
      retry) ((pull_count >= 2)) ;;
      *) return 1 ;;
    esac
  elif [[ ${4:-} == --format ]]; then
    if [[ ${TEST_CASE} == wrong_arch ]]; then
      printf 'linux/arm64\n'
    else
      printf 'linux/amd64\n'
    fi
  else
    [[ ${TEST_CASE} == cached || ${TEST_CASE} == wrong_arch ]]
  fi
}
ensure_bundle_image_local registry.k8s.io/kube-apiserver:v1.36.4
((pull_count == 0))
TEST_CASE=missing
for image in registry.k8s.io/kube-apiserver:v1.36.4 docker.io/calico/node:v3.32.1 docker.io/longhornio/longhorn-manager:v1.12.0; do
  ensure_bundle_image_local "${image}"
done
((pull_count == 3))
TEST_CASE=retry
pull_count=0
ensure_bundle_image_local registry.k8s.io/kube-apiserver:v1.36.4
((pull_count == 2))
TEST_CASE=missing
if (ensure_bundle_image_local clawmanager:2026.8.27); then
  die 'missing application image unexpectedly passed'
fi
TEST_CASE=failed
if (ensure_bundle_image_local registry.k8s.io/kube-apiserver:v1.36.4); then
  die 'failed pull unexpectedly passed'
fi
TEST_CASE=wrong_arch
if (ensure_bundle_image_local registry.k8s.io/kube-apiserver:v1.36.4); then
  die 'incorrect image architecture unexpectedly passed'
fi
printf 'build image checks passed\n'
