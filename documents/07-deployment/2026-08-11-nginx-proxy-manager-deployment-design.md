# Nginx Proxy Manager Deployment Design

**Date:** 2026-08-11
**Status:** Implemented; pending production release and live NPM verification

## Goal

Replace the direct DuckDNS and Let's Encrypt deployment with a single-host Docker deployment behind an existing Nginx Proxy Manager (NPM) instance. NPM owns the public hostname, HTTPS redirect, custom certificate, and TLS lifecycle. Local Music Queue serves HTTP only inside Docker.

## Scope

This design covers:

- Docker networking between NPM, the frontend, and the backend.
- Same-origin REST and WebSocket routing through one public hostname.
- Removal of DuckDNS, Certbot, Let's Encrypt, and certificate-renewal responsibilities from the application images.
- Deployment configuration supplied by a GitHub Environment to the existing self-hosted release runner.
- Health checks, deployment validation, migration, and manual rollback.

The design does not automate NPM through its API, manage public DNS, issue or renew certificates, change application authentication or authorization, change the SQLite schema, or implement automatic rollback.

## Selected Architecture

```text
Browser
  | HTTPS / WSS
  v
Nginx Proxy Manager
  | HTTP over the shared Docker proxy network
  v
Frontend Nginx :80
  |-- /              -> Vue static files
  |-- /api/*         -> backend:1111
  `-- /ws            -> backend:1111 with WebSocket upgrade
                           |
                           v
                       Go backend
```

NPM forwards the complete public hostname to the frontend container. The frontend Nginx instance is the application's only ingress and proxies backend traffic over the project network. This keeps application routing in version control, avoids a second public backend origin, and exposes only the frontend to NPM.

## Network Boundaries

The deployment uses two Docker bridge networks:

1. `proxy`: an existing external network whose name is supplied as `PROXY_NETWORK_NAME`. NPM and the frontend join this network. The frontend has the stable network alias `local-music-queue-frontend`.
2. `app`: a Compose-managed project network joined by the frontend and backend. The frontend resolves the backend through Docker DNS as `backend:1111`.

The `app` network is project-private but must not set Compose `internal: true`. The backend needs outbound access to Google and YouTube-related services. Neither application service publishes a host port. `expose` may document ports `80` and `1111`, but it does not publish them.

Docker service names and network aliases are used instead of container IP addresses. The frontend proxy configuration must use Docker's embedded DNS resolver at `127.0.0.11`, with a 30-second validity interval, so a recreated backend can be resolved without a fixed IP or frontend restart.

## TLS and Certificate Ownership

NPM exclusively owns:

- The public hostname.
- Port 80 to 443 redirection.
- The custom TLS certificate and private key.
- Certificate replacement and renewal.
- The external HTTPS and WSS connection.

The application stack receives plain HTTP from NPM. Production Compose does not pass `CERT_FILE` or `KEY_FILE`. The backend's generic optional TLS support may remain for compatibility outside this deployment, but the production container does not activate it.

The backend and frontend images remove their DuckDNS/Let's Encrypt runtime dependencies and startup behavior:

- Certbot and `certbot-dns-duckdns`.
- Python and pip when no other runtime use remains.
- Cron and certificate-renewal jobs.
- DuckDNS credential-file generation.
- Certificate setup entrypoints and certificate volumes.
- Frontend HTTPS listener and HTTP-to-HTTPS redirect.

## Nginx Proxy Manager Configuration

Create one Proxy Host in NPM:

| Setting | Value |
| --- | --- |
| Domain name | Deployment hostname, for example `music.example.com` |
| Scheme | `http` |
| Forward hostname | `local-music-queue-frontend` |
| Forward port | `80` |
| WebSocket support | Enabled |
| Force SSL | Enabled |
| Certificate | Existing NPM-managed custom certificate |

NPM must already be attached to the Docker network named by `PROXY_NETWORK_NAME`. Public or private DNS for the hostname is an infrastructure responsibility outside this repository.

## Frontend Routing

The production frontend Nginx server listens on port `80` and has three routing responsibilities:

- `/` serves the Vue build and retains the SPA fallback to `index.html`.
- `/api/` preserves the request URI and proxies it to `http://backend:1111`.
- `/ws` proxies to `http://backend:1111` with the required WebSocket `Upgrade` and `Connection` headers.

Forwarded requests preserve the host and client forwarding headers. The original public scheme must remain `https` in `X-Forwarded-Proto`; the frontend proxy must not overwrite it with the internal HTTP scheme.

The WebSocket proxy read timeout is 75 seconds, which exceeds the backend's 30-second ping interval and permits the existing ping frames to keep idle connections alive.

## Browser Client Configuration

Production browser traffic uses the current origin:

- REST requests use `/api/...`.
- WebSocket requests derive `ws:` or `wss:` from `window.location.protocol` and use the current host plus `/ws`.

`VITE_API_BASE_URL` is not supplied by the production deployment. An optional explicit API base URL may remain available for local development and compatibility testing. Existing REST payloads, WebSocket event contracts, and backend CORS behavior remain unchanged.

## GitHub Environment Configuration

The existing self-hosted GitHub Actions runner executes against the same Docker daemon as NPM. Production does not use a `.env` file. The workflow maps values from the existing `local-server` GitHub Environment into the deploy step.

Required GitHub Environment variable:

```text
PROXY_NETWORK_NAME
```

Required GitHub Environment secrets:

```text
GOOGLE_CLIENT_ID
HOST_EMAILS
ADMIN_EMAILS
```

The workflow passes `GOOGLE_CLIENT_ID` to the backend and uses the same value as the frontend `VITE_GOOGLE_CLIENT_ID` build argument. A Google OAuth client ID is necessarily visible in the built browser application; storing it as a GitHub secret prevents incidental workflow disclosure but does not make it a confidential browser credential.

Obsolete GitHub configuration:

```text
DUCKDNS_DOMAIN
DUCKDNS_TOKEN
LETSENCRYPT_EMAIL
FRONTEND_HTTP_PORT
FRONTEND_HTTPS_PORT
BACKEND_PORT
VITE_API_BASE_URL
VITE_GOOGLE_CLIENT_ID
```

No workflow command may print the resolved Compose configuration, role email lists, or injected environment values. The backend startup log must not print `HOST_EMAILS` or `ADMIN_EMAILS`.

## Release Workflow

The release-triggered workflow retains the `local-server` environment and PowerShell shell. Its deployment sequence is:

1. Check out the published release.
2. Validate that `PROXY_NETWORK_NAME` is non-empty.
3. Run `docker network inspect` for the exact configured proxy network and fail if it does not exist.
4. Run `docker compose config --quiet` to validate interpolation without rendering resolved sensitive values.
5. Run `docker compose up -d --build --remove-orphans --wait --wait-timeout 120`.
6. Fail the job if either application health check does not become healthy.

The self-hosted runner requires Docker Compose v2.17+ with both `up --wait` and `--wait-timeout`. The release workflow checks both flags before changing the running application.

## Health Checks and Failure Behavior

The backend health check runs `wget --quiet --spider http://127.0.0.1:1111/api/queue`. The frontend health check runs `wget --quiet --spider http://127.0.0.1/`. Both runtime images are Alpine-based and retain BusyBox `wget` for these checks. Health checks use a 10-second interval, 3-second timeout, 5 retries, and 10-second start period.

Expected failures are handled as follows:

- Missing `PROXY_NETWORK_NAME`: the workflow fails before Compose validation or build.
- Missing external Docker network: the workflow fails before changing running containers.
- Invalid Compose interpolation: `docker compose config --quiet` fails before build.
- Unhealthy frontend or backend: `docker compose up --wait` returns failure and the release job fails.
- NPM or certificate outage: the application containers remain available over internal HTTP, while public access remains unavailable until the infrastructure issue is fixed.
- Backend recreation: frontend Nginx re-resolves the `backend` service through Docker DNS.

Deployment failure does not imply automatic rollback. Operators use the preceding release for rollback and inspect container health without printing secret environment values.

## Migration Procedure

1. Confirm the intended custom certificate is installed and valid in NPM.
2. Confirm the deployment hostname resolves to NPM through the chosen external DNS mechanism.
3. Attach NPM to the external Docker network that will be stored as `PROXY_NETWORK_NAME`.
4. Create the NPM Proxy Host using the settings in this design. It may remain unavailable until the new frontend alias exists.
5. Add the required variable and secrets to the `local-server` GitHub Environment.
6. Add the new HTTPS origin to the Google OAuth client's authorized JavaScript origins.
7. Publish the release containing the implementation.
8. Verify the application through NPM.
9. Retain the old certificate volumes and old GitHub values temporarily for manual rollback.
10. After the new release has remained healthy for 24 hours, manually remove obsolete DuckDNS/Let's Encrypt secrets and unused certificate volumes.

The migration must not run `docker compose down -v` or otherwise delete the SQLite volume.

## Verification

Implementation verification must include:

```bash
go test ./...
(cd frontend && npm run test:unit -- --run)
(cd frontend && npm run build)
export PROXY_NETWORK_NAME=local-music-queue-plan-test
export GOOGLE_CLIENT_ID=test.apps.googleusercontent.com
export HOST_EMAILS=host@example.invalid
export ADMIN_EMAILS=admin@example.invalid
docker compose config --quiet
docker compose build
```

The documented baseline reports a pre-existing permission error under a local `letsencrypt-backend` directory that can block `go test ./...`. If still present, the operator must correct that filesystem ownership outside the implementation; the deployment change must not delete the directory automatically or claim the blocked command passed.

Deployment verification must include:

- Both Compose services reach `healthy` state.
- No frontend or backend host ports are published.
- NPM resolves `local-music-queue-frontend` on the configured external network.
- The HTTPS page loads through the deployment hostname.
- Google login succeeds from the new authorized origin.
- A representative REST read and mutation succeed through `/api`.
- A WebSocket connects through `/ws`, receives the initial full sync, receives a live delta, and reconnects after a controlled disconnect.
- The backend retains outbound access required by Google and YouTube integrations.
- Existing SQLite queue data survives the deployment.

## Rollback

Rollback re-runs the preceding published release's successful **Deploy on Release** workflow in GitHub Actions. This preserves `local-server` GitHub Environment injection and reuses the same persistent SQLite volume. Because this design introduces no schema migration, application data does not require transformation. Obsolete GitHub values and old certificate volumes remain available during the 24-hour migration window so the preceding release stays deployable. Never run `docker compose down -v`.

Automatic rollback, image registry promotion, blue-green deployment, and NPM API automation are explicitly deferred.

## Alternatives Considered

### Route `/api` and `/ws` directly in NPM

This would require both application containers on the shared proxy network and would split application routing between repository code and manual NPM configuration. It was rejected because it increases configuration drift and exposes the backend more broadly than necessary.

### Publish host ports for NPM

This avoids a shared external Docker network but retains host-port management and unnecessary exposure. It was rejected because the approved goal is container-to-container routing on the Docker host.

## References

- [Docker Compose networking](https://docs.docker.com/compose/how-tos/networking/)
- [Docker Compose network reference](https://docs.docker.com/reference/compose-file/networks/)
- [GitHub Actions deployment environments](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
- [Nginx WebSocket proxying](https://nginx.org/en/docs/http/websocket.html)
- [Nginx Proxy Manager guide](https://nginxproxymanager.com/guide/)
