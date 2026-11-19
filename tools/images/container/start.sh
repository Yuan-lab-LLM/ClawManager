#!/bin/sh
set -eu

TLS_DIR="/etc/nginx/tls"
TLS_SOURCE_DIR="${TLS_SOURCE_DIR:-/var/run/clawreef-tls}"
TLS_SHARED_DIR="${TLS_SHARED_DIR:-/workspaces/.clawmanager/tls}"
TLS_CN="${TLS_CN:-clawmanager.local}"
TLS_SUBJECT_ALT_NAME="${TLS_SUBJECT_ALT_NAME:-DNS:clawmanager.local,DNS:localhost,IP:127.0.0.1}"
TLS_CERT="${TLS_DIR}/tls.crt"
TLS_KEY="${TLS_DIR}/tls.key"
SOURCE_CERT="${TLS_SOURCE_DIR}/tls.crt"
SOURCE_KEY="${TLS_SOURCE_DIR}/tls.key"
SHARED_CERT="${TLS_SHARED_DIR}/tls.crt"
SHARED_KEY="${TLS_SHARED_DIR}/tls.key"
SHARED_LOCK_DIR="${TLS_SHARED_DIR}.lock"

generate_self_signed_cert() {
  cert_path="$1"
  key_path="$2"
  openssl req \
    -x509 \
    -nodes \
    -days 365 \
    -newkey rsa:2048 \
    -subj "/CN=${TLS_CN}" \
    -addext "subjectAltName=${TLS_SUBJECT_ALT_NAME}" \
    -keyout "${key_path}" \
    -out "${cert_path}" || \
  openssl req \
    -x509 \
    -nodes \
    -days 365 \
    -newkey rsa:2048 \
    -subj "/CN=${TLS_CN}" \
    -keyout "${key_path}" \
    -out "${cert_path}"
}

copy_cert_pair() {
  cp "$1" "${TLS_CERT}"
  cp "$2" "${TLS_KEY}"
  chmod 0644 "${TLS_CERT}"
  chmod 0600 "${TLS_KEY}"
}

ensure_shared_cert() {
  if [ -f "${SHARED_CERT}" ] && [ -f "${SHARED_KEY}" ]; then
    return 0
  fi

  mkdir -p "${TLS_SHARED_DIR}" 2>/dev/null || return 1
  if mkdir "${SHARED_LOCK_DIR}" 2>/dev/null; then
    if [ ! -f "${SHARED_CERT}" ] || [ ! -f "${SHARED_KEY}" ]; then
      tmp_cert="${SHARED_CERT}.$$"
      tmp_key="${SHARED_KEY}.$$"
      generate_self_signed_cert "${tmp_cert}" "${tmp_key}"
      chmod 0644 "${tmp_cert}"
      chmod 0600 "${tmp_key}"
      mv "${tmp_cert}" "${SHARED_CERT}"
      mv "${tmp_key}" "${SHARED_KEY}"
    fi
    rmdir "${SHARED_LOCK_DIR}" 2>/dev/null || true
    return 0
  fi

  for _ in $(seq 1 60); do
    if [ -f "${SHARED_CERT}" ] && [ -f "${SHARED_KEY}" ]; then
      return 0
    fi
    sleep 1
  done
  return 1
}

mkdir -p "${TLS_DIR}"

if [ -f "${SOURCE_CERT}" ] && [ -f "${SOURCE_KEY}" ]; then
  copy_cert_pair "${SOURCE_CERT}" "${SOURCE_KEY}"
elif ensure_shared_cert; then
  copy_cert_pair "${SHARED_CERT}" "${SHARED_KEY}"
elif [ ! -f "${TLS_CERT}" ] || [ ! -f "${TLS_KEY}" ]; then
  echo "TLS certificate not found, generating a self-signed certificate for bootstrap use."
  generate_self_signed_cert "${TLS_CERT}" "${TLS_KEY}"
fi

export SERVER_ADDRESS="${SERVER_ADDRESS:-:9001}"
export SERVER_MODE="${SERVER_MODE:-release}"
export CLAWMANAGER_WORKSPACE_ARCHIVE_MAX_MIB="${CLAWMANAGER_WORKSPACE_ARCHIVE_MAX_MIB:-500}"

case "${CLAWMANAGER_WORKSPACE_ARCHIVE_MAX_MIB}" in
  ''|*[!0-9]*|0)
    echo "Invalid CLAWMANAGER_WORKSPACE_ARCHIVE_MAX_MIB=${CLAWMANAGER_WORKSPACE_ARCHIVE_MAX_MIB}; falling back to 500 MiB."
    export CLAWMANAGER_WORKSPACE_ARCHIVE_MAX_MIB="500"
    ;;
esac

sed -i "s/client_max_body_size [0-9][0-9]*m;/client_max_body_size ${CLAWMANAGER_WORKSPACE_ARCHIVE_MAX_MIB}m;/" /etc/nginx/nginx.conf

# Resolve the cluster DNS server for nginx so the desktop location can resolve
# per-instance Service FQDNs at request time. Prefer the first nameserver from
# /etc/resolv.conf, falling back to the common in-cluster DNS ClusterIP.
DNS_RESOLVER="$(awk '/^nameserver/ {print $2; exit}' /etc/resolv.conf 2>/dev/null || true)"
if [ -z "${DNS_RESOLVER}" ]; then
  DNS_RESOLVER="10.96.0.10"
fi
sed -i "s/__DNS_RESOLVER__/${DNS_RESOLVER}/g" /etc/nginx/nginx.conf

/usr/local/bin/clawreef-server &
backend_pid=$!

nginx -g 'daemon off;' &
nginx_pid=$!

shutdown() {
  kill "${backend_pid}" 2>/dev/null || true
  kill "${nginx_pid}" 2>/dev/null || true
  wait "${backend_pid}" 2>/dev/null || true
  wait "${nginx_pid}" 2>/dev/null || true
}

trap shutdown INT TERM

while kill -0 "${backend_pid}" 2>/dev/null && kill -0 "${nginx_pid}" 2>/dev/null; do
  sleep 2
done

shutdown
