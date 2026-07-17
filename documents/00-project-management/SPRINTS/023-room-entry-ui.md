# Sprint R05b1 — Room entry, creation, and invite redemption UI

**Status (2026-07-16):** **Closed on `dev` (2026-07-16) and accepted by the Product Owner on 2026-07-16.** R05b1 was implemented on `dev` (2026-07-16) at commit `245f3c3`, corrected at commit `24a52449` (corrective pass for archived-room navigation, production-router tests, tightened invite-token sensitivity wording and tests), and accepted by the Product Owner on 2026-07-16. The accepted runtime scope is: authenticated `/rooms` entry route; active-room listing, manual open by slug, create-room, and invite-redemption UI; `Dashboard → Rooms` navigation; `RoomView Back → RoomEntry`; no arbitrary `joinRoom` contract; no player-lease UI or heartbeat lifecycle work. R05b2 is active on `dev`; R05b remains incomplete as an R14c prerequisite until R05b2 is separately implemented, reviewed, and accepted.

R05b is split into two slices:

- **R05b1 — Room entry, creation, and invite redemption UI** — this file. Implemented on `dev` (2026-07-16) and accepted by the Product Owner on 2026-07-16.
- **R05b2 — Player-lease UI and heartbeat lifecycle** — active on `dev`.

R05b remains incomplete as an R14c prerequisite until both slices are accepted.

## Goal

Give authenticated users an actual SPA route for finding and entering rooms, creating a room, and joining through an invite token, while preserving the existing global dashboard until R14d.

## Scope (R05b1)

Frontend-only. R05b1 wires the existing backend R04 / R10a / R06 routes to a focused `RoomEntryView.vue` behind a new `/rooms` route, while preserving the still-authoritative global dashboard as the login destination.

- New authenticated route `/rooms` (router name `RoomEntry`, `requiresAuth: true`).
- New `frontend/src/views/RoomEntryView.vue` providing: active-room list, manual open by slug, create room, redeem invite token, back to dashboard.
- Four thin API methods on `frontend/src/services/api.js`:
  - `api.createRoom(slug, name)` — `POST /rooms` with body exactly `{ slug, name }`.
  - `api.listRooms(status = 'active')` — `GET /rooms?status=active`; the query is omitted when `status` is intentionally empty.
  - `api.getRoom(slug)` — `GET /rooms/{encodedSlug}` for the manual-open affordance.
  - `api.redeemInvite(token)` — `POST /invites/{encodedToken}/redeem` with no body.
- Dashboard gains a Rooms navigation control (router name `RoomEntry`).
- RoomView `Back` navigates to `RoomEntry` (was `Dashboard`).
- Login (`/auth`) and authenticated `/auth` visits continue to redirect to `Dashboard`. R14d owns changing the default destination and retiring the global dashboard.

R05b1 does NOT add `api.joinRoom`. Joining a room means invite redemption — there is no arbitrary membership-creation endpoint. R05b1 does NOT remove `Dashboard`. R05b1 does NOT add player-lease claim/read/heartbeat/release UI methods (R05b2).

## Out of scope (R05b1)

- Player-lease claim/read/heartbeat/release API methods, UI, timers, or lifecycle behavior — **R05b2**.
- Invite creation/listing/revocation, sharing links, or QR codes.
- Arbitrary joining without an invite.
- Membership-filtered room listing or a new backend "my rooms" endpoint.
- Room search, public discovery, pagination, or filtering beyond the existing `status` query.
- Room-role corrections inside RoomView.
- Global dashboard removal or `VITE_ROOM_CUTOVER_AUTHORITATIVE`.
- Any backend, SQL, WebSocket, Docker, Nginx, authentication, or authorization changes.
- Automatic redirect away from Dashboard after login.

## Files touched

Frontend runtime:
- `frontend/src/services/api.js` — four new thin API methods (`createRoom`, `listRooms`, `getRoom`, `redeemInvite`).
- `frontend/src/router/index.js` — added `/rooms` route with `name: 'RoomEntry'`, `meta: { requiresAuth: true }`.
- `frontend/src/views/RoomEntryView.vue` — new focused view (local state only; no globalStore mutations).
- `frontend/src/views/DashboardView.vue` — added `Rooms` nav control + `handleOpenRooms` (router push to `RoomEntry`) + matching CSS.
- `frontend/src/views/RoomView.vue` — `handleBack` now navigates to `RoomEntry`.

Frontend tests:
- `frontend/src/services/__tests__/roomApi.spec.js` — added `Room Entry / Create / Redeem API (R05b1)` describe block (13 tests + 1 negative assertion that `api.joinRoom` does NOT exist).
- `frontend/src/views/__tests__/RoomEntryView.spec.js` — new file (22 tests).
- `frontend/src/views/__tests__/DashboardView.spec.js` — added `Rooms navigation (R05b1)` describe block (2 tests).
- `frontend/src/views/__tests__/RoomView.spec.js` — added `handleBack navigates to RoomEntry (not Dashboard) per R05b1` test.
- `frontend/src/router/__tests__/router.spec.js` — new file (7 tests) pinning the production router's `requiresAuth` guard: `/rooms` requires auth, authenticated `/rooms` → `RoomEntry`, authenticated `/auth` → `Dashboard`, unauthenticated `/` and `/rooms/:slug` → `Auth`, stale localStorage user without a valid session is cleared on protected-route visits.

Project management:
- `documents/00-project-management/SPRINTS/023-room-entry-ui.md` — this file.
- `documents/00-project-management/SPRINTS/active.md` — record R05b1 implementation + the R05b1/R05b2 split + corrected `api.joinRoom` wording.
- `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md` — R05b1 active + R05b1/R05b2 split + R05b still blocking R14c until both slices are accepted.
- `documents/00-project-management/PROJECT_STATE.md` — current-state summary refresh after R05b1 implementation.

No backend, SQL, WebSocket, Docker, Nginx, auth, or dependency changes.

## Wire contract (frontend ↔ backend)

The four methods map to existing R04 / R10a / R06 routes registered in `cmd/server/main.go`. The frontend does NOT send any client identity field. Identity comes exclusively from the bearer token added by `api.request`. Slugs and tokens are URL-encoded through `encodeURIComponent`. Reserved characters survive the trip.

```text
api.createRoom(slug, name)
  → POST /api/rooms
  body: { slug, name }
  identity: bearer (api.request)

api.listRooms(status = 'active')
  → GET /api/rooms?status=active      # status defaults to "active"
  → GET /api/rooms                    # status == '' → query omitted
  identity: bearer

api.getRoom(slug)
  → GET /api/rooms/{encodedSlug}
  identity: bearer

api.redeemInvite(token)
  → POST /api/invites/{encodedToken}/redeem
  no body
  identity: bearer
```

## Implementation details

### Router

`frontend/src/router/index.js` adds one route:

```text
path: '/rooms'
name: 'RoomEntry'
component: () => import('../views/RoomEntryView.vue')
meta: { requiresAuth: true }
```

The existing `beforeEach` guard enforces `requiresAuth` via the same path used for the global Dashboard. Authenticated `/auth` visits still redirect to `Dashboard` (R14d owns changing the default destination).

### RoomEntryView

Keeps all room-entry state local (no globalStore slots). Independent guards for each async flow.

State surfaces:

| Local ref | Purpose | Cleared on |
| --- | --- | --- |
| `activeRooms` | Rendered active-room list | Stale refresh + post-create refresh overwrite |
| `listInFlight` | Refresh in flight | Refresh settle |
| `listError` | Refresh error text | Refresh start + non-empty error |
| `listGeneration` | Monotonic refresh generation | Bumped on every refresh start + before manual `refreshActiveRooms` |
| `manualSlug` | Manual-open form input | Trim before submit |
| `manualOpenInFlight` | Manual-open in flight | Settle |
| `manualOpenError` | Manual-open error | Next attempt |
| `createSlug` / `createName` | Create-room form input | Cleared on success |
| `createInFlight` | Create in flight | Settle |
| `createError` | Create error | Next attempt |
| `inviteToken` | Redeem form input | Cleared on success AND on failure (sensitive) |
| `redeemInFlight` | Redeem in flight | Settle |
| `redeemError` | Redeem error | Next attempt |
| `redeemSuccess` | Redeem success message | Next attempt |

Async ownership:

- **List refresh** records `listGeneration` at start; resolves only apply if the generation still matches. A slow older refresh can never overwrite a newer one.
- **Manual open** uses an in-flight `manualOpenInFlight` ref so a double-click cannot fire two requests.
- **Create room** uses `createInFlight`; on success clears the form, bumps `listGeneration` (so any in-flight refresh is dropped), calls `refreshActiveRooms()`, then navigates to the new room.
- **Redeem invite** uses `redeemInFlight`; on success clears the token input, calls `refreshActiveRooms()`, and resolves `membership.room_id` → slug through the refreshed list. If the post-redeem refresh fails or cannot resolve the slug, a success message is surfaced ("Membership created. Use Refresh and try opening the room by slug.") — the redemption is NOT retried automatically.

Status-code mapping (frontend-only, presentational):

| Operation | Status | Message |
| --- | --- | --- |
| list | 401 | You are signed out. Log in again. |
| list | 403 | You are not allowed to list rooms. |
| list | 400 | Invalid status filter. |
| list | other | Could not load active rooms. |
| manual-open | 400 | Invalid room slug. |
| manual-open | 401 | You are signed out. Log in again. |
| manual-open | 404 | Room not found. |
| manual-open | other | Could not open room. |
| manual-open (archived room) | — | This room is no longer available. (MUST NOT navigate; the user remains on RoomEntry; archived rooms cannot be opened or joined) |
| create | 400 | Invalid or reserved slug or name. |
| create | 401 | You are signed out. Log in again. |
| create | 409 | A room with this slug already exists. |
| create | other | Could not create room. |
| redeem | 401 | You are signed out. Log in again. |
| redeem | 404 | This invite is invalid, expired, or revoked. |
| redeem | 409 | This room is archived. |
| redeem | 410 | This invite has been used up. |
| redeem | other | Could not redeem invite. |

The active-room list heading is **Active rooms**. The hint states that an invite is required to open a room the actor has not joined. Listed rooms navigate directly to `/rooms/{slug}` — the manual-open form calls `getRoom` first, but a listed room is NEVER probed with an additional membership check.

### Archived-room behavior (manual-open)

Per the R05b1 contract: an active room navigates to `RoomView`; an archived (or otherwise non-active) room shows "This room is no longer available." and **does not navigate**. The user remains on RoomEntry; archived rooms cannot be opened or joined (the backend `RedeemInvite` path rejects an archived room with `ErrArchived`, surfaced as HTTP 409). The handler short-circuits with an immediate `return` after surfacing the error message; no `router.push` is invoked for non-active rooms.

### Invite-token sensitivity

Treat invite tokens as sensitive across the entire flow. The raw (unencoded) token MUST NOT appear in:

- the SPA / router URL (route path, route query, route params, or any other router state);
- `localStorage` or `sessionStorage` (as a key OR as the value of any key);
- `console.log` / `console.warn` / `console.error`;
- toast messages, error text, or any other displayed message;
- the rendered DOM text of RoomEntryView.

The encoded backend API URL (`/api/invites/{encodedToken}/redeem`) DOES contain the encoded token — that is the documented wire shape and is verified separately by `frontend/src/services/__tests__/roomApi.spec.js`. The SPA-side restriction is on the raw token string and on every SPA-visible surface above.

The invite input is cleared from the form on both successful AND failed redemption. The clear-on-failure path is intentional: a failed redemption must not leave the token lingering in the form after the user sees the error.

### Dashboard navigation

`DashboardView.vue` gains a `Rooms` button (data-testid `rooms-nav-btn`) between the Copy-token button and the user name. Click handler `handleOpenRooms` pushes `{ name: 'RoomEntry' }`.

### RoomView Back

`handleBack` now navigates to `{ name: 'RoomEntry' }` (was `Dashboard`). The R05b1 contract requires this change so a user exploring a room from the RoomEntry surface can navigate back to the entry surface instead of skipping it.

## Tests

### `frontend/src/services/__tests__/roomApi.spec.js` — Room Entry / Create / Redeem API (R05b1)

- `createRoom` posts ONLY `{ slug, name }` — no `user_id`, role, or display name.
- `createRoom` stringifies null slug/name safely.
- `createRoom` propagates APIError on 400 / 401 / 409 / 500.
- `listRooms` defaults to `?status=active` query string.
- `listRooms` honors an explicit non-default status (e.g. `archived`).
- `listRooms` omits the status query when status is empty string.
- `listRooms` propagates APIError on 401 / 403 / 400.
- `getRoom` targets `GET /rooms/{slug}` and URL-encodes the slug.
- `getRoom` propagates APIError on 400 / 401 / 404.
- `redeemInvite` targets `POST /invites/{token}/redeem` with NO body and URL-encodes reserved characters.
- `redeemInvite` URL-encodes a token with no special chars verbatim.
- `redeemInvite` propagates APIError on 401 / 404 / 409 / 410.
- Lifecycle requests never carry identity fields (combined audit).
- `api.joinRoom` is NOT exposed (no arbitrary join endpoint exists).

### `frontend/src/views/__tests__/RoomEntryView.spec.js` — RoomEntryView

- Fetches active rooms on mount and renders the list.
- Renders the empty hint when the active-room list is empty.
- Renders the loading state during the in-flight refresh.
- Surfaces a clear 401 error when the active-room list fetch is unauthorized.
- A stale refresh response does NOT overwrite a newer refresh result.
- Opens a listed room on click without making an additional membership probe.
- Renders an Open button for each listed room with the per-room data-testid.
- Manual open by slug: active room navigates; 400/401/404 surface clear errors; archived room shows the "no longer available" message AND does NOT navigate. The test asserts `pushMock` is never called for the archived case.
- Create room success clears the form, refreshes the list, and navigates.
- Create room maps 400 / 401 / 409 / 500 to clear error messages.
- Create room trims whitespace before submitting slug and name.
- Create room does NOT silently rewrite slug beyond trimming (backend is authoritative on normalization).
- Invite redemption resolves `room_id` to slug via a refreshed active-room list and navigates.
- Invite token is cleared after a successful redemption.
- Invite token is cleared after a FAILED redemption (no lingering token in the form).
- Invite token is NEVER placed in the SPA/router URL, route query/history, storage (every key/value of `localStorage` and `sessionStorage`), `console.log` / `console.warn` / `console.error`, toast messages, or the rendered DOM text. Every router `push` argument is also asserted to not contain the raw token.
- Invite redemption existing-member outcome (room_id present) navigates normally.
- Invite redemption with follow-up list failure surfaces a non-token success message and does NOT retry redemption.
- Invite redemption maps 401 / 404 / 409 / 410 to distinct error messages.
- In-flight guards prevent duplicate submissions for manual-open / create / redeem.
- Back to dashboard navigates to Dashboard.
- RoomEntryView keeps all room-entry state LOCAL (no globalStore mutation).

### `frontend/src/views/__tests__/DashboardView.spec.js` — Rooms navigation (R05b1)

- Renders a `Rooms` button in the top nav.
- `Rooms` button navigates to the `RoomEntry` route.

### `frontend/src/views/__tests__/RoomView.spec.js` — R05b1 Back navigation

- `handleBack` navigates to `RoomEntry` (NOT `Dashboard`).

### `frontend/src/router/__tests__/router.spec.js` — Production router guard (R05b1)

- The production `frontend/src/router/index.js` exposes the `/rooms` route as `name: 'RoomEntry'` with `meta.requiresAuth: true`.
- Unauthenticated `/rooms` redirects to `Auth`.
- Authenticated `/rooms` lands on `RoomEntry`.
- Authenticated `/auth` redirects to `Dashboard` (R14d owns changing the default destination).
- Unauthenticated `/rooms/:slug` redirects to `Auth`.
- Unauthenticated `/` redirects to `Auth`.
- Stale `localStorage` user without a valid session is cleared on protected-route visits and the visit is redirected to `Auth`.

## Verification

```text
cd frontend && npm run test:unit -- --run   # 352/352 pass
cd frontend && npm run build                 # clean
git diff --check                             # clean
```

Manual walk-through (out-of-band, recorded for the Product Owner):

1. `login` → `Dashboard` → click `Rooms`.
2. Create a room → navigated to `RoomView` → click `Back` → returns to `RoomEntry`.
3. Redeem an invite token → navigated to the target `RoomView`.
4. Open a non-member room by typing its slug and clicking `Open` → existing `RoomView` authorization error surface.
5. Click `Back to dashboard` → returns to `Dashboard`.

## Implementation summary

Implemented on `dev` (2026-07-16). The frontend SPA now exposes:

- A new authenticated route `/rooms` (router name `RoomEntry`) backed by `RoomEntryView.vue`.
- Four new thin API methods: `createRoom`, `listRooms`, `getRoom`, `redeemInvite`.
- A `Rooms` navigation control on the global `Dashboard`.
- A `Back` button on `RoomView` that navigates to `RoomEntry` (was `Dashboard`).

R05b1 preserves: the global Dashboard, the login destination, the global `/ws` 16-event inventory, the R07b/R07d/R09a/R09b/R09c/R09d/R09f/R10b/R11a per-room WebSocket events, the R06 `room_archived` wire value + payload, the R05 in-memory session-token model, the R10a members-list response wrapper, the schema version 8, and all prior sprint runtime surfaces.

R05b1 does NOT introduce a `joinRoom` endpoint, a global error framework, or any change to the backend, SQL, WebSocket, Docker, Nginx, or auth contracts. R05b1 keeps the `/auth` redirect-to-dashboard contract intact.

Verification (recorded 2026-07-16, post corrective-pass):

- `cd frontend && npm run test:unit -- --run` — **352 / 352 pass** (345 baseline + 7 new production-router-guard tests in `frontend/src/router/__tests__/router.spec.js`).
- `cd frontend && npm run build` — **clean** (RoomEntryView chunks present: `RoomEntryView-BT-bzez4.js`, `RoomEntryView-EmcJCVrD.css`).
- `git diff --check` — **clean**.

Corrective-pass changes (recorded 2026-07-16):

- Archived (or otherwise non-active) rooms in the manual-open form now short-circuit with an immediate `return` after surfacing the "This room is no longer available." message — they MUST NOT navigate to `RoomView`. The user remains on `RoomEntry`; archived rooms cannot be opened or joined.
- Invite-token sensitivity wording tightened: the raw (unencoded) token MUST NOT appear in the SPA / router URL, route query, route history, `localStorage` / `sessionStorage` (every key/value, not just the token string as a key), `console.log` / `console.warn` / `console.error`, toast messages, or the rendered DOM text. The encoded backend API URL (`/api/invites/{encodedToken}/redeem`) still contains the encoded token — that is the documented wire shape and is verified separately by `frontend/src/services/__tests__/roomApi.spec.js`.
- New `frontend/src/router/__tests__/router.spec.js` exercises the production `frontend/src/router/index.js` so the production route registration + `beforeEach` guard are not silently regressed (unauthenticated `/rooms` → `Auth`, authenticated `/rooms` → `RoomEntry`, authenticated `/auth` → `Dashboard`). The production router is a module-level singleton (constructed once at first import); each test resets auth/session state in `beforeEach` and performs an explicit navigation so the guard runs against a clean slate.

## Closure pass (2026-07-16)

Documentation-only closure. R05b1 was accepted by the Product Owner on 2026-07-16 at corrective commit `24a52449`. The closure pass recorded here corrects the inaccurate explanatory wording in three locations:

- `frontend/src/views/RoomEntryView.vue` — `openBySlug` comment no longer claims a user can join an archived room through invite redemption. Archived rooms cannot be opened or joined (the backend `RedeemInvite` path rejects an archived room with `ErrArchived`, surfaced as HTTP 409).
- `frontend/src/views/__tests__/RoomEntryView.spec.js` — archived-case comment now reads "The user remains on RoomEntry; archived rooms cannot be opened or joined."
- `documents/00-project-management/SPRINTS/023-room-entry-ui.md` — archived-room status-code table row, the "Archived-room behavior (manual-open)" section, and the test inventory bullet all use the corrected wording.

The closure pass also corrects the `frontend/src/router/__tests__/router.spec.js` comment that implied each `import('../index')` produces a fresh production-router module: the production router is a cached module-level singleton; tests share that instance and reset auth/session state in `beforeEach` before each navigation.

The closure pass changes NO runtime behavior. It records R05b1 as closed/accepted, leaves R05b2 planned and inactive, and leaves R05b incomplete as an R14c prerequisite until R05b2 is separately implemented, reviewed, and accepted.

## R05b2 — next slice (planned, NOT active)

R05b2 is the planned follow-up slice that lands the player-lease claim/read/heartbeat/release UI and timer-driven lifecycle. R05b2 is separate from R10c (R10c is the host-only delete-room / remove-member controls; R05b2 is the broader player-lease surface). R05b2 remains a BLOCKING PREREQUISITE for R14c — without it, retiring `/api/queue` / `/ws` etc. strands users on a global dashboard with no way to enter or operate a room. R05b2 is NOT pulled forward until the Product Owner accepts R05b1.

## R05b status after R05b1 implementation

R05b remains incomplete as an R14c prerequisite until both R05b1 AND R05b2 are accepted by the Product Owner.
