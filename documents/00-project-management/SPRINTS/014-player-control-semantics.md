# R09 – Player control semantics

**Status:** planned (stub – work not yet started)

**Sprint name:** Player control semantics

## Goal

Define and implement the semantics for controlling playback within a room.  Users must be able to play, pause, skip and adjust playback attributes (e.g. volume).  Control commands should respect the current `PlayerLease` holder while still allowing democratic actions such as vote‑to‑skip.  The sprint aims to provide clear rules, APIs and WebSocket events so that clients can build consistent user interfaces.

## Current behaviour (pre‑sprint baseline)

At present, there are no explicit APIs or event types for controlling playback.  The player automatically progresses through tracks as they are provided, and there is no way to pause or skip ahead.  Because there is no lease concept implemented yet (this depends on R06), there is also no authoritative controller.  As a result, clients have no reliable method to alter playback state.

## Desired behaviour (post‑sprint)

* **Control commands**: Implement REST and/or WebSocket commands for the following actions:
  * **Play/Pause** – toggle playback state.  Only the active lease holder may directly issue this command.
  * **Skip** – move to the next track in the queue.  Users who are not the lease holder may request a skip, but it will only occur once a configurable number or percentage of room members vote in favour (vote‑to‑skip).  The lease holder can skip unilaterally.
  * **Previous** – optional: return to the previous track if available.  This may be restricted to the lease holder.
  * **Volume change** – adjust playback volume.  Only the lease holder may set the volume directly.  Other users may set local client volume but not global volume.
* **Vote‑to‑skip mechanism**: Implement a mechanism to tally skip votes from non‑lease members.  When the threshold is reached (e.g. more than 50 % of connected members), automatically send a skip command on behalf of the room.  Reset the vote count after each skip or if the track changes by other means.
* **Authorisation**: Tie direct control commands to the `PlayerLease` concept introduced in R06.  If no lease exists, treat the first control command as an implicit claim (subject to R06 rules) or reject the command.
* **Events**: Emit WebSocket events to all clients when playback state changes (play → pause, track skipped, volume changed).  Include relevant metadata (e.g. track index, new state).
* **Error handling**: Return clear error messages when unauthorised users attempt control actions, or when actions cannot be performed (e.g. no track to skip).

## Required context

* The `PlayerLease` mechanism defined in sprint R06.
* The persistent queue and vote tracking from sprint R07.
* The session token auth layer from R05.
* Understanding of the underlying player implementation to know how to trigger play, pause, skip and volume changes.

## Requirements

1. **API endpoints and WebSocket commands.**  Design HTTP endpoints or WebSocket messages for play, pause, skip, previous and volume adjustments.  Document each command’s parameters and response shape.
2. **Vote‑to‑skip logic.**  Implement a service to record skip votes from members.  The service should track votes per track and reset on track change.  Parameterise the threshold either by absolute count or percentage of active members.
3. **Lease enforcement.**  Check that the caller holds the current `PlayerLease` before executing direct commands (play, pause, previous, volume).  For skip, check for either lease or sufficient votes.
4. **State broadcast.**  Upon any control action, broadcast a `player_state_changed` event via WebSocket to all room participants.  Ensure that clients can update their UI accordingly.
5. **Tests.**  Add unit tests for vote tallying and command routing.  Write integration tests simulating multiple clients voting to skip and verifying that the skip occurs when the threshold is reached.
6. **Documentation.**  Update API documentation with control semantics, including error cases and authorisation requirements.

## Out of scope

* Implementing a GUI or mobile UI for control – only server‑side semantics and events are covered here.
* Playback device integration (e.g. controlling Spotify or YouTube players) – assume that an internal player service exists and can respond to commands.
* Persisting playback history or analytics.

## Implementation guidance

* Choose one channel (REST or WebSocket) as the primary control path.  WebSocket commands may provide lower latency, but REST endpoints offer simplicity for clients.  You can support both by translating HTTP requests into internal commands emitted on the WebSocket bus.
* Represent the vote‑to‑skip threshold in configuration (e.g. `SKIP_VOTE_THRESHOLD_PERCENT=50`).  For small rooms, consider a minimum number of votes to avoid one person skipping in a two‑person room.
* Store skip votes in a transient in‑memory data structure keyed by `(room_id, track_id)`.  No long‑term persistence is required; votes reset when the track changes or when the player is paused for an extended period.
* Use idempotent command handlers: if two identical play commands arrive back‑to‑back, ensure that the state remains consistent.

## Execution note

This stub serves as a high‑level plan for the **player control semantics** sprint.  When the sprint is taken up, refine the requirements, consult with stakeholders on skip vote thresholds and authorisation rules, and update this document with the final implementation details.  Upon completion, mark the sprint as **closed** in this document and in `ROOM_EPIC_SPRINT_SEQUENCE.md`.