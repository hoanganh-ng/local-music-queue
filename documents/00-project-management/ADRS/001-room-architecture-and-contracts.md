# ADR 001 — Room Architecture and Contracts

- Status: Accepted (Sprint 006 / R00 deliverable; awaiting Architect review and Product Owner approval)
- Date: 2026-06-24
- Scope: Defines the room domain, room-scoped REST and WebSocket contracts, persistence and migration direction, PostgreSQL timing, frontend flow, authorization caveats, and the transition strategy for the existing global API. This ADR is a contract plan; it does not authorize runtime implementation in this sprint.

## 1. Context

The application currently behaves as a single global playback context:

- One singleton `entity.Queue` JSON blob is persisted in `queue_state` (single row, `id = 1`) in SQLite ([`internal/infrastructure/persistence/sqlite_repository.go:41`](../../internal/infrastructure/persistence/sqlite_repository.go#L41)).
- REST, voting, and auto-queue endpoints are global ([`cmd/server/main.go:168`](../../cmd/server/main.go#L168)).
- The WebSocket hub is a single shared broadcast channel serving one global event stream ([`internal/delivery/ws/hub.go:62`](../../internal/delivery/ws/hub.go#L62)).
- The hub accepts any origin (`CheckOrigin: return true`) and uses an unvalidated `user_id` query parameter for connection identification ([`internal/delivery/ws/hub.go:16`](../../internal/delivery/ws/hub.go#L16)).
- Vote sessions live in process memory, scoped by a session ID derived from the current song ID (`"skip:<videoID>"` / `"prioritize:<videoID>"`) ([`internal/usecase/vote/interactor.go:63`](../../internal/usecase/vote/interactor.go#L63)).
- Auto-queue holds a single process-wide in-flight `triggering` flag and a single global config row (`auto_queue_config`) plus a single process-wide `play_history` ([`internal/usecase/autoqueue/interactor.go:65`](../../internal/usecase/autoqueue/interactor.go#L65), [`sqlite_repository.go:88`](../../internal/infrastructure/persistence/sqlite_repository.go#L88)).
- The frontend exposes one global `globalStore` with one `queueState`, one WebSocket connection, and one user session ([`frontend/src/store/index.js:25`](../../frontend/src/store/index.js#L25)).
- The host is currently whichever account has `role === 'host'` and the device that renders the YouTube iframe ([`frontend/src/components/dashboard/NowPlaying.vue:18`](../../frontend/src/components/dashboard/NowPlaying.vue#L18)).

The Product Owner has approved the move to explicit rooms. The decisions captured in [`ROOM_EPIC_SPRINT_SEQUENCE.md`](../ROOM_EPIC_SPRINT_SEQUENCE.md) and the closed Sprint 005 baseline ([`PROJECT_STATE.md`](../PROJECT_STATE.md)) are the inputs this ADR freezes into contracts. This ADR is intentionally documentation-only: no runtime change is authorized by it.

## 2. Current global model (reconstructed)

| Layer | Current behavior |
| --- | --- |
| Domain | `entity.Queue` is the only playback aggregate. `entity.User` has a process-wide `Role` (`host`/`admin`/`guest`) assigned by email allowlists at login. |
| Persistence | Single-row JSON storage in `queue_state`. Global `activities`, `users`, `user_sessions`, `priority_transactions`, `auto_queue_config`, `play_history`. |
| REST | 19 endpoints under `/api/...`. All state-changing endpoints trust client-supplied `requested_by` / `added_by` / `added_by_id` / `user_id` / `user_role`. Only `POST /api/queue/remove` resolves a server-issued session token. |
| WebSocket | Single hub, one client set, one global sequence counter. Initial sync sends `full_sync` plus active vote sessions as `vote_updated { initial_sync: true }`. |
| Voting | In-memory sessions keyed by `skip:<songID>` / `prioritize:<songID>`. Threshold = `max(2, connectedClients / 2)` captured at session creation. |
| Auto-queue | Single in-flight flag, one config row, one play history table (capped at 50 by trigger), `yt-dlp` related fetch, single-flight guard via `triggering` boolean. |
| Priority | Account-scoped daily token award via `user_sessions` and `priority_transactions`. |
| Frontend | One `globalStore`, one `WebSocketClient` reconnect loop (3 s), one `api.*` namespace, one `DashboardView`. The host's device renders the YouTube iframe because only it sets `showPlayer = true`. |

## 3. Decision summary

1. The application moves from one implicit global room to **explicit rooms**. The first implementation is single-process; cross-process coordination is explicitly deferred.
2. There is **no permanent `main` room** and **no hidden default source of truth** in the final model. The existing global state is migrated into exactly one real room whose name is supplied by the Product Owner at migration time.
3. A room has exactly three lifecycle states: **create → active → archived**. There is no implicit "room 0".
4. Membership roles are **host**, **admin**, **guest/member** (one host per active room). Promotion rules are explicit; client-supplied role strings are never trusted.
5. The **host/player is the device that holds the active player lease**. Admin can control an active room but does not keep it alive if the player lease expires.
6. **Room context is mandatory** on every queue, playback, voting, auto-queue, invite, member, and priority REST call after the migration sprint, and on the WebSocket connection. The old global routes transition through a documented compatibility shim to a final `410 Gone`.
7. **PostgreSQL is adopted early**, before room runtime, unless Sprint R01 (PostgreSQL Migration Design) surfaces a blocking reason.
8. **Authorization is acknowledged as incomplete**; rooms are a feature boundary, not a security/privacy boundary, until Sprint R13 closes the gap.
9. Users and daily priority balances remain **account-scoped** for the first room implementation; vote sessions remain **in-memory** for the first room implementation. Both are documented as deferred, not silent.
10. The frontend gains a non-room `Welcome` landing screen, an explicit room-context store slice, and a redirect path from archived rooms back to `Welcome`.

## 4. Room lifecycle

A room exists in exactly one of three states:

- **create** — A room is created through the user flow (no admin-only creation). The creator becomes host. The room transitions to `active` immediately after creation.
- **active** — The room accepts members, queue/playback mutations, invites, votes, and auto-queue behavior. There is exactly one host and zero or one active player lease at any moment.
- **archived** — The room is closed. Every active member is kicked. The frontend redirects to `Welcome`. All queue, playback, invite, vote, and auto-queue mutations return `409 Conflict` (or `410 Gone` on routes that are also retired) for an archived room.

State transitions:

```
[none] --create--> active
active  --host/player leaves AND player lease expires--> archived
active  --explicit archive (out of scope for first impl, reserved)--> archived
archived is terminal; clients must create or join another room.
```

The first implementation **does not implement a "reactive" archive** when a non-host member disconnects. Only loss of the host's player lease archives the room.

## 5. Roles and membership

| Concern | Decision |
| --- | --- |
| Roles | `host`, `admin`, `guest` (a.k.a. member). The legacy process-wide `Role` on `entity.User` remains for non-room identity metadata (e.g. email allowlist logging) but is **not** the source of truth for room permissions after R04. |
| Per-room membership | A room-scoped `RoomMember` record holds `(room_id, user_id, role)`. There is exactly one `(role = 'host')` row per active room. |
| Creator default | Room creator is inserted as `host`. |
| Invitee default | Anyone joining by valid invite is inserted as `guest`. |
| Promotion | Host promotes a `guest` to `admin`. Host can also demote an `admin` back to `guest`. A host can transfer host to another `admin` or `guest`; the transfer writes the new host and demotes the old host to `admin`. |
| Self-promotion | **Forbidden**. Promotion/demotion endpoints accept only an authenticated actor whose room role is `host`. The backend never reads a role string from the request body or query. |
| Admin capabilities | Admin can call all queue/playback REST endpoints that do not require the active player lease (e.g. add, prioritize-with-tokens, vote). Admin cannot perform operations reserved to the host (e.g. claiming or releasing the player lease, transferring host). |
| Host capabilities | Host has admin capabilities plus player-lease control and host transfer. |
| Member listing | Members and their room roles are visible to other members through the room-scoped REST API. |
| Identity | Room permissions are resolved from the authenticated session's `user_id` plus the room-scoped `RoomMember` row. `entity.User.Role` is **not** consulted for room permission checks. |

## 6. Host/player lease model

The host is a person. The player is a device. They are **not** the same thing.

| Concept | Decision |
| --- | --- |
| Player lease | Exactly one active `PlayerLease(room_id, lease_id, claimed_by_user_id, claimed_at, last_heartbeat_at, expires_at)` per active room. |
| Claim | The active host's browser claims the lease through `POST /api/rooms/{roomId}/player/claim`. The lease is created with a default 60-second `expires_at` and a 30-second `last_heartbeat_at`. |
| Heartbeat | `POST /api/rooms/{roomId}/player/heartbeat` extends `expires_at` by another 60 seconds. The host's browser issues a heartbeat every ~20 seconds while its tab is visible. |
| Grace | A 30-second grace after `expires_at` permits a missed heartbeat (network blip, brief tab-switch). During grace, the lease is not yet considered expired; new heartbeats revive it. |
| Release | `POST /api/rooms/{roomId}/player/release` deletes the lease immediately. The host's browser calls this on `beforeunload`, `pagehide`, and explicit "leave room". |
| Expiry | After grace elapses without a successful heartbeat, the lease is **expired**. The room is archived. See §8. |
| Duplicate claims | If a lease already exists and is still within grace, a second `claim` returns `409 Conflict` and identifies the current holder. After grace but before archive completes, the claim is rejected with `410 Gone`. |
| Admin actions | Admin can control an active room, but **cannot** claim the player lease and cannot prevent archive if the lease expires. Admin calling `/player/release` while the lease belongs to the host is rejected with `403 Forbidden`. |
| Storage | The lease is persisted (SQLite or PostgreSQL depending on storage phase); it survives process restart in the persistence layer it lands in. The grace timer runs in-process against `expires_at`. |

## 7. Invite model

| Concern | Decision |
| --- | --- |
| Invite token | A cryptographic random token (≥ 128 bits of entropy, opaque to clients) generated by the backend. Tokens are not derivable from room IDs. |
| Storage | `room_invites(room_id, token_hash, created_by_user_id, created_at, expires_at, revoked_at, max_uses, use_count)`. The token is **stored hashed**; only the creator sees the plaintext once. |
| URL shape | `https://<host>/invite/<token>` is the share link. The frontend parses the token, looks up the active room, and routes to the join view. |
| Validation | The backend compares a constant-time hash of the presented token against the stored hash. Validation rejects expired, revoked, exhausted, or unknown tokens with `404 Not Found` (a generic message; we do not leak existence). |
| Expiry | Default 7 days. The Product Owner can override at creation time within the validated range (max 30 days). |
| Use limit | Default `max_uses = 0` (unlimited while not revoked). A non-zero `max_uses` increments `use_count` per successful join and rejects further joins with `410 Gone` when the limit is reached. |
| Revocation | Host (or admin) revokes through `DELETE /api/rooms/{roomId}/invites/{inviteId}`. Revoked tokens return `404 Not Found` thereafter. |
| Re-invite | After a member leaves an active room, they rejoin by invite. There is no implicit "still in the room" state. |

## 8. Archive, kick, and `Welcome` redirect behavior

1. When a room is archived, the backend broadcasts a room-scoped `room_archived` WebSocket event (see §10) to every connected client in that room.
2. Each client's WebSocket handler receives the event, clears room-scoped state (queue, votes, auto-queue config snapshot, player lease view), and the frontend router pushes the `Welcome` view.
3. The WebSocket connection is **closed** by the client; it must not be reused for a different room.
4. Subsequent REST calls into the archived room return `409 Conflict` for mutation endpoints and `410 Gone` for routes that are also retired as part of the global-cleanup sprint (R14).
5. Activities, auto-queue config, and play history for the archived room remain in persistence (read-only after archive for audit). They are not deleted on archive in the first implementation.
6. The migrated room follows the same archive rules as any other room once migration completes (see §11).

## 9. REST contract direction

### Final route shape (room-scoped)

All state-changing and read endpoints (other than `Welcome`, auth, and global health) require an explicit room slug/identifier in the path. Examples of the final shape:

```
GET    /api/rooms
POST   /api/rooms
GET    /api/rooms/{roomId}
GET    /api/rooms/{roomId}/members
POST   /api/rooms/{roomId}/members/{userId}/promote
POST   /api/rooms/{roomId}/members/{userId}/demote
POST   /api/rooms/{roomId}/invites
GET    /api/rooms/{roomId}/invites
DELETE /api/rooms/{roomId}/invites/{inviteId}
POST   /api/invites/{token}/redeem
POST   /api/rooms/{roomId}/player/claim
POST   /api/rooms/{roomId}/player/heartbeat
POST   /api/rooms/{roomId}/player/release

GET    /api/rooms/{roomId}/queue
POST   /api/rooms/{roomId}/queue/add
POST   /api/rooms/{roomId}/queue/skip
POST   /api/rooms/{roomId}/queue/status
POST   /api/rooms/{roomId}/queue/sync
POST   /api/rooms/{roomId}/queue/ended
POST   /api/rooms/{roomId}/queue/prev
POST   /api/rooms/{roomId}/queue/remove
POST   /api/rooms/{roomId}/queue/clear
POST   /api/rooms/{roomId}/queue/volume
POST   /api/rooms/{roomId}/queue/prioritize
GET    /api/rooms/{roomId}/user/priority-balance
GET    /api/rooms/{roomId}/youtube/search
POST   /api/rooms/{roomId}/vote/skip
POST   /api/rooms/{roomId}/vote/prioritize
POST   /api/rooms/{roomId}/autoqueue/toggle
GET    /api/rooms/{roomId}/autoqueue/status
```

Unchanged global endpoints (no room context):

```
POST   /api/auth/google
GET    /api/health (planned; out of scope here)
```

### Transition strategy

The transition is staged across multiple sprints:

| Phase | Behavior of old `/api/queue/...`, `/api/vote/...`, `/api/autoqueue/...` |
| --- | --- |
| Pre-migration (current state) | Global routes work; behavior unchanged. |
| Room-API introduction (Sprint R07) | New room-scoped routes work. Old global routes continue to work as a **compatibility shim** that resolves the single migrated room by default for any caller without room context. The shim is logged at startup. |
| R14 cleanup | Old global routes return `410 Gone` with a documented `Link` header to the room-scoped equivalent. The compatibility shim is removed. |

The compatibility shim is a deliberate, time-boxed bridge — it is not a permanent dual API. The exact window is decided by Sprint R14.

### Room slug/ID validation

- `roomId` is a URL-safe slug (regex `[a-z0-9][a-z0-9-]{1,38}[a-z0-9]`). Reserved slugs (e.g. `api`, `admin`, `static`, `ws`) are rejected at create time.
- Duplicate slugs are rejected with `409 Conflict` at create time.
- Slug changes after creation are out of scope for the first implementation.

### Error model

The existing `http.Error(...)` text body continues for now. The first room implementation keeps the existing status-code surface and only adds new error codes:

- `409 Conflict` — archived room mutation, duplicate invite, duplicate player claim, duplicate slug.
- `410 Gone` — retired global route (R14), exhausted invite, post-grace player claim.
- `403 Forbidden` — non-host attempting promotion or player-lease control; admin attempting host-only operation.
- `404 Not Found` — unknown room, unknown invite token (generic; does not leak existence).

## 10. WebSocket contract direction

### Connection shape

The WebSocket URL gains an explicit room context. The room can be supplied either via path or via authenticated identity lookup; the ADR **locks the path approach** because it is unambiguous, supports anonymous read-only viewers in the future, and removes the dependence on trusting `user_id` as a query parameter.

```
wss://<host>/ws/rooms/{roomId}
```

The `user_id` query parameter is **removed** in the room-scoped WS. Identity is taken from the authenticated session cookie / `Authorization: Bearer <session_token>` header during the upgrade. Anonymous (no session) upgrades return `401 Unauthorized` during the HTTP upgrade phase.

### Initial sync and per-room deltas

- On a successful upgrade to `/ws/rooms/{roomId}`, the hub sends:
  1. One `full_sync` event carrying the room-scoped snapshot.
  2. Each active vote session in the room as a `vote_updated` event with `initial_sync: true`.
- All subsequent delta events are broadcast **only to clients in the same room**.
- `ConnectedCount()` becomes per-room. Vote thresholds are computed from per-room connected count.

### Sequence numbers

`seq_num` is **per room**, not global. Justification:

- Sequence numbers are a per-client ordering concern. A client connected to room A only ever sees A's deltas.
- Per-room counters avoid cross-room coupling of two unrelated rooms' event rates.
- The hub runs a separate monotonic counter per room.

A `request_full_sync` client message remains supported and is answered with a room-scoped `full_sync`.

### Archived/kicked event

```json
{
  "type": "room_archived",
  "data": {
    "room_id": "string",
    "reason": "player_lease_expired" | "host_left" | "explicit",
    "archived_at": "RFC3339 timestamp"
  },
  "seq_num": 0,
  "timestamp": "RFC3339 timestamp"
}
```

The hub then closes the WebSocket with the standard close code `1000` after a brief drain. The frontend treats this event as the redirect trigger to `Welcome` (see §13).

### Reconnect behavior

- A client that reconnects after a `room_archived` event must create or join a room; it cannot resume a closed room's WebSocket.
- A client that lost the socket due to a network blip reconnects with the same room URL. If the room has been archived in the interim, the upgrade succeeds, the client receives `full_sync` (which will reflect the archived state) followed by `room_archived`, and the client redirects to `Welcome`. This preserves the invariant that clients always receive an authoritative post-disconnect snapshot.

### Backward compatibility for the global `/ws`

During the room API introduction (R08), `/ws` continues to accept upgrades and serves a synthesized "default room" view backed by the migrated room. The `/ws` route is removed in R14 once the frontend is fully room-aware.

## 11. Persistence and migration direction

### Room-scoped persistence ownership

| Concern | Storage | Notes |
| --- | --- | --- |
| `rooms`, `room_members`, `room_invites` | PostgreSQL (after R02) | Relational, FK-enforced. |
| Room-scoped queue state | PostgreSQL | One row per room; the existing JSON blob model is preserved per room for the first room implementation to limit blast radius. A future sprint may move to per-song rows. |
| Room-scoped activities | PostgreSQL | One `activities` row per event with `room_id`. The existing single global `activities` table is migrated. |
| Room-scoped auto-queue config | PostgreSQL | One `auto_queue_config` row per room, scoped by `room_id`. |
| Room-scoped play history | PostgreSQL | One `play_history` row per play event with `room_id`. The existing `play_history` cap trigger (50 rows) is preserved per room. |
| Users, user sessions, priority transactions, user-scoped daily priority balance | PostgreSQL | **Account-scoped** (not room-scoped) for the first room implementation. A user keeps the same priority balance across rooms. This is a deliberate decision; see Deferred Work. |
| Vote sessions | In-memory (per room) | The map in `vote.Interactor` becomes `map[roomID]map[sessionID]*VoteSession`. Lost on restart in the first implementation. |

### Migration direction

- R03 migrates the existing SQLite data into PostgreSQL while preserving the global shape. The single global `queue_state` row becomes the `queue_state` of the **migrated room**.
- R06 converts the migrated room's data into room-scoped rows: the migrated room gets `room_id = <PO-supplied slug>`, all activities get that `room_id`, the global `auto_queue_config` becomes the migrated room's row, the global `play_history` is rewritten with that `room_id`.
- R06 does **not** create a permanent `main` room and does **not** leave the old global state as an alternate source of truth. The global rows are deleted at the end of R06; only the migrated room remains.
- The Product Owner supplies the migrated room name at migration time. The name is the **slug** (URL-safe) AND the **display name**. The two are equal in the first implementation.

### Migrated room with no active host/player

If R06 completes while no host/player lease exists for the migrated room, the room starts in `active` state but **without** an active player lease. The first joiner can claim the lease if they join as host, OR an existing admin can promote a member and that promoted member can claim the lease. There is no automatic bootstrap to a "phantom host".

### Account-scoped vs room-scoped priority

Decision: **Account-scoped** for the first room implementation. Reasoning: the daily token award is a per-user identity concern, not a per-room concern. Room-scoped priority is deferred to a later sprint that revisits economy and fairness.

### SQLite-to-PostgreSQL move (if PostgreSQL is adopted early)

- R02 introduces PostgreSQL alongside SQLite. R02 does not move room behavior; it preserves the current global shape on PostgreSQL.
- R03 migrates data from SQLite to PostgreSQL with a documented, idempotent script that preserves all existing rows.
- R03's acceptance criteria include a documented rollback window and an offline data-integrity verification step.

## 12. PostgreSQL timing

- R01 (PostgreSQL Migration Design) is scheduled before any room persistence work.
- Unless R01 returns a blocking reason, the room epic uses PostgreSQL from R02 onward.
- The first room implementation does not assume SQLite-only features (e.g. triggers that cap per-table rows). The `play_history` 50-row cap is preserved per room using a `DELETE ... WHERE room_id = ? AND id NOT IN (...)` path or a server-side cap, not a per-table trigger.
- Local development and CI use a deterministic PostgreSQL container; this is an R01 deliverable.

## 13. Frontend flow direction

| View | Responsibility |
| --- | --- |
| `Welcome` | Landing view for users without an active room. Lists rooms the user can join (if the user is authenticated) and exposes a "Create room" action. Acts as the redirect target after `room_archived`. |
| `CreateRoom` | Form for room name (the slug) and optional display name. POSTs to `/api/rooms`. On success, routes to the new room's dashboard. |
| `Invite` (modal) | Generated after room creation, shows the invite URL, copy-to-clipboard, and revoke action for the host. |
| `JoinRoom` (`/invite/:token`) | Validates the invite token, calls `/api/invites/{token}/redeem`, then routes to the room dashboard. |
| `DashboardView` (room-scoped) | The existing dashboard, scoped to `activeRoom`. Reads `globalStore.activeRoom` for room context; resets queue, vote, and auto-queue UI state when entering or leaving rooms. |
| `Archived` notice (transient) | Optional sub-state on `DashboardView` that confirms redirect and offers a "Back to Welcome" button. |

Active room context storage:

- `globalStore.activeRoom = { id, slug, name, role }` is the canonical in-memory active-room context.
- The room ID is persisted in `localStorage` under `lmq_active_room` so a page reload can attempt to re-join the same room. The frontend calls a re-resolve endpoint on reload; if the room no longer exists or is archived, the frontend clears `activeRoom` and routes to `Welcome`.
- The room context is **cleared** on `room_archived`, on explicit logout, and on user-driven "Leave room".

Player-device rendering expectations:

- The YouTube iframe renders only when **both** (a) the user holds an active player lease for the room, and (b) the room is `active`.
- The lease holder is decided by the backend, not by the frontend. The frontend reflects the lease holder through a room-scoped REST endpoint and through `player_lease_updated` WS events (see §10).
- When the current user is not the lease holder, `NowPlaying.vue` shows artwork + transport disabled; remote play/pause/skip/volume commands still work but are subject to backend authorization.

## 14. Authorization and security caveats

- The first room implementation does **not** make rooms a strong security or privacy boundary. The same risks flagged in [`PROJECT_STATE.md`](../PROJECT_STATE.md) (client-supplied identity, `user_id` query parameter on WebSocket, no per-request backend authorization on most endpoints) persist until R13 closes them.
- The room contract **reduces the surface** of those risks by: (1) requiring an authenticated session on the WebSocket upgrade, (2) resolving identity from the session, and (3) routing all sensitive operations through use cases that consult room-scoped membership rows.
- The first room implementation explicitly does **not** add new cryptographic guarantees beyond what already exists (HTTPS, Google OAuth token verification, session tokens for `remove`). It also does not introduce cross-process or cross-instance trust.
- All Room API consumers must be authenticated for state-changing endpoints; the ADR documents this as a hard requirement for R13.
- Until R13 lands, room membership checks on the backend are advisory, not authoritative. This caveat is repeated in `ROOM_EPIC_SPRINT_SEQUENCE.md` and is non-negotiable for the first room implementation.

## 15. Compatibility and old endpoint transition

| Sprint | Old global routes | New room routes | WebSocket |
| --- | --- | --- | --- |
| Pre-R07 | Work as today | Do not exist | `/ws` global |
| R07 | Compatibility shim → resolved to migrated room | Live | `/ws` global + `/ws/rooms/{roomId}` (both) |
| R08 | Compatibility shim | Live | `/ws/rooms/{roomId}` primary; `/ws` deprecated but live |
| R14 | `410 Gone` with `Link` header | Live (only) | `/ws` removed |

`Link: </api/rooms/{roomId}/queue/add>; rel="successor-version"` is the documented response header on retired global routes during the R14 transition.

## 16. Deferred work (explicit)

- Cross-process / multi-instance room coordination. The first implementation is single-process.
- Persisted vote sessions. In-memory only in the first room implementation.
- Room-scoped priority balances (per-room token economy). Account-scoped for now.
- Slug rename and room archival recovery / un-archive.
- Per-song row storage in place of the JSON blob.
- Strong backend authorization on every endpoint (R13).
- Anonymous (read-only) room views.
- Cross-room moderation tools (global admin).

## 17. Risks

| Risk | Mitigation |
| --- | --- |
| Hidden default-room behavior sneaking back into the design (e.g. "global queue as fallback") | R07 shim is explicit and time-boxed to R14; no implicit fallback paths in new code. |
| Confusing human host with the player device | Player lease is a separate, documented concept with its own endpoints. |
| Immediate disconnect archiving rooms without grace | Lease has a 30-second grace period after `expires_at` before archive. |
| Invitees self-selecting privileged roles through client payloads | Promotion endpoints read role from the authenticated session + room membership, never from request bodies. |
| Treating rooms as a security boundary while authorization is incomplete | This ADR explicitly does not; R13 closes the gap. |
| PostgreSQL scope expanding into implementation before contracts are approved | R01 is a design sprint; R02 is the first implementation sprint and is bounded by R00's contracts. |
| Old global routes remaining as undocumented permanent compatibility behavior | R14 cleanup is part of the epic; routes move to `410 Gone`. |
| WebSocket / vote / auto-queue room isolation being under-specified for later implementation | §10, §11, and the Sprint R07/R08/R09/R10 checklists capture concrete cross-room isolation expectations. |

## 18. Consequences for later sprints

- R01 (PostgreSQL Migration Design) must respect the JSON-blob-per-room shape called out in §11; do not propose a schema that breaks the per-room blob.
- R02 (PostgreSQL Foundation) must add PostgreSQL without altering room behavior; the existing global queue remains a single global queue.
- R03 (SQLite-to-PostgreSQL Migration) must preserve all existing rows and produce a verifiable offline integrity check.
- R04 (Room Domain) must create `Room`, `RoomMember`, `RoomInvite`, room archive state, and the invite / promotion / demotion use cases described in §5, §7.
- R05 (Player Lease) must implement the lease model described in §6, including grace, duplicate-claim rejection, and admin-cannot-prevent-archive.
- R06 (Room-Scoped Persistence) must convert queue state, activities, auto-queue config, and play history to room-scoped storage and migrate existing global data into the Product-Owner-named room.
- R07 (Room-Scoped REST) must implement the route shape in §9 and the compatibility shim for old routes. It must not start until R06 closes.
- R08 (Room-Scoped WebSocket) must implement `/ws/rooms/{roomId}`, per-room sequence numbers, the `room_archived` event, and the per-room `full_sync` behavior.
- R09 (Room-Scoped Voting) must scope vote sessions, thresholds, expiry broadcasts, and queue mutations by room.
- R10 (Room-Scoped Auto-Queue) must scope config, history, single-flight guard, stale-candidate checks, and broadcasts by room.
- R11 (Welcome / Create / Invite / Join) must implement the views in §13 and the redirect to `Welcome` on `room_archived`.
- R12 (Room-Aware Dashboard) must consume `globalStore.activeRoom`, reset queue / vote / auto-queue UI state on room transitions, and render the YouTube iframe only for the active player lease.
- R13 (Room Authorization Hardening) must enforce room membership and role permissions in the backend and remove trust of client-supplied identity strings.
- R14 (Global Contract Cleanup) must retire the old global routes to `410 Gone` and remove `/ws`.

## Out of scope for this ADR

- Runtime code, runtime configuration, Docker, and database migration scripts.
- Test rewrites unrelated to documenting current contracts.
- New features beyond the contract decisions listed above.

## Verification

This ADR is a contract plan. Acceptance for Sprint R00 requires:

- This document exists at the path above.
- The accompanying Sprint 006/R00 sprint document records the ADR location and verification results.
- `git diff --check` is clean.
- `git status --short --branch` shows only this ADR and the sprint document changed; no runtime, configuration, or deployment files were modified.
