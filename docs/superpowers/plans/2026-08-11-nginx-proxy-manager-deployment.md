# Nginx Proxy Manager Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the direct DuckDNS/Let's Encrypt deployment with a same-origin HTTP application stack behind the existing Nginx Proxy Manager instance.

**Architecture:** Nginx Proxy Manager terminates HTTPS and forwards the single public hostname to the frontend Nginx container over an existing external Docker network. The frontend serves Vue and proxies `/api` and `/ws` to the Go backend over a Compose-managed application network; only the frontend joins the NPM network, and neither service publishes host ports.

**Tech Stack:** Docker Compose, multi-stage Alpine container images, Nginx, Go 1.22 `net/http`, Vue 3/Vite, Vitest, GitHub Actions on a self-hosted PowerShell runner.

## Global Constraints

- Follow the approved design in `documents/07-deployment/2026-08-11-nginx-proxy-manager-deployment-design.md`.
- Nginx Proxy Manager owns the hostname, HTTPS redirect, custom certificate, and TLS lifecycle.
- Do not add DuckDNS, Let's Encrypt, Certbot, certificate renewal, public application ports, or NPM API automation.
- Production configuration comes from the `local-server` GitHub Environment; do not create or depend on a production `.env` file.
- Preserve all REST payloads, WebSocket event names/envelopes, SQLite schema, and the `backend-db` volume.
- The application network must allow backend outbound access to Google and YouTube; do not set it to `internal: true`.
- Do not add production dependencies.
- Do not print role email lists, resolved secrets, or resolved Compose configuration.
- Do not run `docker compose down -v` or delete old certificate volumes during migration.
- Do not modify the active sprint pointer or project-state baseline.
- Do not commit, push, or open a pull request; repository instructions reserve history changes for the Product Owner.

---

## File Map

- `frontend/src/services/api.js`: build same-origin REST URLs with an optional development override.
- `frontend/src/services/websocket.js`: build same-origin WebSocket URLs with an optional development override.
- `frontend/src/services/__tests__/api.spec.js`: verify relative production REST URLs and explicit development URLs.
- `frontend/src/services/__tests__/websocket.spec.js`: verify HTTPS/WSS, HTTP/WS, and `user_id` URL construction.
- `frontend/nginx.conf`: serve the SPA and proxy `/api` and `/ws` through Docker DNS.
- `frontend/Dockerfile`: build a port-80-only Nginx runtime without certificate tooling.
- `frontend/docker-entrypoint.sh`: delete; certificate bootstrap is no longer part of the image.
- `frontend/setup-certs.sh`: delete; certificate bootstrap is no longer part of the image.
- `Dockerfile`: build a port-1111 backend runtime without certificate tooling.
- `docker/setup-certs.sh`: delete; certificate bootstrap is no longer part of the image.
- `cmd/server/main.go`: stop logging configured role email values.
- `cmd/server/main_test.go`: verify configured role emails are absent from startup logs.
- `docker-compose.yml`: define the private application network, external NPM network, health checks, and persistent SQLite volume.
- `.github/workflows/deploy.yml`: inject GitHub Environment values, preflight the NPM network, validate Compose quietly, and wait for health.
- `.env.example`: retain only a clearly labeled local/manual reference with no certificate or host-port values.
- `frontend/.env.example`: describe the optional local-development API override rather than DuckDNS production.
- `README.md`: replace the obsolete direct-HTTPS deployment summary.
- `documents/02-getting-started/docker-deployment.md`: replace the old deployment procedure with the NPM/GitHub Environment procedure.
- `documents/02-getting-started/environment-variables.md`: separate local-development variables from GitHub deployment configuration.
- `documents/07-deployment/https-setup.md`: replace the DuckDNS/Let's Encrypt guide with the NPM custom-certificate runbook.
- `documents/README.md`: update the deployment guide description.

---

### Task 1: Same-origin browser clients

**Files:**
- Modify: `frontend/src/services/api.js:1-37`
- Modify: `frontend/src/services/websocket.js:98-115`
- Modify: `frontend/src/services/__tests__/api.spec.js`
- Modify: `frontend/src/services/__tests__/websocket.spec.js`

**Interfaces:**
- Produces: `buildAPIURL(endpoint: string, configuredBase?: string): string`
- Produces: `buildWebSocketURL(configuredBase: string | undefined, browserOrigin: string, userID?: number | string): string`
- Preserves: `api`, `APIError`, `wsClient`, and all existing request/event behavior.

- [ ] **Step 1: Add failing REST URL tests**

Update the API test import and add these cases:

```js
import { api, APIError, buildAPIURL } from '../api'

it('builds a same-origin API URL when no override is configured', () => {
  expect(buildAPIURL('/queue', '')).toBe('/api/queue')
})

it('keeps an explicit development API base URL without a double slash', () => {
  expect(buildAPIURL('/queue', 'http://localhost:1111/'))
    .toBe('http://localhost:1111/api/queue')
})
```

- [ ] **Step 2: Add failing WebSocket URL tests**

Export the new helper in the test import and add:

```js
import { buildWebSocketURL, wsClient } from '../websocket'

it('builds a same-origin secure WebSocket URL with the user ID', () => {
  expect(buildWebSocketURL('', 'https://music.example.com', 42))
    .toBe('wss://music.example.com/ws?user_id=42')
})

it('builds a development WebSocket URL from an explicit HTTP API base', () => {
  expect(buildWebSocketURL('http://localhost:1111/', 'https://music.example.com'))
    .toBe('ws://localhost:1111/ws')
})
```

- [ ] **Step 3: Run the narrow tests and confirm the helpers are missing**

Run:

```bash
cd frontend && npm run test:unit -- --run src/services/__tests__/api.spec.js src/services/__tests__/websocket.spec.js
```

Expected: FAIL because `buildAPIURL` and `buildWebSocketURL` are not exported.

- [ ] **Step 4: Implement REST URL construction**

Replace the module-level `API_BASE` concatenation with:

```js
export function buildAPIURL(endpoint, configuredBase = import.meta.env.VITE_API_BASE_URL) {
  const base = (configuredBase || '').replace(/\/+$/, '')
  return `${base}/api${endpoint}`
}
```

In `api.request`, use:

```js
const url = buildAPIURL(endpoint)
```

- [ ] **Step 5: Implement WebSocket URL construction**

Add this helper near the top of `websocket.js`:

```js
export function buildWebSocketURL(configuredBase, browserOrigin, userID) {
  const url = new URL(configuredBase || browserOrigin)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  url.pathname = `${url.pathname.replace(/\/+$/, '')}/ws`
  url.search = ''
  if (userID) {
    url.searchParams.set('user_id', String(userID))
  }
  return url.toString()
}
```

Replace the current string replacement and query concatenation in `connect()` with:

```js
const currentUser = globalStore.currentUser
const wsUrl = buildWebSocketURL(
  import.meta.env.VITE_API_BASE_URL,
  window.location.origin,
  currentUser?.id
)
```

- [ ] **Step 6: Run the narrow tests**

Run:

```bash
cd frontend && npm run test:unit -- --run src/services/__tests__/api.spec.js src/services/__tests__/websocket.spec.js
```

Expected: both files PASS, including all pre-existing sequence and event tests.

- [ ] **Step 7: Run the frontend suite and production build**

Run:

```bash
(cd frontend && npm run test:unit -- --run)
(cd frontend && npm run build)
```

Expected: all frontend tests PASS and Vite completes a production build.

---

### Task 2: Frontend application gateway container

**Files:**
- Modify: `frontend/nginx.conf`
- Modify: `frontend/Dockerfile`
- Delete: `frontend/docker-entrypoint.sh`
- Delete: `frontend/setup-certs.sh`

**Interfaces:**
- Consumes: backend service DNS name `backend` and internal HTTP port `1111` from Task 4.
- Produces: HTTP ingress on container port `80` for NPM.
- Produces: SPA `/`, REST `/api/*`, and WebSocket `/ws` routing.

- [ ] **Step 1: Record the failing configuration contract**

Run:

```bash
rg -n "listen 443|ssl_certificate|certbot|duckdns|setup-certs|EXPOSE 80 443" frontend/nginx.conf frontend/Dockerfile frontend/docker-entrypoint.sh frontend/setup-certs.sh
```

Expected: matches show that the current frontend still owns TLS and certificate renewal.

- [ ] **Step 2: Replace the Nginx server configuration**

Use this configuration, retaining no HTTPS listener:

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

map $http_x_forwarded_proto $forwarded_proto {
    default $http_x_forwarded_proto;
    ''      $scheme;
}

server {
    listen 80;
    server_name _;

    resolver 127.0.0.11 valid=30s ipv6=off;
    set $backend_upstream backend:1111;

    location /api/ {
        proxy_pass http://$backend_upstream$request_uri;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $forwarded_proto;
    }

    location = /ws {
        proxy_pass http://$backend_upstream$request_uri;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $forwarded_proto;
        proxy_read_timeout 75s;
        proxy_send_timeout 75s;
    }

    location / {
        root /usr/share/nginx/html;
        index index.html;
        try_files $uri $uri/ /index.html;
    }
}
```

- [ ] **Step 3: Reduce the frontend runtime image**

Keep the existing Node build stage and `VITE_GOOGLE_CLIENT_ID` build argument. Remove the `VITE_API_BASE_URL` build argument from the production image contract. Replace the runtime stage with:

```dockerfile
FROM nginx:alpine AS production-stage

COPY --from=build-stage /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
```

Do not add Certbot, Python, pip, cron, OpenSSL, certificate files, or a custom entrypoint.

- [ ] **Step 4: Delete certificate bootstrap scripts**

Delete:

```text
frontend/docker-entrypoint.sh
frontend/setup-certs.sh
```

- [ ] **Step 5: Verify the frontend image contract**

Run:

```bash
docker build --build-arg VITE_GOOGLE_CLIENT_ID=test.apps.googleusercontent.com -t local-music-queue-frontend:plan-test ./frontend
docker run --rm local-music-queue-frontend:plan-test nginx -t
rg -n "listen 443|ssl_certificate|certbot|duckdns|setup-certs|EXPOSE 80 443" frontend/nginx.conf frontend/Dockerfile
```

Expected: image build PASS, `nginx -t` PASS, and `rg` returns no matches.

---

### Task 3: Backend HTTP runtime and startup-log privacy

**Files:**
- Modify: `cmd/server/main.go:61-82`
- Modify: `cmd/server/main_test.go`
- Modify: `Dockerfile`
- Delete: `docker/setup-certs.sh`

**Interfaces:**
- Produces: backend HTTP listener on container port `1111` when `CERT_FILE` and `KEY_FILE` are absent.
- Preserves: generic optional application-level TLS support for non-Compose callers.
- Preserves: `/api` and `/ws` handlers and SQLite data path `/app/data/music_queue.db`.

- [ ] **Step 1: Add a failing startup-log privacy test**

Extend `cmd/server/main_test.go` imports with `bytes`, `log`, `path/filepath`, and `strings`, then add:

```go
func TestSetupAppDoesNotLogConfiguredRoleEmails(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}

	t.Setenv("APP_ENV", "production")
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("YTDLP_PATH", executable)
	t.Setenv("HOST_EMAILS", "host-private@example.com")
	t.Setenv("ADMIN_EMAILS", "admin-private@example.com")

	originalWriter := log.Writer()
	var output bytes.Buffer
	log.SetOutput(&output)
	defer log.SetOutput(originalWriter)

	if _, _, err := setupApp(); err != nil {
		t.Fatalf("setupApp failed: %v", err)
	}

	for _, privateValue := range []string{
		"host-private@example.com",
		"admin-private@example.com",
	} {
		if strings.Contains(output.String(), privateValue) {
			t.Fatalf("startup log exposed configured role email %q", privateValue)
		}
	}
}
```

- [ ] **Step 2: Run the privacy test and confirm the leak**

Run:

```bash
go test ./cmd/server -run TestSetupAppDoesNotLogConfiguredRoleEmails -count=1
```

Expected: FAIL because `setupApp` currently logs both configured email lists.

- [ ] **Step 3: Remove sensitive startup logging**

Remove only these statements from `setupApp`:

```go
log.Printf("Host emails: %v", hostEmails)
log.Printf("Admin emails: %v", adminEmails)
```

Keep parsing and role assignment behavior unchanged.

- [ ] **Step 4: Run the privacy and package tests**

Run:

```bash
go test ./cmd/server -run TestSetupAppDoesNotLogConfiguredRoleEmails -count=1
go test ./cmd/server -count=1
```

Expected: PASS.

- [ ] **Step 5: Reduce the backend runtime image**

Keep the existing Go build stage. Replace the final-stage certificate setup with this runtime contract:

```dockerfile
FROM alpine:latest

WORKDIR /app

RUN apk add --no-cache \
    yt-dlp \
    ffmpeg \
    ca-certificates \
    tzdata

RUN mkdir -p /app/data

COPY --from=builder /app/server .

ENV APP_ENV=production
ENV PORT=1111
ENV DB_PATH=/app/data/music_queue.db
ENV YTDLP_PATH=/usr/bin/yt-dlp

EXPOSE 1111

CMD ["/app/server"]
```

Do not set `CERT_FILE` or `KEY_FILE`.

- [ ] **Step 6: Delete backend certificate bootstrap**

Delete:

```text
docker/setup-certs.sh
```

- [ ] **Step 7: Verify the backend image contract**

Run:

```bash
docker build -t local-music-queue-backend:plan-test .
rg -n "certbot|duckdns|letsencrypt|setup-certs|CERT_FILE|KEY_FILE|EXPOSE 443" Dockerfile cmd/server/main.go
test ! -e docker/setup-certs.sh
```

Expected: image build PASS. Matches are allowed only for the generic `CERT_FILE`/`KEY_FILE` branch in `cmd/server/main.go`; the Dockerfile and `docker/` directory contain no certificate bootstrap references.

---

### Task 4: Compose topology and release deployment workflow

**Files:**
- Modify: `docker-compose.yml`
- Modify: `.github/workflows/deploy.yml`

**Interfaces:**
- Consumes: GitHub Environment variable `PROXY_NETWORK_NAME`.
- Consumes: GitHub Environment secrets `GOOGLE_CLIENT_ID`, `HOST_EMAILS`, and `ADMIN_EMAILS`.
- Produces: external network alias `local-music-queue-frontend:80` for NPM.
- Produces: internal backend endpoint `backend:1111` for frontend Nginx.
- Preserves: named volume `backend-db:/app/data`.

- [ ] **Step 1: Record the obsolete Compose/workflow contract**

Run:

```bash
rg -n "DUCKDNS|LETSENCRYPT|FRONTEND_HTTP_PORT|FRONTEND_HTTPS_PORT|BACKEND_PORT|VITE_API_BASE_URL|ports:|443" docker-compose.yml .github/workflows/deploy.yml
```

Expected: matches identify the certificate variables, host ports, and separate API origin that must be removed.

- [ ] **Step 2: Replace the Compose service topology**

Implement the following structure, preserving the existing build contexts and named database volume:

```yaml
services:
  backend:
    build:
      context: .
      dockerfile: Dockerfile
    environment:
      APP_ENV: production
      PORT: "1111"
      DB_PATH: /app/data/music_queue.db
      YTDLP_PATH: /usr/bin/yt-dlp
      GOOGLE_CLIENT_ID: ${GOOGLE_CLIENT_ID:?GOOGLE_CLIENT_ID is required}
      HOST_EMAILS: ${HOST_EMAILS:?HOST_EMAILS is required}
      ADMIN_EMAILS: ${ADMIN_EMAILS:?ADMIN_EMAILS is required}
    expose:
      - "1111"
    volumes:
      - backend-db:/app/data
    healthcheck:
      test: ["CMD", "wget", "--quiet", "--spider", "http://127.0.0.1:1111/api/queue"]
      interval: 10s
      timeout: 3s
      retries: 5
      start_period: 10s
    restart: unless-stopped
    networks:
      - app

  frontend:
    build:
      context: ./frontend
      dockerfile: Dockerfile
      args:
        VITE_GOOGLE_CLIENT_ID: ${GOOGLE_CLIENT_ID:?GOOGLE_CLIENT_ID is required}
    expose:
      - "80"
    depends_on:
      backend:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "--quiet", "--spider", "http://127.0.0.1/"]
      interval: 10s
      timeout: 3s
      retries: 5
      start_period: 10s
    restart: unless-stopped
    networks:
      app:
      proxy:
        aliases:
          - local-music-queue-frontend

networks:
  app:
    driver: bridge
  proxy:
    external: true
    name: ${PROXY_NETWORK_NAME:?PROXY_NETWORK_NAME is required}

volumes:
  backend-db:
    driver: local
```

Do not keep `container_name`, `ports`, certificate environment values, or certificate volumes. Do not mark `app` as `internal: true`.

- [ ] **Step 3: Update GitHub Environment mappings**

Replace the deploy step environment with:

```yaml
env:
  PROXY_NETWORK_NAME: ${{ vars.PROXY_NETWORK_NAME }}
  GOOGLE_CLIENT_ID: ${{ secrets.GOOGLE_CLIENT_ID }}
  HOST_EMAILS: ${{ secrets.HOST_EMAILS }}
  ADMIN_EMAILS: ${{ secrets.ADMIN_EMAILS }}
```

- [ ] **Step 4: Add safe PowerShell preflight and deployment commands**

Use this deploy body:

```powershell
if ([string]::IsNullOrWhiteSpace($env:PROXY_NETWORK_NAME)) {
  throw "PROXY_NETWORK_NAME is required"
}

docker network inspect $env:PROXY_NETWORK_NAME | Out-Null
if ($LASTEXITCODE -ne 0) {
  throw "Docker proxy network '$env:PROXY_NETWORK_NAME' does not exist"
}

$composeUpHelp = docker compose up --help | Out-String
if ($LASTEXITCODE -ne 0 -or
    -not $composeUpHelp.Contains("--wait") -or
    -not $composeUpHelp.Contains("--wait-timeout")) {
  throw "Docker Compose must support up --wait and --wait-timeout"
}

docker compose config --quiet
if ($LASTEXITCODE -ne 0) {
  throw "Docker Compose configuration validation failed"
}

docker compose up -d --build --remove-orphans --wait --wait-timeout 120
if ($LASTEXITCODE -ne 0) {
  throw "Docker Compose deployment did not become healthy"
}
```

Do not call `docker compose config` without `--quiet`, `Get-ChildItem Env:`, or any equivalent environment dump.

- [ ] **Step 5: Validate interpolation with synthetic values**

Run from Bash with non-sensitive synthetic values:

```bash
PROXY_NETWORK_NAME=local-music-queue-plan-test \
GOOGLE_CLIENT_ID=test.apps.googleusercontent.com \
HOST_EMAILS=host@example.invalid \
ADMIN_EMAILS=admin@example.invalid \
docker compose config --quiet
```

Expected: PASS with no rendered configuration output.

- [ ] **Step 6: Confirm required-variable failures**

Run in an environment where `PROXY_NETWORK_NAME` is explicitly empty:

```bash
PROXY_NETWORK_NAME= \
GOOGLE_CLIENT_ID=test.apps.googleusercontent.com \
HOST_EMAILS=host@example.invalid \
ADMIN_EMAILS=admin@example.invalid \
docker compose config --quiet
```

Expected: FAIL with `PROXY_NETWORK_NAME is required` and no secret values.

- [ ] **Step 7: Run an isolated Compose smoke test**

Use a unique, explicit test network and project name:

```bash
docker network create local-music-queue-plan-test
PROXY_NETWORK_NAME=local-music-queue-plan-test \
GOOGLE_CLIENT_ID=test.apps.googleusercontent.com \
HOST_EMAILS=host@example.invalid \
ADMIN_EMAILS=admin@example.invalid \
docker compose -p local-music-queue-plan-test up -d --build --wait --wait-timeout 120
docker compose -p local-music-queue-plan-test ps
docker run --rm --network local-music-queue-plan-test busybox:1.37 wget -qO- http://local-music-queue-frontend/api/queue
```

Expected: both services report healthy; the request through the frontend alias returns queue JSON.

Clean up only the explicitly named test project and network after recording results:

```bash
PROXY_NETWORK_NAME=local-music-queue-plan-test \
GOOGLE_CLIENT_ID=test.apps.googleusercontent.com \
HOST_EMAILS=host@example.invalid \
ADMIN_EMAILS=admin@example.invalid \
docker compose -p local-music-queue-plan-test down
docker network rm local-music-queue-plan-test
```

Do not pass `-v`; the command must not remove named volumes.

---

### Task 5: Deployment documentation and final verification

**Files:**
- Modify: `.env.example`
- Modify: `frontend/.env.example`
- Modify: `README.md:14-24,100-139,156-169`
- Modify: `documents/02-getting-started/docker-deployment.md`
- Modify: `documents/02-getting-started/environment-variables.md`
- Modify: `documents/07-deployment/https-setup.md`
- Modify: `documents/README.md:18-22,54-59`
- Verify: `documents/07-deployment/2026-08-11-nginx-proxy-manager-deployment-design.md`

**Interfaces:**
- Documents: NPM target `local-music-queue-frontend:80` over `PROXY_NETWORK_NAME`.
- Documents: GitHub Environment variable `PROXY_NETWORK_NAME`.
- Documents: GitHub Environment secrets `GOOGLE_CLIENT_ID`, `HOST_EMAILS`, and `ADMIN_EMAILS`.
- Documents: no production `.env`, public application ports, DuckDNS, Let's Encrypt, or Certbot.

- [ ] **Step 1: Update environment examples**

Make root `.env.example` explicitly local/manual and limited to:

```dotenv
# Local or manual Compose use only.
# Release deployments use the GitHub Environment named local-server.
PROXY_NETWORK_NAME=nginx-proxy-manager
GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
HOST_EMAILS=host@example.com
ADMIN_EMAILS=admin@example.com
```

Make `frontend/.env.example` development-only:

```dotenv
# Optional in local development. Production uses the browser's current origin.
VITE_API_BASE_URL=http://localhost:1111
VITE_GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
```

- [ ] **Step 2: Replace obsolete README deployment claims**

Document these exact production facts:

- HTTPS is terminated by an existing Nginx Proxy Manager instance using a custom certificate.
- NPM forwards to `local-music-queue-frontend:80` over the shared external Docker network.
- The frontend proxies `/api` and `/ws` to `backend:1111`.
- Release deployment configuration is stored in the `local-server` GitHub Environment.
- No application host ports or production `.env` file are used.
- Link to the Docker deployment guide and NPM HTTPS guide.

Remove the port-collision warning and every claim that DuckDNS/Let's Encrypt values are required.

- [ ] **Step 3: Rewrite the Docker deployment guide as an operator runbook**

The rewritten `documents/02-getting-started/docker-deployment.md` must contain, in order:

1. Prerequisites: same-host NPM, self-hosted runner, Docker Compose v2.17+ with both `up --wait` and `--wait-timeout`, custom certificate, and DNS.
2. Network setup: attach NPM to the network and record its exact name as `PROXY_NETWORK_NAME`.
3. GitHub Environment setup: one variable and three secrets, with no production `.env`.
4. NPM Proxy Host settings: `http`, `local-music-queue-frontend`, port `80`, WebSocket enabled, Force SSL enabled, custom certificate selected.
5. Google OAuth authorized JavaScript origin update.
6. Release deployment flow and health checks.
7. Post-deploy REST/WebSocket/login/data checks, including persistence across the release workflow.
8. Rollback by re-running the preceding published release's successful GitHub Actions workflow, preserving `local-server` Environment injection and `backend-db`.
9. Removal of old certificate secrets, GitHub values, and volumes only after 24 healthy hours so the preceding release remains deployable during the rollback window.

- [ ] **Step 4: Update the environment-variable reference**

State that:

- `VITE_API_BASE_URL` is optional and development-only; production defaults to the browser origin.
- `PROXY_NETWORK_NAME` is a GitHub Environment variable and must name an existing Docker network.
- `GOOGLE_CLIENT_ID`, `HOST_EMAILS`, and `ADMIN_EMAILS` are supplied as GitHub Environment secrets for release deployment.
- `PORT`, `DB_PATH`, `YTDLP_PATH`, and `APP_ENV` are fixed by Compose for the production containers.
- The removed DuckDNS, Let's Encrypt, host-port, certificate-path, and duplicate frontend client-ID variables are not part of the production deployment contract.

- [ ] **Step 5: Replace the HTTPS guide**

Change `documents/07-deployment/https-setup.md` to the NPM custom-certificate procedure. It must explicitly assign certificate ownership to NPM and must not include certificate issuance commands, DuckDNS API calls, Certbot commands, port mappings for the application containers, or certificate copying.

- [ ] **Step 6: Update the documentation index**

Describe `https-setup.md` as the Nginx Proxy Manager custom-certificate guide and add the approved design document to the deployment section.

- [ ] **Step 7: Scan for contradictory deployment guidance**

Run:

```bash
rg -n "DuckDNS|DUCKDNS_|Let's Encrypt|LETSENCRYPT_|certbot|FRONTEND_HTTP_PORT|FRONTEND_HTTPS_PORT|BACKEND_PORT" \
  README.md .env.example frontend/.env.example \
  documents/02-getting-started/docker-deployment.md \
  documents/02-getting-started/environment-variables.md \
  documents/07-deployment/https-setup.md documents/README.md
```

Expected: no active instructions use the obsolete deployment. Historical references may remain only when explicitly labeled as removed or obsolete.

- [ ] **Step 8: Run the complete verification set**

Run the narrowest checks first, followed by the broader checks:

```bash
go test ./cmd/server -count=1
go test ./...
go vet ./...
(cd frontend && npm run test:unit -- --run)
(cd frontend && npm run build)
export PROXY_NETWORK_NAME=local-music-queue-plan-test
export GOOGLE_CLIENT_ID=test.apps.googleusercontent.com
export HOST_EMAILS=host@example.invalid
export ADMIN_EMAILS=admin@example.invalid
docker compose config --quiet
docker compose build
git diff --check
git status --short
```

Expected: all tests, vet, builds, Compose validation, and `git diff --check` PASS. `git status --short` lists only the approved implementation, tests, deleted certificate scripts, workflow, and documentation changes.

If `go test ./...` or `go vet ./...` is blocked by the documented pre-existing permission problem under a local `letsencrypt-backend` directory, report the exact error. Do not delete or change ownership of that directory without explicit Product Owner approval, and do not claim the command passed.

- [ ] **Step 9: Perform the live NPM checks after release**

Record results for:

```text
HTTPS page load through the deployment hostname
Google login from the authorized origin
GET /api/queue through the public hostname
One queue mutation through /api
WebSocket initial full_sync
One live WebSocket delta
WebSocket reconnect after a controlled disconnect
Backend outbound Google/YouTube access
SQLite queue data retained across deployment
No published frontend or backend host ports
```

Do not remove the old certificate volumes or obsolete GitHub values until the new release has remained healthy for 24 hours.
