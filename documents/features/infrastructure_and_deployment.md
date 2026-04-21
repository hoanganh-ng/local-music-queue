# Infrastructure & Deployment Features

> Documentation of infrastructure, deployment, and DevOps features beyond the initial requirements (v0.1)
> Last Updated: 2026-04-21

## Overview

The application is fully containerized with a production-ready deployment pipeline using Docker and Docker Compose, with an Nginx reverse proxy handling routing for both the REST API and WebSocket connections.

---

## 1. Docker Containerization

### 1.1 Backend: Multi-Stage Build
**Location**: `Dockerfile`

Two-stage build minimizes final image size.

**Build Stage** (Go 1.22 Alpine):
```dockerfile
FROM golang:1.22-alpine AS builder
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server/*
```

**Key Decisions**:
- `CGO_ENABLED=0`: Produces a fully static binary, no shared libraries required
- `GOOS=linux`: Explicit cross-compilation target
- `alpine AS builder`: Small base image, only needed during build

**Final Stage** (Alpine + Runtime Dependencies):
```dockerfile
FROM alpine:latest
RUN apk add --no-cache python3 yt-dlp ffmpeg ca-certificates tzdata
```

**Runtime Dependencies**:
- `python3`: Required by yt-dlp
- `yt-dlp`: YouTube metadata fetching and search
- `ffmpeg`: Recommended for yt-dlp audio/video extraction tasks
- `ca-certificates`: Required for HTTPS requests (YouTube API)
- `tzdata`: Timezone data for accurate log timestamps

**Default Environment Variables** (overridable via docker-compose):
```dockerfile
ENV PORT=1111
ENV CLIENT_PIN=5555
ENV HOST_PIN=6666
ENV DB_PATH=/app/data/music_queue.db
ENV YTDLP_PATH=/usr/bin/yt-dlp
```

---

### 1.2 Frontend: Multi-Stage Build
**Location**: `frontend/Dockerfile`

Two-stage build produces a minimal Nginx-based production image.

**Build Stage** (Node 20 Alpine):
```dockerfile
FROM node:20-alpine AS build-stage
RUN npm install && npm run build
```

**Production Stage** (Nginx Alpine):
```dockerfile
FROM nginx:alpine AS production-stage
COPY --from=build-stage /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
```

**Why Multi-Stage?**:
- Build stage: ~500MB (Node.js + all npm packages)
- Production stage: ~25MB (Nginx + static files only)
- **~95% image size reduction**

**Exposed Port**: `80` (standard HTTP)

---

## 2. Docker Compose Orchestration

### 2.1 Full Stack with Single Command
**Location**: `docker-compose.yml`

Launch the entire application with one command.

```yaml
services:
  backend:
    build: .
    ports:
      - "1111:1111"
    volumes:
      - ./.localdb:/app/data
    environment:
      - PORT=1111
      - CLIENT_PIN=5555
      - HOST_PIN=6666
      - DB_PATH=/app/data/music_queue.db
      - YTDLP_PATH=/usr/bin/yt-dlp
    restart: unless-stopped

  frontend:
    build:
      context: ./frontend
    ports:
      - "8011:80"
    depends_on:
      - backend
    restart: unless-stopped
```

**Launch**:
```bash
docker compose up -d
```

**Access**:
- Frontend: `http://<host-ip>:8011`
- Backend API: `http://<host-ip>:1111`

---

### 2.2 SQLite Database Persistence
**Location**: `docker-compose.yml:volumes`

Database survives container restarts.

**Volume Mount**:
```yaml
volumes:
  - ./.localdb:/app/data
```

**Implementation**:
- Host directory `.localdb/` mapped to `/app/data` in container
- SQLite file: `music_queue.db`
- Data persists across `docker compose down/up` cycles
- Easy backup: copy `.localdb/music_queue.db`

**Why SQLite?**:
- Zero configuration (no separate DB server)
- LAN-only app (single writer, low concurrency)
- Pure Go driver (`modernc.org/sqlite`) — no CGO required
- File-based = trivially portable and backupable

---

### 2.3 Automatic Restart Policy
**Location**: `docker-compose.yml:restart`

Both services restart automatically on failure.

```yaml
restart: unless-stopped
```

**Behavior**:
- Restarts after crash or OOM kill
- Stops only on explicit `docker compose stop`
- Survives host reboots (if Docker daemon is set to start on boot)

---

### 2.4 Service Dependency Ordering
**Location**: `docker-compose.yml:depends_on`

Frontend waits for backend to start.

```yaml
frontend:
  depends_on:
    - backend
```

**Note**: `depends_on` ensures start order, but not health. The Nginx proxy handles backend unavailability gracefully.

---

## 3. Nginx Reverse Proxy

### 3.1 SPA Routing
**Location**: `frontend/nginx.conf`

Serve Vue Router's client-side routes correctly.

```nginx
location / {
    root /usr/share/nginx/html;
    index index.html;
    try_files $uri $uri/ /index.html;
}
```

**Why `try_files ... /index.html`?**:
- Vue Router uses HTML5 history mode
- Direct URL access (e.g. `/dashboard`) would 404 without this
- Falls back to `index.html` for all unmatched routes
- Vue Router handles the rest client-side

---

### 3.2 API Proxy
**Location**: `frontend/nginx.conf`

Route all `/api` requests from frontend to backend.

```nginx
location /api {
    proxy_pass http://backend:1111/api;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection 'upgrade';
    proxy_set_header Host $host;
    proxy_cache_bypass $http_upgrade;
}
```

**Benefits**:
- Frontend and backend served on same origin
- No CORS issues in production
- Hides backend port from end users
- Docker internal network (`backend:1111`) not exposed to LAN

---

### 3.3 WebSocket Proxy
**Location**: `frontend/nginx.conf`

Proxy WebSocket upgrades through Nginx to backend.

```nginx
location /ws {
    proxy_pass http://backend:1111/ws;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "Upgrade";
    proxy_set_header Host $host;
}
```

**Critical Headers**:
- `Upgrade: websocket`: Required for WebSocket handshake
- `Connection: Upgrade`: Signals connection protocol change
- `HTTP/1.1`: Required (WebSocket doesn't support HTTP/2)

**Why Important?**:
- WebSocket upgrades are non-standard HTTP
- Default Nginx config doesn't handle them
- Missing headers = silent connection failure for all clients

---

## 4. Environment-Based Configuration

### 4.1 Config Loading
**Location**: `internal/infrastructure/config/config.go`

All settings configurable via environment variables.

**Variables**:
| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `1111` | Backend HTTP/WS port |
| `CLIENT_PIN` | `5555` | Guest access PIN |
| `HOST_PIN` | `6666` | Host access PIN |
| `DB_PATH` | `./.localdb/music_queue.db` | SQLite database path |
| `YTDLP_PATH` | `yt-dlp` | Path to yt-dlp executable |

**Implementation**:
```go
func Load() *Config {
    return &Config{
        Port:      getEnv("PORT", "1111"),
        ClientPIN: getEnv("CLIENT_PIN", "5555"),
        // ...
    }
}
```

**Benefits**:
- No recompilation to change PINs
- Docker-friendly (pass via `environment:`)
- Secure (PINs not hardcoded in source)
- LAN deployment customization

---

### 4.2 Config Validation
**Location**: `internal/infrastructure/config/config.go:Validate()`

Fail-fast with clear error messages on startup.

```go
func (c *Config) Validate() error {
    if _, err := exec.LookPath(c.YTDLPPath); err != nil {
        return fmt.Errorf("yt-dlp executable not found at %s: %w", c.YTDLPPath, err)
    }
    return nil
}
```

**Checks**:
- `yt-dlp` executable exists at configured path
- Returns clear error if missing

**Why Fail-Fast?**:
- Prevents cryptic runtime errors during song submission
- Docker health check can detect startup failures
- Clear error: "yt-dlp not found at /usr/bin/yt-dlp"

---

## 5. Clean Architecture

### 5.1 Layer Structure
**Location**: `internal/`

Code organized by responsibility, not by framework.

```
internal/
├── domain/            # Business entities and rules
│   ├── entity/        # Song, Queue, User, Activity, SearchResult
│   ├── repository/    # Interface definitions (ports)
│   └── service/       # YouTubeService interface
├── usecase/           # Application orchestration
│   ├── queue/         # Queue management logic
│   ├── auth/          # Authentication logic
│   └── activity/      # Activity log logic
├── infrastructure/    # External adapters (implements interfaces)
│   ├── youtube/       # yt-dlp adapter
│   ├── persistence/   # SQLite adapter
│   └── config/        # Configuration loading
└── delivery/          # Request handlers
    ├── http/          # REST API handlers
    └── ws/            # WebSocket hub and events
```

**Dependency Flow**:
```
Delivery → Usecase → Domain ← Infrastructure
```

**Benefits**:
- Domain has zero external dependencies
- Infrastructure can be swapped (SQLite → Postgres) without changing business logic
- Each layer testable in isolation

---

### 5.2 Interface-Based Dependencies
**Location**: `internal/domain/repository/` and `internal/domain/service/`

Dependencies injected via interfaces, not concrete types.

**Example**:
```go
// Domain defines the interface
type QueueRepository interface {
    Load(ctx context.Context) (*entity.Queue, error)
    Save(ctx context.Context, q *entity.Queue) error
    AddActivity(ctx context.Context, a entity.Activity) error
    GetActivities(ctx context.Context, limit int) ([]entity.Activity, error)
}

// Infrastructure provides the implementation
type SQLiteRepository struct { ... }
func (r *SQLiteRepository) Load(ctx context.Context) (*entity.Queue, error) { ... }
```

**Benefits**:
- Usecase layer tests use mock implementations
- Real implementations only in integration tests
- Easy to swap storage backends

---

## 6. Development Workflow

### 6.1 Development Server Configuration
**Location**: `frontend/src/services/websocket.js`

Auto-detect development environment.

```javascript
const host = window.location.port === '5173'
  ? 'localhost:1111'  // Vite dev server → backend
  : window.location.host  // Production: same host
```

**Dev Mode** (`npm run dev` on port 5173):
- Frontend: `http://localhost:5173`
- Backend: `http://localhost:1111`
- CORS middleware handles cross-origin requests

**Production Mode** (Docker on port 8011):
- Frontend: `http://<host>:8011`
- Nginx proxies `/api` and `/ws` to backend container

---

### 6.2 .dockerignore
**Location**: `.dockerignore`

Exclude unnecessary files from Docker build context.

**Excluded**:
- `node_modules/` (rebuilt in container)
- `.localdb/` (mounted as volume)
- `bin/` (Go binaries)
- `.git/`, `.agents/`

**Benefits**:
- Faster build context upload
- Prevents dev artifacts in production image
- Avoids volume data being baked into image

---

## 7. Testing Infrastructure

### 7.1 Backend Testing Stack
**Location**: `cmd/server/` and `internal/`

Comprehensive Go testing setup.

**Tools**:
- Standard `testing` package
- `net/http/httptest`: Mock HTTP servers
- Manual mocks for repository interfaces
- Integration tests with real SQLite

**Test Files**:
| File | What's Tested |
|------|--------------|
| `cmd/server/api_test.go` | HTTP endpoint responses |
| `cmd/server/main_test.go` | Server startup and routing |
| `cmd/server/middleware_test.go` | Request logging, CORS |
| `internal/delivery/http/handlers_test.go` | Handler logic |
| `internal/delivery/ws/hub_test.go` | WebSocket events |
| `internal/usecase/queue/interactor_test.go` | Queue business logic |
| `internal/usecase/auth/interactor_test.go` | Auth logic |
| `internal/usecase/activity/interactor_test.go` | Activity logic |
| `internal/domain/entity/*.go` | Entity behavior |
| `internal/infrastructure/config/config_test.go` | Config loading |
| `internal/infrastructure/persistence/sqlite_repository_test.go` | DB operations |
| `internal/infrastructure/youtube/ytdlp_service_test.go` | yt-dlp integration |

**Run Tests**:
```bash
go test ./...
```

---

### 7.2 Frontend Testing Stack
**Location**: `frontend/`

Modern Vitest + Playwright setup.

**Unit Tests** (Vitest + jsdom):
```bash
npm run test:unit
```

**E2E Tests** (Playwright + Chromium):
```bash
npm run test:e2e
```

**Test Files**:
| File | What's Tested |
|------|--------------|
| `src/store/index.spec.js` | State management mutations |
| `src/services/websocket.spec.js` | WebSocket client |
| `src/components/ui/BaseButton.spec.js` | Button variants |
| `src/components/dashboard/QueueList.spec.js` | Queue rendering |
| `e2e/auth.spec.js` | Full auth flow (Playwright) |

---

## 8. Workflow Documentation

### 8.1 Agent Workflows
**Location**: `.agents/workflows/`

Documented workflows for common development tasks.

**Available Workflows**:
- `backend-implementation.md`: Step-by-step backend feature development
- `backend-testing.md`: Backend test running strategy
- `backend-api-testing.md`: API endpoint testing
- `frontend-implementation.md`: Frontend feature development
- `frontend-testing.md`: Frontend test running
- `adjust-frontend.md`: Frontend adjustment procedures
- `full-app-testing.md`: Full stack integration testing

---

## Summary

| Feature | Purpose | Impact |
|---------|---------|--------|
| Multi-stage Docker builds | Minimal production images | ~95% image size reduction |
| Docker Compose | One-command deployment | **High** — Any user can deploy |
| SQLite persistence | Data survives restarts | **High** — Queue not lost |
| Restart policy | Self-healing containers | Medium — Resilience |
| Nginx SPA routing | Correct URL handling | **High** — Pages load correctly |
| Nginx API proxy | Single-origin production | **High** — No CORS in prod |
| Nginx WS proxy | WebSocket in production | **High** — Real-time works |
| Env-based config | Flexible deployment | **High** — No code changes for customization |
| Config validation | Fail-fast startup | Medium — Clear error messages |
| Clean Architecture | Maintainable codebase | **High** — Long-term sustainability |
| Dev/prod mode detection | Seamless dev experience | Medium — DX improvement |

---

## Deployment Instructions

### Quick Start (Docker Compose)
```bash
# Clone and enter project
cd local-music-queue

# Optional: customize PINs in docker-compose.yml
# CLIENT_PIN=<your-guest-pin>
# HOST_PIN=<your-host-pin>

# Launch everything
docker compose up -d

# Access from any device on the network
# Frontend: http://<your-ip>:8011
# Host PIN: 6666 (default)
# Guest PIN: 5555 (default)
```

### Development (Without Docker)
```bash
# Terminal 1: Backend
go run ./cmd/server

# Terminal 2: Frontend
cd frontend && npm install && npm run dev
```

### Custom PIN Configuration
```bash
# Via environment variables
CLIENT_PIN=1234 HOST_PIN=9876 go run ./cmd/server

# Via docker-compose.yml
environment:
  - CLIENT_PIN=1234
  - HOST_PIN=9876
```
