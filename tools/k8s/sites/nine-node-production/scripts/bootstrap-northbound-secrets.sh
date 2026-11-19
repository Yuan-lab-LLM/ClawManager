#!/usr/bin/env sh
set -eu

NAMESPACE="${NAMESPACE:-clawmanager-system}"
NORTHBOUND_HOST="${NORTHBOUND_HOST:-10.130.15.40}"
OUTPUT_DIR="${OUTPUT_DIR:-/home/hxc/nine-node-production/northbound-client}"
VALID_DAYS="${VALID_DAYS:-3650}"

command -v kubectl >/dev/null 2>&1
command -v openssl >/dev/null 2>&1

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT INT TERM
umask 077

if ! kubectl get secret clawmanager-northbound-secrets -n "$NAMESPACE" >/dev/null 2>&1; then
  db_password="$(openssl rand -hex 32)"
  jwt_secret="$(openssl rand -base64 48 | tr -d '\n')"
  refresh_pepper="$(openssl rand -base64 48 | tr -d '\n')"
  internal_jwt="$(openssl rand -base64 48 | tr -d '\n')"
  kubectl create secret generic clawmanager-northbound-secrets -n "$NAMESPACE" \
    --from-literal=db-user=clawmanager_northbound \
    --from-literal=db-password="$db_password" \
    --from-literal=jwt-secret="$jwt_secret" \
    --from-literal=refresh-token-pepper="$refresh_pepper" \
    --from-literal=internal-jwt-secret="$internal_jwt"
fi

if ! kubectl get secret clawmanager-iei-sso -n "$NAMESPACE" >/dev/null 2>&1; then
  iei_key="$(openssl rand -hex 8)"
  session_secret="$(openssl rand -base64 48 | tr -d '\n')"
  kubectl create secret generic clawmanager-iei-sso -n "$NAMESPACE" \
    --from-literal=aes-key="$iei_key" \
    --from-literal=aes-iv=CLAWMANAGETOKENS \
    --from-literal=session-secret="$session_secret"
fi

certificates_ready=true
for secret_name in \
  clawmanager-northbound-gateway-tls \
  clawmanager-northbound-gateway-client-tls \
  clawmanager-northbound-core-tls \
  clawmanager-northbound-core-ca \
  clawmanager-northbound-gateway-client-ca \
  clawmanager-northbound-gateway-ca \
  clawmanager-northbound-jwe
do
  if ! kubectl get secret "$secret_name" -n "$NAMESPACE" >/dev/null 2>&1; then
    certificates_ready=false
  fi
done

if [ "$certificates_ready" != true ]; then
  openssl req -x509 -newkey rsa:4096 -sha256 -nodes -days "$VALID_DAYS" \
    -subj "/CN=ClawManager Northbound Gateway CA" \
    -addext "basicConstraints=critical,CA:TRUE" \
    -addext "keyUsage=critical,keyCertSign,cRLSign" \
    -keyout "$work_dir/gateway-ca.key" -out "$work_dir/gateway-ca.crt"
  openssl req -newkey rsa:3072 -nodes \
    -subj "/CN=$NORTHBOUND_HOST" \
    -keyout "$work_dir/gateway.key" -out "$work_dir/gateway.csr"
  cat >"$work_dir/gateway.ext" <<EOF
subjectAltName=IP:$NORTHBOUND_HOST,DNS:10-130-15-40.nip.io
extendedKeyUsage=serverAuth
keyUsage=digitalSignature,keyEncipherment
EOF
  openssl x509 -req -sha256 -days "$VALID_DAYS" \
    -in "$work_dir/gateway.csr" \
    -CA "$work_dir/gateway-ca.crt" -CAkey "$work_dir/gateway-ca.key" \
    -CAcreateserial -extfile "$work_dir/gateway.ext" \
    -out "$work_dir/gateway.crt"

  openssl req -x509 -newkey rsa:4096 -sha256 -nodes -days "$VALID_DAYS" \
    -subj "/CN=ClawManager Northbound Internal CA" \
    -addext "basicConstraints=critical,CA:TRUE" \
    -addext "keyUsage=critical,keyCertSign,cRLSign" \
    -keyout "$work_dir/internal-ca.key" -out "$work_dir/internal-ca.crt"
  openssl req -newkey rsa:3072 -nodes \
    -subj "/CN=clawmanager-northbound-core.clawmanager-system.svc.cluster.local" \
    -keyout "$work_dir/core.key" -out "$work_dir/core.csr"
  cat >"$work_dir/core.ext" <<'EOF'
subjectAltName=DNS:clawmanager-northbound-core.clawmanager-system.svc.cluster.local
extendedKeyUsage=serverAuth
keyUsage=digitalSignature,keyEncipherment
EOF
  openssl x509 -req -sha256 -days "$VALID_DAYS" \
    -in "$work_dir/core.csr" \
    -CA "$work_dir/internal-ca.crt" -CAkey "$work_dir/internal-ca.key" \
    -CAcreateserial -extfile "$work_dir/core.ext" \
    -out "$work_dir/core.crt"
  openssl req -newkey rsa:3072 -nodes \
    -subj "/CN=clawmanager-northbound-gateway" \
    -keyout "$work_dir/gateway-client.key" -out "$work_dir/gateway-client.csr"
  cat >"$work_dir/gateway-client.ext" <<'EOF'
extendedKeyUsage=clientAuth
keyUsage=digitalSignature
EOF
  openssl x509 -req -sha256 -days "$VALID_DAYS" \
    -in "$work_dir/gateway-client.csr" \
    -CA "$work_dir/internal-ca.crt" -CAkey "$work_dir/internal-ca.key" \
    -CAcreateserial -extfile "$work_dir/gateway-client.ext" \
    -out "$work_dir/gateway-client.crt"
  openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 \
    -out "$work_dir/private.pem"

  kubectl create secret tls clawmanager-northbound-gateway-tls -n "$NAMESPACE" \
    --cert="$work_dir/gateway.crt" --key="$work_dir/gateway.key" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl create secret tls clawmanager-northbound-gateway-client-tls -n "$NAMESPACE" \
    --cert="$work_dir/gateway-client.crt" --key="$work_dir/gateway-client.key" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl create secret tls clawmanager-northbound-core-tls -n "$NAMESPACE" \
    --cert="$work_dir/core.crt" --key="$work_dir/core.key" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl create secret generic clawmanager-northbound-core-ca -n "$NAMESPACE" \
    --from-file=ca.crt="$work_dir/internal-ca.crt" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl create secret generic clawmanager-northbound-gateway-client-ca -n "$NAMESPACE" \
    --from-file=ca.crt="$work_dir/internal-ca.crt" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl create secret generic clawmanager-northbound-gateway-ca -n "$NAMESPACE" \
    --from-file=ca.crt="$work_dir/gateway-ca.crt" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl create secret generic clawmanager-northbound-jwe -n "$NAMESPACE" \
    --from-file=private.pem="$work_dir/private.pem" \
    --dry-run=client -o yaml | kubectl apply -f -
fi

mkdir -p "$OUTPUT_DIR"
kubectl get secret clawmanager-northbound-gateway-ca -n "$NAMESPACE" \
  -o jsonpath='{.data.ca\.crt}' | base64 -d >"$OUTPUT_DIR/gateway-ca.crt"
chmod 0644 "$OUTPUT_DIR/gateway-ca.crt"

db_password="$(kubectl get secret clawmanager-northbound-secrets -n "$NAMESPACE" -o jsonpath='{.data.db-password}' | base64 -d)"
mysql_pod="$(kubectl get pod -n "$NAMESPACE" -l app=mysql -o jsonpath='{.items[0].metadata.name}')"
cat <<EOF | kubectl exec -i -n "$NAMESPACE" "$mysql_pod" -- sh -c 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD"'
CREATE USER IF NOT EXISTS 'clawmanager_northbound'@'%' IDENTIFIED BY '$db_password';
ALTER USER 'clawmanager_northbound'@'%' IDENTIFIED BY '$db_password';
GRANT SELECT ON clawmanager.users TO 'clawmanager_northbound'@'%';
GRANT SELECT, INSERT, UPDATE ON clawmanager.northbound_auth_challenges TO 'clawmanager_northbound'@'%';
GRANT SELECT, INSERT, UPDATE ON clawmanager.northbound_sessions TO 'clawmanager_northbound'@'%';
GRANT SELECT ON clawmanager.northbound_admin_settings TO 'clawmanager_northbound'@'%';
GRANT SELECT ON clawmanager.northbound_caller_policies TO 'clawmanager_northbound'@'%';
GRANT INSERT ON clawmanager.audit_events TO 'clawmanager_northbound'@'%';
FLUSH PRIVILEGES;
EOF

kubectl get secret -n "$NAMESPACE" \
  clawmanager-northbound-secrets \
  clawmanager-iei-sso \
  clawmanager-northbound-gateway-tls \
  clawmanager-northbound-gateway-client-tls \
  clawmanager-northbound-core-tls \
  clawmanager-northbound-core-ca \
  clawmanager-northbound-gateway-client-ca \
  clawmanager-northbound-gateway-ca \
  clawmanager-northbound-jwe >/dev/null

printf '%s\n' "Northbound secrets and restricted database account are ready."
printf '%s\n' "Client CA: $OUTPUT_DIR/gateway-ca.crt"
