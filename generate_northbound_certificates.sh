#!/usr/bin/env bash

set -euo pipefail

NAMESPACE="${NAMESPACE:-clawmanager-system}"
OUTPUT_DIR="${OUTPUT_DIR:-$PWD/northbound-pki}"
GATEWAY_SAN_IP="${GATEWAY_SAN_IP:-}"
GATEWAY_SAN_DNS="${GATEWAY_SAN_DNS:-}"
CORE_DNS="${CORE_DNS:-clawmanager-northbound-core.clawmanager-system.svc.cluster.local}"
APPLY_SECRETS="${APPLY_SECRETS:-false}"
CA_DAYS="${CA_DAYS:-3650}"
LEAF_DAYS="${LEAF_DAYS:-825}"

fail() {
  echo "ERROR: $1" >&2
  exit 1
}

command -v openssl >/dev/null 2>&1 || fail "openssl is required"

if [ -z "$GATEWAY_SAN_IP" ] && [ -z "$GATEWAY_SAN_DNS" ]; then
  fail "Set GATEWAY_SAN_IP and/or GATEWAY_SAN_DNS to the address used by clients"
fi

if [ -e "$OUTPUT_DIR" ]; then
  fail "OUTPUT_DIR already exists; refusing to overwrite: $OUTPUT_DIR"
fi

umask 077
mkdir -p "$OUTPUT_DIR"

gateway_san=""
if [ -n "$GATEWAY_SAN_DNS" ]; then
  gateway_san="DNS:${GATEWAY_SAN_DNS}"
fi
if [ -n "$GATEWAY_SAN_IP" ]; then
  if [ -n "$gateway_san" ]; then
    gateway_san="${gateway_san},IP:${GATEWAY_SAN_IP}"
  else
    gateway_san="IP:${GATEWAY_SAN_IP}"
  fi
fi

echo "Generating private PKI in: $OUTPUT_DIR"
echo "Gateway SAN: $gateway_san"
echo "Core SAN: DNS:$CORE_DNS"

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:4096 \
  -out "$OUTPUT_DIR/gateway-ca.key"
openssl req -x509 -new -sha256 -key "$OUTPUT_DIR/gateway-ca.key" \
  -days "$CA_DAYS" -subj "/CN=ClawManager Northbound Gateway CA" \
  -addext "basicConstraints=critical,CA:TRUE,pathlen:0" \
  -addext "keyUsage=critical,keyCertSign,cRLSign" \
  -out "$OUTPUT_DIR/gateway-ca.crt"

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 \
  -out "$OUTPUT_DIR/gateway.key"
openssl req -new -sha256 -key "$OUTPUT_DIR/gateway.key" \
  -subj "/CN=clawmanager-northbound-gateway" \
  -out "$OUTPUT_DIR/gateway.csr"
cat >"$OUTPUT_DIR/gateway.ext" <<EOF
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=${gateway_san}
EOF
openssl x509 -req -sha256 -in "$OUTPUT_DIR/gateway.csr" \
  -CA "$OUTPUT_DIR/gateway-ca.crt" -CAkey "$OUTPUT_DIR/gateway-ca.key" \
  -CAcreateserial -days "$LEAF_DAYS" -extfile "$OUTPUT_DIR/gateway.ext" \
  -out "$OUTPUT_DIR/gateway.crt"
cat "$OUTPUT_DIR/gateway.crt" "$OUTPUT_DIR/gateway-ca.crt" \
  >"$OUTPUT_DIR/gateway-chain.crt"

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:4096 \
  -out "$OUTPUT_DIR/core-ca.key"
openssl req -x509 -new -sha256 -key "$OUTPUT_DIR/core-ca.key" \
  -days "$CA_DAYS" -subj "/CN=ClawManager Northbound Core CA" \
  -addext "basicConstraints=critical,CA:TRUE,pathlen:0" \
  -addext "keyUsage=critical,keyCertSign,cRLSign" \
  -out "$OUTPUT_DIR/core-ca.crt"

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 \
  -out "$OUTPUT_DIR/core.key"
openssl req -new -sha256 -key "$OUTPUT_DIR/core.key" \
  -subj "/CN=${CORE_DNS}" -out "$OUTPUT_DIR/core.csr"
cat >"$OUTPUT_DIR/core.ext" <<EOF
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=DNS:${CORE_DNS}
EOF
openssl x509 -req -sha256 -in "$OUTPUT_DIR/core.csr" \
  -CA "$OUTPUT_DIR/core-ca.crt" -CAkey "$OUTPUT_DIR/core-ca.key" \
  -CAcreateserial -days "$LEAF_DAYS" -extfile "$OUTPUT_DIR/core.ext" \
  -out "$OUTPUT_DIR/core.crt"
cat "$OUTPUT_DIR/core.crt" "$OUTPUT_DIR/core-ca.crt" \
  >"$OUTPUT_DIR/core-chain.crt"

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:4096 \
  -out "$OUTPUT_DIR/gateway-client-ca.key"
openssl req -x509 -new -sha256 -key "$OUTPUT_DIR/gateway-client-ca.key" \
  -days "$CA_DAYS" -subj "/CN=ClawManager Northbound Gateway Client CA" \
  -addext "basicConstraints=critical,CA:TRUE,pathlen:0" \
  -addext "keyUsage=critical,keyCertSign,cRLSign" \
  -out "$OUTPUT_DIR/gateway-client-ca.crt"

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 \
  -out "$OUTPUT_DIR/gateway-client.key"
openssl req -new -sha256 -key "$OUTPUT_DIR/gateway-client.key" \
  -subj "/CN=clawmanager-northbound-gateway" \
  -out "$OUTPUT_DIR/gateway-client.csr"
cat >"$OUTPUT_DIR/gateway-client.ext" <<EOF
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=clientAuth
EOF
openssl x509 -req -sha256 -in "$OUTPUT_DIR/gateway-client.csr" \
  -CA "$OUTPUT_DIR/gateway-client-ca.crt" \
  -CAkey "$OUTPUT_DIR/gateway-client-ca.key" \
  -CAcreateserial -days "$LEAF_DAYS" \
  -extfile "$OUTPUT_DIR/gateway-client.ext" \
  -out "$OUTPUT_DIR/gateway-client.crt"
cat "$OUTPUT_DIR/gateway-client.crt" "$OUTPUT_DIR/gateway-client-ca.crt" \
  >"$OUTPUT_DIR/gateway-client-chain.crt"

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 \
  -out "$OUTPUT_DIR/private.pem"

openssl verify -CAfile "$OUTPUT_DIR/gateway-ca.crt" "$OUTPUT_DIR/gateway.crt"
openssl verify -CAfile "$OUTPUT_DIR/core-ca.crt" "$OUTPUT_DIR/core.crt"
openssl verify -purpose sslclient -CAfile "$OUTPUT_DIR/gateway-client-ca.crt" \
  "$OUTPUT_DIR/gateway-client.crt"

rm -f -- "$OUTPUT_DIR"/*.csr "$OUTPUT_DIR"/*.ext "$OUTPUT_DIR"/*.srl

if [ "$APPLY_SECRETS" = "true" ]; then
  command -v kubectl >/dev/null 2>&1 || fail "kubectl is required when APPLY_SECRETS=true"
  kubectl get namespace "$NAMESPACE" >/dev/null 2>&1 || \
    fail "Kubernetes namespace does not exist: $NAMESPACE"

  kubectl create secret tls clawmanager-northbound-gateway-tls \
    -n "$NAMESPACE" --cert="$OUTPUT_DIR/gateway-chain.crt" \
    --key="$OUTPUT_DIR/gateway.key" --dry-run=client -o yaml | kubectl apply -f -
  kubectl create secret tls clawmanager-northbound-gateway-client-tls \
    -n "$NAMESPACE" --cert="$OUTPUT_DIR/gateway-client-chain.crt" \
    --key="$OUTPUT_DIR/gateway-client.key" --dry-run=client -o yaml | kubectl apply -f -
  kubectl create secret tls clawmanager-northbound-core-tls \
    -n "$NAMESPACE" --cert="$OUTPUT_DIR/core-chain.crt" \
    --key="$OUTPUT_DIR/core.key" --dry-run=client -o yaml | kubectl apply -f -
  kubectl create secret generic clawmanager-northbound-core-ca \
    -n "$NAMESPACE" --from-file=ca.crt="$OUTPUT_DIR/core-ca.crt" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl create secret generic clawmanager-northbound-gateway-client-ca \
    -n "$NAMESPACE" --from-file=ca.crt="$OUTPUT_DIR/gateway-client-ca.crt" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl create secret generic clawmanager-northbound-jwe \
    -n "$NAMESPACE" --from-file=private.pem="$OUTPUT_DIR/private.pem" \
    --dry-run=client -o yaml | kubectl apply -f -
fi

echo
echo "Certificate generation completed."
echo "Client trust certificate (safe to distribute): $OUTPUT_DIR/gateway-ca.crt"
echo "Keep every *.key file and private.pem private."
if [ "$APPLY_SECRETS" = "true" ]; then
  echo "Kubernetes certificate Secrets were applied to namespace: $NAMESPACE"
else
  echo "Kubernetes Secrets were not changed (APPLY_SECRETS=$APPLY_SECRETS)."
fi
