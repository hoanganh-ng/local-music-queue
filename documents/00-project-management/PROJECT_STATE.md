# Project State Baseline (Sprint 001)

**Baseline Date:** 2026-06-17
**Branch:** dev
**Inspected Commit:** `0131b44ff1ac6b263cebef6d2526196042c5560f`

## Source-Priority Rule
This `PROJECT_STATE.md` document is the authoritative source for current implementation facts. If any other documentation conflicts with this document, this document is correct regarding the current state of the codebase.

## Architecture/Composition Summary
The project follows a Clean Architecture pattern in Go (Domain, Usecase, Infrastructure, Delivery) for the backend, paired with a Vue 3 frontend using Vite. Real-time updates are handled via WebSocket delta broadcasting. 

## Deployment Topology
Current deployment topology consists of a two-service direct-HTTPS deployment (`local-music-queue-backend` and `local-music-queue-frontend`) orchestrating via Docker Compose.

## REST Endpoints
There are exactly 19 registered HTTP REST endpoints plus 1 WebSocket endpoint (`/ws`):
1. `GET /api/queue`
2. `POST /api/queue/add`
3. `POST /api/queue/remove`
4. `POST /api/queue/skip`
5. `POST /api/queue/previous`
6. `POST /api/queue/clear`
7. `GET /api/queue/status`
8. `POST /api/queue/status`
9. `POST /api/queue/volume`
10. `POST /api/auth/login`
11. `POST /api/auth/google`
12. `GET /api/activities`
13. `GET /api/search`
14. `GET /api/priority/balance`
15. `GET /api/priority/transactions`
16. `POST /api/priority/prioritize`
17. `POST /api/vote/skip`
18. `POST /api/vote/prioritize`
19. `GET /api/vote/status`

## WebSocket Envelope & Events
WebSocket uses a structured envelope for delta state distribution.
- **Envelope fields:** `type` (string), `data` (JSON payload), `seq_num` (integer), `timestamp` (integer/string).
- **Exact 16-event inventory:** 
  1. `full_sync`
  2. `user_joined`
  3. `song_added`
  4. `song_skipped`
  5. `song_previous`
  6. `song_removed`
  7. `queue_cleared`
  8. `status_changed`
  9. `elapsed_sync`
  10. `volume_changed`
  11. `song_prioritized`
  12. `priority_balance_updated`
  13. `vote_updated`
  14. `vote_resolved`
  15. `error`
  16. `pong`

## Database
SQLite with 7 tables, index, trigger, and single-row queue JSON storage.
- **Tables:** `queue_state`, `activities`, `users`, `user_sessions`, `priority_transactions`, `auto_queue_config`, `play_history`.
- **Queue State:** Single-row queue JSON storage defining invariants including active songs, currently playing index, and playback history.

## Authentication & Authorization
- **Login Verification:** Verifies Google OAuth token audience and `email_verified` fields. Hard-coded allowed domain check.
- **Identity Management:** There is a complete absence of JWT, server session, or per-request authenticated identity. Role assignment is performed, but identity and role are client-supplied on subsequent requests.
- **Authorization Enforcement:** Absence of backend authorization checks on most playback operations (e.g. skip, clear queue) and auto-queue toggling.

## Community Voting Mechanics
- **Implementation & Persistence:** Vote sessions are stored in-memory.
- **Expiry:** Sessions expire after a 30-second duration.
- **Identity & Threshold:** Voting identity and role are client-supplied via JSON payloads (`user_id` and `user_role`). Connected-client counting is used for threshold calculation. The implemented threshold formula evaluates as `max(2, connectedUsers / 2)`.

## Auto-Queue Behavior
- **Trigger:** Activates upon reaching the end of the queue (no-upcoming-song).
- **Execution:** Asynchronous execution with debounce.
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

## Deferred Runtime Sprint Candidates
- Implement secure JWT-based or session-based authentication.
- Enforce backend authorization for all sensitive endpoints.
- Fix broken unit tests.
- Persist vote sessions to SQLite.
