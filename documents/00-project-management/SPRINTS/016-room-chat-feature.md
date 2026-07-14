# R11 – Room chat feature

**Status:** R11a implemented on `dev` (2026-07-14); accepted by the Product Owner (pending). R11b+ deferred.

**Sprint name:** Room chat feature

## Goal

Introduce a simple chat system within each room, enabling participants to send and receive text messages in real time.  Messages should be persisted for a reasonable period, broadcast to all current participants via WebSocket and retrievable via an API for clients joining mid‑conversation.  Basic moderation and message formatting rules must be established to prevent abuse.

## Current behaviour (pre‑R11a baseline)

The Local Music Queue offered no built‑in chat functionality.  Participants communicated out‑of‑band (e.g. in a separate chat app).  This limited collaboration around track selection and general conversation within the room context.

## R11a — Room chat contract and minimal vertical slice

R11a is the first narrow, usable slice of the legacy R11 bucket.  It is a **full‑stack runtime sprint**: backend (domain entity, repository, use case, HTTP handlers, WebSocket envelope) + frontend (api methods, store mutators, RoomView chat panel) + a new PG migration.  R11a introduces the minimal vertical slice needed for active room members to send a plain‑text message, fetch recent chat history, and receive new messages in real time inside the existing `RoomView`.

R11a is **intentionally not** the full R11 contract: it ships plain text only, no retention purge, no moderation surface, no audit log, no attachments, no markdown/rich text, no typing indicators, no read receipts, no WebSocket send command, no dedicated chat WebSocket endpoint, and no cross-process guarantees.  Richer sender profiles and lifecycle hardening are deferred to R10f+ and future R11b+ slices.

### Functional scope (R11a)

* **Message persistence** — `room_chat_messages` table (migration `0008_room_chat_messages`) with columns `id`, `room_id`, `sender_id`, `content`, `created_at`; index on `(room_id, created_at)` for the per-call history fetch; FK cascades from `rooms` and `users`.
* **Send message** — `POST /api/rooms/{slug}/chat/messages` behind `roomAuth`.  Any active member of an active room may post.  The active-room + active-membership gates are enforced at the use-case layer; archived rooms map to `409` and non-member callers map to `403`.
* **Receive messages** — `GET /api/rooms/{slug}/chat/messages?limit=50` behind `roomAuth`.  Returns `{"messages": [{ id, room_slug, sender: { user_id, display_name }, content, created_at }, …]}` ordered oldest → newest.  Default limit `50`, max `100`; out-of-range or non-numeric limits return `400`.
* **Real-time fan-out** — additive per-room WebSocket envelope `room_chat_message_created` on `/ws/rooms/{slug}`.  The global `/ws` 16-event inventory is unchanged.  Broadcasts fire AFTER a successful persistence.  The wire NEVER exposes the sender email.
* **Validation** — trim leading/trailing whitespace (Unicode-aware), normalize CRLF/CR to LF, reject empty after trim, reject content > 500 chars, plain text only (no HTML/rich-text processing).  Sender identity comes from the bearer/session actor — NEVER from the request body.  Room identity comes from the path slug — NEVER from the request body.
* **Frontend** — minimal `chat-panel` inside `RoomView`: history list (rendered as plain text via `{{ }}`, never `v-html`), input + Send button disabled when unauthenticated, disconnected, archived, removed, empty/whitespace-only, or a previous send is in flight.  Local cache capped at 100 entries.  Initial REST seed on mount + slug change; incoming WS events append and re-cap.

### REST contracts (R11a)

```
GET  /api/rooms/{slug}/chat/messages?limit=<1..100>   (default 50)
POST /api/rooms/{slug}/chat/messages
```

Both routes sit behind `roomAuth`; the actor user id is server-resolved from the bearer token.  The request body is exactly `{"content": "<plain text>"}` — no `sender_id` / `user_id` fields are accepted.

| Status | When |
|---|---|
| `200` | GET success — `{"messages": [...]}` oldest → newest |
| `201` | POST success — the post-mutation envelope (same shape as a list entry) |
| `400` | Invalid slug, invalid limit, empty-after-trim content, content > 500 chars |
| `401` | Missing session |
| `403` | Non-member caller |
| `404` | Unknown slug |
| `409` | Archived room |

### WebSocket envelope (R11a, per-room endpoint only)

`room_chat_message_created` rides `GET /ws/rooms/{slug}` only.  The global `/ws` 16-event inventory is unchanged.  Payload:

```json
{
  "type": "room_chat_message_created",
  "data": {
    "message": {
      "id": 123,
      "room_slug": "lobby",
      "sender": { "user_id": 42, "display_name": "Alex" },
      "content": "hello",
      "created_at": "2026-07-14T12:34:56Z"
    }
  },
  "seq_num": 17,
  "timestamp": "2026-07-14T12:34:56Z"
}
```

`display_name` is the resolved `user.DisplayName` with a safe `user #<id>` fallback when the user row's `display_name` is empty.  The wire NEVER carries the sender email.  The envelope matches the REST response shape so the frontend routes both through the same `applyRoomChatMessageCreated` store mutator.

### Files (R11a)

Backend:
* `internal/domain/entity/room_chat_message.go` — entity + `MaxChatContentLen` + `NormalizeChatContent`.
* `internal/domain/repository/room_chat_message_repository.go` — repo interface.
* `internal/infrastructure/persistence/migrations/postgres/0008_room_chat_messages.up.sql` (and matching `.down.sql`).
* `internal/infrastructure/persistence/postgres_room_chat_message_repository.go` — PG impl.
* `internal/usecase/roomchat/interactor.go` — use case (validation, gating, persistence, broadcast seam).
* `internal/delivery/http/room_chat_handlers.go` — `RoomChatHandlers{HandleListChatMessages, HandlePostChatMessage}`.
* `internal/delivery/ws/events.go` — `EventRoomChatMessageCreated` constant + `RoomChatMessage` / `RoomChatMessageSender` / `RoomChatMessageCreatedData` payload structs.
* `internal/delivery/ws/room_hub.go` — `BroadcastRoomChatMessageCreated` on `*RoomWSHub`.
* `cmd/server/main.go` — wire `pgRoomChat` repo, `roomChatInteractor`, `roomChatHandlers`, `roomChatBroadcasterAdapter`; register `GET /api/rooms/{slug}/chat/messages` and `POST /api/rooms/{slug}/chat/messages` under `roomAuth`.

Frontend:
* `frontend/src/services/api.js` — `getRoomChatMessages(slug, limit = 50)`, `sendRoomChatMessage(slug, content)`.
* `frontend/src/store/index.js` — `messages: []` slice on each `_ensureRoomEntry(slug)`; `setRoomChatMessages` (REST seed) + `applyRoomChatMessageCreated` (WS append) mutators; `MaxRoomChatMessages = 100` cap.
* `frontend/src/views/RoomView.vue` — `<section class="chat-panel">` (input + Send button + history list, plain text), `chatDraft` + `canChat` + `sendChat()` script logic, `seedRoomChatMessagesFromRest()` on mount + slug change, `case 'room_chat_message_created'` in `applyMessage`.

### Verification (R11a)

Backend (all pass):
* `go test -count=1 ./internal/usecase/roomchat/...`
* `go test -count=1 ./internal/delivery/http/...`
* `go test -count=1 ./internal/delivery/ws/...`
* `go test -count=1 ./internal/infrastructure/persistence/...`
* `go test -count=1 ./cmd/server/...`
* `go test -race -count=1 ./internal/delivery/ws/... ./internal/usecase/roomchat/...`

Frontend (all pass):
* `cd frontend && npm run test:unit -- --run`
* `cd frontend && npm run build`
* `git diff --check`

R11a preserves: the global `/ws` 16-event inventory, the R06 `room_archived` wire value + payload, the R07b/R07d/R09a/R09b/R09c/R09d/R09f/R10b per-room WebSocket events, the R05 in-memory session-token model, the schema version (now 8 after migration `0008_room_chat_messages`), the R10a members-list response wrapper, the global room-queue + auto-queue + vote + playback + member-removal surfaces, and the R10f+ deferred lifecycle hardening bucket.

### Deferred R11b+ scope (NOT in R11a)

* Message edit / delete endpoints and UI
* Reports / flags surface
* Audit log table + admin read surface
* Retention purge job (configurable window; cron-driven)
* Attachments / media
* Markdown / rich text / HTML
* Emoji picker
* Typing indicators
* Read receipts
* WebSocket command-based sending (so the chat panel can send without going through REST)
* Dedicated chat WebSocket endpoint (vs. reusing the existing per-room hub)
* Offline push notifications
* Cross-room / global chat
* Cross-process ordering and broadcast guarantees (multi-instance)
* Richer sender profile data (avatars, custom display names) in the wire envelope

R10f+ (deferred room lifecycle hardening) is unchanged by R11a.  The chat history is bound to the lifetime of the room under the current R11a scope; the chat table does NOT have a `room_id` retention trigger or a separate cleanup job.  Hard delete of archived rooms and a retention window for chat are both out of R11a scope.

## Original R11 requirements (preserved for reference)

The remaining R11 requirements from the original stub — moderation, paginated history by timestamp, audit logging, role hierarchies — are tracked as future R11b+ slices.  The original `006-room-architecture-adr-contract-plan.md` (R00) and `010-room-domain-invite-membership-lifecycle.md` (R04) set the architectural context this sprint conforms to.

## Required context

* The membership model from R04 — used to verify that senders are participants of the room.
* Session token authentication (R05) — resolves the sender identity.
* Player lease and queue semantics (R06/R07) are orthogonal; chat is independent.

## Requirements

1. **Database schema.**  R11a adds `room_chat_messages` (migration 0008) with appropriate indexes on `room_id` and `created_at` for fast retrieval.  R11a does NOT introduce a retention mechanism; that is R10f+ scope.
2. **Endpoints.**  R11a implements the POST endpoint for sending messages and the GET endpoint for retrieving message history.  R11a does NOT add a WebSocket send command.
3. **WebSocket events.**  R11a emits `room_chat_message_created` (per-room, additive on top of R07b/R07d/R09a/R09b/R09c/R09d/R09f/R10b).  Clients update their chat UI immediately on receipt.
4. **Moderation tools.**  Out of R11a scope — see "Deferred R11b+ scope" above.
5. **Validation and sanitisation.**  R11a trims leading/trailing whitespace, normalizes CRLF/CR to LF, enforces a 500-character max, and rejects empty content.  No HTML/rich text processing; the wire carries plain text only.
6. **Tests.**  R11a covers use case (send success, empty content, too-long content, non-member, archived room, list recent bounded oldest-to-newest), HTTP (GET/POST success shape and 400/401/403/404/409 mappings, no email leak), WebSocket (`room_chat_message_created` broadcast after successful persistence; no broadcast on failure), frontend (API endpoint/payload, history fetch on mount + slug change, send button disabled states, successful send clears input, WS chat event appends a message, message content rendered as plain text not HTML).
7. **Documentation.**  This file documents the R11a contract and the R11b+ deferred scope.  `PROJECT_STATE.md`, `SPRINTS/active.md`, and `ROOM_EPIC_SPRINT_SEQUENCE.md` are updated narrowly to mark R10e accepted, R11a current/active, R11 still the broader feature bucket, and R12/R13/R14 still planned.

## Out of scope (R11a)

All of the items listed under "Deferred R11b+ scope" above; see also the R10f+ bucket.