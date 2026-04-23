#!/bin/sh
set -e

CERT_FILE=${CERT_FILE:-/etc/nginx/ssl/cert.crt}
KEY_FILE=${KEY_FILE:-/etc/nginx/ssl/cert.key}
LOCAL_IP=${LOCAL_IP:-localhost}

if [ ! -f "$CERT_FILE" ]; then
    echo "Generating self-signed SSL certificate for frontend..."

    mkdir -p "$(dirname "$CERT_FILE")"

    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout "$KEY_FILE" \
        -out "$CERT_FILE" \
        -subj "/C=US/ST=State/L=City/O=LocalMusicQueue/CN=${LOCAL_IP}" \
        -addext "subjectAltName=IP:${LOCAL_IP},DNS:localhost,IP:127.0.0.1"

    chmod 644 "$CERT_FILE"
    chmod 600 "$KEY_FILE"

    echo "SSL certificate generated for frontend at ${LOCAL_IP}"
else
    echo "SSL certificate already exists at $CERT_FILE"
fi

# Start nginx
exec nginx -g "daemon off;"
