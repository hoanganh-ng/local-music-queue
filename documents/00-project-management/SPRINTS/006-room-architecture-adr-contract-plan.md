# Sprint 006 / R00 — Room Architecture ADR and Contract Plan

## Status

In progress — Product Owner approved activation.

## Sprint name

Sprint 006 / R00 — Room Architecture ADR and Contract Plan

## Goal

Create the authoritative room architecture ADR and contract plan before any PostgreSQL, room runtime, WebSocket, voting, auto-queue, or frontend room implementation begins.

This sprint is documentation and contract planning only.

## Current behavior

- The application currently behaves as one global playback context.
- Queue state is persisted as one singleton JSON document.
- REST queue, playback, voting, and auto-queue endpoints are global.
- The WebSocket hub is global and broadcasts one shared event stream.
- Voting sessions and thresholds are effectively global.
- Auto-queue configuration, play history, and single-flight behavior are effectively global.
- The frontend has one global dashboard state and no active room context.
- The host role currently implies the browser/device that renders the actual YouTube player.
- Backend authorization is not yet complete for all privileged operations, so rooms must not be described as a strong security boundary until a later authorization-hardening sprint.

## Desired behavior

The sprint must produce an approved ADR and contract plan for moving from the current global app into explicit rooms.

Product Owner decisions already locked:

- No permanent `main` room and no hidden default room in the final model.
- Existing global app state migrates into one real room during migration.
- The Product Owner supplies the migrated room name at migration time.
- Users create rooms through the room flow; no admin is required to create/use a room.
- Room creator becomes host.
- Invitees join as guests/members by default.
- One active room has exactly one host.
- Host can promote another member to admin.
- Admin can help control an active room but is not automatically the host/player owner.
- When the host/player leaves and the player lease expires, the room is archived.
- Archived-room clients are kicked out and redirected to a non-room landing screen such as `Welcome`.
- PostgreSQL should be planned and implemented early unless this ADR finds a blocking reason.

## Required context

### Files/docs/tests/contracts to inspect

- `documents/00-project-management/PROJECT_STATE.md`
- `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md`
- `documents/00-project-management/SPRINTS/active.md`
- `documents/00-project-management/SPRINTS/005-in-app-vote-event-notifications.md`
- `documents/README.md`
- `README.md`
- `cmd/server/main.go`
- `internal/domain/entity/queue.go`
- `internal/domain/repository/queue_repository.go`
- `internal/infrastructure/persistence/sqlite_repository.go`
- `internal/delivery/http/handlers.go`
- `internal/delivery/ws/events.go`
- `internal/delivery/ws/hub.go`
- `internal/usecase/queue/interactor.go`
- `internal/usecase/vote/interactor.go`
- `internal/usecase/autoqueue/interactor.go`
- `internal/usecase/priority/interactor.go`
- `internal/usecase/auth/interactor.go`
- `frontend/src/services/api.js`
- `frontend/src/services/websocket.js`
- `frontend/src/store/index.js`
- `frontend/src/views/DashboardView.vue`
- `frontend/src/components/dashboard/NowPlaying.vue`
- Existing nearby tests only when needed to confirm current contracts.

### Unrelated areas not to scan/refactor

- Browser extension files or packaged extension artifacts.
- Unrelated UI styling.
- YouTube search behavior unless needed to describe room-scoped queue add behavior.
- Priority balance implementation details beyond whether balance is global or room-scoped.
- Docker/runtime PostgreSQL implementation beyond ADR-level storage planning.
- Any file outside the room architecture contracts unless needed to resolve a direct contradiction.

## Requirements

### Behavior, permissions, ownership, invariants

- Define the `Room` lifecycle: create, active, archived.
- Define the room host rule: exactly one host for an active room.
- Define room membership roles: host, admin, guest/member.
- Define creator and invitee defaults.
- Define promotion/demotion rules.
- Define what admin can and cannot do compared with host/player owner.
- Define host/player separation explicitly.
- Define player lease, heartbeat, release, grace period, expiry, and archive trigger semantics.
- Define room archive behavior, including kicking active clients and redirecting frontend to `Welcome`.
- Define archived-room rejection behavior for queue, playback, invite, vote, and auto-queue mutations.
- State clearly that this first room implementation remains single-instance unless a later sprint adds cross-process coordination.
- State clearly that rooms are not a strong security/privacy boundary until backend authorization is hardened.

### REST/WebSocket contracts

- Define final explicit room-scoped REST route shape.
- Define the temporary transition behavior for old global routes: compatibility shim, redirect, removal, or `410 Gone`.
- Define WebSocket connection shape with explicit room context.
- Define room-scoped full sync and room-scoped delta broadcast expectations.
- Define the room archived/kicked event shape at ADR level.
- Define whether WebSocket sequence numbers are per room or global, and justify the choice.
- Define how clients reconnect after room archive or invalid room join.

### Persistence/compatibility

- Define room-scoped persistence ownership for queue state, activities, auto-queue config, and play history.
- Define how current singleton global state migrates into one Product-Owner-named room.
- Confirm no permanent `main` room and no hidden default source of truth.
- Define how the migrated room behaves when no active host/player exists at migration completion.
- Define how old SQLite data will be moved if PostgreSQL is adopted early.
- Define whether users and daily priority balances stay account-scoped for the first implementation.
- Define whether vote sessions remain in memory for the first implementation.

### Frontend/config/deployment

- Define `Welcome` view responsibilities.
- Define create-room, invite-link, join-by-invite, dashboard entry, and archived-room redirect flows.
- Define active room context storage expectations.
- Define player-device rendering expectations for the YouTube iframe.
- Define PostgreSQL timing and deployment assumptions at architecture level.
- Do not implement PostgreSQL, UI, or runtime behavior in this sprint.

### Validation, errors, docs, tests

- Define room slug/id validation expectations.
- Define invite token validation expectations.
- Define error behavior for archived rooms, invalid invites, duplicate host/player claims, and stale player leases.
- Define documentation files that later implementation sprints must update.
- Define expected verification categories for later implementation sprints.

## Out of scope

- Runtime code changes.
- PostgreSQL implementation.
- SQLite-to-PostgreSQL migration scripts.
- Room schema implementation.
- REST handler implementation.
- WebSocket hub implementation.
- Voting or auto-queue code changes.
- Frontend room UI implementation.
- Authorization hardening implementation.
- Docker Compose changes.
- Test rewrites unrelated to documenting current contracts.
- Commits, pushes, merges, issue closure, or sprint advancement by Builder.

## Implementation guidance

Create a focused ADR at:

- `documents/00-project-management/ADRS/001-room-architecture-and-contracts.md`

The ADR should be practical, decision-oriented, and explicit about what is decided now versus deferred to later sprints.

## Execution Note

> **Status update (post-review):** Sprint 006 / R00 remains in progress. The ADR was authored and saved to `documents/00-project-management/ADRS/001-room-architecture-and-contracts.md`. A focused documentation-fix pass was applied to address Architect review feedback (migrated-room bootstrap deadlock, player lease release semantics, heartbeat visibility wording, lease timestamp wording, ADR status wording, broken relative links). No runtime, frontend, backend, configuration, deployment, or test files were modified. PostgreSQL and room implementation have not been started.

### ADR Location

- [`documents/00-project-management/ADRS/001-room-architecture-and-contracts.md`](../ADRS/001-room-architecture-and-contracts.md)

### ADR Review Fix Pass (Architect Feedback)

The following issues identified during review were corrected in the ADR:

1. **Migrated-room bootstrap deadlock** — §11 now defines an explicit migration-time bootstrap rule. The Product Owner supplies the migrated room name/slug and an initial migrated-room host user (e.g. email). Migration resolves that user and creates the `RoomMember` row with `role = 'host'`. If the user cannot be resolved, migration fails clearly and atomically. Normal invitees cannot self-select `host`.
2. **Player lease release semantics** — §6 release row now separates explicit "Leave Room" from transient page lifecycle events (`beforeunload`, `pagehide`, network disconnect). Explicit leave may release/archive; transient events rely on heartbeat expiry + grace. Reconnect within grace may renew or reclaim.
3. **Heartbeat visibility wording** — §6 heartbeat row no longer says "while the tab is visible". Heartbeat runs while the page is loaded and the user holds the active player lease; browser throttling of timers is acknowledged as an implementation-time tuning concern.
4. **Lease timestamp wording** — §6 claim row now states the timestamps explicitly: `claimed_at = now`, `last_heartbeat_at = now`, `expires_at = now + 60 seconds`, grace ends at `expires_at + 30 seconds`. The misleading "30-second last_heartbeat_at" wording is removed.
5. **ADR status** — Front-matter status is now `Proposed — awaiting Architect review and Product Owner approval`. The ADR no longer claims `Accepted` while still under review.
6. **Broken relative links** — Cross-repo links in §1 (`internal/...`, `cmd/...`, `frontend/...`) now use the correct `../../../` prefix from the ADR path. Links to nearby docs (`../ROOM_EPIC_SPRINT_SEQUENCE.md`, `../PROJECT_STATE.md`) remain unchanged.

### ADR Location

- [`documents/00-project-management/ADRS/001-room-architecture-and-contracts.md`](../ADRS/001-room-architecture-and-contracts.md)

### Decisions Made

- Move from one implicit global room to explicit rooms; single-process scope for the first implementation.
- No permanent `main` room, no hidden default source of truth.
- Existing global app state migrates into one real room named by the Product Owner at migration time.
- Room lifecycle: create → active → archived (terminal). Archived rooms reject mutations.
- Roles per room: host, admin, guest. Exactly one host per active room.
- Host = person; player = device holding the active player lease. Admin cannot prevent archive when the player lease expires.
- Player lease: 60 s expiry, 20 s heartbeat cadence, 30 s grace before archive; admin cannot claim or release host's lease.
- Invite model: hashed token, 7-day default expiry, configurable max_uses, host-or-admin revocation.
- REST route shape moves to `/api/rooms/{roomId}/...`; old global routes transition through a compatibility shim to `410 Gone` in R14.
- WebSocket connection shape moves to `/ws/rooms/{roomId}`; `user_id` query parameter removed; identity taken from authenticated session.
- Per-room sequence numbers and per-room `ConnectedCount()`; per-room `full_sync`.
- `room_archived` event shape defined at ADR level.
- Reconnect after archive must create or join a new room.
- Persistence: rooms, members, invites, queue state, activities, auto-queue config, play history become room-scoped in PostgreSQL (R06).
- Users, user sessions, priority transactions, and daily priority balance stay account-scoped for the first room implementation.
- Vote sessions stay in-memory per room for the first room implementation.
- Frontend adds `Welcome`, `CreateRoom`, `Invite` modal, and `JoinRoom` views; room context stored in `globalStore.activeRoom` and persisted in `localStorage` under `lmq_active_room`.
- YouTube iframe renders only for the active player-lease holder; backend is the source of truth for lease.
- Error codes added: 409 (archived/duplicate), 410 (retired/exhausted), 403 (forbidden), 404 (unknown; generic for invites).

### Decisions Deferred

- Cross-process / multi-instance room coordination.
- Persisted vote sessions.
- Room-scoped priority balances (per-room token economy).
- Slug rename, archive recovery / un-archive.
- Per-song row storage in place of the JSON blob.
- Strong backend authorization on every endpoint (R13).
- Anonymous (read-only) room views.
- Cross-room moderation tools (global admin).

### Verification Results

- `git diff --check` — PASS (no whitespace/indent warnings)
- `git status --short --branch` — see commit-time output below.

```text
## dev...origin/dev
 M documents/00-project-management/ADRS/001-room-architecture-and-contracts.md
 M documents/00-project-management/SPRINTS/006-room-architecture-adr-contract-plan.md
```

- Confirmation: no runtime behavior changed.
- Confirmation: PostgreSQL and room implementation were NOT started.

Recommended ADR sections:

1. Context
2. Current global model
3. Decision summary
4. Room lifecycle
5. Roles and membership
6. Host/player lease model
7. Invite model
8. Archive/kick/Welcome redirect behavior
9. REST contract direction
10. WebSocket contract direction
11. Persistence and migration direction
12. PostgreSQL timing
13. Frontend flow direction
14. Authorization/security caveats
15. Compatibility and old endpoint transition
16. Deferred work
17. Risks
18. Consequences for later sprints

Update this sprint document with verification notes and ADR location after the Builder finishes.

Only update `ROOM_EPIC_SPRINT_SEQUENCE.md` if the ADR exposes a direct contradiction or typo. Do not rewrite the whole sequence document.

## Verification points

Because this is a documentation/ADR sprint, minimum verification is:

- `git diff --check`
- `git status --short --branch`

If any runtime, frontend source, backend source, config, or deployment file is changed unexpectedly, stop and report it instead of continuing.

## Risks and review focus

- Hidden default-room behavior sneaking back into the design.
- Confusing human host with speaker/player device.
- Immediate disconnect archiving rooms without a grace period.
- Letting invitees self-select privileged roles.
- Treating rooms as secure while backend authorization is still incomplete.
- PostgreSQL scope expanding into implementation before contracts are approved.
- Old global routes remaining as undocumented permanent compatibility behavior.
- WebSocket/vote/auto-queue room isolation not being specific enough for later implementation.

## Builder reasoning effort

High. This sprint defines cross-layer contracts, persistence direction, migration behavior, real-time behavior, lifecycle semantics, and security caveats.

## Handoff prompt for Builder

```text
You are Builder for Local Music Queue.

Sprint:
Sprint 006 / R00 — Room Architecture ADR and Contract Plan.

Product Owner approval:
Sprint R00 is approved and active. This is a documentation/ADR sprint only.

Do not commit, push, merge, start PostgreSQL implementation, start room runtime implementation, modify runtime code, or advance the sprint.

Goal:
Create the authoritative room architecture ADR and contract plan before implementation begins.

Required context:
- documents/00-project-management/PROJECT_STATE.md
- documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md
- documents/00-project-management/SPRINTS/active.md
- documents/00-project-management/SPRINTS/005-in-app-vote-event-notifications.md
- documents/00-project-management/SPRINTS/006-room-architecture-adr-contract-plan.md
- documents/README.md
- README.md
- cmd/server/main.go
- internal/domain/entity/queue.go
- internal/domain/repository/queue_repository.go
- internal/infrastructure/persistence/sqlite_repository.go
- internal/delivery/http/handlers.go
- internal/delivery/ws/events.go
- internal/delivery/ws/hub.go
- internal/usecase/queue/interactor.go
- internal/usecase/vote/interactor.go
- internal/usecase/autoqueue/interactor.go
- internal/usecase/priority/interactor.go
- internal/usecase/auth/interactor.go
- frontend/src/services/api.js
- frontend/src/services/websocket.js
- frontend/src/store/index.js
- frontend/src/views/DashboardView.vue
- frontend/src/components/dashboard/NowPlaying.vue

Unrelated areas not to scan/refactor:
- browser extension files and packaged extension artifacts
- unrelated UI styling
- YouTube search details unless needed for room-scoped queue add contracts
- Docker/PostgreSQL implementation details beyond ADR planning
- unrelated tests

Tasks:
1. Inspect the required context and reconstruct the current global model accurately.
2. Create `documents/00-project-management/ADRS/001-room-architecture-and-contracts.md`.
3. In the ADR, define:
   - room lifecycle
   - no-default-room final model
   - Product-Owner-named migrated room behavior
   - room roles and promotion rules
   - host/player lease and heartbeat/grace semantics
   - invite link behavior
   - room archive/kick/Welcome redirect behavior
   - final REST room route direction
   - WebSocket room connection/full-sync/delta/archive-event direction
   - persistence and migration direction
   - PostgreSQL timing and assumptions
   - frontend flow direction
   - authorization/security caveats
   - old global endpoint transition strategy
   - risks, deferred work, and consequences for later sprints
4. Update this Sprint 006/R00 document with an execution note and verification results.
5. Do not update `SPRINTS/active.md` unless correcting a typo.
6. Do not update `ROOM_EPIC_SPRINT_SEQUENCE.md` unless fixing a direct contradiction or typo.
7. Do not modify runtime code.

Verification:
- git diff --check
- git status --short --branch

Expected output:
- Files changed.
- ADR summary.
- Decisions made.
- Decisions deferred.
- Verification results.
- Confirmation that no runtime behavior changed.
- Confirmation that PostgreSQL and room implementation were not started.
```
