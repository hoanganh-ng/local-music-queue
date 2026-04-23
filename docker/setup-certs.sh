#!/bin/sh
set -e

# Environment variables
CERT_FILE=${CERT_FILE:-/app/certs/server.crt}
KEY_FILE=${KEY_FILE:-/app/certs/server.key}
DUCKDNS_DOMAIN=${DUCKDNS_DOMAIN}
DUCKDNS_TOKEN=${DUCKDNS_TOKEN}
LETSENCRYPT_EMAIL=${LETSENCRYPT_EMAIL}

# Validate required environment variables
if [ -z "$DUCKDNS_DOMAIN" ] || [ -z "$DUCKDNS_TOKEN" ] || [ -z "$LETSENCRYPT_EMAIL" ]; then
    echo "ERROR: DUCKDNS_DOMAIN, DUCKDNS_TOKEN, and LETSENCRYPT_EMAIL must be set"
    exit 1
fi

# Create DuckDNS credentials file
mkdir -p /app
cat > /app/duckdns-credentials.ini <<EOF
dns_duckdns_token = ${DUCKDNS_TOKEN}
EOF
chmod 600 /app/duckdns-credentials.ini

# Create certificate directories
mkdir -p /app/certs
mkdir -p /etc/letsencrypt

# Check if certificates already exist and are valid
if [ -f "/etc/letsencrypt/live/${DUCKDNS_DOMAIN}/fullchain.pem" ]; then
    echo "Certificates already exist for ${DUCKDNS_DOMAIN}"
    # Check if certificate is still valid (more than 30 days)
    if openssl x509 -checkend 2592000 -noout -in "/etc/letsencrypt/live/${DUCKDNS_DOMAIN}/fullchain.pem" 2>/dev/null; then
        echo "Certificate is valid for more than 30 days, skipping renewal"
    else
        echo "Certificate expires soon, attempting renewal..."
        certbot renew --quiet || echo "Renewal failed, will retry via cron"
    fi
else
    echo "Obtaining new certificate for ${DUCKDNS_DOMAIN}..."
    certbot certonly \
        --non-interactive \
        --agree-tos \
        --email "${LETSENCRYPT_EMAIL}" \
        --authenticator dns-duckdns \
        --dns-duckdns-credentials /app/duckdns-credentials.ini \
        --dns-duckdns-propagation-seconds 60 \
        -d "${DUCKDNS_DOMAIN}"
fi

# Create symlinks to expected paths
ln -sf "/etc/letsencrypt/live/${DUCKDNS_DOMAIN}/fullchain.pem" "${CERT_FILE}"
ln -sf "/etc/letsencrypt/live/${DUCKDNS_DOMAIN}/privkey.pem" "${KEY_FILE}"

echo "Certificate setup complete"
echo "Certificate: ${CERT_FILE}"
echo "Private key: ${KEY_FILE}"

# Setup cron job for automatic renewal (daily at 2 AM)
echo "0 2 * * * certbot renew --quiet --deploy-hook 'pkill -HUP server' 2>&1 | logger -t certbot" | crontab -

echo "Cron job configured for automatic certificate renewal"
