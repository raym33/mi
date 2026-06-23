#!/usr/bin/env bash
set -euo pipefail

OUT_DIR="${OUT_DIR:-certs}"
COMMON_NAME="${COMMON_NAME:-localhost}"
ALT_DNS="${ALT_DNS:-localhost}"
ALT_IP="${ALT_IP:-127.0.0.1}"

mkdir -p "$OUT_DIR"

cat > "$OUT_DIR/server.openssl.cnf" <<EOF
[req]
default_bits = 2048
prompt = no
default_md = sha256
distinguished_name = dn
req_extensions = req_ext

[dn]
CN = $COMMON_NAME

[req_ext]
subjectAltName = @alt_names

[alt_names]
DNS.1 = $ALT_DNS
IP.1 = $ALT_IP
EOF

openssl genrsa -out "$OUT_DIR/ca.key" 4096
openssl req -x509 -new -nodes \
  -key "$OUT_DIR/ca.key" \
  -sha256 \
  -days 3650 \
  -subj "/CN=mi-dev-ca" \
  -out "$OUT_DIR/ca.crt"

openssl genrsa -out "$OUT_DIR/server.key" 2048
openssl req -new \
  -key "$OUT_DIR/server.key" \
  -out "$OUT_DIR/server.csr" \
  -config "$OUT_DIR/server.openssl.cnf"

openssl x509 -req \
  -in "$OUT_DIR/server.csr" \
  -CA "$OUT_DIR/ca.crt" \
  -CAkey "$OUT_DIR/ca.key" \
  -CAcreateserial \
  -out "$OUT_DIR/server.crt" \
  -days 825 \
  -sha256 \
  -extensions req_ext \
  -extfile "$OUT_DIR/server.openssl.cnf"

chmod 600 "$OUT_DIR"/*.key

cat <<EOF
Wrote development certificates to $OUT_DIR:
  CA:      $OUT_DIR/ca.crt
  Server:  $OUT_DIR/server.crt
  Key:     $OUT_DIR/server.key

Use configs/coordinator.city.tls.example.yaml and configs/node-agent.city.tls.example.yaml.
EOF
