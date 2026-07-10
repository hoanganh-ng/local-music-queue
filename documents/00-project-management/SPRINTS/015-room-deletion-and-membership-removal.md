# R10a — Room deletion and membership removal (contract design)

**Status:** **Accepted (2026-07-02)** — R10a is the contract design. R10b runtime implementation is implemented on `dev` (2026-07-02) and **accepted by the Product Owner on 2026-07-10**.

**Sprint name:** Room deletion and membership removal — R10a contract design

## Goal

Shape the **first approved contract slice** for room deletion and membership removal. R10a is a **documentation-only** planning sprint. It settles every decision the future R10b runtime implementation will need to conform to, but it does **not** ship any Go runtime code, Vue runtime code, migrations, routes, WebSocket constants, tests, Docker, or config changes. The runtime implementation that conforms to this contract is a future R10b slice and remains explicitly out of scope.

R10a is the first half of the legacy R10 stub. The legacy stub stays as the high-level goal reference; R10a replaces it with a narrow, reviewable contract.

## Scope and non-goals

### In scope (decisions captured in this document)

1. Define public "delete room" as **host-requested soft archive** for the first runtime slice. Hard delete / hard purge / data cleanup are **explicitly out of scope** for R10b.
2. Treat the existing room `host` role as the **owner equivalent**. Do not introduce a new owner role, owner flag, or owner column.
3. Define the future endpoint contracts:
   - `DELETE /api/rooms/{slug}`
   - `DELETE /api/rooms/{slug}/members/{userId}`
4. Define confirmation, request/response bodies, HTTP status-code mapping, and archived-room behavior for both endpoints.
5. Define member-removal behavior: host-only; cannot remove self; cannot remove the host; can remove admin/guest; the removed user loses room membership immediately.
6. Settle session-token revocation stance: **do not** claim room-scoped session-token revocation exists. Current sessions are global in-memory tokens (R05). The future runtime MUST enforce removal through **membership checks** and **per-room WebSocket disconnect/notification**.
7. Define player-lease effects: removed active lease holder has their lease ended/released; room archive ends active lease idempotently.
8. Define room-vote effects: removed user MUST NOT count in future thresholds; active sessions for the current song are **left to natural expiry** (no forced resolve, no forced cancel).
9. Define per-room WebSocket event contracts for room archive/delete and member removal, including whether removed clients receive a **targeted event before close**.
10. Define an **optional** audit table shape for the future runtime slice.
11. Define a future test matrix covering usecase, repository, HTTP handlers, WebSocket hub, and race-sensitive lease/vote/member paths.

### Out of scope (deferred, future work)

- **Hard delete** of rooms, queue items, leases, invites, history, or any other persisted state. Soft archive is the only operation this contract introduces.
- **Automated cleanup / cron / vacuum** of archived rooms. The soft-archive state is durable; a separate future sprint may introduce retention windows.
- **Front-end UI** for delete / remove controls. UI wiring is a future slice.
- **Owner transfer** beyond what R04 already provides. R10a does not extend R04's promote/demote; deletion/removal is the new surface.
- **Session-token revocation** (room-scoped or otherwise). The future runtime enforces removal through membership checks + per-room WS disconnect/notification only.
- **Cross-process safety** for any new state introduced by R10b. Single-instance only; no Redis / pubsub / shared-bus claim.
- **R10b runtime implementation**. This document only shapes the contract; a separate future sprint implements it.
- **Modifications** to global queue, global auto-queue, YouTube fetcher internals, Docker/Nginx/HTTPS, public read-only API/CORS, chat, search/discovery, frontend implementation, or broad auth/session redesign. R10a does not touch any of these.

### Runtime surface explicitly NOT changed by R10a

R10a is a docs-only contract design. The following are unchanged by this sprint and remain unchanged by the future R10b implementation except where this contract says otherwise:

- No Go runtime files under `internal/` are modified.
- No Vue runtime files under `frontend/src/` are modified.
- No PostgreSQL migrations are added (the audit table is **optional** and is described for the future runtime; no migration is part of R10a).
- No HTTP routes are added or modified.
- No WebSocket event-name constants are added or modified.
- No WebSocket event payloads are added or modified.
- No tests are added or modified.
- No Docker, Nginx, or HTTPS configuration is changed.
- No deployment / environment-variable configuration is changed.
- The 16-event global `/ws` inventory is unchanged.
- The R06 `room_archived` event and its payload are unchanged.
- The R07b/R07d/R09a/R09b/R09c/R09d/R09f per-room WebSocket events and payloads are unchanged.
- The R05 in-memory session-token model is unchanged.

## Definitions

For the purposes of this contract:

- **Host** — the room member with `role = 'host'`. There is exactly one host per room (enforced by `idx_room_members_one_host_per_room`, migration 0004).
- **Owner equivalent** — the host. R10a does not introduce a separate owner role or owner column.
- **Soft archive** — transitioning a room from `status = 'active'` to `status = 'archived'` via the existing `RoomRepository.ArchiveRoomIfActive` (R06). The room row remains; all associated rows remain; queries against active rooms simply exclude it.
- **Hard delete** — out of scope. Mentioned only to clarify it is not introduced.
- **Removed user** — a user whose `room_members` row is deleted as a consequence of a successful `DELETE /api/rooms/{slug}/members/{userId}` call.
- **Removed client** — a WebSocket connection owned by a removed user at the moment the membership row is deleted.
- **Session** — a server-issued, in-memory bearer token (R05). Sessions are global (not room-scoped). Sessions are lost on server restart. R10a does not change this.
- **Active vote session** — an in-memory `roomvote` session (R09b) for the current song that has not yet passed its 30-second expiry.

## Decisions

### Decision 1 — "Delete room" = host-requested soft archive

The public "delete room" operation for the first runtime slice is **soft archive only**. Concretely, the future R10b implementation invokes the existing `RoomRepository.ArchiveRoomIfActive` (R06) path. The room row, its members, leases, invites, queue state, history, and audit rows (if present) are preserved on disk.

- "Already archived" is **idempotent**. Re-calling `DELETE /api/rooms/{slug}` on an archived room returns `204` (or `200`) without mutation, without a broadcast, and without an audit-log row.
- Hard delete, hard purge, scheduled cleanup, and retention windows are out of scope. A future R10c+ sprint may introduce them.
- Front-end wording for the "Delete room" control should reflect "Archive" semantics, but UI is out of scope for R10a.

### Decision 2 — Host is the owner equivalent

No new role is introduced. The existing `host` role (R04, migration 0004) is the authorization gate for both new endpoints. The unique partial index `idx_room_members_one_host_per_room` continues to enforce exactly-one-host.

### Decision 3 — Future endpoint contracts

#### `DELETE /api/rooms/{slug}`

- **Authorization:** `roomAuth` + host role (the host member of the room). Reuses the existing `room.Interactor.requireHost` pattern.
- **Pre-condition:** the room MUST exist (else `404`) and MUST be `active` (else the call is idempotent — see Decision 4).
- **Path:** `DELETE /api/rooms/{slug}` — slug pattern is the existing `entity.SlugPattern`.
- **Idempotency:** yes. Calling on an archived room returns success without mutation. Calling on a missing room returns `404`.
- **Body:** none. The endpoint does NOT accept a request body in R10b.

#### `DELETE /api/rooms/{slug}/members/{userId}`

- **Authorization:** `roomAuth` + host role. Reuses the existing `room.Interactor.requireHost` pattern.
- **Path:** `DELETE /api/rooms/{slug}/members/{userId}` — `userId` is the integer member id.
- **Pre-condition:** the room MUST exist (else `404`) and MUST be `active` (else `409 Conflict` with `room archived`).
- **Body:** none. The endpoint does NOT accept a request body in R10b.
- **Target validity rules:**
  - If `userId == actorUserID` (host trying to remove self): `400 Bad Request` — host cannot remove self. The host must either archive the room (Decision 1) or transfer ownership through existing R04 mechanics first. R10a does not introduce new transfer semantics.
  - If the target is the host of the room: `400 Bad Request` — the host is the owner equivalent and cannot be removed while the room is active. The host may only be replaced via the existing R04 promote-then-archive flow (or by archiving the room outright).
  - If the target is `admin` or `guest`: removal proceeds.
  - If the target is not a member of the room: `404 Not Found`.
- **Idempotency:** no. If the target is already non-member (e.g. already removed by a prior call), the endpoint returns `404` for that userId.

### Decision 4 — Confirmation, request/response bodies, status-code mapping

#### Confirmation

R10a picks the **lightest confirmation surface** that fits the host-only / host-equivalent model:

- **Confirmation token in request body is NOT required in R10b.** R10b does not introduce a separate confirm step.
- Rationale: the operation is host-only, the actor identity is the bearer-token-resolved user, the soft-archive preserves data (recoverable via a future R10c+ admin surface), and a body confirmation would only protect against an accidental host client click — which is out of scope for this contract.
- A future R10c+ sprint may add a typed confirmation token (e.g. `{ "confirm_room_name": "<exact name>" }` echoed back) without changing the wire shape documented here. That is **explicitly deferred**.

#### Request bodies

Both endpoints take **no request body** in R10b. A non-empty body is not a 400; it is simply ignored (the contract does not declare it).

#### Response bodies

| Endpoint | Outcome | Status | Body |
| --- | --- | --- | --- |
| `DELETE /api/rooms/{slug}` | Room archived by this call | `204 No Content` | empty |
| `DELETE /api/rooms/{slug}` | Room already archived | `204 No Content` | empty (idempotent) |
| `DELETE /api/rooms/{slug}` | Slug invalid | `400 Bad Request` | `{ "error": "invalid room slug" }` |
| `DELETE /api/rooms/{slug}` | Caller not authenticated | `401 Unauthorized` | empty (existing middleware) |
| `DELETE /api/rooms/{slug}` | Caller not host | `403 Forbidden` | empty (existing `requireHost` mapping) |
| `DELETE /api/rooms/{slug}` | Room not found | `404 Not Found` | empty |
| `DELETE /api/rooms/{slug}/members/{userId}` | Member removed | `204 No Content` | empty |
| `DELETE /api/rooms/{slug}/members/{userId}` | Slug invalid | `400 Bad Request` | `{ "error": "invalid room slug" }` |
| `DELETE /api/rooms/{slug}/members/{userId}` | `userId` invalid (non-integer / `<= 0`) | `400 Bad Request` | `{ "error": "invalid user id" }` |
| `DELETE /api/rooms/{slug}/members/{userId}` | Actor is host removing self | `400 Bad Request` | `{ "error": "host cannot remove self" }` (use case returns explicit `ErrHostCannotRemoveSelf` sentinel) |
| `DELETE /api/rooms/{slug}/members/{userId}` | Target is the host | `400 Bad Request` | `{ "error": "cannot remove host" }` (use case returns explicit `ErrCannotRemoveHost` sentinel) |
| `DELETE /api/rooms/{slug}/members/{userId}` | Caller not authenticated | `401 Unauthorized` | empty |
| `DELETE /api/rooms/{slug}/members/{userId}` | Caller not host | `403 Forbidden` | empty |
| `DELETE /api/rooms/{slug}/members/{userId}` | Target not a member | `404 Not Found` | empty |
| `DELETE /api/rooms/{slug}/members/{userId}` | Room archived | `409 Conflict` | `{ "error": "room archived" }` |

#### Archived-room behavior

- `DELETE /api/rooms/{slug}` on an archived room is **idempotent** — returns `204` without mutation, without a broadcast, and without an audit row. It is NOT a `409`.
- `DELETE /api/rooms/{slug}/members/{userId}` on an archived room is **`409 Conflict`** with `{ "error": "room archived" }`. You cannot remove members from an archived room.

### Decision 5 — Member-removal behavior

- **Who can call:** host only.
- **Cannot remove self:** the host cannot remove itself. The host can archive the room (Decision 1) or transfer ownership via existing R04 promote-then-archive flow.
- **Cannot remove the host:** the host cannot be removed while the room is active.
- **Can remove admin or guest:** all non-host members are valid targets.
- **Effect on the removed user:**
  - The `room_members` row for `(room_id, userId)` is deleted in the same transaction as the membership-check guard (so a concurrent remove cannot double-remove; second call returns `404`).
  - The removed user **loses room membership immediately** — they are no longer in the member list, cannot call room-scoped endpoints as a member, cannot receive per-room broadcasts, and any future WebSocket connect to `/ws/rooms/{slug}` returns `403 forbidden`.
  - The removed user **does NOT lose their global session token** (R05). Their session is still valid for non-room endpoints. This is intentional — sessions are global.
  - The removed user's **per-room WebSocket connection is closed** with a close code and an optional targeted `room_member_removed` envelope (see Decision 9).

### Decision 6 — Session-token stance

- **R10a does not claim room-scoped session-token revocation exists.** Sessions are global in-memory tokens (R05). There is no per-room session table, no per-room session revocation, and no per-room session index.
- The future R10b runtime MUST enforce removal through:
  1. **Membership checks** — every room-scoped endpoint already checks `room_members` membership via `room.Interactor.requireHost*` or the per-room WS membership gate (`IsActiveMember`). A removed user fails every such check and is rejected with `403`.
  2. **Per-room WebSocket disconnect/notification** — every active per-room WS connection owned by the removed user is closed (Decision 9). A removed user cannot remain subscribed to `/ws/rooms/{slug}` after removal.
- The R05 global session token survives a removal. This is the documented behavior for R10a. A future R13 (auth hardening) sprint may introduce per-room session revocation; that is **explicitly deferred** and **NOT** part of R10b.

### Decision 7 — Player-lease effects

#### Effect of `DELETE /api/rooms/{slug}/members/{userId}` when the target holds the active lease

- If the target user is the **active lease holder** for the room, the lease MUST be ended (released) in the same transaction as the membership row delete.
- Ending the lease follows the existing `PlayerLeaseRepository.EndLease` path (R06). The active lease row's `ended_at` is set; no new lease is created.
- The room is **NOT archived** as a side effect of member removal. (Archiving a room is a separate host-driven action; see Decision 1.) The lease ends; the room remains active.
- The WebSocket broadcast for the membership removal is separate from the existing `room_archived` event. See Decision 9.

#### Effect of `DELETE /api/rooms/{slug}` (room archive) on an active lease

- Archiving a room ends any active lease **idempotently**, via the existing `PlayerLeaseInteractor.Release`-style flow OR by direct `EndLease` + `ArchiveRoomIfActive`. The R06 contract guarantees idempotence: ending a lease on an already-archived room is a no-op.
- The **wire value** `room_archived` and the `RoomArchivedData` payload (`room_id`, `reason`, `archived_at`) are unchanged and are reused by R10b. **R10b does NOT introduce a new archive event-name / wire value.**
- **Broadcast-path caveat:** the existing R06 archive broadcast is wired through the **global** `*ws.Hub.BroadcastRoomArchived` (R06) and rides the global `/ws` endpoint — it is NOT currently delivered to per-room clients on `/ws/rooms/{slug}`. R10b MUST add explicit per-room hub support so that room clients on `/ws/rooms/{slug}` receive `room_archived` with `reason: "host_archived"` on a successful host-driven archive. Concretely, R10b MUST add a `BroadcastRoomArchived(roomSlug, RoomArchivedEvent)` (or equivalent narrow seam) on `*ws.RoomWSHub` that emits the existing `room_archived` wire value with the existing payload shape. R10b MUST NOT bypass the per-room hub and MUST NOT require clients to subscribe to the global `/ws` endpoint to learn that their room was archived.
- The future R10b runtime should prefer calling the existing `RoomRepository.ArchiveRoomIfActive` path (which is idempotent) directly, rather than re-implementing the lease-end + archive dance from R06. The "cleanest" wiring is for the new use case to call `ArchiveRoomIfActive` after the optional `EndLease`, then invoke the new per-room hub broadcast, but the implementation choice is left to R10b.

#### Race-sensitive invariant

- A removed active lease holder must NOT be able to call `POST /api/rooms/{slug}/playback/{status,sync,skip,ended,prev}` (R09a/R09c/R09d) after the membership row is gone. The existing `RequireActiveLeaseHolder` (R09a) does a lease lookup by room id; once `EndLease` is committed in the membership-remove transaction, the lease row is gone, so `RequireActiveLeaseHolder` returns `ErrPlayerLeaseNotFound` (mapped to `404`).
- The membership row delete and the lease end MUST be in the **same database transaction** so that a concurrent playback command cannot observe the post-membership-delete state while the lease is still active.

### Decision 8 — Room-vote effects

#### Future vote-threshold computation

- A removed user MUST NOT count in future thresholds. The R09b threshold is captured at session creation via `(*RoomWSHub).UniqueConnectedUserIDs(slug)`. After removal:
  - The removed user's per-room WS connections are closed (Decision 9), so the next `UniqueConnectedUserIDs` call (e.g. for a future vote session on the next song) excludes them.
  - A removed user cannot reconnect to the per-room WS (the membership gate returns `403`).
  - A vote session created BEFORE the removal is NOT recomputed (R09b captures the threshold at session creation — that contract is unchanged).

#### Active vote sessions at the moment of removal

- Active vote sessions for the current song are **left to natural expiry**. The R10b runtime MUST NOT force-resolve an active session, MUST NOT cancel it, and MUST NOT mutate its threshold.
- A removed user CANNOT cast a new vote through a per-room WS frame (R09b is REST-via-bearer-token, not WS-frame-driven). A removed user CANNOT call `POST /api/rooms/{slug}/vote/skip` after removal because the membership check fails (`403`).
- A removed user's prior in-session ballot (already cast before the removal) is **not** retroactively cancelled. The vote session continues with the existing ballot and the existing threshold; it expires naturally after 30 seconds or when the threshold is reached.
- This is the simplest behavior that keeps R09b invariant-safe and avoids a new cross-use-case seam.

### Decision 9 — WebSocket event contracts

R10a does **not** introduce new event-name constants in `internal/delivery/ws/events.go`. The future R10b implementation WILL introduce **two new per-room event types / envelopes** on `/ws/rooms/{slug}`: `room_member_removed` (delivered to the removed client before close) and `room_members_changed` (delivered to remaining clients). The global `/ws` inventory remains unchanged. R10b does NOT add any new event types to the global `/ws` endpoint, and it does NOT modify the existing 16-event global inventory or any pre-existing per-room event (R07b/R07d/R09a/R09b/R09c/R09d/R09f). The two new per-room envelopes are the only additive WebSocket surface R10b introduces, and their wire-value names and payload shapes are fixed by this contract.

#### Room archive / delete (`DELETE /api/rooms/{slug}`)

- The R06 `room_archived` **wire value** (`room_archived`) and its payload (`RoomArchivedData` — `room_id`, `reason`, `archived_at`) are unchanged and reused by R10b.
- The existing R06 archive broadcast path is wired through the **global** `*ws.Hub.BroadcastRoomArchived` (R06) and rides the global `/ws` endpoint. R10b MUST add explicit per-room hub support so that room clients on `/ws/rooms/{slug}` receive `room_archived` exactly when the call actually transitioned the room from `active` to `archived` (not on the idempotent re-call of an already-archived room). The new per-room broadcast uses the same wire value and the same payload shape as R06. The global `/ws` archive broadcast continues to ride the R06 path unchanged.
- The reason string for a host-driven archive is a new sentinel: `"host_archived"`. The existing R06 sentinels (`"lease_expired"`, `"explicit"`) are unchanged.
- Connected per-room clients receive `room_archived` and may navigate away (existing R06 client-side behavior).

#### Member removal (`DELETE /api/rooms/{slug}/members/{userId}`)

- **Removed clients receive a targeted `room_member_removed` envelope before close.** This is the **one** new envelope R10a contemplates, and the contract documents its shape for the future runtime.
- Payload shape:

  ```json
  {
    "type": "room_member_removed",
    "data": {
      "room_slug": "<slug>",
      "user_id": <int>,
      "reason": "host_removed"
    },
    "seq_num": <int>,
    "timestamp": "<time.Time>"
  }
  ```

- The envelope is delivered **before** the server closes the connection. The connection is then closed with a standard WebSocket close frame.
- This envelope is per-room only (rides `/ws/rooms/{slug}`). It is NOT on the global `/ws` endpoint, and the 16-event global inventory is unchanged.
- The future R10b implementation MUST add the event-name constant **only when implementing the runtime**. R10a does not add the constant now. The constant name `EventRoomMemberRemoved` and the wire value `room_member_removed` are documented here for the future runtime; the symbol is NOT introduced in R10a.
- **Remaining members** receive a `room_members_changed` envelope describing the new membership list. Payload shape:

  ```json
  {
    "type": "room_members_changed",
    "data": {
      "room_slug": "<slug>",
      "members": [
        { "user_id": <int>, "role": "host" },
        { "user_id": <int>, "role": "admin" },
        { "user_id": <int>, "role": "guest" }
      ]
    },
    "seq_num": <int>,
    "timestamp": "<time.Time>"
  }
  ```

- The `room_members_changed` envelope is per-room only and is broadcast once per successful removal. It does NOT carry `joined_at` (the entity has it; the wire omits it to keep the payload small — clients can fetch via the existing `GET /api/rooms/{slug}/members` if needed).
- The `room_member_removed` and `room_members_changed` envelopes ride the existing per-room seq counter on `/ws/rooms/{slug}` and are allocated by the existing `nextSeq` hub-loop mechanism. The hub is unchanged in R10a.
- **Do NOT introduce a separate `room_deleted` event.** Room archive is announced via the existing R06 `room_archived` envelope. A separate delete event would be redundant given soft archive = delete from the user's perspective.

#### Sequencing

- Both new envelopes ride the per-room seq counter. The seq is allocated by the existing hub-loop `nextSeq` path. No new seq-allocation path is introduced.

#### Close-code contract

- When a per-room WS connection is closed because of a removal, the server sends a standard WebSocket close frame with code `1000` (normal closure) or `1008` (policy violation) — R10a documents both as acceptable, with the R10b runtime picking one and documenting it in the implementation summary. R10a does NOT pick one.

### Decision 10 — Optional audit table shape

The audit table is **optional** for R10b. The contract documents the recommended shape; the implementation may or may not introduce the table in the same R10b slice, or it may defer the table to R10c+. No R10b migration is required by R10a.

Recommended shape:

```sql
-- R10b (optional): audit log for room delete / member remove.
-- Append-only. No FK to users so deleted users (future) do not
-- orphan audit rows; the acting/target user IDs are captured at
-- the moment of the action.
CREATE TABLE IF NOT EXISTS room_audit_log (
    id          BIGSERIAL PRIMARY KEY,
    room_id     BIGINT NOT NULL,                   -- rooms.id, NOT FK
    action      TEXT   NOT NULL,                   -- 'room_archived' | 'member_removed'
    actor_user_id BIGINT NOT NULL,                 -- bearer-resolved host
    target_user_id BIGINT NULL,                    -- NULL for 'room_archived'
    reason      TEXT   NOT NULL,                   -- free-text, e.g. 'host_archived', 'host_removed'
    metadata    JSONB  NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_room_audit_log_room_created_at
    ON room_audit_log (room_id, created_at DESC);
```

- `action` enum is closed at `'room_archived' | 'member_removed'`. New actions require a new contract slice.
- `metadata` carries any future structured fields (e.g. `{ "prior_member_count": 4 }`) without a schema change.
- The table is append-only. No UPDATE / DELETE path is part of R10b.
- No HTTP endpoint for reading the audit log is part of R10b. A future R10c+ admin slice may add `GET /api/admin/rooms/{slug}/audit` or similar.

### Decision 11 — Future test matrix for R10b

R10a documents the test matrix the future R10b runtime must satisfy. R10a does NOT add tests.

#### Usecase tests (`internal/usecase/room`)

- `TestDeleteRoom_ArchivesActiveRoom_ReturnsEvent` — host calls archive; `ArchiveRoomIfActive` is invoked; the room transitions to `archived`; the use case returns the new `RoomArchivedEvent{Reason:"host_archived"}`.
- `TestDeleteRoom_AlreadyArchived_Idempotent` — re-call on archived room; no mutation; no event returned.
- `TestDeleteRoom_NonHost_Forbidden` — admin or guest calls; returns `ErrForbidden` / `ErrPlayerLeaseForbidden`-equivalent.
- `TestDeleteRoom_RoomNotFound` — non-existent slug; returns `ErrRoomNotFound`.
- `TestDeleteRoom_InvalidSlug` — slug pattern violation; returns `ErrInvalidSlug`.
- `TestRemoveMember_RemovesGuestMember` — host removes a guest; membership row deleted; no audit row (or one, if the optional table is in scope).
- `TestRemoveMember_RemovesAdminMember` — host removes an admin; membership row deleted.
- `TestRemoveMember_HostCannotRemoveSelf` — actor is host; actor is the target; returns the explicit sentinel `ErrHostCannotRemoveSelf` (NOT generic `ErrForbidden`). Handler maps this sentinel to `400 Bad Request` with `{ "error": "host cannot remove self" }`.
- `TestRemoveMember_CannotRemoveHost` — target is the host of the room; returns the explicit sentinel `ErrCannotRemoveHost` (NOT generic `ErrForbidden`). Handler maps this sentinel to `400 Bad Request` with `{ "error": "cannot remove host" }`.
- `TestRemoveMember_TargetNotMember` — target user has no `room_members` row; returns `ErrMemberNotFound`.
- `TestRemoveMember_RoomArchived_Returns409Sentinel` — room is archived; returns `ErrArchived`.
- `TestRemoveMember_EndsActiveLease_WhenTargetIsLeaseHolder` — target holds lease; the lease is ended in the same transaction as the membership delete.
- `TestRemoveMember_LeaseAlreadyEnded_NoOp` — target is not the lease holder; no lease mutation.

#### Repository tests (`internal/infrastructure/persistence/postgres`)

- `TestRemoveMember_DeletesMembershipRow` — direct SQL; row count drops by one.
- `TestRemoveMember_RemovedUserCannotBeReshapedAsMember` — after remove, `GetMember` returns `sql.ErrNoRows`.
- `TestArchiveRoom_AlreadyArchived_Idempotent` — second archive call returns `false` from `ArchiveRoomIfActive`.
- `TestArchiveRoom_ActiveLeaseEndedInSameTransaction` — wrap remove + lease end in a transaction; verify both commit or both roll back.
- `TestRemoveMember_AuditRowWrittenIfTableExists` — only if the optional audit table is migrated.

#### HTTP handler tests (`internal/delivery/http`)

- `TestDeleteRoom_204_OnSuccess` — host token; `204`; body empty.
- `TestDeleteRoom_204_OnAlreadyArchived` — host token; room already archived; `204`.
- `TestDeleteRoom_401_OnMissingToken`.
- `TestDeleteRoom_403_OnNonHost`.
- `TestDeleteRoom_404_OnMissingRoom`.
- `TestDeleteRoom_400_OnInvalidSlug`.
- `TestRemoveMember_204_OnSuccess`.
- `TestRemoveMember_400_OnHostRemovesSelf`.
- `TestRemoveMember_400_OnCannotRemoveHost`.
- `TestRemoveMember_404_OnTargetNotMember`.
- `TestRemoveMember_403_OnNonHost`.
- `TestRemoveMember_409_OnArchivedRoom`.
- `TestRemoveMember_401_OnMissingToken`.
- `TestRemoveMember_400_OnInvalidSlug`.
- `TestRemoveMember_400_OnInvalidUserId` — `userId` is non-integer or `<= 0`; returns `400` with `{ "error": "invalid user id" }`.

#### WebSocket hub tests (`internal/delivery/ws`)

- `TestRoomMemberRemoved_DeliveredToRemovedClient_BeforeClose` — connect a client, run a removal, observe the `room_member_removed` envelope, then observe the connection close.
- `TestRoomMembersChanged_DeliveredToRemainingClients_AfterRemoval` — two connected clients, remove one, the other receives `room_members_changed` with the new member list.
- `TestRoomArchived_DeliveredToAllPerRoomClients_OnHostArchive` — host archives the room; all clients connected to `/ws/rooms/{slug}` (per-room hub) receive `room_archived` with `reason: "host_archived"`; the global `/ws` archive broadcast continues to ride the R06 path unchanged.
- `TestRoomArchived_NotDelivered_OnIdempotentReCall` — re-call archive on archived room; no `room_archived` is broadcast on either the global `/ws` or the per-room `/ws/rooms/{slug}`.
- `TestRemovedClient_CannotReconnect_ToPerRoomWS` — removed user reconnects to `/ws/rooms/{slug}`; receives `403`.

#### Race-sensitive tests (run with `-race`)

- `TestRemoveMember_ConcurrentPlayback_DeniedAfterCommit` — two goroutines: one calls `DELETE /api/rooms/{slug}/members/{userId}` where `{userId}` is the active lease holder; the other calls `POST /api/rooms/{slug}/playback/skip`. The playback call must NOT succeed after the remove commits.
- `TestRemoveMember_ConcurrentReRemove_SecondReturns404` — two concurrent removes on the same target; exactly one returns `204`, the other returns `404`.
- `TestRoomArchive_ConcurrentSweeper_DoubleArchiveIdempotent` — host archive races the R06 sweeper; only one archive event is broadcast; the other side observes `archive_returned = false`.
- `TestRemoveMember_ActiveVoteSession_NotResolved` — open a vote session for the current song; remove a voter; verify the session continues, the threshold is unchanged, and the session expires naturally.
- `TestRemoveMember_ThresholdRecomputedOnNextSong` — remove a user; wait for vote session to expire; next song opens a new vote session; verify the threshold excludes the removed user.

## Future-runtime must-include requirements (R10b checklist)

The future R10b implementation MUST, at minimum:

1. Add `DELETE /api/rooms/{slug}` and `DELETE /api/rooms/{slug}/members/{userId}` HTTP routes behind `roomAuth`.
2. Enforce host-only authorization using the existing `room.Interactor.requireHost` pattern.
3. Implement the soft-archive path via `RoomRepository.ArchiveRoomIfActive` (idempotent).
4. Implement the member-removal path via a new `room.Interactor.RemoveMember` (or equivalent), wrapping the membership row delete + lease end + audit row insert (if in scope) in a single transaction.
5. Add the `room_member_removed` and `room_members_changed` per-room WebSocket envelopes documented above.
6. Reuse the existing R06 `room_archived` **wire value** and payload for room archive (no new event name). Add explicit per-room hub support on `*ws.RoomWSHub` so room clients on `/ws/rooms/{slug}` receive `room_archived` with `reason: "host_archived"` on a successful host-driven archive. The global `/ws` archive broadcast continues to ride the R06 path unchanged.
7. Reuse the existing R09a `RequireActiveLeaseHolder` semantics — the playback interactor path is unchanged; the lease row simply disappears when the lease holder is removed.
8. Leave active vote sessions to natural expiry (no forced resolve / cancel).
9. Document the chosen WebSocket close code (1000 or 1008) in the R10b implementation summary.
10. Cover the Decision 11 test matrix with passing tests.
11. Update `PROJECT_STATE.md` (full REST endpoint list, per-room WebSocket inventory, route matrix).
12. Update `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md` (mark R10b closed; update R10a to "Accepted").
13. Update `documents/00-project-management/SPRINTS/active.md`.
14. Provide a verification block matching the R06/R07/R09a/R09b/R09f pattern (`go test -count=1 ...`, `go test -race ...`, `git diff --check`, frontend tests if any frontend file is touched).

## Required context

- `documents/00-project-management/SPRINTS/015-room-deletion-and-membership-removal.md` (this file; R10a contract design).
- `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md` (epic placement; R10a + future R10b).
- `documents/00-project-management/SPRINTS/active.md` (active planning record; identifies R10a as the shaped next sprint).
- `documents/00-project-management/PROJECT_STATE.md` (short R10a docs-only planning note).
- `internal/domain/entity/room.go` (`Room`, `RoomMember`, `RoomStatus` constants, `RoomMemberRole` constants, `SlugPattern`).
- `internal/domain/repository/room_repository.go` (existing `ArchiveRoomIfActive`, `GetRoomBySlug`, `GetMember`, `ListMembers`, `UpdateMemberRole`).
- `internal/usecase/room/interactor.go` (existing `requireHost`, `requireHostOrAdmin`, `PromoteMember`, `DemoteMember`, `ArchiveRoom`, `ErrInvalidSlug`, `ErrRoomNotFound`, `ErrMemberNotFound`, `ErrArchived`, `ErrForbidden`).
- `internal/usecase/room/player_lease_interactor.go` (existing `Release`, `SweepExpired`, `RequireActiveLeaseHolder`, `RoomArchivedEvent`, sentinels).
- `internal/delivery/ws/room_hub.go` (existing `BroadcastRoomArchived` pattern, `nextSeq` allocation, `RegisterHandler` membership gate, `UniqueConnectedUserIDs`).
- `internal/delivery/ws/events.go` (existing `EventRoomArchived`, `RoomArchivedData`, `EventRoomQueueSync` family; per-room event-name constants).
- `internal/infrastructure/persistence/migrations/postgres/0004_rooms.up.sql` (`rooms`, `room_members`, `idx_room_members_one_host_per_room`).
- `internal/infrastructure/persistence/migrations/postgres/0005_player_leases.up.sql` (`player_leases`, `idx_player_leases_one_active_per_room`).
- `internal/infrastructure/persistence/migrations/postgres/0006_room_queue_state.up.sql` (`room_queue_state`).
- `internal/infrastructure/persistence/migrations/postgres/0007_room_auto_queue.up.sql` (no direct interaction with R10a; documented for context only).

## Verification (R10a itself)

R10a is a docs-only contract design. The verification block is:

- `git diff --check` — clean (whitespace-only diffs acceptable; all changes are docs).
- `git status --short --untracked-files=all` — only the four documentation files are touched:
  - `documents/00-project-management/SPRINTS/015-room-deletion-and-membership-removal.md`
  - `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md`
  - `documents/00-project-management/SPRINTS/active.md`
  - `documents/00-project-management/PROJECT_STATE.md`
- No runtime files under `internal/`, `frontend/src/`, `cmd/`, `migrations/`, `docker/`, `nginx/`, or anywhere else are modified.
- No GitHub Action / CI / config file is modified.
- The docs explicitly state R10a introduces no Go runtime code, no Vue runtime code, no migrations, no routes, no WebSocket constants, no config changes, and no deployment changes.

## Execution note

This document is the contract design for the **first approved slice** of room deletion and membership removal. R10a is intentionally documentation-only — it shapes the contract that the future R10b runtime implementation MUST conform to. After R10a is accepted by the Product Owner, R10b (a separate future sprint) implements the runtime per the Decision 1–11 contract and the Decision 11 test matrix. The original R10 stub is now superseded by this R10a + future R10b split; R10c+ may introduce hard delete / retention / admin audit views as separate slices.

## Implementation summary (R10b)

**Sprint goal:** R10b implements the R10a contract — `DELETE /api/rooms/{slug}` + `DELETE /api/rooms/{slug}/members/{userId}` endpoints, atomic lease end, per-room archive broadcast, targeted member-removed + remaining-clients members-changed envelopes, close-code 1008.

### Endpoint contracts implemented

- `DELETE /api/rooms/{slug}` — host-only soft archive behind `roomAuth`. Idempotent `204` on already-archived rooms (no mutation, no broadcast). `404` on missing room, `400` on invalid slug, `401`/`403` per existing middleware. Successful active → archived transition invokes `RoomRepository.ArchiveRoomIfActive` (R06) and dispatches `room_archived` on `/ws/rooms/{slug}` with `reason: "host_archived"`.
- `DELETE /api/rooms/{slug}/members/{userId}` — host-only member removal behind `roomAuth`. `400 Bad Request` for `ErrHostCannotRemoveSelf` (host trying to remove self) and `ErrCannotRemoveHost` (target is the host of the room); `404` when target is not a member; `409` when the room is archived. Membership row delete + active-lease end happen in a single DB transaction (`RemoveMemberAndEndLeaseAtomic` seam).

### WebSocket event envelopes

- `room_archived` — **reuses** the existing R06 wire value + payload (`room_id`, `reason`, `archived_at`). R10b adds explicit per-room hub support (`*ws.RoomWSHub.BroadcastRoomArchived`) so clients connected to `/ws/rooms/{slug}` receive the archive event with `reason: "host_archived"` on a successful host-driven archive. The global `/ws` archive broadcast continues to ride the R06 path unchanged.
- `room_member_removed` — **new** envelope, delivered to the removed user's per-room WebSocket connections **before** the server closes them. Payload shape: `{ room_slug: string, user_id: int, reason: "host_removed" }`.
- `room_members_changed` — **new** envelope, broadcast to all remaining clients connected to `/ws/rooms/{slug}` after a successful member removal. Payload shape: `{ room_slug: string, members: [{ user_id: int, role: "host"|"admin"|"guest" }] }`. `joined_at` is intentionally omitted from the wire; clients can fetch via the existing `GET /api/rooms/{slug}/members` if needed.

All three envelopes ride the existing per-room seq counter allocated by the existing `nextSeq` hub-loop mechanism. The 16-event global `/ws` inventory is byte-for-byte unchanged.

### WebSocket close code

The server closes the removed client's per-room WebSocket connections with **close code `1008` (policy violation)**. Rationale: distinguishes a host-driven removal-driven close from a benign disconnect (e.g. user closed their tab), so the client can surface a clearer message. The `closeWithCode` helper documents this rationale in `internal/delivery/ws/room_hub.go`.

### Files touched (R10b)

- `internal/domain/repository/room_repository.go` — `RemoveMemberAndEndLeaseAtomic` seam + `ErrHostCannotRemoveSelf` + `ErrCannotRemoveHost` sentinels (interface contract).
- `internal/infrastructure/persistence/postgres_room_repository.go` — PostgreSQL implementation of the atomic transaction (membership delete + lease end, single transaction).
- `internal/usecase/room/interactor.go` — `ArchiveRoomByHost` + `RemoveMemberByHost` interactor methods, `RoomMembersBroadcaster` seam, `CloseRemovedClient` helper moved from handler into the interactor for test isolation, host-removal sentinel mapping.
- `internal/delivery/http/room_handlers.go` — `DELETE /api/rooms/{slug}` + `DELETE /api/rooms/{slug}/members/{userId}` HTTP handlers, status-code mapping (400 / 404 / 409 / 204).
- `internal/delivery/ws/events.go` — `room_member_removed` + `room_members_changed` event-name constants and payload types.
- `internal/delivery/ws/room_hub.go` — per-room hub support: `BroadcastRoomArchived(roomSlug, RoomArchivedEvent)` (reuses existing R06 wire value + payload), `BroadcastRoomMemberRemoved(...)`, `BroadcastRoomMembersChanged(...)`, removed-client close path with code 1008.
- `cmd/server/main.go` — production wiring: per-room members broadcaster adapter connected to `*ws.RoomWSHub`, DELETE routes registered.
- `internal/usecase/room/room_delete_member_test.go` — usecase Decision 11 subset tests (archive + remove-member happy paths + sentinel paths) + concurrent duplicate-remove race test.
- `internal/delivery/http/room_delete_members_test.go` — HTTP handler tests covering 204 / 400 / 404 / 409 / 401 / 403 for both endpoints.
- `internal/delivery/ws/room_hub_test.go` — WebSocket hub tests for archive + member-removal envelopes (removed-client-before-close, remaining-clients-after-removal, archive delivered to per-room clients, archive NOT delivered on idempotent re-call).

### Verification results

- `go test -count=1 ./internal/usecase/room/... ./internal/delivery/http/... ./internal/delivery/ws/... ./internal/infrastructure/persistence/... ./cmd/server/...` — all PASS (room 5.995s, http 38.159s, ws 4.988s, persistence 6.499s, cmd/server 1.588s).
- `go test -race -count=1 ./internal/usecase/room/... ./internal/delivery/ws/... ./internal/infrastructure/persistence/...` — all PASS (room 7.740s, ws 6.018s, persistence 8.584s).
- `go vet ./internal/... ./cmd/...` — clean (exit 0).
- `git diff --check` — clean (exit 0).
- 12 commits on `dev` from baseline `ad7a9e4`; `git log ad7a9e4..HEAD --oneline` lists the runtime + test + doc commits.
