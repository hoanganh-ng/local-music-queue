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
* REST endpoints exist to **claim**, **heartbeat**, and **release** a player lease (`POST /api/rooms/{roomId}/player/claim`, `POST /api/rooms/{roomId}/player/heartbeat`, `POST /api/rooms/{roomId}/player/release`).  These endpoints require host authentication via bearer token.  Heartbeat extends the expiry; release ends the lease immediately.  Duplicate claims while a valid lease exists return `409 Conflict`.
* The backend enforces that at most one active lease exists per room.  Claim requests when a lease is valid reject; claim requests after the grace period expire the old lease and succeed.
* A grace period (default 30 s after `expires_at`) allows transient disconnects.  Within grace, heartbeats revive the lease.  After grace, the room archives automatically and broadcasts a `room_archived` event.
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
2. **Claim use case.** Add a use‑case method that allows the authenticated host to claim the player lease.  When no lease exists or the existing lease has expired beyond grace, create the lease and set timestamps (`claimed_at` = now, `last_heartbeat_at` = now, `expires_at` = now + 60 s).  Reject claims within grace.
3. **Heartbeat use case.** Add a method to extend the lease by updating `last_heartbeat_at` and pushing `expires_at` forward by the configured interval (e.g. 60 s) when called by the current lease holder.  If called outside grace, return an error.
4. **Release use case.** Add a method to explicitly end the lease when the host leaves the room.  Release is invoked only on explicit user action (e.g. “Leave Room”) and deletes the lease immediately.  Transient page lifecycle events do not call release.
5. **Grace period semantics.** Define a fixed grace window (default 30 s after `expires_at`).  Within grace, heartbeats can renew the lease; outside grace, any heartbeat or claim triggers archive.
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
* Wire the new endpoints in `cmd/server/main.go` after verifying authentication middleware.  Ensure route paths follow the `/api/rooms/{roomId}/player/...` pattern.
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
