#!/usr/bin/env bash
set -euo pipefail

candidate=${1:?candidate CM image required}
previous=${2:-}
image_profile=tools/k8s/single-node/ghcr-images.json
origin=https://127.0.0.1:30443
namespace=clawmanager-system
private=$(mktemp -d)
export KUBECONFIG="$private/kubeconfig"
cluster=clawmanager-smoke-${GITHUB_RUN_ID:-$$}-${GITHUB_RUN_ATTEMPT:-1}
context=kind-$cluster
owned=false

on_exit() {
  local status=$?
  trap - EXIT
  if [[ $owned == true ]] && (( status != 0 )); then
    kubectl --context "$context" -n "$namespace" get deployments,pods,pvc -o wide || true
    kubectl --context "$context" -n "$namespace" get events --sort-by=.lastTimestamp | tail -40 || true
    kubectl --context "$context" -n "$namespace" logs -l clawmanager.io/runtime-type -c slot-0 \
      --prefix --tail=25 --request-timeout=15s |
      sed -E -e 's#(https?://)[^ /@]+:[^ /@]+@#\1[redacted]@#g' \
        -e 's#[[:alnum:]_~./+-]{32,}#[redacted]#g' || true
  fi
  if [[ $owned == true ]]; then kind delete cluster --name "$cluster" || true; fi
  rm -rf -- "$private"
  exit "$status"
}
trap on_exit EXIT
if [[ $candidate != clawmanager:ci && ! $candidate =~ ^ghcr\.io/[^@]+@sha256:[0-9a-f]{64}$ ]]; then
  echo "expected immutable GHCR candidate image" >&2
  exit 1
fi
if [[ -n $previous && ! $previous =~ ^ghcr\.io/[^@]+@sha256:[0-9a-f]{64}$ ]]; then
  echo "expected immutable GHCR upgrade baseline image" >&2
  exit 1
fi
if kind get clusters | grep -Fxq "$cluster"; then
  echo "kind cluster $cluster already exists" >&2
  exit 1
fi

if [[ ${GITHUB_ACTIONS:-false} == true ]] &&
   [[ $(sysctl -n kernel.apparmor_restrict_unprivileged_userns 2>/dev/null || true) == 1 ]]; then
  sudo apparmor_parser -r tools/k8s/kind-elastic/bwrap.apparmor
  printf '%s\n' 'Loaded the bubblewrap user namespace profile on the CI runner'
fi

printf 'Candidate CM image: %s\n' "$candidate"
if [[ -n $previous ]]; then printf 'Previous CM image: %s\n' "$previous"; fi
jq -e '
  (keys == ["clawmanager-team-redis", "deepseek-harness-runtime", "hermes-runtime", "minio", "mysql",
            "openclaw-runtime", "opencode-runtime", "skill-scanner"])
  and all(.[]; test("^ghcr\\.io/[^@]+@sha256:[0-9a-f]{64}$"))
' "$image_profile" > /dev/null
jq . "$image_profile"

kube() {
  kubectl --context "$context" -n "$namespace" "$@"
}

verify_ready() {
  local expected=$1
  test "$(kube get deployment clawmanager-app -o jsonpath='{.spec.template.spec.containers[0].image}')" = "$expected"
  curl -kfsS --retry 8 --retry-all-errors --retry-delay 2 --max-time 12 "$origin/healthz"
  curl -kfsS --max-time 12 "$origin/api/v1/version" | jq -e '.data.version | length > 0'
  kube get service clawmanager-browser-origins -o json | jq -e '
    .spec.type == "NodePort" and ([.spec.ports[].nodePort] | sort) == [32100,32101]
    and all(.spec.ports[]; .targetPort == 8444)'
  local attempt
  for attempt in {1..15}; do
    if kube get endpointslices -l kubernetes.io/service-name=clawmanager-browser-origins -o json | jq -e '
      any(.items[]; any(.ports[]; .port == 8444) and any(.endpoints[]; .conditions.ready == true))'; then
      break
    fi
    if [[ $attempt == 15 ]]; then return 1; fi
    sleep 2
  done
  kube get deployment openclaw-runtime hermes-runtime opencode-runtime deepseek-harness-runtime -o json |
    jq -e --slurpfile profile "$image_profile" '(.items | length) == 4 and all(.items[];
      $profile[0][.metadata.name] as $image |
      .spec.template.metadata.labels["clawmanager.io/runtime-protocol"] == "container-slots-v1"
      and .spec.template.spec.hostUsers == false
      and any(.spec.template.spec.containers[]; .name == "slot-0" and .image == $image))'
  kube get pvc mysql-data minio-data redis-data clawmanager-workspaces clawmanager-observability -o json |
    jq -e '(.items | length) == 5 and all(.items[]; .status.phase == "Bound")'
}

install() {
  local image=$1 credentials=$2
  local manifest="$private/single-node.yaml"
  owned=true
  kind create cluster --name "$cluster" --config .github/scripts/k8s-smoke-kind.yaml --wait 180s
  python3 tools/k8s/kind-elastic/prepare_node.py --docker-context "$(docker context show)" --cluster "$cluster" \
    --node "$cluster-control-plane" --kubeconfig "$KUBECONFIG" --skip-private-registry --workspace-subdirectory
  if [[ $image == clawmanager:ci ]]; then
    kind load docker-image "$image" --name "$cluster"
  fi
  # The user-namespace preflight mounts all HostPaths. Put each one in its own
  # directory on the ext4 filesystem to support ID-mapped mounts.
  sed -E \
    -e 's|^([[:space:]]*path: /data/clawmanager/system/)([^/[:space:]]+)$|\1workspaces/data/\2|' \
    -e 's|^([[:space:]]*value: )"/data/clawreef"$|\1"/data/clawmanager/system/workspaces/data/users"|' \
    deployments/k8s/single-node/clawmanager.yaml > "$manifest"
  python3 tools/k8s/install_fresh.py --context "$context" \
    --manifest "$manifest" --credentials-dir "$credentials" \
    --images-file "$image_profile" --image "clawmanager-app=$image" \
    --browser-origin "$origin" --browser-ports 32100-32101
  verify_ready "$image"
}

export CM_SMOKE_PASSWORD=admin123
install "$candidate" "$private/fresh"
python3 e2e/kubernetes-install-smoke.py fresh --origin "$origin"
if [[ -n $previous ]]; then
  kind delete cluster --name "$cluster"
  owned=false
  install "$previous" "$private/upgrade"
  python3 e2e/kubernetes-install-smoke.py seed-upgrade --origin "$origin" --fixture "$private/fixture.json"
  python3 - "$context" "$namespace" "$candidate" <<'PY'
from functools import partial
import sys
from tools.k8s.install_fresh import kubectl, migration_window, mysql_admin

context, namespace, image = sys.argv[1:]
kube = partial(kubectl, context, "-n", namespace)
with migration_window(partial(mysql_admin, kube)):
    kube("set", "image", "deployment/clawmanager-app", f"clawmanager-app={image}")
    kube("rollout", "status", "deployment/clawmanager-app", "--timeout=300s")
PY
  verify_ready "$candidate"
  python3 e2e/kubernetes-install-smoke.py verify-upgrade --origin "$origin" --fixture "$private/fixture.json"
fi
