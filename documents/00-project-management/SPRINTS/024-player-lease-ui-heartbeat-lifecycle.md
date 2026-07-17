# Sprint R05b2 — Player-lease UI and heartbeat lifecycle

**Status (2026-07-17):** **Closed on `dev` and accepted by the Product
Owner on 2026-07-17.** R05b1 was closed and accepted on 2026-07-16.
R05b2 is the second and final slice of the legacy R05b bucket; it
implements the player-lease UI and heartbeat lifecycle in `RoomView`
against the already-accepted backend contract. During Architect review,
an accepted corrective sequence hardened the `useRoomPlayerLease`
lifecycle: `1f63a3a6` (generation-aware pending-tick drain + shared
gate-release helper) and `a9e09880` (terminal-boundary guards),
following the first-review commit `3c5ab0fd`. With
R05b2 accepted, the legacy R05b bucket (R05b1 + R05b2) is **COMPLETE**,
satisfying the R05b blocking prerequisite for R14c. **No sprint is
currently active on `dev`.** R09h and every R14 implementation slice
(R14b / R09i / R14c / R14d / R14e) remain planned and inactive.

## Goal

Expose the accepted player-lease backend contract inside `RoomView`:

- The room host can claim the player lease.
- The holding browser keeps the lease alive via a heartbeat.
- All active room members can see the lease status.
- Direct playback controls are enabled only for the current lease
  holder.

## Out of scope

Backend, SQL, migration, WebSocket-event, Docker, Nginx, session, or
authentication changes. A new `room_player_lease_changed` event.
Device fingerprints, tab IDs, browser IDs, cross-tab leadership, or
lease-transfer design. Automatic lease claim after room creation.
Automatic release on navigation, unload, disconnect, offline, or tab
close. Lease transfer between users. Reopening or restoring archived
rooms. Server-configurable lease or grace durations. Playback engine,
YouTube player ownership, media synchronization, or host audio output.
Global dashboard removal. R09h vote-to-prioritize parity. Broad
`RoomView` restructuring or a global lease store.

## Files touched

Frontend runtime:
- `frontend/src/services/api.js` — four thin API methods
  (`claimRoomPlayerLease`, `heartbeatRoomPlayerLease`,
  `releaseRoomPlayerLease`, `getRoomPlayerLease`).
- `frontend/src/composables/useRoomPlayerLease.js` — new lifecycle
  composable; sole owner of lease state, timers, listeners, and
  single-flight guards.
- `frontend/src/views/RoomView.vue` — always-visible Player device
  panel; `Release player and archive room` button; lease-gated direct
  playback; corrected 410 toast copy; room-host source now comes from
  the room membership list.

Frontend tests:
- `frontend/src/services/__tests__/roomApi.spec.js` — new
  `Player Lease API (R05b2)` describe block (10 tests).
- `frontend/src/composables/__tests__/useRoomPlayerLease.spec.js` —
  new file, fake-timer-driven composable tests (20 tests).
- `frontend/src/views/__tests__/RoomView.spec.js` — new
  `RoomView (R05b2 player-lease UI)` describe block (9 tests).

Project management:
- `documents/00-project-management/SPRINTS/024-player-lease-ui-heartbeat-lifecycle.md`
  — this file.
- `documents/00-project-management/SPRINTS/active.md` — record R05b2
  implementation.
- `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md` —
  R05b2 closed/accepted line; R05b prerequisite for R14c satisfied.
- `documents/00-project-management/PROJECT_STATE.md` — current-state
  summary refresh after R05b2 implementation.

No backend, SQL, WebSocket, Docker, Nginx, auth, or dependency
changes.

## Wire contract (frontend ↔ backend)

The four methods map to the existing R05b backend routes registered in
`cmd/server/main.go` (lines 477–488). The frontend does NOT send any
client identity field. Identity comes exclusively from the bearer
token added by `api.request`. Slugs are URL-encoded through
`encodeURIComponent`. Reserved characters survive the trip.

```text
api.claimRoomPlayerLease(slug)
  → POST /api/rooms/{encodedSlug}/player/claim
  identity: bearer (api.request)
  no body

api.heartbeatRoomPlayerLease(slug)
  → POST /api/rooms/{encodedSlug}/player/heartbeat
  identity: bearer (api.request)
  no body

api.releaseRoomPlayerLease(slug)
  → POST /api/rooms/{encodedSlug}/player/release
  identity: bearer (api.request)
  no body; 204 → null

api.getRoomPlayerLease(slug)
  → GET /api/rooms/{encodedSlug}/player/lease
  identity: bearer (api.request)
  no body
```

## Implementation details

### Player-lease composable

`useRoomPlayerLease(slugRef, deps)` is the sole owner of:

- Current lease object + presentation state.
- Initial and periodic lease reads.
- Claim, heartbeat, and release operations.
- Normal (20-second) and retry (5-second) timers.
- `visibilitychange` and `online` listeners.
- Single-flight request guards.
- Slug / mount generation protection (stale responses cannot mutate
  the new room).
- Terminal cleanup on unmount and slug change.

State enum (reactive ref):

```text
loading
none
held_by_me
held_by_other
retrying
expired_pending_archive
unavailable
```

At each normal tick:

- If `claimed_by_user_id === currentUser.id`, call `heartbeat`.
- Otherwise, refresh with `GET`.

Visibility and `online` triggers schedule an immediate lifecycle tick.
Only one lifecycle request is in flight at a time; if a trigger
arrives during an in-flight request, exactly one follow-up tick is
scheduled after the in-flight resolves.

On heartbeat 410 the composable stops heartbeating and enters
`expired_pending_archive`, then polls `api.getRoom(slug)` every 5
seconds with generation-safe cleanup until the backend reports the
room non-active. It does NOT offer reclaim. The backend sweeper owns
archival.

On heartbeat 403 the composable exits holder mode and refreshes the
lease once. On 404 it exits holder mode and sets `none`. On 409 it
marks the room archived locally. On 401 it stops the lifecycle and
surfaces a "signed out" toast.

On heartbeat 5xx or network failure the composable retries after 5
seconds without toast spam.

On release 204 the composable stops the lifecycle, marks the room
archived locally, and shows a success toast. On release 404 it does
NOT mark archived (the backend defines 404 as no mutation, no
broadcast).

### Player-device panel

Always-visible `Player device` panel renders one of:

| State | Text |
| --- | --- |
| loading | Loading player status… |
| none | No active player. |
| held_by_me | Player held by you. |
| held_by_other | Player held by user #{claimed_by_user_id}. |
| retrying | Heartbeat temporarily interrupted — retrying. |
| expired_pending_archive | Lease expired. Room is being archived. |
| unavailable | Room archived or unavailable. |

The panel never auto-claims.

### Claim

Visible when ALL are true:

- Viewer is the room host (membership.role === 'host').
- Room is connected, not archived, not removed.
- Composable state is `none`.
- No claim is in flight.

On 409 the composable refreshes the lease once:

- 200 → apply, set state to `held_by_me` or `held_by_other`.
- 409 → mark the room archived locally.
- 404 → show a generic "Claim is in conflict with another holder." toast.

### Explicit release

Button labelled exactly `Release player and archive room`. Visible
when the viewer is the room host AND is the current lease holder. The
`window.confirm()` text explicitly states that release archives the
room and is not a transfer.

### Playback gating

`canControlPlayback` requires:

- Authenticated current user.
- Connected `RoomView`.
- Active and non-removed room.
- Lease state `held_by_me`.
- Lease not in `retrying` / `expired_pending_archive` / `unavailable`.
- `lease.claimed_by_user_id === currentUser.id`.

Applied to Play, Pause, Prev, Skip, Ended, Vol+, Vol−. NOT applied to
queue additions, ordinary removals, chat, voting, or other unrelated
controls. No admin bypass — the backend authorizes by
`claimed_by_user_id`.

### Room-role source

`isHost` is derived from
`roomState.members.find(m => Number(m.user_id) === Number(currentUser.id))?.role === 'host'`.

NOT `currentUser.role`. Applied to both the new lease controls AND
the existing R10c host controls (host panel + remove-member gates).
Presentation gate only — backend authorization remains authoritative.

### Browser / route lifecycle

While mounted, scheduled heartbeats continue while the document is
hidden. On `visibilitychange` → `visible` OR on `window.online`, an
immediate lifecycle tick is scheduled.

On slug change or unmount:

- Invalidate the old generation (numeric token).
- Clear every timer.
- Remove every listener.
- Prevent pending old-slug responses from mutating the new room.

Back navigation, route changes, tab closure, `pagehide`,
`beforeunload`, offline transitions, and visibility changes MUST
NEVER call `release`.

## Tests

### `frontend/src/services/__tests__/roomApi.spec.js` — Player Lease API (R05b2)

- `claimRoomPlayerLease` targets `POST /rooms/{slug}/player/claim`
  with NO body and bearer when valid.
- `heartbeatRoomPlayerLease` targets `POST /rooms/{slug}/player/heartbeat`
  with NO body.
- `releaseRoomPlayerLease` targets `POST /rooms/{slug}/player/release`
  with NO body and returns `null` on 204.
- `getRoomPlayerLease` targets `GET /rooms/{slug}/player/lease` with
  NO body.
- `getRoomPlayerLease` URL-encodes slugs with reserved characters.
- All four propagate `APIError` on the documented status codes.
- Lifecycle requests never carry identity or lease fields.

### `frontend/src/composables/__tests__/useRoomPlayerLease.spec.js`

- Initial state is `loading` until the first GET resolves.
- Initial GET with no lease (404) → state `none`.
- Initial GET held by current user → state `held_by_me`.
- Initial GET held by another user → state `held_by_other`.
- Non-holder tick is always a GET (never a heartbeat).
- Current holder heartbeats every 20 seconds.
- Claim success begins the holder cadence.
- Transient failures retry after 5 seconds without toast spam.
- Heartbeat 403 exits holder mode; refresh GET once.
- Heartbeat 404 exits holder mode and sets `none`.
- Heartbeat 409 marks archived locally.
- Heartbeat 410 enters `expired_pending_archive`; heartbeats stop.
- Hidden state never calls `release`.
- Visibility → visible triggers an immediate tick.
- Unmount clears timers and listeners.
- Slug change clears timers; stale old-slug responses cannot mutate
  the new room.
- Explicit release on 204 stops lifecycle and marks archived locally.
- Release 404 does NOT mark archived.
- Claim 409 refreshes once; refresh 200 applies the new holder.
- Claim 409 with refresh 404 surfaces a generic conflict toast.

#### Accepted corrective-pass race/terminal-boundary regressions

Added by the accepted corrective sequence (`1f63a3a6`, `a9e09880`):

- A slug change during an in-flight old-slug GET starts exactly one
  new-room GET after the old request releases the shared gate.
- A slug change during an in-flight heartbeat starts the new-room GET
  after the heartbeat releases the gate.
- An `online` event during Claim produces exactly one immediate
  post-Claim tick.
- A Claim queued behind a passive GET aborts if that GET reports the
  room archived (409) — no claim request crosses the terminal boundary.
- A Release queued behind a heartbeat aborts if that heartbeat reports
  410 — no release request crosses the boundary.
- Stale pending work in a terminal room fires no extra request after a
  later slug change.

### `frontend/src/views/__tests__/RoomView.spec.js` — RoomView (R05b2 player-lease UI)

- Renders an always-visible Player device panel even with an empty
  queue.
- Room-host status comes from the membership list, NOT
  `currentUser.role`.
- A globally admin user who is only a room guest cannot claim.
- A globally ordinary user who is the room host can claim.
- All active members see the Player device panel state.
- Only the current holder may use direct playback controls.
- Renders the `Release player and archive room` button when host +
  holder.
- Release confirmation copy explicitly mentions archival and is not a
  transfer.
- Navigation and unmount do not release.
- Expired-lease 410 toast copy does NOT suggest reclaiming.

## Verification

```text
cd frontend && npm run test:unit -- --run   # 408/408 pass (post-corrective)
cd frontend && npm run build                 # clean (built in ~240ms)
git diff --check                             # clean
```

Manual walk-through (out-of-band, recorded for the Product Owner):

1. Create a room → Player device shows "No active player."
2. Claim → status shows "Player held by you."
3. Observe periodic heartbeat requests on the network tab.
4. Open as another member → holder is visible; playback controls
   disabled.
5. Hide and restore the host tab → immediate heartbeat.
6. Brief network outage → "Heartbeat temporarily interrupted —
   retrying" → recovery.
7. Back to Rooms → no release request.
8. Explicit release → immediate "Room archived" surface.
9. Leave a claimed room without release → backend archives after
   lease + grace.
