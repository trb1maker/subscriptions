#!/bin/sh
set -eu

cd "$(dirname "$0")"

openssl req -x509 -newkey rsa:2048 -sha256 -days 3650 -nodes \
  -keyout ca.key -out ca.crt \
  -subj "/CN=subscriptions-dev-ca"

openssl req -newkey rsa:2048 -sha256 -nodes \
  -keyout auth.key -out auth.csr \
  -subj "/CN=auth"

cat > auth.ext <<'EOF'
subjectAltName=DNS:localhost,DNS:auth,IP:127.0.0.1
extendedKeyUsage=serverAuth
keyUsage=digitalSignature,keyEncipherment
EOF

openssl x509 -req -sha256 -in auth.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out auth.crt -days 3650 -extfile auth.ext

openssl req -newkey rsa:2048 -sha256 -nodes \
  -keyout subscriptions.key -out subscriptions.csr \
  -subj "/CN=subscriptions"

cat > subscriptions.ext <<'EOF'
subjectAltName=DNS:localhost,DNS:subscriptions,IP:127.0.0.1
extendedKeyUsage=serverAuth,clientAuth
keyUsage=digitalSignature,keyEncipherment
EOF

openssl x509 -req -sha256 -in subscriptions.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out subscriptions.crt -days 3650 -extfile subscriptions.ext

openssl req -newkey rsa:2048 -sha256 -nodes \
  -keyout gateway.key -out gateway.csr \
  -subj "/CN=gateway"

cat > gateway.ext <<'EOF'
extendedKeyUsage=clientAuth
keyUsage=digitalSignature,keyEncipherment
EOF

openssl x509 -req -sha256 -in gateway.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out gateway.crt -days 3650 -extfile gateway.ext

rm -f auth.csr auth.ext subscriptions.csr subscriptions.ext gateway.csr gateway.ext ca.srl

echo "Auth: TLS_CERT_FILE=$PWD/auth.crt TLS_KEY_FILE=$PWD/auth.key TLS_CA_FILE=$PWD/ca.crt"
echo "Subscriptions: TLS_CERT_FILE=$PWD/subscriptions.crt TLS_KEY_FILE=$PWD/subscriptions.key TLS_CA_FILE=$PWD/ca.crt AUTH_GRPC_SERVER_NAME=localhost"
echo "Gateway: TLS_CERT_FILE=$PWD/gateway.crt TLS_KEY_FILE=$PWD/gateway.key TLS_CA_FILE=$PWD/ca.crt AUTH_GRPC_SERVER_NAME=localhost SUBSCRIPTIONS_GRPC_SERVER_NAME=localhost"
