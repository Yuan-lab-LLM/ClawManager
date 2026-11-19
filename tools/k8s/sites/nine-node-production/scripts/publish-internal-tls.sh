#!/usr/bin/env bash
set -Eeuo pipefail

if [ "$#" -ne 2 ]; then
  echo "Usage: $0 <certificate-directory> <expected-kube-context>" >&2
  exit 2
fi

readonly CERTIFICATE_DIRECTORY="$(realpath "$1")"
readonly EXPECTED_CONTEXT="$2"
readonly NAMESPACE="${NAMESPACE:-clawmanager-system}"
readonly DEPLOYMENT="${DEPLOYMENT:-clawmanager-app}"
readonly GATEWAY_IP="${GATEWAY_IP:-10.130.15.40}"
readonly GATEWAY_DNS_NAME="${GATEWAY_DNS_NAME:-10-130-15-40.nip.io}"
readonly NODE_PORT="${NODE_PORT:-30443}"
readonly ROOT_CERTIFICATE="${CERTIFICATE_DIRECTORY}/root-ca.crt"
readonly SERVER_CERTIFICATE="${CERTIFICATE_DIRECTORY}/clawmanager.crt"
readonly FULL_CHAIN="${CERTIFICATE_DIRECTORY}/clawmanager-fullchain.crt"
readonly SERVER_KEY="${CERTIFICATE_DIRECTORY}/clawmanager.key"

for command_name in openssl kubectl; do
  command -v "${command_name}" >/dev/null 2>&1 || {
    echo "ERROR: required command is missing: ${command_name}" >&2
    exit 1
  }
done
for required_file in "${ROOT_CERTIFICATE}" "${SERVER_CERTIFICATE}" "${FULL_CHAIN}" "${SERVER_KEY}"; do
  [ -f "${required_file}" ] || {
    echo "ERROR: required certificate file is missing: ${required_file}" >&2
    exit 1
  }
done

current_context="$(kubectl config current-context)"
if [ "${current_context}" != "${EXPECTED_CONTEXT}" ]; then
  echo "ERROR: kubectl context is ${current_context}, expected ${EXPECTED_CONTEXT}." >&2
  exit 1
fi

openssl verify -CAfile "${ROOT_CERTIFICATE}" "${SERVER_CERTIFICATE}"
openssl x509 -checkend 31536000 -noout -in "${SERVER_CERTIFICATE}"
san_text="$(openssl x509 -in "${SERVER_CERTIFICATE}" -noout -ext subjectAltName)"
grep -Fq "DNS:*.${GATEWAY_DNS_NAME}" <<<"${san_text}"
grep -Fq "IP Address:${GATEWAY_IP}" <<<"${san_text}"

kubectl --context "${EXPECTED_CONTEXT}" get deployment "${DEPLOYMENT}" \
  --namespace "${NAMESPACE}" -o name >/dev/null

readonly OPENCODE_TEMPLATE="https://opencode-{instance_id}.${GATEWAY_DNS_NAME}:${NODE_PORT}/"
readonly DEEPSEEK_TEMPLATE="https://deepseek-harness-{instance_id}.${GATEWAY_DNS_NAME}:${NODE_PORT}/"

kubectl --context "${EXPECTED_CONTEXT}" create secret tls clawmanager-tls \
  --namespace "${NAMESPACE}" --cert "${FULL_CHAIN}" --key "${SERVER_KEY}" \
  --dry-run=client -o yaml \
  | kubectl --context "${EXPECTED_CONTEXT}" apply -f -

kubectl --context "${EXPECTED_CONTEXT}" set env deployment/"${DEPLOYMENT}" \
  --namespace "${NAMESPACE}" \
  "CLAWMANAGER_OPENCODE_PUBLIC_URL_TEMPLATE=${OPENCODE_TEMPLATE}" \
  "CLAWMANAGER_DEEPSEEK_HARNESS_PUBLIC_URL_TEMPLATE=${DEEPSEEK_TEMPLATE}"

kubectl --context "${EXPECTED_CONTEXT}" rollout status deployment/"${DEPLOYMENT}" \
  --namespace "${NAMESPACE}" --timeout=10m

echo "Published the trusted runtime origins without changing Runtime or Workspace configuration."
echo "OpenCode: ${OPENCODE_TEMPLATE}"
echo "DeepSeek Harness: ${DEEPSEEK_TEMPLATE}"
