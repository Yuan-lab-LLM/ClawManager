#!/usr/bin/env bash

set -uo pipefail

NAMESPACE="${NAMESPACE:-clawmanager-system}"
CORE_DEPLOYMENT="${CORE_DEPLOYMENT:-clawmanager-app}"
GATEWAY_DEPLOYMENT="${GATEWAY_DEPLOYMENT:-clawmanager-northbound-gateway}"
GATEWAY_SERVICE="${GATEWAY_SERVICE:-clawmanager-northbound-gateway}"
NODE_PORT="${NODE_PORT:-30000}"
TEST_HOST="${TEST_HOST:-127.0.0.1}"

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
MANIFEST_SOURCE="$SCRIPT_DIR/deployments/k8s/northbound/gateway.yaml"
GATEWAY_MANIFEST=""
CORE_PATCH_MANIFEST=""

cleanup() {
  if [ -n "$GATEWAY_MANIFEST" ] && [ -f "$GATEWAY_MANIFEST" ]; then
    rm -f -- "$GATEWAY_MANIFEST"
  fi
  if [ -n "$CORE_PATCH_MANIFEST" ] && [ -f "$CORE_PATCH_MANIFEST" ]; then
    rm -f -- "$CORE_PATCH_MANIFEST"
  fi
}

trap cleanup EXIT

fail() {
  echo
  echo "================ 执行失败 ================"
  echo "$1"
  echo "=========================================="
  exit 1
}

check_secret_key() {
  local secret_name="$1"
  local key_name="$2"
  local escaped_key
  local value

  escaped_key=${key_name//./\\.}
  value=$(
    kubectl -n "$NAMESPACE" get secret "$secret_name" \
      -o "jsonpath={.data.${escaped_key}}" 2>/dev/null || true
  )

  [ -n "$value" ] || fail "Secret $secret_name 缺少字段 $key_name"
  echo "[字段存在] $secret_name/$key_name"
}

echo
echo "===== 1. 检查 kubectl 和项目文件 ====="

command -v kubectl >/dev/null 2>&1 || fail "找不到 kubectl 命令"
command -v curl >/dev/null 2>&1 || fail "找不到 curl 命令"
kubectl cluster-info >/dev/null 2>&1 || fail "kubectl 无法连接 Kubernetes 集群"
if [ -f "$MANIFEST_SOURCE" ]; then
  echo "使用仓库部署模板：$MANIFEST_SOURCE"
else
  echo "未找到仓库部署模板，将使用脚本内置的 Gateway Deployment 模板"
fi
kubectl get namespace "$NAMESPACE" >/dev/null 2>&1 || fail "Namespace $NAMESPACE 不存在"
kubectl -n "$NAMESPACE" get deployment "$CORE_DEPLOYMENT" >/dev/null 2>&1 || \
  fail "Core Deployment $CORE_DEPLOYMENT 不存在"

echo "kubectl 和项目文件检查通过"

echo
echo "===== 2. 获取当前 Core 镜像 ====="

CORE_IMAGE=$(
  kubectl -n "$NAMESPACE" get deployment "$CORE_DEPLOYMENT" \
    -o jsonpath='{.spec.template.spec.containers[?(@.name=="clawmanager-app")].image}'
)

if [ -z "$CORE_IMAGE" ]; then
  CORE_IMAGE=$(
    kubectl -n "$NAMESPACE" get deployment "$CORE_DEPLOYMENT" \
      -o jsonpath='{.spec.template.spec.containers[0].image}'
  )
fi

[ -n "$CORE_IMAGE" ] || fail "无法取得 Core 镜像地址"
echo "Core 镜像：$CORE_IMAGE"

echo
echo "===== 3. 检查 Core 服务状态 ====="

kubectl -n "$NAMESPACE" rollout status deployment/"$CORE_DEPLOYMENT" --timeout=2m || \
  fail "Core Deployment 当前未就绪，请先处理 Core"

echo
echo "===== 4. 检查镜像中是否包含 Gateway 程序 ====="

CORE_POD=$(
  kubectl -n "$NAMESPACE" get pods -l app=clawmanager-app \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null
)

[ -n "$CORE_POD" ] || fail "没有找到 app=clawmanager-app 的 Core Pod"
echo "检查 Pod：$CORE_POD"

kubectl -n "$NAMESPACE" exec "$CORE_POD" -c clawmanager-app -- \
  /bin/sh -c 'test -x /usr/local/bin/clawreef-northbound-gateway' || \
  fail "当前 Core 镜像不包含 /usr/local/bin/clawreef-northbound-gateway，需要先升级 ClawManager 镜像"

echo "Gateway 程序存在"

echo
echo "===== 5. 检查 Gateway 必需的 Secret ====="

REQUIRED_SECRETS="
clawmanager-northbound-secrets
clawmanager-northbound-gateway-tls
clawmanager-northbound-gateway-client-tls
clawmanager-northbound-core-tls
clawmanager-northbound-core-ca
clawmanager-northbound-gateway-client-ca
clawmanager-northbound-jwe
"

for secret_name in $REQUIRED_SECRETS; do
  if kubectl -n "$NAMESPACE" get secret "$secret_name" >/dev/null 2>&1; then
    echo "[存在] $secret_name"
  else
    fail "缺少 Secret：$secret_name"
  fi
done

check_secret_key clawmanager-northbound-secrets db-user
check_secret_key clawmanager-northbound-secrets db-password
check_secret_key clawmanager-northbound-secrets jwt-secret
check_secret_key clawmanager-northbound-secrets refresh-token-pepper
check_secret_key clawmanager-northbound-secrets internal-jwt-secret
check_secret_key clawmanager-northbound-gateway-tls tls.crt
check_secret_key clawmanager-northbound-gateway-tls tls.key
check_secret_key clawmanager-northbound-gateway-client-tls tls.crt
check_secret_key clawmanager-northbound-gateway-client-tls tls.key
check_secret_key clawmanager-northbound-core-tls tls.crt
check_secret_key clawmanager-northbound-core-tls tls.key
check_secret_key clawmanager-northbound-core-ca ca.crt
check_secret_key clawmanager-northbound-gateway-client-ca ca.crt
check_secret_key clawmanager-northbound-jwe private.pem

echo
echo "===== 6. 启用 Core 北向 mTLS 服务 ====="

CORE_PATCH_MANIFEST=$(mktemp /tmp/clawmanager-northbound-core-patch.XXXXXX.yaml) || \
  fail "无法创建 Core 临时 Patch 文件"

cat >"$CORE_PATCH_MANIFEST" <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${CORE_DEPLOYMENT}
  namespace: ${NAMESPACE}
spec:
  template:
    spec:
      containers:
        - name: clawmanager-app
          ports:
            - name: northbound-core
              containerPort: 9002
          env:
            - name: CLAWMANAGER_NORTHBOUND_ENABLED
              value: "true"
            - name: NORTHBOUND_CORE_INTERNAL_ADDRESS
              value: ":9002"
            - name: NORTHBOUND_INTERNAL_JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: clawmanager-northbound-secrets
                  key: internal-jwt-secret
            - name: NORTHBOUND_CORE_TLS_CERT_FILE
              value: /etc/clawmanager/northbound/core/tls.crt
            - name: NORTHBOUND_CORE_TLS_KEY_FILE
              value: /etc/clawmanager/northbound/core/tls.key
            - name: NORTHBOUND_CORE_CLIENT_CA_FILE
              value: /etc/clawmanager/northbound/gateway-ca/ca.crt
          volumeMounts:
            - name: northbound-core-tls
              mountPath: /etc/clawmanager/northbound/core
              readOnly: true
            - name: northbound-gateway-client-ca
              mountPath: /etc/clawmanager/northbound/gateway-ca
              readOnly: true
      volumes:
        - name: northbound-core-tls
          secret:
            secretName: clawmanager-northbound-core-tls
        - name: northbound-gateway-client-ca
          secret:
            secretName: clawmanager-northbound-gateway-client-ca
EOF

kubectl patch deployment "$CORE_DEPLOYMENT" -n "$NAMESPACE" \
  --type strategic --patch-file "$CORE_PATCH_MANIFEST" || \
  fail "应用 Core 北向 Patch 失败"

kubectl -n "$NAMESPACE" rollout status deployment/"$CORE_DEPLOYMENT" --timeout=10m || \
  fail "Core 北向 Patch 应用后未能就绪"

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Service
metadata:
  name: clawmanager-northbound-core
  namespace: ${NAMESPACE}
spec:
  type: ClusterIP
  selector:
    app: clawmanager-app
  ports:
    - name: mtls
      port: 9002
      targetPort: 9002
EOF

CORE_ENDPOINTS=""
for _ in $(seq 1 30); do
  CORE_ENDPOINTS=$(
    kubectl -n "$NAMESPACE" get endpoints clawmanager-northbound-core \
      -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null || true
  )
  [ -n "$CORE_ENDPOINTS" ] && break
  sleep 2
done

[ -n "$CORE_ENDPOINTS" ] || fail "clawmanager-northbound-core 没有 Endpoint"
echo "Core Endpoint：$CORE_ENDPOINTS"

echo
echo "===== 7. 生成现场部署清单 ====="

GATEWAY_MANIFEST=$(mktemp /tmp/clawmanager-northbound-gateway.XXXXXX.yaml) || \
  fail "无法创建临时部署文件"

if [ -f "$MANIFEST_SOURCE" ]; then
  cp "$MANIFEST_SOURCE" "$GATEWAY_MANIFEST" || fail "无法复制 Gateway 部署模板"

  sed -i \
    "s|image: ghcr.io/yuan-lab-llm/clawmanager:latest|image: ${CORE_IMAGE}|" \
    "$GATEWAY_MANIFEST"

  sed -i \
    "s|nodePort: 38443|nodePort: ${NODE_PORT}|" \
    "$GATEWAY_MANIFEST"
else
  cat >"$GATEWAY_MANIFEST" <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: clawmanager-northbound-gateway
  namespace: ${NAMESPACE}
  labels:
    app: clawmanager-northbound-gateway
spec:
  replicas: 2
  selector:
    matchLabels:
      app: clawmanager-northbound-gateway
  template:
    metadata:
      labels:
        app: clawmanager-northbound-gateway
    spec:
      containers:
        - name: gateway
          image: ${CORE_IMAGE}
          imagePullPolicy: IfNotPresent
          command: ["/usr/local/bin/clawreef-northbound-gateway"]
          ports:
            - name: northbound
              containerPort: 9443
          env:
            - name: TZ
              value: Asia/Shanghai
            - name: SERVER_MODE
              value: release
            - name: CLAWMANAGER_NORTHBOUND_ENABLED
              value: "true"
            - name: NORTHBOUND_GATEWAY_ADDRESS
              value: ":9443"
            - name: NORTHBOUND_CORE_BASE_URL
              value: https://clawmanager-northbound-core.clawmanager-system.svc.cluster.local:9002
            - name: DB_HOST
              value: mysql
            - name: DB_PORT
              value: "3306"
            - name: DB_NAME
              value: clawmanager
            - name: DB_USER
              valueFrom:
                secretKeyRef:
                  name: clawmanager-northbound-secrets
                  key: db-user
            - name: DB_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: clawmanager-northbound-secrets
                  key: db-password
            - name: NORTHBOUND_JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: clawmanager-northbound-secrets
                  key: jwt-secret
            - name: NORTHBOUND_REFRESH_TOKEN_PEPPER
              valueFrom:
                secretKeyRef:
                  name: clawmanager-northbound-secrets
                  key: refresh-token-pepper
            - name: NORTHBOUND_INTERNAL_JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: clawmanager-northbound-secrets
                  key: internal-jwt-secret
            - name: NORTHBOUND_JWE_KEY_ID
              value: nb-login-v1
            - name: NORTHBOUND_GATEWAY_TLS_CERT_FILE
              value: /etc/clawmanager/northbound/server/tls.crt
            - name: NORTHBOUND_GATEWAY_TLS_KEY_FILE
              value: /etc/clawmanager/northbound/server/tls.key
            - name: NORTHBOUND_GATEWAY_CLIENT_CERT_FILE
              value: /etc/clawmanager/northbound/core-client/tls.crt
            - name: NORTHBOUND_GATEWAY_CLIENT_KEY_FILE
              value: /etc/clawmanager/northbound/core-client/tls.key
            - name: NORTHBOUND_CORE_CA_FILE
              value: /etc/clawmanager/northbound/core-ca/ca.crt
            - name: NORTHBOUND_JWE_PRIVATE_KEY_FILE
              value: /etc/clawmanager/northbound/jwe/private.pem
          volumeMounts:
            - name: gateway-server-tls
              mountPath: /etc/clawmanager/northbound/server
              readOnly: true
            - name: gateway-core-client-tls
              mountPath: /etc/clawmanager/northbound/core-client
              readOnly: true
            - name: core-ca
              mountPath: /etc/clawmanager/northbound/core-ca
              readOnly: true
            - name: jwe-key
              mountPath: /etc/clawmanager/northbound/jwe
              readOnly: true
          readinessProbe:
            tcpSocket:
              port: northbound
            initialDelaySeconds: 5
            periodSeconds: 10
          livenessProbe:
            tcpSocket:
              port: northbound
            initialDelaySeconds: 15
            periodSeconds: 20
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: "1"
              memory: 512Mi
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            runAsNonRoot: true
            runAsUser: 65532
            capabilities:
              drop: ["ALL"]
      volumes:
        - name: gateway-server-tls
          secret:
            secretName: clawmanager-northbound-gateway-tls
        - name: gateway-core-client-tls
          secret:
            secretName: clawmanager-northbound-gateway-client-tls
        - name: core-ca
          secret:
            secretName: clawmanager-northbound-core-ca
        - name: jwe-key
          secret:
            secretName: clawmanager-northbound-jwe
---
apiVersion: v1
kind: Service
metadata:
  name: clawmanager-northbound-gateway
  namespace: ${NAMESPACE}
spec:
  type: NodePort
  selector:
    app: clawmanager-northbound-gateway
  ports:
    - name: https
      port: 443
      targetPort: northbound
      nodePort: ${NODE_PORT}
---
apiVersion: v1
kind: Service
metadata:
  name: clawmanager-northbound-core
  namespace: ${NAMESPACE}
spec:
  type: ClusterIP
  selector:
    app: clawmanager-app
  ports:
    - name: mtls
      port: 9002
      targetPort: 9002
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: northbound-gateway-boundaries
  namespace: ${NAMESPACE}
spec:
  podSelector:
    matchLabels:
      app: clawmanager-northbound-gateway
  policyTypes: [Ingress, Egress]
  ingress:
    - ports:
        - protocol: TCP
          port: 9443
  egress:
    - to:
        - podSelector:
            matchLabels:
              app: clawmanager-app
      ports:
        - protocol: TCP
          port: 9002
    - to:
        - podSelector:
            matchLabels:
              app: mysql
      ports:
        - protocol: TCP
          port: 3306
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: northbound-core-ingress
  namespace: ${NAMESPACE}
spec:
  podSelector:
    matchLabels:
      app: clawmanager-app
  policyTypes: [Ingress]
  ingress:
    - ports:
        - protocol: TCP
          port: 8443
        - protocol: TCP
          port: 9001
    - from:
        - podSelector:
            matchLabels:
              app: clawmanager-northbound-gateway
      ports:
        - protocol: TCP
          port: 9002
EOF
fi

grep -q "name: clawmanager-northbound-gateway" "$GATEWAY_MANIFEST" || \
  fail "生成的部署文件中没有 Gateway 资源"
grep -Fq "image: ${CORE_IMAGE}" "$GATEWAY_MANIFEST" || \
  fail "没有成功替换 Gateway 镜像"
grep -q "nodePort: ${NODE_PORT}" "$GATEWAY_MANIFEST" || \
  fail "没有成功把 NodePort 设置为 ${NODE_PORT}"

echo "临时部署文件：$GATEWAY_MANIFEST"
echo "Gateway 镜像：$CORE_IMAGE"
echo "Gateway NodePort：$NODE_PORT"

echo
echo "===== 7. Kubernetes 服务端预检 ====="

kubectl apply --dry-run=server -f "$GATEWAY_MANIFEST" || \
  fail "服务端预检失败，请查看上方 kubectl 错误"

echo "服务端预检通过"

echo
echo "===== 8. 创建 Gateway Deployment ====="

kubectl apply -f "$GATEWAY_MANIFEST" || fail "应用 Gateway 部署清单失败"

echo
echo "===== 9. 等待 Gateway 启动 ====="

if ! kubectl -n "$NAMESPACE" rollout status \
  deployment/"$GATEWAY_DEPLOYMENT" --timeout=10m; then
  echo
  echo "Gateway 启动失败，下面输出诊断信息："
  kubectl -n "$NAMESPACE" get deployment,pod,service -o wide | grep northbound || true
  kubectl -n "$NAMESPACE" describe pods -l app=clawmanager-northbound-gateway || true
  kubectl -n "$NAMESPACE" logs deployment/"$GATEWAY_DEPLOYMENT" \
    --all-containers=true --tail=200 || true
  fail "Gateway Deployment 没有在规定时间内就绪"
fi

echo
echo "===== 10. 检查最终状态 ====="

kubectl -n "$NAMESPACE" get deployment,pod,service -o wide | grep northbound || true

echo
echo "Gateway EndpointSlice："

kubectl -n "$NAMESPACE" get endpointslice \
  -l kubernetes.io/service-name="$GATEWAY_SERVICE" -o wide

READY_ENDPOINTS=$(
  kubectl -n "$NAMESPACE" get endpointslice \
    -l kubernetes.io/service-name="$GATEWAY_SERVICE" \
    -o jsonpath='{.items[*].endpoints[*].addresses[*]}' 2>/dev/null || true
)

[ -n "$READY_ENDPOINTS" ] || \
  fail "Gateway Deployment 已创建，但 Service 仍没有 Endpoint"

echo "Gateway Endpoints：$READY_ENDPOINTS"

echo
echo "===== 11. 输出 Gateway 日志 ====="

kubectl -n "$NAMESPACE" logs deployment/"$GATEWAY_DEPLOYMENT" \
  --all-containers=true --tail=100

echo
echo "===== 12. 测试挑战接口 ====="

TEST_URL="https://${TEST_HOST}:${NODE_PORT}/api/northbound/v1/auth/challenge"
echo "测试地址：$TEST_URL"
echo "注意：下面的 -k 只用于首次诊断 TLS 连通性，正式调用必须使用可信 CA。"

curl -vk --connect-timeout 10 --max-time 30 -X POST "$TEST_URL" || \
  fail "Gateway 已部署，但从本机调用挑战接口失败"

echo
echo "=========================================="
echo "Gateway Deployment 已恢复"
echo "对外地址：https://${TEST_HOST}:${NODE_PORT}"
echo "=========================================="
