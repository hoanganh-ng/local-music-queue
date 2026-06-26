# R07 – Room‑scoped playback queue

**Status:** planned (stub – this document defines the intent of the sprint but the work has not begun)

**Sprint name:** Room‑scoped playback queue

## Goal

Persist the playback queue as a first‑class, room‑scoped entity and expose operations to manage it.  The queue should support adding tracks, removing tracks, reordering, and tracking up‑votes/down‑votes.  Changes must be persisted to PostgreSQL and broadcast to all connected clients via WebSocket events.  Concurrency must be handled gracefully so that users can collaborate without race conditions.

## Current behaviour (pre‑sprint baseline)

The current implementation does not maintain a durable queue.  Tracks are passed ad‑hoc from client to player, and there is no persistent ordering or metadata.  When a page reloads, the queue is lost, and only the currently playing track is known.  There is no API to inspect or modify the queue, and queue state is not broadcast to spectators.  This prevents co‑operative management of playback and makes it impossible to implement skip voting or other queue‑related features.

## Desired behaviour (post‑sprint)

* Introduce a `room_playback_queue` table or equivalent persistence layer with fields such as `id`, `room_id`, `track_uri` (or provider‑specific identifiers), `added_by_user_id`, `position`, and a vote tally (up‑votes / down‑votes).
* Expose REST endpoints for:
  * **Add track** – add a new track to the end of the queue.  Requires an active session token.  Validate that the track can be fetched/played by the room’s source (e.g. YouTube, Spotify) and record who added it.
  * **Remove track** – remove a track at a given queue position or by ID.  Allow only the user who added it or the current lease holder (host) to remove.  Reject if the track is currently playing.
  * **Reorder tracks** – move a track to a new position.  Only the lease holder may reorder the queue.  Validate that positions remain contiguous.
  * **Vote on track** – up‑vote or down‑vote a track.  Maintain a per‑user vote record to prevent duplicate votes and allow changing a vote from up to down.
* Emit WebSocket events whenever the queue changes.  Include the full queue state in a `queue_sync` message so clients can update their UIs.
* On room creation and on new client connections, send the current queue state alongside `player_state` and other metadata.
* Ensure concurrent modifications do not corrupt the queue.  Use optimistic locking or transactions at the repository layer to handle concurrent add/remove/reorder operations.  Resolve conflicts by retrying or returning a conflict error to the client.
* Add unit and integration tests covering all endpoints, including concurrent modifications and permission checks.

## Required context

* Review `ROOM_EPIC_SPRINT_SEQUENCE.md` for the overall plan and preceding sprints.
* The baseline domain entities and database setup, as defined in ADR 001 and ADR 002.
* The session token authentication layer from sprint R05 (session token auth) and the planned `PlayerLease` semantics from sprint R06.  Some operations may require an active lease (host) whereas others (add, vote) only require a valid session.
* Any existing queue or playback logic in the codebase.

## Requirements

1. **Database schema.**  Add a migration introducing `room_playback_queue` with appropriate fields and indexes.  Include a composite unique constraint on `(room_id, position)` to prevent duplicate positions.
2. **Domain model.**  Introduce a `QueueItem` domain type with fields matching the schema.  Update the service layer to encapsulate queue operations with proper business rules.
3. **API endpoints.**  Implement controller functions for add, remove, reorder, vote and read operations.  Apply middleware for session token resolution and permission checks.
4. **WebSocket integration.**  Extend the hub to broadcast queue updates to all clients in the room.  Define new event types such as `queue_updated` and `queue_sync`.
5. **Concurrency handling.**  Use database transactions or application‑level locks to ensure queue operations are atomic.  Return an HTTP `409 Conflict` on version mismatch and let clients retry.
6. **Validation and error handling.**  Validate track URIs, membership status, lease status and input positions.  Provide clear error messages for unauthorised or invalid requests.
7. **Testing.**  Write unit tests for the queue repository, service and controller.  Add integration tests demonstrating concurrent modifications and correct WebSocket messages.
8. **Documentation.**  Update API documentation and developer guides to cover the new endpoints and queue semantics.

## Out of scope

* Implementing actual audio playback or integration with streaming providers.  The queue stores URIs but does not fetch or play tracks.
* UI or client‑side changes beyond necessary WebSocket event handling.  Front‑end integration may be handled in a subsequent sprint.
* Search functionality for tracks or automatic playlist generation – that will be addressed in later sprints.
* Persisting vote history beyond tally counts (e.g. for analytics).

## Implementation guidance

* Keep queue logic within a dedicated service.  Do not embed database calls in HTTP controllers or WebSocket handlers.
* Use database constraints (unique on `(room_id, position)`) to enforce ordering invariants.  When reordering, update positions within a transaction to avoid gaps.
* Consider storing votes in a separate table keyed by `(queue_item_id, user_id)` to prevent duplicate votes and enable up/down switching.  Aggregate vote counts on read.
* Use optimistic locking (e.g. `row_version` column) if available, or check the highest position value before inserting a new item to avoid race conditions.
* Minimise coupling to the `PlayerLease` concept – queue operations should be valid regardless of whether a lease is currently held, except where explicitly restricted to the host (reorder, remove others’ tracks).

## Execution note

This document is a planning stub.  It captures the intent and requirements for the **room‑scoped playback queue** sprint but does not reflect any implemented changes.  When the sprint begins, update this file with any clarifications that arise during shaping, and upon completion summarise the outcome and mark the status as **closed** in both this document and `ROOM_EPIC_SPRINT_SEQUENCE.md`.
