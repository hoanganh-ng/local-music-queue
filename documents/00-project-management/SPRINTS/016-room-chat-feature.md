# R11 – Room chat feature

**Status:** planned (stub – work not yet started)

**Sprint name:** Room chat feature

## Goal

Introduce a simple chat system within each room, enabling participants to send and receive text messages in real time.  Messages should be persisted for a reasonable period, broadcast to all current participants via WebSocket and retrievable via an API for clients joining mid‑conversation.  Basic moderation and message formatting rules must be established to prevent abuse.

## Current behaviour (pre‑sprint baseline)

The Local Music Queue currently offers no built‑in chat functionality.  Participants may communicate out‑of‑band (e.g. in a separate chat app), but there is no integrated messaging system.  This limits collaboration around track selection and general conversation within the room context.

## Desired behaviour (post‑sprint)

* **Message persistence**: Create a `room_chat_messages` table with fields such as `id`, `room_id`, `sender_id`, `content`, `created_at` and optionally `edited_at` or `deleted_at`.  Messages should be stored for a configurable retention period (e.g. 7 days) before being purged.
* **Send message** endpoint or WebSocket command: Allow authenticated room participants to send a message.  Validate that the user is a current member and that the message content conforms to length and content rules (e.g. no excessive length or banned words).
* **Receive messages**: Broadcast new messages to all connected clients via WebSocket.  Provide pagination or time‑based retrieval via `GET /rooms/{id}/chat?before=<timestamp>` so that clients joining later can fetch recent history.
* **Moderation**: Define a minimal moderation policy.  Owners and possibly lease holders should be able to delete messages.  Support a flag or report mechanism for abusive content.  Store moderation actions in an audit log.
* **Formatting**: Support plain text with minimal formatting (e.g. newline separation).  Explicitly disallow rich text or HTML to prevent injection attacks.  If needed, escape special characters server‑side.
* **Privacy**: Do not expose private user details.  When returning messages, include only the sender’s display name or anonymised identifier, not their email or full user ID.

## Required context

* The membership model from R04 – used to verify that senders are participants of the room.
* Session token authentication (R05) to resolve the sender identity.
* Player lease and queue semantics (R06/R07) are orthogonal but may inform moderator roles (e.g. lease holder as temporary moderator).

## Requirements

1. **Database schema.**  Add a `room_chat_messages` table with appropriate indexes on `room_id` and `created_at` for fast retrieval.  Provide a retention mechanism (e.g. scheduled job) to purge old messages.
2. **Endpoints.**  Implement a POST endpoint for sending messages and a GET endpoint for retrieving message history.  Optionally support WebSocket commands for sending to reduce latency.
3. **WebSocket events.**  Emit a `chat_message` event with message content and metadata when a new message is saved.  Clients should update their chat UI immediately.
4. **Moderation tools.**  Add endpoints or commands for message deletion.  Restrict deletion to room owners or designated moderators.  Log moderation actions.
5. **Validation and sanitisation.**  Trim leading/trailing whitespace, enforce maximum length (e.g. 2 000 characters) and escape special characters.  Optionally filter profanity or provide hooks for a profanity filter.
6. **Tests.**  Add tests for message creation, retrieval, WebSocket broadcasting, and moderation workflows.  Include tests for unauthorized attempts to send or delete messages.
7. **Documentation.**  Update API docs with chat endpoints, message event schema and moderation capabilities.

## Out of scope

* File or media attachments – chat is limited to plain text in this sprint.
* Advanced formatting (markdown, emojis) – these can be considered later.
* Push notifications or offline message delivery.
* Complex moderation frameworks (e.g. role hierarchies, automated blocking) – this sprint aims for a minimal viable moderation feature set.

## Implementation guidance

* Use a WebSocket channel dedicated to chat messages to avoid mixing chat and queue/player events.  Alternatively, namespace chat events under a single WebSocket stream with clear event types.
* For message retention, a simple cron or scheduled task can periodically delete messages older than a configured threshold.  Make the threshold configurable via environment variables.
* Consider using a UUID or ULID for message IDs to ensure ordering across distributed systems.
* Ensure chat retrieval endpoints paginate by creation timestamp rather than offset, as offsets can become inefficient with large message volumes.

## Execution note

This stub defines the proposed scope for the **room chat feature** sprint.  During shaping, confirm message retention policies and moderation roles with stakeholders.  Once implemented, summarise the real behaviour, adjust documentation accordingly and set the status to **closed** in this file and in `ROOM_EPIC_SPRINT_SEQUENCE.md`.