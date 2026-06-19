# Project State Baseline (Sprint 003 Active)

**Baseline Date:** 2026-06-19
**Branch:** dev
**Original Sprint 001 Baseline Commit:** `0131b44ff1ac6b263cebef6d2526196042c5560f` (pre-sprint baseline)
**Current Inspected Dev Commit:** `9c0fba72ca21f933c88817c3b4975bf3319f9b2b` (containing Sprint 003 implementation)

## Source-Priority Rule
This `PROJECT_STATE.md` document is the authoritative documentation snapshot for the inspected commit. If any other documentation conflicts with this document, this document is correct regarding the documented state of the codebase. However, if conflicts are discovered between this document and the actual implementation or tests, the implementation and tests themselves remain the ultimate source of truth.

## Architecture/Composition Summary
The project follows a Clean Architecture pattern in Go (Domain, Usecase, Infrastructure, Delivery) for the backend, paired with a Vue 3 frontend using Vite. Real-time updates are handled via WebSocket delta broadcasting. 

## Deployment Topology
Current deployment topology consists of a two-service direct-HTTPS deployment orchestrating via Docker Compose. The Compose service names are `backend` and `frontend`, and their configured container names are `music-queue-backend` and `music-queue-frontend`.
- **Port Defaults & Collisions:** The `.env.example` defines sample override values (e.g., 8011, 8012, 1111). However, the `docker-compose.yml` defaults to mapping the host's `443` port for both frontend and backend unless `FRONTEND_HTTPS_PORT` and `BACKEND_PORT` are explicitly overridden, which causes a default host-port 443 collision.
- **Certificate Requirements:** `DUCKDNS_DOMAIN`, `DUCKDNS_TOKEN`, and `LETSENCRYPT_EMAIL` are mandatory for both current container startup scripts. Missing values cause container startup to exit during certificate setup.

## REST Endpoints
There are exactly 19 registered HTTP REST endpoints plus 1 WebSocket endpoint (`/ws`):
1. `POST /api/auth/google`
2. `POST /api/auth` (Deprecated)
3. `GET /api/queue`
4. `POST /api/queue/add`
5. `POST /api/queue/skip`
6. `POST /api/queue/status`
7. `POST /api/queue/sync`
8. `POST /api/queue/ended`
9. `POST /api/queue/prev`
10. `POST /api/queue/remove`
11. `POST /api/queue/clear`
12. `POST /api/queue/volume`
13. `POST /api/queue/prioritize`
14. `GET /api/user/priority-balance`
15. `GET /api/youtube/search`
16. `POST /api/vote/skip`
17. `POST /api/vote/prioritize`
18. `POST /api/autoqueue/toggle`
19. `GET /api/autoqueue/status`

## WebSocket Envelope & Events
WebSocket uses a structured envelope for delta state distribution.
- **Envelope fields:** `type` (string), `data` (JSON payload), `seq_num` (integer), `timestamp` (serialized `time.Time` field).
- **Control Frames:** Ping/pong uses standard WebSocket control frames, not application-level JSON events.
- **Exact 16-event backend application inventory:**
  1. `full_sync`
  2. `user_joined`
  3. `song_added`
  4. `song_skipped`
  5. `status_changed`
  6. `elapsed_sync`
  7. `song_previous`
  8. `song_removed`
  9. `queue_cleared`
  10. `volume_changed`
  11. `song_prioritized`
  12. `priority_balance_updated`
  13. `vote_updated`
  14. `vote_resolved`
  15. `auto_queue_added`
  16. `auto_queue_config_changed`
- **Frontend Compatibility:** The frontend also accepts legacy `queue_updated` and `status_updated` messages.

## Database
SQLite with 7 tables, index, trigger, and single-row queue JSON storage.
- **Tables:** `queue_state`, `activities`, `users`, `user_sessions`, `priority_transactions`, `auto_queue_config`, `play_history`.
- **Queue State:** Single-row queue JSON storage defining invariants including active songs, currently playing index. Note: queue JSON activity history is distinct from the separate `play_history` table used by auto-queue.

## Authentication & Authorization
- **Login Verification:** Verifies Google OAuth token audience and `email_verified` fields. Hard-coded allowed domain check. Also logs configured emails (Host/Admin emails) on startup.
- **Identity Management:** Server-issued session tokens (stored in memory) are used to authenticate requests for song removal (`POST /api/queue/remove`). Sessions are transient and stored strictly in-memory; they are lost upon server restart and are bound to a single-process/single-instance scope. For other endpoints, there remains an absence of server sessions or per-request authenticated identity, where roles and identity are client-supplied.
- **Authorization Enforcement:** Backend authorization checks are enforced on song removal (`POST /api/queue/remove`). There is an absence of backend authorization checks on other playback operations (e.g., skip, clear queue) and auto-queue toggling.
- **WebSocket Caveats:** Accepts any origin without validation. Also uses an insecure query parameter `user_id` for connection identification.

## Community Voting Mechanics
- **Implementation & Persistence:** Vote sessions are stored in-memory.
- **Expiry:** Sessions expire after a 30-second duration.
- **Identity & Threshold:** Voting identity and role are client-supplied via JSON payloads (`user_id` and `user_role`). Connected-client counting is used for threshold calculation. The implemented threshold formula evaluates as `max(2, connectedClients / 2)`, and the threshold is captured at the moment the session is created.

## Auto-Queue Behavior
- **Trigger:** Activates upon reaching the end of the queue (no-upcoming-song).
- **Execution:** Detached asynchronous execution with an in-process single-flight guard (not a debounce).
- **Configuration & Persistence:** Stores persistent history (`play_history`) and toggled configuration (`auto_queue_config`).
- **Authorization:** Complete lack of backend toggle authorization.

## Testing & CI
### Static Inspection
- `frontend/package.json` exposes Vitest through `test:unit`.
- The non-watch command is `npm run test:unit -- --run`.
- No frontend E2E script was found.

### Command Execution
During the Sprint 003 verification:
- `go test -count=1 ./internal/usecase/auth ./internal/usecase/queue ./internal/delivery/http ./internal/infrastructure/session` - PASS
- `go test ./...` - BLOCKED (Pre-existing environmental blocker: `open letsencrypt-backend/accounts: permission denied`)
- `go test -race ./...` - BLOCKED (Pre-existing environmental blocker: `open letsencrypt-backend/accounts: permission denied`)
- `go vet ./...` - BLOCKED (Pre-existing environmental blocker: `open letsencrypt-backend/accounts: permission denied`)
- `cd frontend && npm run test:unit -- --run` - BLOCKED (Environmental: `npm: command not found`)
- `cd frontend && npm run build` - BLOCKED (Environmental: `npm: command not found`)
- `docker compose config` - PASS
- `git diff --check` - PASS
- `git status --short --untracked-files=all` - PASS

## Prioritized Known-Risk Register
1. **Critical:** Absence of JWT/server session/per-request identity allowing trivial spoofing of identity/roles on most endpoints, with the song removal endpoint (`POST /api/queue/remove`) as the explicit exception.
2. **Critical:** Lack of backend authorization checks on most queue operations (except song removal) and auto-queue configuration.
3. **High:** In-memory voting state is lost on restart.
4. **High:** WebSocket vulnerabilities (origin not validated, `user_id` via query string).

## Deferred Runtime Sprint Candidates
- Implement secure JWT-based or session-based authentication.
- Enforce backend authorization for all sensitive endpoints.
- Fix broken unit tests.
- Persist vote sessions to SQLite.
- Address Docker Compose port collisions.
