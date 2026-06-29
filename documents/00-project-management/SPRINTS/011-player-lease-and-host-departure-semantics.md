# Sprint 011 / R06 — Player Lease and Host Departure Semantics

## Status

Implemented (R06) — documentation stub for the **R06 – Player Lease and Host Departure Semantics** sprint.  This file sketches the intent and scope of the sprint based on the approved room epic sequence.  The actual implementation and verification notes will be added when the sprint is executed and closed.

## Sprint name

Sprint 011 / R06 — Player Lease and Host Departure Semantics

## Goal

Model the one‑speaker/player responsibility explicitly.  The global application currently does not distinguish between the human host (room owner) and the device that holds the active player.  This sprint introduces a per‑room **player lease** so that the host’s browser can claim, heartbeat, and release control of the playback device.  Losing the active player (e.g. by disconnecting or missing heartbeats) triggers room archival under defined grace semantics.  Admins remain able to control queue/playback while a lease exists, but they cannot prevent archive once the lease expires.

## Current behavior (pre‑R06 baseline)

* No `player_lease` entity exists.  The host concept conflates the person and the playback device.
* Rooms introduced in R04 lack any notion of player lease or host device tracking.
* When the host leaves the room or closes the browser, there is no automatic archive.  The room stays active indefinitely until manually archived in later sprints.
* Admins can control queue/playback but there is no safeguard preventing a room from staying alive without a player.

## Desired behavior (post‑R06)

* A new `PlayerLease` persistence model associates exactly one active lease with an active room.  Each lease records the claiming user ID, a `claimed_at` timestamp, a `last_heartbeat_at` timestamp and an `expires_at` timestamp.
* REST endpoints exist to **claim**, **heartbeat**, **release**, and **read** a player lease using the room **slug** as the external identifier:
  * `POST /api/rooms/{slug}/player/claim`
  * `POST /api/rooms/{slug}/player/heartbeat`
  * `POST /api/rooms/{slug}/player/release`
  * `GET  /api/rooms/{slug}/player/lease`

  These endpoints require host authentication via bearer token.  Heartbeat extends the expiry; release ends the lease immediately.  Duplicate claims while a valid lease exists return `409 Conflict`.
* The backend enforces that at most one active lease exists per room.  Claim requests when a lease is valid reject.
* A grace period (default 30 s after `expires_at`) allows transient disconnects.  Within grace, heartbeats revive the lease.  After grace, the lease is considered expired.
* **Archive on expiry is sweep-owned.**  When the lease passes grace, the periodic sweep (`PlayerLeaseInteractor.SweepExpired`, called from the hub ticker) is the single owner of the archive transition.  A request path (claim, heartbeat, release) does **not** archive on expiry; only the sweep archives after grace.  This keeps the archive transition idempotent and avoids racing request handlers against the sweep.
* On archive (whether triggered by the sweep or by explicit release), a `room_archived` WebSocket event is broadcast to all connected clients.  The usecase layer emits a `RoomArchivedEvent` value; `delivery/http` maps it to the `room_archived` envelope through a small broadcaster interface (`room.RoomArchivedBroadcaster`) so `usecase/room` stays independent of `delivery/ws`.
* **Release without an active lease returns `404 Not Found`.**  An explicit `POST /player/release` on a room that has no active lease MUST NOT archive the room and MUST NOT broadcast an event.  This prevents an idempotent release from racing the sweep into a double-archive.
* Admins can control queue/playback but cannot claim or release the player lease.  Admin controls do not keep the room alive if the host’s lease expires.
* Tests cover duplicate‑claim prevention, heartbeat revival within grace, lease expiry and archive trigger, and admin‑role limitations.

## Required context

When implementing this sprint the team should review:

* [`documents/00-project-management/PROJECT_STATE.md`](../PROJECT_STATE.md) for the authoritative baseline of current behavior.
* [`documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md`](../ROOM_EPIC_SPRINT_SEQUENCE.md) for the approved ordering and checklist.
* Closed sprint records up through R04 (`006‑010`) to understand current room domain behavior and migrations.
* ADR 001 (Room Architecture and Contracts) for lease semantics and grace period definitions.
* `cmd/server/main.go`, `internal/usecase/room`, and planned `internal/usecase/player` or equivalent packages when adding new handlers and interactors.

## Requirements

The sprint must satisfy all of the following points without regressing previously closed surfaces:

1. **Define persistence.** Add a `player_leases` table (or extend an existing room table) storing `room_id`, `claimed_by_user_id`, `claimed_at`, `last_heartbeat_at`, and `expires_at`.  Enforce one active lease per room via SQL and repository logic.
2. **Claim use‑case.** Add a use‑case method that allows the authenticated host to claim the player lease.  When no active lease exists, create the lease and set timestamps (`claimed_at` = now, `last_heartbeat_at` = now, `expires_at` = now + 60 s).  Reject claims while an active lease exists.  Reject claims on archived rooms (the room must be re-created before a new claim can succeed).
3. **Heartbeat use case.** Add a method to extend the lease by updating `last_heartbeat_at` and pushing `expires_at` forward by the configured interval (e.g. 60 s) when called by the current lease holder.  If called outside grace, return an error.
4. **Release use case.** Add a method to explicitly end the lease when the host leaves the room.  Release is invoked only on explicit user action (e.g. “Leave Room”) and deletes the lease immediately.  Transient page lifecycle events do not call release.
5. **Grace period semantics.** Define a fixed grace window (default 30 s after `expires_at`).  Within grace, heartbeats can renew the lease; outside grace, the lease is considered expired.  **Archive on expiry is sweep-owned** (see Behaviour); request paths (claim / heartbeat) do NOT archive on expiry.
6. **Archive trigger.** When grace elapses without a heartbeat, automatically archive the room and broadcast a `room_archived` WebSocket event to all connected clients.  Ensure reconnecting clients see the archived state and redirect to `Welcome`.
7. **Admin rules.** Permit admins to control queue/playback within an active lease but forbid them from claiming or releasing the lease.  Ensure admins do not unintentionally keep a room alive without a lease.
8. **Tests.** Add focused tests for duplicate claim prevention, heartbeat revival within grace, expiry and archive behavior, and admin‑role gate enforcement.

## Out of scope

* Moving queue/playback/voting/auto‑queue into rooms (R07+).
* Removing `users.legacy_id` or migrating global data into rooms (R06).
* Frontend changes beyond adding the necessary API calls.  Room creation UI and invite flow remain out of scope (R11+).
* Cross‑process or multi‑instance coordination; the lease runs in a single process.

## Implementation guidance

* Follow the Clean Architecture layering used elsewhere: domain entities, repository interfaces, use cases, and delivery handlers.  Do not embed SQL into handlers.
* Use PostgreSQL migrations to add the lease table; avoid altering unrelated migrations.
* Wire the new endpoints in `cmd/server/main.go` after verifying authentication middleware.  Ensure route paths follow the `/api/rooms/{slug}/player/...` pattern; numeric room ids MUST NOT appear in routes (ADR 001 §6).
* Grace period and heartbeat intervals should be configurable via environment variables with sane defaults.  However, tuning is deferred; start with 60‑s lease and 30‑s grace as described in ADR 001.
* Log lease expiry and archive events clearly without exposing sensitive identifiers.  Do not log raw session tokens.

## Execution note

> **Status update:** This stub file documents the intended scope of Sprint 011 / R06.  It does not imply that implementation has started.  When the sprint begins, update this document with the current baseline, the implemented behavior, verification results, and closure notes.  When closed, update the status to “Closed” and link to the authoritative commit hashes.  Do not implement R06, R07, or later sprints while R06 is in progress.

## Implementation summary (R06)

- Added `player_leases` table with one-active-lease-per-room partial unique
  index (migration 0005). Schema version 5.
- Added claim / heartbeat / release / get REST endpoints under
  `/api/rooms/{slug}/player/...` behind bearer-token auth.
- Added additive `room_archived` WebSocket event; the 16 pre-existing events
  remain byte-for-byte compatible.
- Sweep ticker (existing 5 s hub loop) ends leases past grace, archives the
  room exactly once, and broadcasts `room_archived { room_id, reason, archived_at }`.
- Explicit `POST /player/release` archives the active lease exactly once and
  publishes the same `room_archived` envelope (reason=explicit).  Release on
  a room with no active lease returns 404 and does NOT archive or broadcast.
- Sweep broadcasts are dispatched from goroutines to avoid the WS hub
  deadlocking itself on the unbuffered broadcast channel.
- `RoomArchivedBroadcaster` is the seam that maps a `RoomArchivedEvent`
  from the usecase layer to the ws `room_archived` envelope without
  `usecase/room` importing `delivery/ws`.
- No changes to global queue/playback/voting/auto-queue, frontend, Docker,
  or migration CLI.

## Verification Results (R06)

- `go build ./cmd/... ./internal/...` — PASS
- `go vet ./cmd/... ./internal/...` — PASS
- Targeted tests — PASS (entity, usecase/room, persistence, delivery/http)
- Race tests on touched packages — PASS
- `cmd/server` setup smoke — PASS
- `git diff --check` — clean
- `git status --short` — clean after the closure pass

No frontend, Docker/HTTPS, or migration CLI changes. Pre-existing
`letsencrypt-backend/accounts: permission denied` blocker documented in
PROJECT_STATE.md remains orthogonal and is not in R06 scope.
