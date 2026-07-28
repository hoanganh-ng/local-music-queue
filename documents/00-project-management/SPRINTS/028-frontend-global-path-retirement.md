# Sprint 028 — Frontend global-path retirement behind the cutover build gate (R14d)

**Status:** Active (activated 2026-07-28)
**Branch:** `sprint/r14d-frontend-global-path-retirement`
**Base:** `dev` at approved base commit `f9760693e174344dba4bccd92fde22c281b3a4e5`
**Parent epic:** Issue #17
**Scope:** Frontend only. No Go, migration, Docker, Nginx, TLS, OAuth/session-architecture, or backend REST/WebSocket contract changes.

R14c and R14e remain inactive. Production cutover has **not** been executed. The
true artifact must not be deployed during R14d; deployment remains R14c scope.

## Goal

Prepare the Vue frontend for the future R14c cutover using the single approved
Vite build-time setting:

```text
VITE_ROOM_CUTOVER_AUTHORITATIVE=true|false
```

The same source produces two compatible artifacts:

- **False artifact (pre-cutover / rollback):** preserves the existing Dashboard,
  global store, global REST usage, and global `/ws` behavior.
- **True artifact (post-R14c):** RoomEntry is the authenticated landing surface,
  the Dashboard route is removed from the route graph, legacy global client
  state is retired at bootstrap, and the global WebSocket cannot connect or
  reconnect.

## Contract delivered

### Central cutover configuration

`frontend/src/config/cutover.js` is the single owner of the setting. Parse
rules: missing/empty → `false`; `"false"` → `false`; `"true"` → `true`; any
other non-empty value fails explicitly (the error never echoes the raw value).
No other module reads `import.meta.env.VITE_ROOM_CUTOVER_AUTHORITATIVE`, and no
runtime configuration request, local-storage switch, query parameter, cookie,
or feature-flag service exists. The setting is documented in
`frontend/.env.example`, defaulting to `false`.

### Shared authenticated landing decision

`authenticatedLandingRouteName()` (false → `Dashboard`, true → `RoomEntry`) is
the one shared decision used by the router guard for authenticated navigation
away from `/auth` and by `AuthView` after a successful Google login. No
duplicated conditionals.

### Router

- False mode: `/` remains the named `Dashboard` route with unchanged behavior.
- True mode: `/` redirects to the named `RoomEntry` route; no route named
  `Dashboard` exists; `DashboardView` cannot mount through normal routing.
- `/rooms` and `/rooms/:slug` remain unchanged authenticated room routes in
  both modes.
- `DashboardView.vue` and the legacy services are intentionally **not**
  deleted: the false build is the pre-cutover and rollback artifact.

### RoomEntry

- False mode preserves "Back to dashboard".
- True mode hides the dashboard control and shows a visible sign-out control
  that calls the existing session clear behavior, clears
  `globalStore.currentUser`, navigates to `Auth`, and never logs or exposes
  the session token. Authentication is not redesigned.

### Legacy global-state retirement

`globalStore.retireLegacyGlobalRuntimeState()` resets `queueState`,
`voteSessions`, `autoQueueConfig`, and the global `connectionStatus` to their
documented initial values via fresh-state factories (no reset reuses a mutable
array or object). It preserves `currentUser`, the exact `roomQueues` object
(same identity, every room entry intact), session storage, and room-local
WebSocket state. Bootstrap (`main.js`) is the only production caller.

### Global WebSocket lifecycle

- `disconnect()` marks the close intentional **before** closing, detaches the
  socket, cancels any pending reconnect timer, and leaves status
  `disconnected`.
- An intentional close never schedules reconnection; stale `onclose`
  callbacks from detached or replaced sockets are neutralized by a
  socket-identity guard.
- False-mode `connect()` deliberately re-enables normal reconnection.
- True-mode `connect()` is a no-op: no WebSocket is constructed and no
  reconnect can ever be scheduled (`scheduleReconnect` is also gated).
- Room WebSocket behavior (`room-websocket.js`) is untouched.

### Bootstrap

True mode: retire legacy global runtime state → intentionally disconnect and
disable the global WebSocket → mount with the cutover-aware router. False
mode: no state reset, no forced disconnect; current behavior preserved.

## Changed files

- `frontend/src/config/cutover.js` (new)
- `frontend/.env.example`
- `frontend/src/router/index.js`
- `frontend/src/views/AuthView.vue`
- `frontend/src/views/RoomEntryView.vue`
- `frontend/src/store/index.js`
- `frontend/src/services/websocket.js`
- `frontend/src/main.js`

Tests (new): `frontend/src/config/__tests__/cutover.spec.js`,
`frontend/src/router/__tests__/cutover-modes.spec.js`,
`frontend/src/views/__tests__/cutover-modes.spec.js`,
`frontend/src/services/__tests__/cutover-modes.spec.js`,
`frontend/src/store/__tests__/retire-legacy-global-runtime-state.spec.js`,
`frontend/src/__tests__/cutover-bootstrap.spec.js`.

Tests (updated, pinned to the false artifact via a config-module mock so the
suite is deterministic under both baked env values):
`frontend/src/router/__tests__/router.spec.js`,
`frontend/src/router/__tests__/index.spec.js`,
`frontend/src/views/__tests__/AuthView.spec.js` (also asserts login lands on
Dashboard), `frontend/src/views/__tests__/RoomEntryView.spec.js`,
`frontend/src/services/__tests__/websocket.spec.js`.

## Verification (Node 22.14.0)

```text
VITE_ROOM_CUTOVER_AUTHORITATIVE=false npm run test:unit -- --run
  → Test Files 24 passed (24); Tests 443 passed (443)
VITE_ROOM_CUTOVER_AUTHORITATIVE=true  npm run test:unit -- --run
  → Test Files 24 passed (24); Tests 443 passed (443)
VITE_ROOM_CUTOVER_AUTHORITATIVE=false npm run build → ✓ built (exit 0)
VITE_ROOM_CUTOVER_AUTHORITATIVE=true  npm run build → ✓ built (exit 0)
```

Note: the true build still **emits** the `DashboardView` chunk (Rollup bundles
every `import()` expression it sees), but no route references it, so it is
unreachable through normal routing.

The true artifact is **not** claimed production-compatible with the future
true server; that requires the paired R14c maintenance-window smoke test.
