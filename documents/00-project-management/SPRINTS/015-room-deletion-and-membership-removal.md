# R10 – Room deletion and membership removal

**Status:** planned (stub – work not yet started)

**Sprint name:** Room deletion and membership removal

## Goal

Allow room owners to delete a room and remove members.  Deleting a room should cascade through related data (playback queue, player leases, invites, memberships) without leaving orphaned records.  Removing a member should revoke their session token for that room and prevent future access.  The sprint also aims to formalise the concept of room ownership and provide audit logs of deletion/removal actions.

## Current behaviour (pre‑sprint baseline)

Rooms, once created, persist indefinitely.  There is no API to delete a room, nor is there a way to forcibly remove a member other than them leaving voluntarily.  Data associated with a room (e.g. queue items, invites) remain in the database even if all participants leave.  Consequently, the database may accumulate unused rooms and data, and owners have no control over membership.

## Desired behaviour (post‑sprint)

* **Delete room** endpoint: Introduce an authenticated endpoint (e.g. `DELETE /rooms/{id}`) that allows the room owner to delete the room.  Deletion should remove or archive all associated records (queue items, leases, invites, chat messages if any) in a transaction.  Respond with a confirmation and broadcast a `room_deleted` event via WebSocket so connected clients can redirect.
* **Remove member** endpoint: Provide an endpoint (e.g. `DELETE /rooms/{id}/members/{user_id}`) for the room owner to remove a member.  Removing a member should invalidate their session token for that room and remove them from any active lease or vote counts.  Notify the removed user via WebSocket and emit a membership update event to remaining members.
* **Ownership checks**: Enforce that only the room owner can delete the room or remove members.  If the owner is transferring ownership or if the owner wants to leave, require that ownership is transferred first.  Define clear responses when non‑owners attempt these actions.
* **Cascade handling**: Use database constraints or application logic to cascade deletes.  Alternatively, soft‑delete rooms and related data by marking them as archived, then clean them up in a background job.
* **Audit logging**: Record deletion and removal actions in an audit log table with timestamps, acting user ID, target IDs and reason (if provided).  Provide a simple way for administrators to review these logs.

## Required context

* The membership and invite lifecycle implemented in sprint R04.
* The session token authentication and lease semantics (R05/R06).  Removing a member must revoke their tokens and leases.
* Database schema details for rooms, queue items, leases, invites, chat messages (future sprint), etc., to ensure correct cascade deletion.

## Requirements

1. **Database changes.**  Introduce foreign key constraints with `ON DELETE CASCADE` or implement a soft‑delete flag on rooms and related tables.  Decide whether to hard‑delete or soft‑delete based on regulatory requirements.
2. **Endpoints.**  Implement secure endpoints for room deletion and member removal.  Apply middleware for session resolution and ownership checks.  Accept optional JSON body containing a reason or confirmation flag.
3. **Session invalidation.**  When a member is removed, invalidate any associated session tokens and active WebSocket connections.  Optionally send a close code over WebSocket explaining the reason.
4. **Event broadcasting.**  Emit appropriate WebSocket events (`room_deleted`, `member_removed`) so clients can handle UI updates and redirection.
5. **Audit log.**  Create a new table for audit entries if not already existing.  Log room deletion and member removal events with metadata.
6. **Tests.**  Add tests covering deletion, removal, cascade behaviour and permission checks.  Test both hard‑delete and soft‑delete scenarios if supported.
7. **Documentation.**  Update API docs to include the new endpoints and explain the consequences of deletion and removal.  Clarify that deletion is irreversible (if hard‑delete) or explain how long soft‑deleted data is retained.

## Out of scope

* Automated cleanup or cron jobs for soft‑deleted data – this may be implemented later.
* Front‑end UI changes for deletion/removal – although necessary for user experience, they are outside the scope of server implementation.
* Transfer of ownership beyond what was already provided in the membership lifecycle sprint; enhancements can be part of a later sprint if needed.

## Implementation guidance

* Prefer transactions when deleting a room to ensure that either all associated data is removed or none is (in case of failure).  This avoids partial state.
* To minimise the risk of accidental deletion, require a confirmation flag or a secondary check (e.g. the client must send the room name) before executing the delete.  Log the confirmation input for audit purposes.
* For member removal, check that the target user is a member of the room and is not the owner.  If they hold the current `PlayerLease`, release or transfer the lease before removal.
* If soft‑delete is chosen, ensure that all queries respect the `archived` flag so that deleted rooms do not appear in listings or match invites.  Provide a separate admin interface to view or restore archived rooms.

## Execution note

This document serves as a planning stub for the **room deletion and membership removal** sprint.  As part of sprint shaping, refine the cascade strategy (hard vs. soft delete) and confirm audit log requirements.  After implementation, update this document with real outcomes and mark the sprint as **closed** in both this file and in `ROOM_EPIC_SPRINT_SEQUENCE.md`.