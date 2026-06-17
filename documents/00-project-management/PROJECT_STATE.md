# Project State Baseline (Sprint 001)

**Baseline Date:** 2026-06-17
**Branch:** dev
**Inspected Commit:** `0131b44ff1ac6b263cebef6d2526196042c5560f`

## Source-Priority Rule
This `PROJECT_STATE.md` document is the authoritative documentation snapshot for the inspected commit. If any other documentation conflicts with this document, this document is correct regarding the documented state of the codebase. However, if conflicts are discovered between this document and the actual implementation or tests, the implementation and tests themselves remain the ultimate source of truth.

## Architecture/Composition Summary
The project follows a Clean Architecture pattern in Go (Domain, Usecase, Infrastructure, Delivery) for the backend, paired with a Vue 3 frontend using Vite. Real-time updates are handled via WebSocket delta broadcasting. 

## Deployment Topology
Current deployment topology consists of a two-service direct-HTTPS deployment (`music-queue-backend` and `music-queue-frontend`) orchestrating via Docker Compose.
- **Port Defaults & Collisions:** The `.env.example` defines fallback application ports (e.g., 8011, 8012, 1111). However, the `docker-compose.yml` defaults to mapping the host's `443` port for both frontend and backend unless `FRONTEND_HTTPS_PORT` and `BACKEND_PORT` are explicitly overridden, which causes a default host-port 443 collision.

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
- **Identity Management:** There is a complete absence of JWT, server session, or per-request authenticated identity. Roles are assigned as identity metadata and UI behavior only; this does not claim effective backend RBAC. Identity and role are client-supplied on subsequent requests.
- **Authorization Enforcement:** Absence of backend authorization checks on most playback operations (e.g. skip, clear queue) and auto-queue toggling.
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
- **Frontend scripts and discovered test files:** Vitest-only scripts configured in `package.json`.
- **Server tests:** Go unit tests contain known stale/failing test cases. Tests were executed, and `TestLoadDefaults` failed. `TestLoadDefaults` expects the historical HostPIN default 6666, while current configuration defaults it to 9512.
- **Verification commands actually run and their limitations:** `npm run test:unit -- --run` runs the frontend vitest suite. `go test ./...` runs backend unit tests. No E2E suite exists.

## Prioritized Known-Risk Register
1. **Critical:** Absence of JWT/server session/per-request identity allowing trivial spoofing of identity/roles (client-supplied `user_id`/`UserRole`).
2. **Critical:** Lack of backend authorization checks on queue operations and auto-queue configuration.
3. **High:** In-memory voting state is lost on restart.
4. **Medium:** Stale backend tests (e.g., `TestLoadDefaults`).
5. **High:** WebSocket vulnerabilities (origin not validated, `user_id` via query string).

## Deferred Runtime Sprint Candidates
- Implement secure JWT-based or session-based authentication.
- Enforce backend authorization for all sensitive endpoints.
- Fix broken unit tests.
- Persist vote sessions to SQLite.
- Address Docker Compose port collisions.
