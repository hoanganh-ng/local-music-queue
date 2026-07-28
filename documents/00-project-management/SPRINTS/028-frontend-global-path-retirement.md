# Sprint 028 — Frontend global-path retirement behind the cutover build gate (R14d)

**Status:** Closed and accepted by the Product Owner (2026-07-28)  
**Branch:** `sprint/r14d-frontend-global-path-retirement`  
**Base:** `dev` at approved base commit `f9760693e174344dba4bccd92fde22c281b3a4e5`  
**Final reviewed head:** `491770ed90e3940307df3933d4236508fac838f3`  
**PR:** #25  
**Merge commit:** `67bd57a8abef92d42cdfefb90069563340147d10`  
**Parent epic:** Issue #17  
**Scope:** Frontend only. No Go, migration, Docker, Nginx, TLS, OAuth/session-architecture, or backend REST/WebSocket contract changes.

R14c and R14e remain inactive. Production cutover has **not** been executed. The true artifact was not deployed during R14d; deployment remains R14c scope.

## Goal

Prepare the Vue frontend for the future R14c cutover using the single approved Vite build-time setting:

```text
VITE_ROOM_CUTOVER_AUTHORITATIVE=true|false
```

The same source produces two compatible artifacts:

- **False artifact (pre-cutover / rollback):** preserves the existing Dashboard, global store, global REST usage, and global `/ws` behavior.
- **True artifact (post-R14c):** RoomEntry is the authenticated landing surface, the Dashboard route is absent, legacy global client state is retired at bootstrap, and the global WebSocket cannot connect or reconnect.

## Delivered contract

### Central cutover configuration

`frontend/src/config/cutover.js` is the single owner of `VITE_ROOM_CUTOVER_AUTHORITATIVE`.

Parse rules:

- missing/empty → `false`
- `"false"` → `false`
- `"true"` → `true`
- any other non-empty value throws explicitly without echoing the raw value

No other production module parses the variable. No runtime configuration request, local-storage switch, query parameter, cookie, or feature-flag service was introduced.

The `frontend/.env.example` wording accurately records that invalid values fail during frontend startup/module evaluation, not during `npm run build`; invalid values never silently select a mode.

### Shared authenticated landing decision

`authenticatedLandingRouteName()` is the one shared landing decision:

- false → `Dashboard`
- true → `RoomEntry`

It is used by both the router guard for authenticated navigation away from `/auth` and `AuthView` after successful Google login.

### Router and RoomEntry behavior

False mode:

- `/` remains the named `Dashboard` route.
- authenticated `/auth` lands on Dashboard.
- successful login targets Dashboard.
- RoomEntry retains `Back to dashboard`.
- existing room routes remain authenticated and unchanged.

True mode:

- `/` redirects to named `RoomEntry`.
- no route named `Dashboard` exists.
- authenticated `/auth` and successful login target RoomEntry.
- RoomEntry hides the dashboard control and shows a sign-out control.
- sign-out clears the existing session, clears `globalStore.currentUser`, and navigates to `Auth` without logging the token.
- `/rooms` and `/rooms/:slug` remain authenticated room routes.

`DashboardView.vue` and the legacy frontend services were intentionally retained so the false artifact remains the pre-cutover and rollback artifact.

### Legacy global-state retirement

`globalStore.retireLegacyGlobalRuntimeState()` resets only:

- `queueState`
- `voteSessions`
- `autoQueueConfig`
- global `connectionStatus`

Fresh-state factories prevent mutable reset objects from being reused.

The operation preserves:

- `currentUser`
- the exact `roomQueues` object and all entries
- session storage
- room-local WebSocket state

### Global WebSocket lifecycle

The global `WebSocketClient` now has explicit intentional-close ownership:

- `disconnect()` marks the close intentional before closing;
- the socket is detached before `close()`;
- pending reconnect timers are cancelled;
- status remains `disconnected`;
- stale callbacks are rejected by socket identity;
- false-mode `connect()` deliberately re-enables normal reconnection;
- true-mode `connect()` and `scheduleReconnect()` cannot open or re-arm global `/ws`.

`frontend/src/services/room-websocket.js` was not changed.

### Bootstrap

True mode performs:

1. `globalStore.retireLegacyGlobalRuntimeState()`;
2. `wsClient.disconnect()`;
3. application mount with the cutover-aware router.

False mode performs no forced global reset or disconnect and preserves existing behavior.

## Changed files

Runtime/configuration:

- `frontend/src/config/cutover.js` (new)
- `frontend/.env.example`
- `frontend/src/main.js`
- `frontend/src/router/index.js`
- `frontend/src/services/websocket.js`
- `frontend/src/store/index.js`
- `frontend/src/views/AuthView.vue`
- `frontend/src/views/RoomEntryView.vue`

Tests added:

- `frontend/src/__tests__/cutover-bootstrap.spec.js`
- `frontend/src/config/__tests__/cutover.spec.js`
- `frontend/src/router/__tests__/cutover-modes.spec.js`
- `frontend/src/services/__tests__/cutover-modes.spec.js`
- `frontend/src/store/__tests__/retire-legacy-global-runtime-state.spec.js`
- `frontend/src/views/__tests__/cutover-modes.spec.js`

Existing tests updated and pinned to the false artifact where appropriate:

- `frontend/src/router/__tests__/index.spec.js`
- `frontend/src/router/__tests__/router.spec.js`
- `frontend/src/services/__tests__/websocket.spec.js`
- `frontend/src/views/__tests__/AuthView.spec.js`
- `frontend/src/views/__tests__/RoomEntryView.spec.js`

Focused project documentation:

- `documents/00-project-management/PROJECT_STATE.md`
- `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md`
- `documents/00-project-management/SPRINTS/028-frontend-global-path-retirement.md`
- `documents/00-project-management/SPRINTS/README.md`
- `documents/00-project-management/SPRINTS/active.md`

## Automated verification

Builder-reported at final reviewed head `491770ed90e3940307df3933d4236508fac838f3` using Node 22.14.0:

```text
VITE_ROOM_CUTOVER_AUTHORITATIVE=false npm run test:unit -- --run
  → Test Files 24 passed (24); Tests 443 passed (443)

VITE_ROOM_CUTOVER_AUTHORITATIVE=true npm run test:unit -- --run
  → Test Files 24 passed (24); Tests 443 passed (443)

VITE_ROOM_CUTOVER_AUTHORITATIVE=false npm run build
  → built successfully, exit 0

VITE_ROOM_CUTOVER_AUTHORITATIVE=true npm run build
  → built successfully, exit 0

git diff --check
  → clean
```

GitHub had no CI status checks or pull-request workflow runs for the reviewed head. The Architect inspected the actual changed files and patches but did not independently rerun the frontend commands.

The true build still emits a `DashboardView` chunk because Rollup sees the false-mode lazy import expression, but no true-mode route references it, so it is unreachable through normal routing.

## Manual compatibility verification

Performed on 2026-07-28 in an isolated local environment:

- backend: `go run ./cmd/server` on `http://localhost:1111`
- fresh dedicated PostgreSQL database, removed after verification
- false artifact: `vite preview` on `http://localhost:4173`
- true artifact: `vite preview` on `http://localhost:4174`
- both artifacts used `VITE_API_BASE_URL=http://localhost:1111`
- nothing was deployed

### False artifact

| Check | Outcome |
|---|---|
| Successful real Google login lands on Dashboard | **Blocked:** real Google ID token and allowed-domain account required; no local bypass |
| Authenticated `/auth` landing surrogate | Pass — redirected to Dashboard through the shared landing helper |
| `/` renders Dashboard | Pass |
| Global queue loads | Pass — `GET /api/queue` returned 200 |
| Global `/ws` connects | Pass — connected status and backend connection log observed |
| Rooms navigation opens RoomEntry | Pass |
| RoomEntry returns to Dashboard | Pass |
| Direct `/rooms` | Pass |
| Direct `/rooms/{slug}` | Pass for routing; room data remained unauthorized without a server session |
| Unauthenticated `/` | Pass — redirected to Auth |

### True artifact

| Check | Outcome |
|---|---|
| Successful real Google login lands on RoomEntry | **Blocked:** same real-login requirement |
| Authenticated `/auth` landing surrogate | Pass — redirected to RoomEntry through the shared landing helper |
| `/` lands on RoomEntry | Pass |
| Dashboard route absent | Pass |
| Sign-out clears session/current user and returns to Auth | Pass |
| Room list/create/manual-open/invite-redemption against authenticated backend | **Blocked:** server-accepted session required; synthetic client token received 401 |
| Direct `/rooms/{slug}` | Pass for routing |
| No global `/ws` attempt | Pass |
| No global `/ws` reconnect after 8 seconds | Pass |

Synthetic JavaScript-dispatched clicks could stall the existing `App.vue` transition in automation. The same behavior reproduced on the pre-existing false-artifact Dashboard Exit control, while trusted clicks completed normally; this was recorded as an automation artifact, not an R14d regression.

## Review and closure

Initial Architect verdict: **Current sprint needs fixes.** Findings covered contradictory project-state fragments, missing manual compatibility evidence, and inaccurate invalid-value wording.

Review-follow-up commit `491770ed90e3940307df3933d4236508fac838f3` corrected the documentation and recorded the manual matrix without changing runtime code.

Final Architect verdict: **Accepted with minor operational follow-up.** No blocking or important implementation findings remained.

The Product Owner approved R14d on 2026-07-28. PR #25 was merged into `dev` at `67bd57a8abef92d42cdfefb90069563340147d10` and the sprint was formally closed.

## Remaining operational gate

Before or during R14c's paired maintenance-window smoke test, verify with a real allowed-domain account:

- false-artifact real login lands on Dashboard;
- true-artifact real login lands on RoomEntry;
- true-artifact room list/create/open/invite flows work with a server-accepted session.

These are R14c entry/deployment checks, not unfinished R14d implementation work.

The true artifact is not claimed production-compatible with the future true server until the paired R14c maintenance-window smoke test passes.

## Scope confirmation

R14d introduced no backend, migration, Docker, Nginx, TLS, deployment, R14c, or R14e work. Production cutover has not occurred. R14c and R14e remain inactive.
