#!/usr/bin/env bash

set -Eeuo pipefail

initialize_control_plane() {
  local control_plane_ip=$1
  local hostname=$2

  if [[ -f /etc/kubernetes/admin.conf ]] && node_step_marked local kubeadm_init_complete; then
    log "Kubernetes control plane is already initialized"
    return
  fi

  # The Ubuntu kubelet package creates /etc/kubernetes/manifests and may add
  # a .kubelet-keep placeholder before kubeadm runs. Only an admin kubeconfig
  # or a real static-pod manifest represents partial kubeadm state.
  local partial_kubeadm_state=false
  if [[ -f /etc/kubernetes/admin.conf ]] \
    || compgen -G '/etc/kubernetes/manifests/*.yaml' >/dev/null; then
    partial_kubeadm_state=true
  fi
  if ${partial_kubeadm_state} && ! node_step_marked local kubeadm_init_started; then
    die "partial kubeadm state is not owned by this installer; refusing cleanup"
  fi

  if node_step_marked local kubeadm_init_started && ! node_step_marked local kubeadm_init_complete; then
    warn "cleaning an incomplete kubeadm init created by this installer"
    kubeadm reset -f || true
    rm -rf /etc/cni/net.d
    # containerd 2.3 can exit when kubeadm reset removes the watched CNI
    # directory. Restart synchronously so kubeadm cannot race systemd's
    # delayed automatic restart and hit a refused CRI socket.
    systemctl restart containerd
    systemctl is-active --quiet containerd
    ctr plugins ls | grep -Eq '^io\.containerd\.grpc\.v1[[:space:]]+cri[[:space:]].*[[:space:]]ok[[:space:]]*$'
  fi

  log "initializing Kubernetes ${KUBERNETES_VERSION} on ${control_plane_ip}"
  mark_node_step local kubeadm_init_started
  local config_file=${STATE_DIR}/kubeadm-init.yaml
  mkdir -p "${STATE_DIR}"
  cat >"${config_file}" <<EOF
apiVersion: kubeadm.k8s.io/v1beta4
kind: InitConfiguration
localAPIEndpoint:
  advertiseAddress: ${control_plane_ip}
  bindPort: 6443
nodeRegistration:
  criSocket: unix:///run/containerd/containerd.sock
  imagePullPolicy: Never
  kubeletExtraArgs:
    - name: node-ip
      value: ${control_plane_ip}
---
apiVersion: kubeadm.k8s.io/v1beta4
kind: ClusterConfiguration
kubernetesVersion: ${KUBERNETES_VERSION}
controlPlaneEndpoint: ${control_plane_ip}:6443
networking:
  podSubnet: ${POD_CIDR}
  serviceSubnet: ${SERVICE_CIDR}
  dnsDomain: cluster.local
apiServer:
  certSANs:
    - ${control_plane_ip}
    - ${hostname}
imageRepository: registry.k8s.io
---
apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
cgroupDriver: systemd
failSwapOn: true
EOF

  kubeadm init --config "${config_file}"
  mkdir -p /root/.kube
  install -m 600 /etc/kubernetes/admin.conf /root/.kube/config
  mark_node_step local kubeadm_init_complete
}

create_join_material() {
  local control_plane_ip=$1
  JOIN_TOKEN=$(kubeadm token create --ttl 24h)
  CA_CERT_HASH="sha256:$(openssl x509 -pubkey -in /etc/kubernetes/pki/ca.crt \
    | openssl rsa -pubin -outform der 2>/dev/null \
    | openssl dgst -sha256 -hex | awk '{print $2}')"
  JOIN_ENDPOINT=${control_plane_ip}:6443
}

join_worker() {
  local index=$1
  local control_plane_ip=$2
  local target=${NODE_TARGETS[index]}
  local hostname=${NODE_HOSTNAMES[index]}
  local node_ip=${RESOLVED_NODE_IPS[index]}

  if node_step_marked "${target}" kubeadm_join_complete \
    && node_capture "${target}" "test -f /etc/kubernetes/kubelet.conf"; then
    if KUBECONFIG=/etc/kubernetes/admin.conf kubectl get node "${hostname}" >/dev/null 2>&1; then
      log "worker ${hostname} is already joined"
      return
    fi
    warn "worker ${hostname} has local join state but is absent from the cluster; resetting installer-owned state"
    node_bash "${target}" "rm -f '${STATE_DIR}/kubeadm_join_complete'"
  fi

  local join_token_quoted ca_hash_quoted endpoint_quoted
  join_token_quoted=$(shell_quote "${JOIN_TOKEN}")
  ca_hash_quoted=$(shell_quote "${CA_CERT_HASH}")
  endpoint_quoted=$(shell_quote "${JOIN_ENDPOINT}")

  log "joining worker ${hostname} (${node_ip})"
  node_bash "${target}" "
set -Eeuo pipefail
if [[ -f /etc/kubernetes/kubelet.conf && ! -f '${STATE_DIR}/kubeadm_join_started' ]]; then
  echo 'worker kubelet state exists without a completed installer marker' >&2
  exit 49
fi
if [[ -f '${STATE_DIR}/kubeadm_join_started' && ! -f '${STATE_DIR}/kubeadm_join_complete' ]]; then
  kubeadm reset -f || true
  rm -rf /etc/cni/net.d
  systemctl restart containerd
  systemctl is-active --quiet containerd
  ctr plugins ls | grep -Eq '^io\.containerd\.grpc\.v1[[:space:]]+cri[[:space:]].*[[:space:]]ok[[:space:]]*$'
fi
mkdir -p '${STATE_DIR}'
chmod 700 '${STATE_DIR}'
umask 077
touch '${STATE_DIR}/kubeadm_join_started'
cat >'${STATE_DIR}/kubeadm-join.yaml' <<EOF
apiVersion: kubeadm.k8s.io/v1beta4
kind: JoinConfiguration
discovery:
  bootstrapToken:
    apiServerEndpoint: ${endpoint_quoted}
    token: ${join_token_quoted}
    caCertHashes:
      - ${ca_hash_quoted}
nodeRegistration:
  criSocket: unix:///run/containerd/containerd.sock
  imagePullPolicy: Never
  name: ${hostname}
  kubeletExtraArgs:
    - name: node-ip
      value: ${node_ip}
EOF
kubeadm join --config '${STATE_DIR}/kubeadm-join.yaml'
touch '${STATE_DIR}/kubeadm_join_complete'
"
}

install_calico() {
  local operator_manifest=${MANIFEST_DIR}/calico/tigera-operator.yaml
  local custom_manifest=${MANIFEST_DIR}/calico/custom-resources.yaml
  local rendered_custom=${STATE_DIR}/calico-custom-resources.yaml
  [[ -f ${operator_manifest} && -f ${custom_manifest} ]] || die "Calico manifests are missing"

  mkdir -p "${STATE_DIR}"
  python3 - "${custom_manifest}" "${rendered_custom}" "${POD_CIDR}" <<'PY'
from pathlib import Path
import re
import sys

source, destination, pod_cidr = sys.argv[1:]
text = Path(source).read_text(encoding="utf-8")
text, count = re.subn(r"(?m)^(\s*cidr:)\s*[^\s]+\s*$", rf"\1 {pod_cidr}", text, count=1)
if count != 1:
    raise SystemExit("Calico custom resources must contain exactly one configurable pool CIDR")
Path(destination).write_text(text, encoding="utf-8")
PY

  log "applying Calico ${CALICO_VERSION}"
  KUBECONFIG=/etc/kubernetes/admin.conf kubectl apply --server-side -f "${operator_manifest}"
  local crd_deadline=$((SECONDS + 180))
  until KUBECONFIG=/etc/kubernetes/admin.conf kubectl get \
    crd/installations.operator.tigera.io >/dev/null 2>&1; do
    ((SECONDS < crd_deadline)) || die \
      "Calico operator did not create installations.operator.tigera.io within 3 minutes"
    sleep 2
  done
  KUBECONFIG=/etc/kubernetes/admin.conf kubectl wait \
    --for=condition=Established crd/installations.operator.tigera.io --timeout=3m
  KUBECONFIG=/etc/kubernetes/admin.conf kubectl apply -f "${rendered_custom}"
  mark_node_step local calico_applied
}

wait_for_all_nodes() {
  log "waiting up to ${NODE_READY_TIMEOUT} for all Kubernetes nodes"
  KUBECONFIG=/etc/kubernetes/admin.conf kubectl wait nodes \
    --all --for=condition=Ready --timeout="${NODE_READY_TIMEOUT}"

  local expected=${#NODE_HOSTNAMES[@]}
  local actual
  actual=$(KUBECONFIG=/etc/kubernetes/admin.conf kubectl get nodes \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | wc -l)
  ((actual == expected)) || die "expected ${expected} nodes, but Kubernetes reports ${actual}"
}

configure_node_roles() {
  local cp_index
  cp_index=$(control_plane_index)
  local cp_hostname=${NODE_HOSTNAMES[cp_index]}

  if [[ ${PROFILE} == single-node ]]; then
    log "configuring ${cp_hostname} for single-node scheduling and storage"
    KUBECONFIG=/etc/kubernetes/admin.conf kubectl taint node "${cp_hostname}" \
      node-role.kubernetes.io/control-plane- 2>/dev/null || true
    KUBECONFIG=/etc/kubernetes/admin.conf kubectl label node "${cp_hostname}" \
      clawmanager.io/storage-node=true --overwrite
    return
  fi

  KUBECONFIG=/etc/kubernetes/admin.conf kubectl label node "${cp_hostname}" \
    clawmanager.io/storage-node- 2>/dev/null || true
  local index
  for index in "${!NODE_ROLES[@]}"; do
    [[ ${NODE_ROLES[index]} == worker ]] || continue
    KUBECONFIG=/etc/kubernetes/admin.conf kubectl label node "${NODE_HOSTNAMES[index]}" \
      node-role.kubernetes.io/worker=worker --overwrite
    if [[ ${NODE_STORAGE[index]} == true ]]; then
      KUBECONFIG=/etc/kubernetes/admin.conf kubectl label node "${NODE_HOSTNAMES[index]}" \
        clawmanager.io/storage-node=true \
        node.longhorn.io/create-default-disk=true --overwrite
    else
      KUBECONFIG=/etc/kubernetes/admin.conf kubectl label node "${NODE_HOSTNAMES[index]}" \
        clawmanager.io/storage-node- \
        node.longhorn.io/create-default-disk- 2>/dev/null || true
    fi
  done
}
