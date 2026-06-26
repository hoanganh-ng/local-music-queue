# Project State Baseline

**Baseline Date:** 2026-06-19
**Branch:** dev
**Original Sprint 001 Baseline Commit:** `0131b44ff1ac6b263cebef6d2526196042c5560f`
**Sprint 003 Implementation Predecessor Commit:** `9c0fba72ca21f933c88817c3b4975bf3319f9b2b`
**Sprint 003 Final Reviewed Commit:** `b0a822478c5d4cee6162702d5969605cfc2702f2`

The most recently closed sprint that introduced architectural changes documented here is **Sprint 020 / A01 — Allowed Origins and WebSocket Origin Policy** (closed 2026-06-26, pending Product Owner acceptance). A01 introduced a shared `ALLOWED_ORIGINS` policy used by HTTP CORS and WebSocket upgrades, with fail-fast validation outside `APP_ENV=local`, and deprecated the legacy `?user_id=...` WebSocket query parameter as an identity or daily-priority attribution signal. Earlier sprints (001-010, 011, 020) are recorded in `documents/00-project-management/SPRINTS/`; the recent closed sprint records are `009-sqlite-to-postgresql-data-migration.md` (R03), `010-room-domain-invite-membership-lifecycle.md` (R04, pending PO acceptance), `011-session-token-authentication-authorization.md` (R05), and `020-allowed-origins-and-websocket-origin-policy.md` (A01).

## Source-Priority Rule
This `PROJECT_STATE.md` document is the authoritative documentation snapshot for the inspected commit. If any other documentation conflicts with this document, this document is correct regarding the documented state of the codebase. However, if conflicts are discovered between this document and the actual implementation or tests, the implementation and tests themselves remain the ultimate source of truth.

## Architecture/Composition Summary
The project follows a Clean Architecture pattern in Go (Domain, Usecase, Infrastructure, Delivery) for the backend, paired with a Vue 3 frontend using Vite. Real-time updates are handled via WebSocket delta broadcasting. 

## Deployment Topology
Current deployment topology consists of a two-service direct-HTTPS deployment orchestrating via Docker Compose. The Compose service names are `backend` and `frontend`, and their configured container names are `music-queue-backend` and `music-queue-frontend`. The Compose stack also includes the `postgres` service and the `db-init` one-shot job introduced by R02 (and finalized by R03).
- **Port Defaults & Collisions:** The `.env.example` defines sample override values (e.g., 8011, 8012, 1111). However, the `docker-compose.yml` defaults to mapping the host's `443` port for both frontend and backend unless `FRONTEND_HTTPS_PORT` and `BACKEND_PORT` are explicitly overridden, which causes a default host-port 443 collision.
- **Certificate Requirements:** `DUCKDNS_DOMAIN`, `DUCKDNS_TOKEN`, and `LETSENCRYPT_EMAIL` are mandatory for both current container startup scripts. Missing values cause container startup to exit during certificate setup.
- **PostgreSQL Required at Startup:** `DATABASE_URL` (with `POSTGRES_*` overrides) is now mandatory. The backend refuses to start without it; the SQLite runtime fallback was removed in R03. The `backend-db` volume and the SQLite source mount are no longer present in `docker-compose.yml`.

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
- **Sprint 004 Additive Fields (review pending):** `song_added` and `auto_queue_added` carry additional authoritative post-mutation fields `current_index`, `current_song`, `status`, and `elapsed`, captured under the same lock as the queue mutation. All pre-existing fields and their JSON tags are unchanged. The frontend applies the additive fields when present; legacy backends omitting them trigger the single approved fallback (first-song promotion when the queue was empty). The Sprint 004 second-pass implementation also adds `previous_current_index` and `playback_advanced` to the internal `AddSongResult` struct returned from the queue interactor — these are NOT serialized over WebSocket. The compatibility claim in this section should be re-verified after Architect review.
- **R05 Auth Posture (WebSocket):** Client-originated messages (e.g. `request_full_sync`) require a valid session presented at connect time as `?session_token=<opaque>`. When present and valid the backend marks the connection `authenticated` and processes client requests. The legacy `?user_id=...` query parameter remains accepted as a non-authenticated daily-priority hint. Connections presenting neither are read-only spectators and any client message they send is rejected.
- **R05 Additive `error` Event:** The backend emits `{"type":"error","data":{"code":"<code>","message":"<message>"}}` when it rejects a client-originated request. This is purely additive — the existing 16-event inventory is unchanged. Pre-existing fields and JSON tags for all other events remain byte-for-byte compatible.

## Database
PostgreSQL 16 (via `pgx/v5/stdlib`) is the only persistence backend. Schema version is **3** after the embedded migrations run. SQLite is retained ONLY as the offline source reader inside `cmd/migrate-data` / `internal/infrastructure/persistence/migratedata`; the runtime backend, the `DBPath` config field, the `DB_PATH` env fallback, the `backend-db` volume, and the SQLite repository implementations were all removed in R03.
- **Tables:** `queue_state`, `activities`, `users`, `user_sessions`, `priority_transactions`, `auto_queue_config`, `play_history`.
- **Migration identity:** `users.legacy_id BIGINT NULL` (unique index `idx_users_legacy_id`) records the original SQLite `users.id` so dependent tables FK-remap through the new PostgreSQL `users.id` while keeping a stable SQLite-identity audit trail. R06 drops the column after the `room_id` refactor absorbs the user-identity information.
- **Migration marker:** `migration_marker` is a single-row table (`id=1`) carrying per-table SHA256 hashes plus the SQLite file-bytes SHA256 and the source path. It is the durable, exact no-op / idempotency proof for `cmd/migrate-data`.
- **Queue State:** Single-row queue JSON storage defining invariants including active songs, currently playing index. Note: queue JSON activity history is distinct from the separate `play_history` table used by auto-queue.
- **Data migration CLI:** `cmd/migrate-data up` is the operator-driven one-shot, offline, idempotent CLI. Exit codes: 0 = success (including `already migrated; no-op`), 1 = error, 2 = usage. Honors `--sqlite`, `--postgres`, `--report-file`, `--chunk-size`, `--dry-run`. Acquires the `lmq_migration` advisory lock (`pg_try_advisory_lock(987654321)`) on a pinned connection; resyncs BIGSERIAL sequences for `user_sessions`, `priority_transactions`, `activities`, and `play_history` after the copy; writes the `migration_marker` row inside the same transaction; runs a pre-commit in-transaction integrity check.

## Authentication & Authorization
- **Login Verification:** Verifies Google OAuth token audience and `email_verified` fields. Hard-coded allowed domain check. Also logs configured emails (Host/Admin emails) on startup.
- **Identity Management (R05):** Server-issued session tokens (stored in memory) authenticate every privileged REST endpoint via the `Authorization: Bearer <token>` header. Sessions are transient and stored strictly in-memory; they are lost upon server restart and are bound to a single-process/single-instance scope. Client-supplied identity fields (`added_by`, `added_by_id`, `requested_by`, `user_id`, `user_role` in JSON bodies and WebSocket frames) are accepted on the wire for backwards compatibility but are NOT consulted for auth or attribution where R05 changed behavior; the resolved `*entity.User` from the bearer token is the sole source of identity for privileged routes.
- **Authorization Enforcement (R05):** Privileged REST endpoints are server-authenticated and role-gated per the route matrix below. The middleware rejects missing/expired tokens with 401 and role mismatches with 403 before the handler runs.
  - Authenticated-only (any role): `POST /api/queue/add`, `POST /api/queue/remove`, `POST /api/queue/prioritize`.
  - Host or Admin role required: `POST /api/queue/skip`, `POST /api/queue/status`, `POST /api/queue/sync`, `POST /api/queue/ended`, `POST /api/queue/prev`, `POST /api/queue/clear`, `POST /api/queue/volume`, `POST /api/autoqueue/toggle`.
  - Voter or Admin role required: `POST /api/vote/skip`, `POST /api/vote/prioritize`.
  - Unauthenticated (public reads/login): `GET /api/queue`, `GET /api/user/priority-balance`, `GET /api/youtube/search`, `GET /api/autoqueue/status`, `POST /api/auth/google`.
- **Client-Side Compatibility:** Client-supplied `added_by_id` / `user_id` / `user_role` fields in request bodies and WebSocket frames are still parsed and may be persisted for display/audit compatibility, but the authorization gate does not consult them. Frontend and extension clients should not rely on them for auth/identity.

**A01 Allowed Origins (2026-06-26):** `ALLOWED_ORIGINS` is a comma-separated list shared by HTTP CORS and WebSocket upgrades. In `APP_ENV=local` the server falls back to loopback defaults. In any other environment the server refuses to start without a non-empty list. Browser responses echo the allowed origin with `Vary: Origin`; server-to-server requests (empty `Origin`) get no `Access-Control-Allow-Origin` header.

- **WebSocket Caveats (A01):** Both HTTP CORS and WebSocket upgrades are gated by the same configured allow list (`ALLOWED_ORIGINS`). Empty `Origin` is allowed for non-browser/server-to-server traffic but never implies authentication. The legacy `?user_id=...` query parameter is parsed only for diagnostic logging; it does not affect auth, attribution, or daily-priority attribution. Identity requires `?session_token=<opaque>` (R05).
- **Browser Extension (R05 / R06):** `extension/background.js` attaches `Authorization: Bearer <session_token>` to `POST /api/queue/add` whenever the user has saved a token in the extension Options page (storage key `sessionToken` in `chrome.storage.local`). With no token, `/api/queue/add` returns 401 and the extension surfaces a clear "unauthorized" error. R06 added an authenticated in-app **Copy token for extension** button to the web dashboard (success/info/error toast feedback; raw token never enters the DOM or logs), a visible **Token status:** badge on the Options page, and an explicit **Clear Session Token** button that removes only the `sessionToken` key while leaving `apiBase`/`displayName`/`userId` intact. Saving a blank Session Token still preserves the existing saved value.
- **R06 Review Fixes (2026-06-26, closed):** Three small follow-ups landed on `dev` after the R06 review pass — commit `e9fc1a2` (`extension/options.js`) refreshes the "Token status:" badge to "saved" immediately on a non-empty save (blank saves still preserve the existing stored token and leave the badge untouched); commit `5e27789` (`frontend/src/views/DashboardView.vue`) hardens the clipboard-failure catch in `handleCopySessionToken` to log only `err?.name || 'unknown'` instead of the full `Error` object; commit `b01dec0` removes the unapproved R06 plan file `docs/superpowers/plans/2026-06-26-r06-extension-session-token-ux.md`. Verification: `cd frontend && npm run test:unit -- --run` (149/149 pass) and `cd frontend && npm run build` (clean). No backend, REST, WebSocket, storage keys (other than preserving current `sessionToken` semantics), queue, voting, auto-queue, Docker, or deployment changes. This is a small UX/logging closure, not a sprint closure — the planned R06 ("Player Lease and Host Departure Semantics") in `documents/00-project-management/SPRINTS/active.md` remains the next sequenced sprint.

## Community Voting Mechanics
- **Implementation & Persistence:** Vote sessions are stored in-memory.
- **Expiry:** Sessions expire after a 30-second duration.
- **Identity & Threshold (R05):** The REST vote endpoints still parse legacy `user_id` / `user_role` fields in the JSON body for wire compatibility, but they are not consulted for authorization. Backend authorization and the actual vote identity now come from the resolved bearer-token user (see Authentication & Authorization). Connected-client counting still drives threshold calculation; the implemented threshold formula evaluates as `max(2, connectedClients / 2)`, and the threshold is captured at the moment the session is created.

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
- `go test -race -count=1 ./internal/usecase/auth ./internal/usecase/queue ./internal/delivery/http ./internal/infrastructure/session` - PASS
- `go test ./...` - BLOCKED (Pre-existing environmental blocker: `open letsencrypt-backend/accounts: permission denied`)
- `go test -race ./...` - BLOCKED (Pre-existing environmental blocker: `open letsencrypt-backend/accounts: permission denied`)
- `go vet ./...` - BLOCKED (Pre-existing environmental blocker: `open letsencrypt-backend/accounts: permission denied`)
- `cd frontend && npm run test:unit -- --run` - PASS (8 test files passed, 28 tests passed)
- `cd frontend && npm run build` - PASS (production build successful)
- `docker compose config` - PASS
- `git diff --check` - PASS
- `git status --short --untracked-files=all` - PASS

## Prioritized Known-Risk Register

1. **Mitigated by R05 / R06 (residual):** Session-token-based identity now authenticates privileged REST endpoints and the WebSocket accept path. Sessions live strictly in-memory and are lost on restart, so a server restart logs every client out and requires re-login. Browser-extension clients must re-paste the token after each new login; no automatic refresh path exists. The web dashboard's Copy button and the Options Clear button make the manual rotation usable and reversible.
2. **Mitigated by R05 (residual):** Privileged REST endpoints are role-gated per the route matrix above. Authorization still relies on the in-memory session, so the same restart-loss caveat applies.
3. **High:** In-memory voting state is lost on restart.
4. **Mitigated by A01 (closed):** WebSocket origins are validated against the shared `ALLOWED_ORIGINS` allow list (same policy as HTTP CORS); the legacy `?user_id=...` hint is parsed only for diagnostic logging and is no longer used for identity or daily-priority attribution.

## Deferred Runtime Sprint Candidates
- Implement secure JWT-based or session-based authentication.
- Enforce backend authorization for all sensitive endpoints.
- Fix broken unit tests.
- Persist vote sessions to SQLite.
- Address Docker Compose port collisions.
