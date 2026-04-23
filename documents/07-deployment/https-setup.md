# HTTPS Setup with Let's Encrypt and DuckDNS

This guide covers setting up HTTPS for Local Music Queue using Let's Encrypt certificates and DuckDNS dynamic DNS.

## Overview

**Components:**
- **DuckDNS**: Free dynamic DNS service (yourapp.duckdns.org)
- **Let's Encrypt**: Free SSL/TLS certificates
- **Certbot**: Automatic certificate management
- **Nginx**: HTTPS termination and reverse proxy

**Benefits:**
- ✅ Free HTTPS certificates
- ✅ Automatic certificate renewal
- ✅ Dynamic DNS (no static IP required)
- ✅ Browser-trusted certificates
- ✅ Secure WebSocket connections (wss://)

---

## Prerequisites

- **Domain**: DuckDNS subdomain (e.g., myapp.duckdns.org)
- **Ports**: 80 and 443 open on your router/firewall
- **Docker**: Docker and Docker Compose installed
- **Email**: Valid email for Let's Encrypt notifications

---

## Step 1: Register DuckDNS Domain

### 1. Create Account

1. Go to [DuckDNS.org](https://www.duckdns.org/)
2. Sign in with Google, GitHub, or other provider
3. You'll receive a **token** (UUID format)

### 2. Create Subdomain

1. Enter desired subdomain (e.g., `myapp`)
2. Click **Add domain**
3. Your domain: `myapp.duckdns.org`
4. Note your **token** (shown at top of page)

### 3. Update IP Address

DuckDNS will automatically detect your current IP. To manually update:

```bash
# Test DuckDNS update
curl "https://www.duckdns.org/update?domains=myapp&token=YOUR_TOKEN&ip="

# Response should be: OK
```

### 4. Verify DNS

```bash
# Check DNS resolution
nslookup myapp.duckdns.org

# Or
dig myapp.duckdns.org

# Should return your public IP
```

---

## Step 2: Configure Environment Variables

Edit `.env` in project root:

```bash
# Frontend Ports
FRONTEND_HTTP_PORT=80
FRONTEND_HTTPS_PORT=443

# Backend Port
BACKEND_PORT=1111

# Google OAuth
GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
HOST_EMAILS=host@urekamedia.vn
ADMIN_EMAILS=admin@urekamedia.vn

# DuckDNS Configuration
DUCKDNS_DOMAIN=myapp.duckdns.org
DUCKDNS_TOKEN=12345678-1234-1234-1234-123456789abc
LETSENCRYPT_EMAIL=admin@yourdomain.com
```

**Important**: Replace with your actual values!

---

## Step 3: Update Google OAuth Settings

Add your DuckDNS domain to Google Cloud Console:

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Navigate to **APIs & Services** → **Credentials**
3. Edit your OAuth 2.0 Client ID
4. Add to **Authorized JavaScript origins**:
   ```
   https://myapp.duckdns.org
   ```
5. Add to **Authorized redirect URIs**:
   ```
   https://myapp.duckdns.org
   ```
6. Click **Save**

---

## Step 4: Configure Docker Compose for HTTPS

The project includes HTTPS support in `docker-compose.yml`. Verify the configuration:

```yaml
version: '3.8'

services:
  backend:
    build:
      context: .
      dockerfile: Dockerfile.backend
    ports:
      - "${BACKEND_PORT}:1111"
    volumes:
      - ./.localdb:/app/data
      - ./letsencrypt-backend:/etc/letsencrypt
    environment:
      - PORT=1111
      - DB_PATH=/app/data/music_queue.db
      - GOOGLE_CLIENT_ID=${GOOGLE_CLIENT_ID}
      - HOST_EMAILS=${HOST_EMAILS}
      - ADMIN_EMAILS=${ADMIN_EMAILS}
      - CERT_FILE=/etc/letsencrypt/live/${DUCKDNS_DOMAIN}/fullchain.pem
      - KEY_FILE=/etc/letsencrypt/live/${DUCKDNS_DOMAIN}/privkey.pem
    restart: unless-stopped

  frontend:
    build:
      context: ./frontend
      dockerfile: Dockerfile
      args:
        - VITE_API_BASE_URL=https://${DUCKDNS_DOMAIN}:${BACKEND_PORT}
        - VITE_GOOGLE_CLIENT_ID=${GOOGLE_CLIENT_ID}
    ports:
      - "${FRONTEND_HTTP_PORT}:80"
      - "${FRONTEND_HTTPS_PORT}:443"
    volumes:
      - ./letsencrypt-frontend:/etc/letsencrypt
      - ./frontend-certs:/etc/nginx/certs
    environment:
      - DUCKDNS_DOMAIN=${DUCKDNS_DOMAIN}
    depends_on:
      - backend
    restart: unless-stopped
```

---

## Step 5: Generate Let's Encrypt Certificates

### Option A: Using Certbot (Recommended)

```bash
# Install certbot
sudo apt-get update
sudo apt-get install certbot

# Generate certificate (standalone mode)
sudo certbot certonly --standalone \
  -d myapp.duckdns.org \
  --email admin@yourdomain.com \
  --agree-tos \
  --non-interactive

# Certificates will be in:
# /etc/letsencrypt/live/myapp.duckdns.org/fullchain.pem
# /etc/letsencrypt/live/myapp.duckdns.org/privkey.pem
```

### Option B: Using Docker Certbot

```bash
# Create certificate directory
mkdir -p letsencrypt-frontend letsencrypt-backend

# Run certbot in Docker
docker run -it --rm \
  -v $(pwd)/letsencrypt-frontend:/etc/letsencrypt \
  -p 80:80 \
  certbot/certbot certonly --standalone \
  -d myapp.duckdns.org \
  --email admin@yourdomain.com \
  --agree-tos \
  --non-interactive

# Copy certificates for backend
cp -r letsencrypt-frontend/* letsencrypt-backend/
```

### Option C: Using Setup Script

The project includes a setup script:

```bash
cd frontend
chmod +x setup-certs.sh
./setup-certs.sh
```

---

## Step 6: Configure Nginx for HTTPS

Edit `frontend/nginx.conf`:

```nginx
server {
    listen 80;
    server_name myapp.duckdns.org;
    
    # Redirect HTTP to HTTPS
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name myapp.duckdns.org;
    
    # SSL Configuration
    ssl_certificate /etc/letsencrypt/live/myapp.duckdns.org/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/myapp.duckdns.org/privkey.pem;
    
    # SSL Security Settings
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;
    
    # Security Headers
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    
    # Root directory
    root /usr/share/nginx/html;
    index index.html;
    
    # SPA routing
    location / {
        try_files $uri $uri/ /index.html;
    }
    
    # API proxy
    location /api {
        proxy_pass http://backend:1111;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
    
    # WebSocket proxy
    location /ws {
        proxy_pass http://backend:1111;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

---

## Step 7: Start the Application

```bash
# Build and start with HTTPS
docker-compose up -d --build

# Check logs
docker-compose logs -f

# Verify certificates are loaded
docker-compose exec frontend ls -la /etc/letsencrypt/live/
docker-compose exec backend ls -la /etc/letsencrypt/live/
```

---

## Step 8: Verify HTTPS

### Test Certificate

```bash
# Check certificate
openssl s_client -connect myapp.duckdns.org:443 -servername myapp.duckdns.org

# Should show:
# - Verify return code: 0 (ok)
# - Certificate chain from Let's Encrypt
```

### Test in Browser

1. Open `https://myapp.duckdns.org`
2. Check for padlock icon in address bar
3. Click padlock → Certificate should show:
   - Issued by: Let's Encrypt
   - Valid until: (90 days from issue)

### Test WebSocket

```bash
# Test WSS connection
wscat -c wss://myapp.duckdns.org/ws

# Should connect successfully
```

---

## Certificate Renewal

Let's Encrypt certificates expire after **90 days**.

### Manual Renewal

```bash
# Renew certificate
sudo certbot renew

# Or with Docker
docker run -it --rm \
  -v $(pwd)/letsencrypt-frontend:/etc/letsencrypt \
  -p 80:80 \
  certbot/certbot renew

# Restart services
docker-compose restart
```

### Automatic Renewal (Cron)

```bash
# Edit crontab
crontab -e

# Add renewal job (runs daily at 2am)
0 2 * * * certbot renew --quiet && docker-compose -f /path/to/local-music-queue/docker-compose.yml restart
```

### Renewal Script

Create `renew-certs.sh`:

```bash
#!/bin/bash

# Renew certificates
certbot renew --quiet

# Copy to backend
cp -r /etc/letsencrypt/* /path/to/local-music-queue/letsencrypt-backend/

# Restart services
cd /path/to/local-music-queue
docker-compose restart

echo "Certificates renewed: $(date)" >> /var/log/cert-renewal.log
```

Make executable and add to cron:
```bash
chmod +x renew-certs.sh
crontab -e
# Add: 0 2 * * * /path/to/renew-certs.sh
```

---

## Troubleshooting

### Certificate generation fails

**Error**: `Failed to connect to port 80`

**Fix**:
```bash
# Stop any service using port 80
sudo lsof -i :80
sudo systemctl stop nginx  # If running

# Ensure port 80 is open in firewall
sudo ufw allow 80
sudo ufw allow 443
```

### Domain not resolving

**Check**:
```bash
# Verify DNS
nslookup myapp.duckdns.org

# Update DuckDNS IP
curl "https://www.duckdns.org/update?domains=myapp&token=YOUR_TOKEN&ip="
```

### Certificate not found in container

**Fix**:
```bash
# Check volume mount
docker-compose exec frontend ls -la /etc/letsencrypt/live/

# Verify .env has correct domain
echo $DUCKDNS_DOMAIN

# Rebuild with correct domain
docker-compose down
docker-compose up -d --build
```

### Browser shows "Not Secure"

**Check**:
1. Certificate is valid: `openssl s_client -connect myapp.duckdns.org:443`
2. Nginx is using correct certificate paths
3. Certificate hasn't expired
4. Domain matches certificate CN

### WebSocket connection fails over HTTPS

**Check**:
1. Using `wss://` (not `ws://`)
2. Nginx has WebSocket proxy configuration
3. Backend is reachable from frontend container

**Fix** `frontend/src/services/websocket.js`:
```javascript
// Use wss:// for HTTPS
const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
const wsUrl = `${protocol}//${window.location.host}/ws`
```

---

## Security Best Practices

### 1. Use Strong SSL Configuration

```nginx
# Modern SSL configuration
ssl_protocols TLSv1.3 TLSv1.2;
ssl_ciphers 'ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256';
ssl_prefer_server_ciphers off;

# Enable OCSP stapling
ssl_stapling on;
ssl_stapling_verify on;
```

### 2. Add Security Headers

```nginx
add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload" always;
add_header X-Frame-Options "DENY" always;
add_header X-Content-Type-Options "nosniff" always;
add_header X-XSS-Protection "1; mode=block" always;
add_header Referrer-Policy "strict-origin-when-cross-origin" always;
```

### 3. Restrict Access

```nginx
# Allow only specific IPs (optional)
allow 192.168.1.0/24;  # Local network
deny all;
```

### 4. Rate Limiting

```nginx
# Limit requests
limit_req_zone $binary_remote_addr zone=api:10m rate=10r/s;

location /api {
    limit_req zone=api burst=20;
    proxy_pass http://backend:1111;
}
```

---

## Cost

**Total Cost: $0/year**

- DuckDNS: Free
- Let's Encrypt: Free
- Renewal: Automatic and free

---

## Next Steps

- [Nginx Configuration](nginx-configuration.md) - Advanced Nginx setup
- [Production Checklist](production-checklist.md) - Pre-launch verification
- [Docker Deployment](../02-getting-started/docker-deployment.md) - Docker basics
