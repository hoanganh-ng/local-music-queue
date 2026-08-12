# HTTPS Setup with Nginx Proxy Manager

This guide covers HTTPS termination for Local Music Queue using an existing Nginx Proxy Manager instance with a custom certificate.

HTTPS is owned entirely by Nginx Proxy Manager. The application containers run plain HTTP and do not handle certificates, renewal, or TLS termination.

## Prerequisites

- **Nginx Proxy Manager** installed and running on the same Docker host.
- A **custom SSL certificate** for the deployment hostname, ready to import into Nginx Proxy Manager.
- The application's Docker services deployed per the [Docker Deployment Guide](../02-getting-started/docker-deployment.md).

---

## 1. Import Certificate into NPM

In the Nginx Proxy Manager admin interface:

1. Navigate to **SSL Certificates** → **Add SSL Certificate** → **Custom**.
2. Provide a name (e.g. `music-queue`).
3. Paste the certificate, private key, and any intermediate chain.
4. Click **Save**.

---

## 2. Create Proxy Host

1. Navigate to **Hosts** → **Proxy Hosts** → **Add Proxy Host**.
2. Under **Details**:
   - **Domain Names**: enter the deployment hostname (e.g. `music.example.com`).
   - **Scheme**: `http`.
   - **Forward Hostname / IP**: `local-music-queue-frontend`.
   - **Forward Port**: `80`.
   - **WebSocket Support**: toggle on.
3. Under **SSL**:
   - **SSL Certificate**: select the certificate imported in Step 1.
   - **Force SSL**: toggle on.
4. Click **Save**.

---

## 3. Verify Configuration

```bash
# HTTPS page load
curl -I https://music.example.com

# API through NPM
curl https://music.example.com/api/queue

# WebSocket upgrade through NPM
# (verify in browser with developer tools network tab)
```

---

## Certificate Replacement

Nginx Proxy Manager owns the custom certificate lifecycle. Before the current certificate expires, import its replacement under **SSL Certificates**, select the replacement on the Proxy Host, save, and verify HTTPS plus WebSocket access through the public hostname. No application containers, cron jobs, or Compose volumes participate in certificate replacement.

---

## Removing Old Artifacts

After the NPM-proxied deployment has been healthy for 24 hours, the previous deployment's DuckDNS/Let's Encrypt artifacts can be removed:

- Docker volumes: `backend-letsencrypt`, `frontend-letsencrypt`.
- Host directories: `letsencrypt-backend/`, `letsencrypt-frontend/`.
- Certificate scripts: `docker/setup-certs.sh`, `frontend/setup-certs.sh`, `frontend/docker-entrypoint.sh` (already deleted during deployment migration).
