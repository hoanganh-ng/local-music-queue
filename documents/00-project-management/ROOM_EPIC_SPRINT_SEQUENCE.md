# Room Epic Sprint Sequence

## Purpose

This document records the approved implementation order for the multi-room transformation epic.

It is a planning document only. It does not advance the active sprint, replace `SPRINTS/active.md`, or authorize runtime implementation by itself.

Related issue: #17 — Epic: Multi-room playback, invite-based room lifecycle, and storage migration.

## Product Owner Decisions

- The final model has no permanent `main` room and no hidden default room.
- Existing global app state must be migrated into the new room model.
- The migrated room is a real room whose name is supplied by the Product Owner at migration time.
- Room roles are chosen through the room flow, not preconfigured globally.
- The room creator becomes host.
- Invitees join as guests or members by default.
- One room has exactly one host.
- The host can promote another member to admin.
- Admin can help control an active room, but admin is not the same as the room's host/player owner.
- When the host/player leaves and the player lease expires, the room is archived.
- When a room is archived, active clients are kicked from the room and redirected to a non-room landing screen such as a `Welcome` view.
- PostgreSQL should be planned and implemented early, before deep room persistence work, unless the ADR finds a blocking reason not to.
- Sections not explicitly changed by the Product Owner remain accepted as consensus from the epic discussion.

## Ordering Principles

1. Finish/review the current active sprint before beginning room work.
2. Lock contracts before runtime implementation.
3. Move storage early, then build room persistence on the concrete repository.
4. Add room lifecycle before moving queue behavior into rooms.
5. Move persistence before REST/WebSocket behavior.
6. Isolate WebSocket, voting, and auto-queue by room before exposing the full frontend experience.
7. Harden backend authorization before treating rooms as secure permission boundaries.
8. Clean up old global contracts only after the new room model is verified.

## Phase 0 — Finish Current Active Work

### Sprint R00-pre — Close Current Active Sprint

**Goal:** Avoid mixing the current active sprint with the room/storage transformation.

**Checklist:**

- [ ] Review and close the current active sprint before starting room work.
- [ ] Confirm the current `dev` branch baseline after the active sprint is accepted.
- [ ] Freeze unrelated feature work while room/storage/auth architecture is being transformed.

## Phase 1 — Decide the New House Before Moving Data

### Sprint R00 — Room Architecture ADR and Contract Plan

**Goal:** Approve the room contracts before implementation.

**Checklist:**

- [ ] Create an ADR for room lifecycle: create, active, archived.
- [ ] Confirm the final no-default-room model.
- [ ] Confirm migrated-room naming: Product Owner supplies the room name during migration.
- [ ] Define role rules: creator is host; invitees join as guest/member; host/admin can promote admin; exactly one host.
- [ ] Define archive behavior: active clients leave the room and redirect to `Welcome`.
- [ ] Define host/player lease semantics and heartbeat grace period.
- [ ] Define final REST route shape with explicit room context.
- [ ] Define WebSocket connection shape with explicit room context.
- [ ] Define the room archive/kick WebSocket event.
- [ ] Define old global endpoint transition strategy.
- [ ] Define what remains out of scope for the first room implementation.

### Sprint R01 — PostgreSQL Migration Design

**Goal:** Design the storage foundation before room persistence is implemented.

**Checklist:**

- [ ] Confirm PostgreSQL as the target storage unless a blocking reason is found.
- [ ] Define schema migration tooling.
- [ ] Define local development database setup.
- [ ] Define deterministic test database strategy.
- [ ] Define Docker Compose changes.
- [ ] Define production configuration and environment variables.
- [ ] Define backup and rollback assumptions.
- [ ] Define concrete repository boundaries for PostgreSQL-backed persistence.
- [ ] Do not change room runtime behavior in this sprint.

## Phase 2 — Build the Storage Foundation Early

### Sprint R02 — PostgreSQL Foundation with Existing Behavior Preserved

**Goal:** Introduce PostgreSQL while preserving current single-context behavior.

**Checklist:**

- [ ] Add PostgreSQL configuration without exposing credentials.
- [ ] Add PostgreSQL connection setup.
- [ ] Add concrete PostgreSQL repositories for the existing data model.
- [ ] Preserve current queue JSON compatibility.
- [ ] Keep current single global queue/playback behavior working.
- [ ] Keep current REST and WebSocket contracts working.
- [ ] Update backend tests for the chosen PostgreSQL test strategy.
- [ ] Update Docker Compose and deployment docs.
- [ ] Verify existing behavior before adding room behavior.

### Sprint R03 — SQLite-to-PostgreSQL Data Migration

**Goal:** Provide a concrete migration path from the current SQLite database to PostgreSQL.

**Checklist:**

- [ ] Migrate users.
- [ ] Migrate user session/account data.
- [ ] Migrate priority transactions.
- [ ] Migrate current queue state JSON.
- [ ] Migrate activities.
- [ ] Migrate auto-queue config.
- [ ] Migrate play history.
- [ ] Add migration tests or fixtures proving data preservation.
- [ ] Document operator steps.
- [ ] Document rollback limits.

## Phase 3 — Add Room Domain Before Moving Queue Behavior

### Sprint R04 — Room Domain, Invite, Membership, and Lifecycle

**Goal:** Add room concepts without moving queue/playback behavior into rooms yet.

**Checklist:**

- [ ] Add room domain entity.
- [ ] Add room member domain entity.
- [ ] Add room invite domain entity.
- [ ] Add room archive state.
- [ ] Add create-room use case.
- [ ] Add join-by-invite use case.
- [ ] Add member listing use case.
- [ ] Enforce exactly one host per active room at the backend.
- [ ] Ensure creator becomes host.
- [ ] Ensure invitees join as guest/member by default.
- [ ] Add admin promotion/demotion use cases.
- [ ] Ensure users cannot self-promote through client payloads.
- [ ] Add archived-room rejection rules.
- [ ] Add tests for room creation, invite join, promotion, and archive behavior.

### Sprint R05 — Player Lease and Host Departure Semantics

**Goal:** Model the one-speaker/player responsibility explicitly.

**Checklist:**

- [ ] Add room/player lease entity or persistence model.
- [ ] Add player claim use case.
- [ ] Add player heartbeat use case.
- [ ] Add player release use case.
- [ ] Enforce one active player lease per room.
- [ ] Add grace period for refresh, reconnect, and network blips.
- [ ] Define when player/host loss archives the room.
- [ ] Ensure admins can control active rooms but do not accidentally keep a room alive without a player lease.
- [ ] Add tests for duplicate player prevention.
- [ ] Add tests for refresh within grace period.
- [ ] Add tests for lease expiry and archive behavior.

## Phase 4 — Move Existing Global State into Named Room State

### Sprint R06 — Room-Scoped Persistence Migration

**Goal:** Convert global queue-related persistence into room-scoped persistence.

**Checklist:**

- [ ] Convert queue state persistence to room-scoped storage.
- [ ] Convert activities to room-scoped storage.
- [ ] Convert auto-queue config to room-scoped storage.
- [ ] Convert play history to room-scoped storage.
- [ ] Migrate existing global data into one real room named by the Product Owner at migration time.
- [ ] Do not create a permanent `main` room.
- [ ] Ensure old global state is not an alternate source of truth.
- [ ] Preserve songs, current index, status, elapsed, activities, auto-queue config, and play history.
- [ ] Ensure the migrated room follows normal lifecycle rules after migration.
- [ ] Add migration tests and rollback notes.

### Sprint R07 — Room-Scoped Queue/Playback REST API

**Goal:** Route queue and playback behavior through explicit room context.

**Checklist:**

- [ ] Add explicit room-scoped queue endpoints.
- [ ] Add explicit room-scoped playback endpoints.
- [ ] Route queue mutations through room-aware use cases.
- [ ] Validate room slug or room identifier.
- [ ] Validate URLs, indexes, elapsed values, and playback status transitions.
- [ ] Reject queue/playback mutations for archived rooms.
- [ ] Decide and implement temporary behavior for old global endpoints according to the ADR.
- [ ] Update API tests.
- [ ] Update API documentation.

## Phase 5 — Isolate Real-Time Behavior by Room

### Sprint R08 — Room-Scoped WebSocket Hub

**Goal:** Ensure each room has isolated real-time state and broadcasts.

**Checklist:**

- [ ] Require explicit room context on WebSocket connection.
- [ ] Maintain room-specific client sets.
- [ ] Send room-specific initial full sync.
- [ ] Broadcast deltas only to clients in the same room.
- [ ] Make sequence handling room-safe.
- [ ] Add room archived/kicked event.
- [ ] Ensure reconnecting clients must rejoin a valid active room.
- [ ] Add frontend redirect to `Welcome` after room archive event.
- [ ] Add tests proving no cross-room broadcast leakage.

### Sprint R09 — Room-Scoped Voting

**Goal:** Scope all voting state and thresholds by room.

**Checklist:**

- [ ] Scope vote sessions by room.
- [ ] Scope vote thresholds to connected clients in the same room.
- [ ] Prevent vote session ID collisions across rooms.
- [ ] Ensure expired sessions notify only the relevant room.
- [ ] Ensure vote-passed queue mutations affect only the target room queue.
- [ ] Add tests for simultaneous votes in multiple rooms.
- [ ] Add tests for two rooms voting on the same song ID.

### Sprint R10 — Room-Scoped Auto-Queue

**Goal:** Scope auto-queue configuration, history, and concurrency by room.

**Checklist:**

- [ ] Scope auto-queue config by room.
- [ ] Scope play history by room.
- [ ] Scope auto-queue single-flight guard by room.
- [ ] Scope stale-candidate checks by room.
- [ ] Ensure one room's auto-queue fetch cannot block or suppress another room's trigger.
- [ ] Broadcast auto-queue events only to the relevant room.
- [ ] Add race-focused multi-room tests.

## Phase 6 — Frontend Room Experience

### Sprint R11 — Welcome, Create Room, Invite, and Join Flow

**Goal:** Add the frontend entry flow for room-based usage.

**Checklist:**

- [ ] Add `Welcome` view.
- [ ] Add create-room UI.
- [ ] Add invite link generation UI.
- [ ] Add invite link copy behavior.
- [ ] Add join-by-invite route and view.
- [ ] Store active room context.
- [ ] Redirect archived/kicked users to `Welcome`.
- [ ] Add unit tests for create-room, join-room, and archived-room redirect behavior.

### Sprint R12 — Room-Aware Dashboard and API/WebSocket Clients

**Goal:** Make the existing dashboard operate inside the active room context.

**Checklist:**

- [ ] Make API client room-aware.
- [ ] Make WebSocket client room-aware.
- [ ] Reset queue state when entering or leaving rooms.
- [ ] Reset vote state when entering or leaving rooms.
- [ ] Reset auto-queue UI state when entering or leaving rooms.
- [ ] Display room name.
- [ ] Display current user's room role.
- [ ] Render the YouTube iframe only for the active player lease.
- [ ] Keep host/admin controls based on backend-authoritative role.
- [ ] Add unit tests for dashboard room switching and player rendering rules.

## Phase 7 — Authorization Hardening and Cleanup

### Sprint R13 — Room Authorization Hardening

**Goal:** Enforce room permissions on the backend before treating rooms as permission boundaries.

**Checklist:**

- [ ] Resolve server-side session identity for all sensitive room operations.
- [ ] Enforce room membership in backend use cases.
- [ ] Enforce room role permissions in backend use cases.
- [ ] Stop trusting client-supplied user IDs for room permissions.
- [ ] Stop trusting client-supplied role strings for room permissions.
- [ ] Validate WebSocket identity/session instead of trusting query parameters.
- [ ] Add negative tests for spoofed host attempts.
- [ ] Add negative tests for spoofed admin attempts.
- [ ] Add negative tests for non-member room access.

### Sprint R14 — Global Contract Cleanup and Documentation

**Goal:** Remove obsolete global assumptions and document the completed room model.

**Checklist:**

- [ ] Remove or deprecate old global endpoints according to the ADR.
- [ ] Remove global WebSocket assumptions.
- [ ] Update README.
- [ ] Update project state baseline.
- [ ] Update API documentation.
- [ ] Update WebSocket documentation.
- [ ] Update frontend documentation.
- [ ] Update deployment documentation.
- [ ] Update migration guide.
- [ ] Verify the full backend/frontend/build/deployment command set.

## Verification Expectations Across the Epic

Use targeted checks per sprint, plus broader checks before accepting major phases.

Typical commands:

```bash
go test ./...
go test -race ./...
go vet ./...
cd frontend && npm run test:unit -- --run
cd frontend && npm run build
docker compose config
```

Additional expectations:

- Migration tests must verify existing queue JSON data is preserved.
- Room-scoped WebSocket tests must verify no cross-room broadcast leakage.
- Room-scoped voting tests must verify room-local thresholds and session isolation.
- Room-scoped auto-queue tests must verify room-local single-flight behavior.
- Frontend tests must verify archived rooms redirect active users to `Welcome`.

## Recommended First Builder Sprint

Start with **Sprint R00 — Room Architecture ADR and Contract Plan** only.

Do not ask Builder to implement PostgreSQL, room persistence, room APIs, WebSocket changes, or frontend room flow until R00 is reviewed and approved.
