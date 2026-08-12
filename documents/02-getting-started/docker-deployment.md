# Docker Deployment Guide

This guide covers deploying Local Music Queue behind Nginx Proxy Manager using Docker Compose and GitHub Actions.

## Prerequisites

- **Nginx Proxy Manager** instance on the same Docker host.
- **Self-hosted GitHub Actions runner** with Docker and PowerShell.
- **Docker Compose v2.17+** with `up --wait` and `--wait-timeout` support. The release workflow verifies both flags before deployment.
- A **custom certificate** imported into Nginx Proxy Manager for the deployment hostname.
- **DNS** record pointing the hostname to the host's public IP.

---

## 1. Network Setup

Nginx Proxy Manager and the application communicate over a shared external Docker network. Create it once:

```bash
docker network create nginx-proxy-manager
```

Attach NPM to the network if it is not already connected:

```bash
docker network connect nginx-proxy-manager <npm-container-name>
```

Record the exact network name — it becomes the `PROXY_NETWORK_NAME` variable.

---

## 2. GitHub Environment Setup

Release deployment configuration lives in the `local-server` GitHub Environment. No production `.env` file is used.

| Name | Type | Description |
| --- | --- | --- |
| `PROXY_NETWORK_NAME` | Variable | Docker network shared with NPM (e.g. `nginx-proxy-manager`) |
| `GOOGLE_CLIENT_ID` | Secret | Google OAuth 2.0 Client ID |
| `HOST_EMAILS` | Secret | Comma-separated host emails |
| `ADMIN_EMAILS` | Secret | Comma-separated admin emails |

---

## 3. Nginx Proxy Manager Proxy Host

In the NPM admin interface, add a proxy host:

- **Domain Names**: the deployment hostname (e.g. `music.example.com`)
- **Scheme**: `http`
- **Forward Hostname / IP**: `local-music-queue-frontend`
- **Forward Port**: `80`
- **WebSocket Support**: enabled
- **Force SSL**: enabled
- **SSL Certificate**: select the custom certificate

---

## 4. Google OAuth Authorized Origin

In Google Cloud Console, add the deployment hostname to the OAuth client's **Authorized JavaScript origins**:

```text
https://music.example.com
```

Update the **Authorized redirect URIs** to match.

---

## 5. Release Deployment

Deployment is triggered by publishing a GitHub Release. The workflow:

1. Validates `PROXY_NETWORK_NAME` is set and the network exists.
2. Checks Docker Compose supports `--wait` / `--wait-timeout`.
3. Validates the Compose configuration with `docker compose config --quiet`.
4. Runs `docker compose up -d --build --remove-orphans --wait --wait-timeout 120`.

Health checks on both services gate the `--wait` step.

---

## 6. Post-Deployment Checks

Verify the deployment through the public hostname:

- **HTTPS page load** — padlock icon, no mixed-content warnings.
- **Google login** from the authorized origin.
- **REST**: `GET /api/queue` returns queue JSON.
- **Mutation**: queue a song through `/api/queue/add`.
- **WebSocket**: initial `full_sync` arrives after login.
- **WebSocket delta**: events arrive after a mutation.
- **WebSocket reconnect**: client reconnects after a controlled disconnect.
- **Backend outbound**: search YouTube returns results.
- **Database persistence**: a queue item recorded before deployment remains after the release workflow completes, confirming the unchanged `backend-db` volume was reused.
- **No published ports**: `docker ps` shows no host ports for either service.

---

## 7. Rollback

To roll back without bypassing the `local-server` GitHub Environment:

1. Open **GitHub Actions** → **Deploy on Release**.
2. Select the successful workflow run for the preceding published release.
3. Choose **Re-run all jobs**.
4. Wait for both services to become healthy, then repeat the post-deployment checks.

Re-running that release preserves its workflow and injects production values from the `local-server` GitHub Environment. Keep obsolete GitHub values until the 24-hour migration window ends because the preceding release may still require them. The workflow reuses the `backend-db` named volume; never run `docker compose down -v`.

---

## 8. Cleanup of Old Certificate Artifacts

After the new deployment has run healthy for at least 24 hours, the previous deployment's certificate volumes and secrets can be removed manually:

- Docker volumes: `backend-letsencrypt`, `frontend-letsencrypt`.
- GitHub Environment secrets: `DUCKDNS_DOMAIN`, `DUCKDNS_TOKEN`, `LETSENCRYPT_EMAIL`.
- GitHub Environment variables: `FRONTEND_HTTP_PORT`, `FRONTEND_HTTPS_PORT`, `BACKEND_PORT`, `VITE_API_BASE_URL`.
- Host directories: `letsencrypt-backend/`, `letsencrypt-frontend/`.

Do not remove `backend-db`.
