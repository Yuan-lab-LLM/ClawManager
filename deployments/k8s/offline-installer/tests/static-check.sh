#!/usr/bin/env bash

set -Eeuo pipefail

if ! python3 --version >/dev/null 2>&1; then
  python3() { python "$@"; }
  export -f python3
fi

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALLER_ROOT="$(cd "${TEST_DIR}/.." && pwd)"
TEMP_ROOT=$(mktemp -d)
trap 'rm -rf -- "${TEMP_ROOT}"' EXIT

mkdir -p "${TEMP_ROOT}/manifests/calico" "${TEMP_ROOT}/packages" \
  "${TEMP_ROOT}/images" "${TEMP_ROOT}/config"
cp "${INSTALLER_ROOT}/manifests/clawmanager-single-node.yaml" \
  "${TEMP_ROOT}/manifests/clawmanager-single-node.yaml"
cp "${INSTALLER_ROOT}/manifests/clawmanager-cluster.yaml" \
  "${TEMP_ROOT}/manifests/clawmanager-cluster.yaml"
cp "${INSTALLER_ROOT}/images/images.lock" "${TEMP_ROOT}/images/images.lock"
cp "${INSTALLER_ROOT}/config/install.env.example" "${TEMP_ROOT}/config/install.env"
cp "${INSTALLER_ROOT}/config/nodes.conf.example" "${TEMP_ROOT}/config/nodes.conf"

cat >"${TEMP_ROOT}/manifests/calico/tigera-operator.yaml" <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tigera-operator
spec:
  template:
    spec:
      containers:
        - name: tigera-operator
          image: quay.io/tigera/operator:v1.42.3
EOF
cat >"${TEMP_ROOT}/manifests/calico/custom-resources.yaml" <<'EOF'
apiVersion: operator.tigera.io/v1
kind: Installation
metadata:
  name: default
spec:
  calicoNetwork:
    ipPools:
      - cidr: 10.244.0.0/16
EOF

for package_name in kubeadm kubelet kubectl containerd.io open-iscsi nfs-common \
  openssh-client python3-minimal rsync; do
  touch "${TEMP_ROOT}/packages/${package_name}_test_amd64.deb"
done
touch "${TEMP_ROOT}/packages/Packages.gz"
source "${INSTALLER_ROOT}/ubuntu24.04/versions.env"
printf 'UBUNTU_VERSION_ID=%s\nUBUNTU_CODENAME=%s\nTARGET_ARCH=%s\n' \
  "${UBUNTU_VERSION_ID}" "${UBUNTU_CODENAME}" "${TARGET_ARCH}" \
  >"${TEMP_ROOT}/packages/target-release.env"

grep -Fxq 'UBUNTU_VERSION_ID=22.04' "${INSTALLER_ROOT}/ubuntu22.04/versions.env"
grep -Fxq 'UBUNTU_CODENAME=jammy' "${INSTALLER_ROOT}/ubuntu22.04/versions.env"
grep -Fxq 'UBUNTU_VERSION_ID=24.04' "${INSTALLER_ROOT}/ubuntu24.04/versions.env"
grep -Fxq 'UBUNTU_CODENAME=noble' "${INSTALLER_ROOT}/ubuntu24.04/versions.env"
grep -Fxq 'BUNDLE_REVISION=2' "${INSTALLER_ROOT}/ubuntu24.04/versions.env"
grep -Fq -- '-r2' "${INSTALLER_ROOT}/ubuntu24.04/versions.env"
grep -Fq -- '--ubuntu-version 22.04' \
  "${INSTALLER_ROOT}/ubuntu22.04/build-offline-bundle.sh"
grep -Fq -- '--ubuntu-version 24.04' \
  "${INSTALLER_ROOT}/ubuntu24.04/build-offline-bundle.sh"
grep -Fq 'clawmanager-images-*.tar' "${INSTALLER_ROOT}/build-offline-bundle.sh"
grep -Fq 'runtime-images-*.tar' "${INSTALLER_ROOT}/build-offline-bundle.sh"
grep -Fq 'platform-base-images*.tar' "${INSTALLER_ROOT}/build-offline-bundle.sh"
grep -Fq -- '--release-tag' "${INSTALLER_ROOT}/build-offline-bundle.sh"
grep -Fq 'ubuntu${UBUNTU_VERSION_ID}-r${BUNDLE_REVISION}' \
  "${INSTALLER_ROOT}/build-offline-bundle.sh"
grep -Fq 'save-images-from-lock.sh' "${INSTALLER_ROOT}/build-offline-bundle.sh"
grep -Fq -- '--group' "${INSTALLER_ROOT}/save-images-from-lock.sh"
grep -Fq 'ensure_bundle_image_local' "${INSTALLER_ROOT}/build-offline-bundle.sh"
! grep -Fq 'docker pull' "${INSTALLER_ROOT}/save-images-from-lock.sh"
grep -Fq 'preflight.sh' "${INSTALLER_ROOT}/build-offline-bundle.sh"
grep -Fq 'version = 3' "${INSTALLER_ROOT}/scripts/lib/node.sh"
grep -Fq "io.containerd.cri.v1.runtime" "${INSTALLER_ROOT}/scripts/lib/node.sh"
grep -Fq 'ctr plugins ls' "${INSTALLER_ROOT}/scripts/lib/node.sh"
grep -Fq 'systemctl restart containerd' "${INSTALLER_ROOT}/scripts/lib/kubernetes.sh"
test ! -d "${INSTALLER_ROOT}/ubuntu22.04/images"
test ! -d "${INSTALLER_ROOT}/ubuntu24.04/images"
test ! -d "${INSTALLER_ROOT}/ubuntu22.04/manifests"
test ! -d "${INSTALLER_ROOT}/ubuntu24.04/manifests"

python3 "${INSTALLER_ROOT}/scripts/render-bundle-manifests.py" "${TEMP_ROOT}"

grep -Fxq \
  'platform.minio|minio/minio:RELEASE.2025-09-07T16-13-09Z' \
  "${TEMP_ROOT}/images/images.lock"
for optional_key in \
  runtime.openclaw.pro runtime.hermes.pro \
  runtime.dsh.pro runtime.dsh.lite \
  runtime.opencode.pro runtime.opencode.lite; do
  grep -Fxq "${optional_key}|" "${TEMP_ROOT}/images/images.lock"
done
for manifest in "${TEMP_ROOT}/manifests/"clawmanager-*.yaml; do
  ! grep -qE '^\s*imagePullPolicy:\s*Always\s*$' "${manifest}"
  grep -Fq 'image: minio/minio:RELEASE.2025-09-07T16-13-09Z' "${manifest}"
done

render_case=${TEMP_ROOT}/render-case
mkdir -p "${render_case}/manifests" "${render_case}/images"
cp "${INSTALLER_ROOT}/manifests/clawmanager-single-node.yaml" \
  "${render_case}/manifests/clawmanager-single-node.yaml"
cp "${INSTALLER_ROOT}/manifests/clawmanager-cluster.yaml" \
  "${render_case}/manifests/clawmanager-cluster.yaml"
cp "${INSTALLER_ROOT}/images/images.lock" "${render_case}/images/images.lock"
sed -i \
  -e 's#^platform.clawmanager|clawmanager:.*$#platform.clawmanager|clawmanager:test-2026.9.15#' \
  -e 's#^runtime.openclaw.pro|.*$#runtime.openclaw.pro|openclaw-pro:test-2026.9.15#' \
  -e 's#^runtime.hermes.lite|.*$#runtime.hermes.lite|#' \
  -e 's#^runtime.dsh.pro|.*$#runtime.dsh.pro|dsh-pro:test-2026.9.15#' \
  -e 's#^runtime.opencode.lite|.*$#runtime.opencode.lite|opencode-lite:test-2026.9.15#' \
  "${render_case}/images/images.lock"
python3 "${INSTALLER_ROOT}/scripts/render-bundle-manifests.py" "${render_case}"
for manifest in "${render_case}/manifests/"clawmanager-*.yaml; do
  grep -Fq 'image: clawmanager:test-2026.9.15' "${manifest}"
  grep -Fq "'openclaw-pro:test-2026.9.15'" "${manifest}"
  grep -Fq "'dsh-pro:test-2026.9.15'" "${manifest}"
  grep -Fq "'opencode-lite:test-2026.9.15'" "${manifest}"
  ! grep -Fq 'hermes-lite:2026.8.24' "${manifest}"
  ! grep -Eq '^  name: hermes-runtime$' "${manifest}"
  runtime_sql=$(sed -n \
    '/^  099_set_offline_runtime_images.sql: |$/,/^---$/p' "${manifest}")
  ! grep -Fq 'is_enabled' <<<"${runtime_sql}"
done

fake_bin=${TEMP_ROOT}/fake-bin
image_output=${TEMP_ROOT}/image-output
mkdir -p "${fake_bin}" "${image_output}"
cat >"${fake_bin}/docker" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
case "${1:-}" in
  info)
    exit 0
    ;;
  image)
    [[ ${2:-} == inspect ]]
    if [[ ${3:-} == "${FAKE_MISSING_IMAGE:-__none__}" ]]; then
      exit 1
    fi
    if [[ ${4:-} == --format ]]; then
      printf 'linux/amd64\n'
    fi
    ;;
  save)
    shift
    [[ ${1:-} == --output ]]
    archive=$2
    shift 2
    python3 - "${archive}" "$@" <<'PY'
import io
import json
import sys
import tarfile

archive, *images = sys.argv[1:]
payload = json.dumps([{"Config": "config.json", "RepoTags": [image], "Layers": []}
                      for image in images]).encode()
with tarfile.open(archive, "w") as output:
    info = tarfile.TarInfo("manifest.json")
    info.size = len(payload)
    output.addfile(info, io.BytesIO(payload))
PY
    ;;
  *)
    printf 'unexpected fake docker call: %s\n' "$*" >&2
    exit 1
    ;;
esac
EOF
chmod +x "${fake_bin}/docker"
PATH="${fake_bin}:${PATH}" IMAGE_LOCK_FILE="${render_case}/images/images.lock" \
  bash "${INSTALLER_ROOT}/save-images-from-lock.sh" \
    --release-tag test-2026.9.15 --group clawmanager,runtime \
    --output "${image_output}"
test -f "${image_output}/clawmanager-images-test-2026.9.15.tar"
test -f "${image_output}/runtime-images-test-2026.9.15.tar"
test ! -e "${image_output}/platform-base-images-test-2026.9.15.tar"
python3 "${INSTALLER_ROOT}/scripts/verify-image-archive.py" \
  "${image_output}/clawmanager-images-test-2026.9.15.tar" \
  clawmanager:test-2026.9.15
missing_log=${TEMP_ROOT}/missing-image.log
if PATH="${fake_bin}:${PATH}" \
  FAKE_MISSING_IMAGE=clawmanager:test-2026.9.15 \
  IMAGE_LOCK_FILE="${render_case}/images/images.lock" \
  bash "${INSTALLER_ROOT}/save-images-from-lock.sh" \
    --release-tag missing-test --group clawmanager \
    --output "${image_output}/missing" >"${missing_log}" 2>&1; then
  echo 'missing local image unexpectedly passed export' >&2
  exit 1
fi
grep -Fq 'local image is missing: clawmanager:test-2026.9.15' "${missing_log}"

VERSIONS_FILE="${INSTALLER_ROOT}/ubuntu24.04/versions.env" \
INSTALL_CONFIG_FILE="${TEMP_ROOT}/config/install.env" \
NODES_CONFIG_FILE="${TEMP_ROOT}/config/nodes.conf" \
IMAGE_LOCK_FILE="${TEMP_ROOT}/images/images.lock" \
IMAGE_ARCHIVE="${TEMP_ROOT}/images/missing.tar" \
PACKAGE_DIR="${TEMP_ROOT}/packages" \
MANIFEST_DIR="${TEMP_ROOT}/manifests" \
  bash "${INSTALLER_ROOT}/verify-bundle.sh" --allow-missing-archive

(
  export INSTALL_CONFIG_FILE="${TEMP_ROOT}/config/install.env"
  export VERSIONS_FILE="${INSTALLER_ROOT}/ubuntu24.04/versions.env"
  export NODES_CONFIG_FILE="${TEMP_ROOT}/config/nodes.conf"
  export IMAGE_LOCK_FILE="${TEMP_ROOT}/images/images.lock"
  export MANIFEST_DIR="${TEMP_ROOT}/manifests"
  # shellcheck source=../scripts/lib/common.sh
  source "${INSTALLER_ROOT}/scripts/lib/common.sh"
  # shellcheck source=../scripts/lib/platform.sh
  source "${INSTALLER_ROOT}/scripts/lib/platform.sh"
  load_versions
  load_install_config
  load_nodes
  ! read_image_lock | grep -q '^$'
  ! read_image_lock | grep -q '^runtime\.'
  if image_for_key runtime.openclaw.pro >/dev/null 2>&1; then
    echo 'empty optional Runtime key unexpectedly resolved to an image' >&2
    exit 1
  fi
  # shellcheck disable=SC2034 # Consumed by the sourced platform library.
  STATE_DIR="${TEMP_ROOT}/state"
  # shellcheck disable=SC2034 # Consumed by the sourced platform library.
  LONGHORN_DATA_PATH=/mnt/longhorn-test
  # shellcheck disable=SC2034 # Consumed by the sourced platform library.
  LONGHORN_REPLICAS=2
  rendered=$(render_platform_manifest)
  grep -Fq 'mnt/longhorn-test"' "${rendered}"
  grep -Fq 'default-replica-count: "2"' "${rendered}"
  grep -Fq 'numberOfReplicas: "2"' "${rendered}"
)

printf 'offline installer static checks passed\n'
