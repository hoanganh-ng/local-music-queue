# Environment Variables Reference

Complete reference for all environment variables used in Local Music Queue.

## Local Development Variables

### Backend

| Variable | Description | Default | Required |
| --- | --- | --- | --- |
| `PORT` | HTTP server port | `1111` | No |
| `GOOGLE_CLIENT_ID` | Google OAuth 2.0 Client ID | None | Yes |
| `HOST_EMAILS` | Comma-separated host emails | None | Yes |
| `ADMIN_EMAILS` | Comma-separated admin emails | None | No |
| `DB_PATH` | SQLite database path | `./.localdb/music_queue.db` | No |
| `YTDLP_PATH` | yt-dlp executable path | `yt-dlp` | No |
| `CERT_FILE` | TLS certificate path (optional, non-Compose only) | None | No |
| `KEY_FILE` | TLS key path (optional, non-Compose only) | None | No |

### Frontend

| Variable | Description | Default | Required |
| --- | --- | --- | --- |
| `VITE_API_BASE_URL` | Backend API base URL (development only) | Current browser origin | No |
| `VITE_GOOGLE_CLIENT_ID` | Google OAuth 2.0 Client ID | None | Yes |

`VITE_API_BASE_URL` is optional and development-only. In production the frontend constructs same-origin URLs from the browser's current origin.

---

## Release Deployment (GitHub Environment)

Release deployments use the `local-server` GitHub Environment. No production `.env` file is used.

| Name | Type | Description |
| --- | --- | --- |
| `PROXY_NETWORK_NAME` | Variable | Name of the existing Docker network shared with Nginx Proxy Manager |
| `GOOGLE_CLIENT_ID` | Secret | Google OAuth 2.0 Client ID |
| `HOST_EMAILS` | Secret | Comma-separated host emails |
| `ADMIN_EMAILS` | Secret | Comma-separated admin emails |

---

## Compose-Fixed Variables

These values are fixed in `docker-compose.yml` for production containers and are not configurable per-environment:

| Variable | Value |
| --- | --- |
| `APP_ENV` | `production` |
| `PORT` | `1111` |
| `DB_PATH` | `/app/data/music_queue.db` |
| `YTDLP_PATH` | `/usr/bin/yt-dlp` |

---

## Removed Variables

The following variables are no longer part of the production deployment contract:

- `DUCKDNS_DOMAIN` — DuckDNS subdomain (HTTPS now terminated by NPM).
- `DUCKDNS_TOKEN` — DuckDNS API token (HTTPS now terminated by NPM).
- `LETSENCRYPT_EMAIL` — Let's Encrypt registration email (certificates managed by NPM).
- `FRONTEND_HTTP_PORT` — Published host port (no application host ports).
- `FRONTEND_HTTPS_PORT` — Published host port (no application host ports).
- `BACKEND_PORT` — Published host port (no application host ports).
- `CERT_FILE`, `KEY_FILE` in container environment — certificate paths (managed by NPM).
- `VITE_API_BASE_URL` in production build — frontend uses same-origin URLs.
