# R11 – Room chat feature

**Status:** R11a — Room chat contract and minimal vertical slice — is **closed on `dev` (2026-07-15)** and **accepted by the Product Owner on 2026-07-15**. R11a was implemented on `dev` (2026-07-14) with a corrective pass (2026-07-14), a final corrective pass (2026-07-14), and a deferred lifecycle follow-up (2026-07-15) for the chat bootstrap / recovery guarantees, the 500-Unicode-code-point input behavior, the stale-comment sweep, and the stale-onGap-finalization guard. R11a is **no longer current/active**; R11b+ and R10f+ remain planned and are NOT marked active; R12, R13, and R14 remain planned and are NOT marked active.

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
* **Validation** — trim leading/trailing whitespace (Unicode-aware), normalize CRLF/CR to LF, reject empty after trim, reject content > 500 Unicode code points, plain text only (no HTML/rich-text processing).  Sender identity comes from the bearer/session actor — NEVER from the request body.  Room identity comes from the path slug — NEVER from the request body.
* **Frontend** — minimal `chat-panel` inside `RoomView`: history list (rendered as plain text via `{{ }}`, never `v-html`), input + Send button disabled when unauthenticated, disconnected, archived, removed, empty/whitespace-only, or a previous send is in flight.  Local cache capped at 100 entries.  History seed is fired AFTER the first per-room `room_queue_sync` event (the per-room hub's confirmation that the client is registered) and runs at most once per WS connection via a per-connection flag; incoming WS events append and re-cap.

### REST contracts (R11a)

```
GET  /api/rooms/{slug}/chat/messages?limit=<1..100>   (default 50)
POST /api/rooms/{slug}/chat/messages
```

Both routes sit behind `roomAuth`; the actor user id is server-resolved from the bearer token.  The request body is exactly `{"content": "<plain text>"}` — no `sender_id` / `user_id` fields are accepted.

GET response (200):

```json
{
  "messages": [
    { "id": 123, "room_slug": "lobby",
      "sender": { "user_id": 42, "display_name": "Alex" },
      "content": "hello", "created_at": "..." }
  ]
}
```

POST response (201, **wrapped under `message`**, corrective pass):

```json
{
  "message": {
    "id": 123, "room_slug": "lobby",
    "sender": { "user_id": 42, "display_name": "Alex" },
    "content": "hello", "created_at": "..."
  }
}
```

The POST 201 body is wrapped under `message` so the wire shape mirrors the `room_chat_message_created` WS data payload and the frontend can route both the REST response and the later WS event through the same store merge path. Without the wrapper, the POST body would be indistinguishable from a single list entry, which would break the symmetry the R11a frontend store merge relies on. Both responses never expose the sender email; `display_name` falls back to `user #<id>` when the user row's `display_name` is empty.

| Status | When |
|---|---|
| `200` | GET success — `{"messages": [...]}` oldest → newest |
| `201` | POST success — `{"message": { ... }}` (wrapped envelope, corrective pass) |
| `400` | Invalid slug, invalid limit, empty-after-trim content, content > 500 code points |
| `401` | Missing session |
| `403` | Non-member caller |
| `404` | Unknown slug |
| `409` | Archived room |
| `500` | Internal server error — generic body; the detailed cause is logged server-side (no PII) |

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
* `frontend/src/views/RoomView.vue` — `<section class="chat-panel">` (input + Send button + history list, plain text), `chatDraft` + `canEditChat` / `canSendChat` + `sendChat()` script logic, `seedRoomChatMessagesFromRest()` triggered by the first per-room `room_queue_sync` event and re-seeded on slug change, `case 'room_chat_message_created'` in `applyMessage`.

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

### Corrective pass (2026-07-14, still pending Product Owner acceptance)

R11a landed on `dev` but a narrow review pass surfaced four contract / consistency defects that the corrective pass fixes without changing the R11a functional scope. The corrective pass does NOT advance the sprint.

1. **Restored approved POST contract.** `POST /api/rooms/{slug}/chat/messages` now returns 201 with `{ "message": { id, room_slug, sender, content, created_at } }` so the wire shape matches the `room_chat_message_created` WS data payload. The prior landing shipped a bare envelope; the corrective pass wraps the response so the frontend can apply both the POST response and the later WS event through the same store merge path. `GET /api/rooms/{slug}/chat/messages` keeps `{"messages": [...]}`; the WS envelope keeps `{"message": ...}`. The room_chat_message_created WS event is unchanged.

2. **Display-name lookup failure short-circuits Send BEFORE persistence.** The use case now resolves the sender display name AFTER room + membership resolution and BEFORE `CreateMessage`. A `sql.ErrNoRows` from the user repo returns `ErrSenderNotFound` (500). A general repository error is wrapped and mapped to 500. In both cases no row is persisted and no broadcast fires. This prevents a sender whose users row is missing from silently leaking a `{ message: { sender: { display_name: "" } } }` envelope to the room. The new use-case tests `TestSend_DisplayNameMissingUser_NoPersistNoBroadcast` and `TestSend_DisplayNameRepoError_NoPersistNoBroadcast` pin the invariant.

3. **Closed the REST-history / WebSocket registration race.** The corrective pass removes the pre-WS-registration `GET /chat/messages` call from `RoomView.onMounted` and the slug watcher. The history GET is now triggered by the first `room_queue_sync` event (the per-room hub's confirmation that the client is registered) and runs at most once per connection (`chatHistoryFetched` flag, reset on teardown / slug change). The history seed is a MERGE, not a replace: the store dedupes by message id, sorts oldest → newest by `created_at` (with id as the deterministic tie-breaker), and caps at `MaxRoomChatMessages`. A WS event that arrives while the history GET is in flight produces exactly one row, not two (the store merge collapses on id collision). The `seedRoomChatMessagesFromRest(targetSlug)` helper now captures the slug at call time and re-checks it on resolution so a stale GET (the user navigated away while the GET was in flight) does NOT pollute the new room's cache.

4. **Recover chat state on sequence gaps.** `RoomView.onGap` now also fetches recent chat history and merges it into the local cache. The merge is by id so messages already received via the WS events stay present (no destructive clear on a successful recovery). A failed chat recovery logs the error and keeps the prior messages intact. The existing queue recovery is preserved.

5. **Enforced 500 Unicode code points.** The backend switched from byte-length to `utf8.RuneCountInString`, counted AFTER trim and CRLF/CR normalization. The frontend mirrors this with `Array.from(chatDraft).length` (the native `maxlength="500"` counts UTF-16 code units, which under-counts for BMP code points and over-counts for surrogate pairs). The Send button is disabled when the draft exceeds 500 code points, and an over-limit hint is rendered. Server validation remains authoritative. New tests cover: 500 / 501 non-ASCII code points, mixed ASCII + non-ASCII boundary, and CRLF normalization before the count.

6. **Generic 500 with no internal-detail leak.** Unexpected errors from the chat use case are mapped to a generic "internal server error" body; the detailed error is logged server-side via `log.Printf` with the route slug and the actor id (no request body, header, cookie, or bearer token). `ErrSenderNotFound` keeps a 500 with a stable body string for the missing-user invariant violation.

The corrective pass touches the same files as the original R11a landing and does NOT introduce new runtime surface, new migrations, new WebSocket events, or new REST routes. The full backend + frontend test suite is re-verified; the corrective pass is part of R11a (not a separate sprint) and R11a is still current/pending Product Owner review.

### Final corrective pass (2026-07-14, still pending Product Owner acceptance)

A focused second pass closed four frontend-only defects on top of the prior corrective pass. The sprint is NOT advanced; R11a is still current/pending review. Backend, migration, REST, WS, and store contracts are unchanged.

1. **Retry the initial history seed after failure.** The post-sync chat seed is gated on two per-connection flags: `chatHistoryFetched` (true only after a successful merge) and `chatHistoryFetchInFlight` (true while a GET is pending). A failed GET leaves `chatHistoryFetched=false` so a later `room_queue_sync` is allowed to retry; `chatHistoryFetchInFlight` is cleared in `finally` so a future sync is never permanently gated. A second sync arriving while the first GET is in flight is suppressed by the in-flight flag — exactly one network request for the seed. Both flags are reset on teardown and on slug change.
2. **Recover chat independently when a sequence gap occurs.** `onGap` now attempts queue recovery AND chat history recovery through two independent async operations that each succeed or fail on their own. A queue failure does NOT skip the chat history fetch and merge. A chat failure does NOT clear existing chat messages or block queue recovery. Existing queue error/toast behavior is preserved. Each operation re-checks the target slug on resolution so a stale GET (the user navigated away) does not pollute the new room.
3. **Correct the frontend 500-code-point input behavior.** The native `maxlength="500"` attribute (which counts UTF-16 code units and broke valid 500-emoji messages) has been removed. The input stays enabled when the draft exceeds 500 code points so the user can shorten it. The view exposes two separate gates: `canEditChat` (auth + connected + active member + not archived/removed + no send in flight) and `canSendChat` (`canEditChat` + non-empty + ≤ 500 code points). Server validation remains authoritative. An over-limit hint is rendered when the draft exceeds the cap.
4. **Stale-comment sweep.** Comments that still claimed the POST returned a bare list entry, that the POST response was not applied to the local store, that the store did not deduplicate, that the limit was measured in bytes or generic chars, or that `strings.TrimSpace` removed zero-width spaces, were corrected against the post-corrective-pass runtime contracts. No behavior changes were made during this sweep.

The final corrective pass is part of R11a (not a separate sprint). It adds no new files, no new runtime surface, no new migrations, no new WebSocket events, and no new REST routes. The full frontend test suite is verified; `git diff --check` is clean; the frontend build is clean. R11b+, R10f+, R12, R13, and R14 are unchanged.

### Deferred lifecycle follow-up (2026-07-14, still pending Product Owner acceptance)

A focused hardening pass on the chat-history seed state machine, the queue-recovery gap suppression, and the stale-recovery finalization path. The accepted R11a REST, WebSocket, persistence, authorization, and UI contracts are NOT changed — this is a frontend-only refactor of internal flags and store mutators that is invisible to the wire. R11a is still current/pending Product Owner review; R11b+, R10f+, R12, R13, and R14 are unchanged.

1. **Connection-generation-scoped chat-history seed state.** The `chatHistoryFetched` and `chatHistoryFetchInFlight` flags are now scoped to a numeric `chatGeneration` token. Each new mount and each slug change bumps the token. A pending `seedRoomChatMessagesFromRest` GET records the generation it was started in; on resolve (success, failure, or `finally`) it only mutates the flags when its generation still matches the current token. A slow GET that was started for an OLD connection (the user already navigated away) can never flip the NEW connection's fetched / in-flight flags.
2. **Generation-scoped `suppressSeqGap` + release timer.** The `suppressSeqGap` flag and its 50 ms release `setTimeout` are now scoped to a numeric `seqGapGeneration` token. `teardownCurrentClient` (called on unmount AND on the slug watcher) cancels any pending release timer and bumps the generation. The `setTimeout` closure captures the generation it was queued in; if a teardown or slug change has advanced the generation by the time the timer fires, the closure short-circuits without touching the flag — a stale timer cannot re-enable gap detection for the next connection.
3. **Stale onGap finalization does not recreate a cleared old-room entry.** `onGap` recovery now writes through new `setRoomQueueStateIfExists`, `setRoomQueueErrorIfExists`, and `setRoomChatMessagesIfExists` store mutators. The standard mutators still call `_ensureRoomEntry` (correct for live-author paths: WS events, active-room seeds) and are intentionally NOT changed. The `IfExists` variants are a no-op when the slug's entry was already cleared by teardown / slug change, so a stale onGap resolution that lands after the old room's entry has been removed cannot recreate it. The queue and chat recovery paths each additionally re-check the connection generation (`myGapGeneration !== seqGapGeneration`) so the recovery is dropped if a teardown or slug change advanced the generation while the GET was in flight.
4. **onGap `finally` recovery finalization guard.** A new `setRoomQueueRecoveryInFlightIfExists` store mutator is used by `onGap`'s `finally` block. The standard `setRoomQueueRecoveryInFlight` calls `_ensureRoomEntry`, which would recreate a cleared old-room entry with `recoveryInFlight = false` on a stale onGap finalization. The `IfExists` variant is a no-op when the slug's entry has already been removed. The `finally` block additionally gates the call on `myGapGeneration === seqGapGeneration && targetSlug === slug.value` — a stale recovery from an old connection MUST NOT clear or modify the new room's recovery state, must NOT show a stale toast, and must NOT change chat or queue state. `onGap`'s initial `recoveryInFlight = true` set at the top of the function was also switched to the `IfExists` variant for consistency.
5. **Lifecycle test.** A real onGap lifecycle test exercises the full path: mount at `/rooms/lobby`, capture the lobby WS client, fire onGap (both recovery GETs reject), navigate to `/rooms/lounge` so teardown clears the lobby entry and bumps the seqGapGeneration token, confirm the lounge establishes its own entry, await the original onGap promise, and assert: the lobby entry remains absent (no recreation), the lounge entry remains present and unchanged, the lounge `recoveryInFlight` is not touched by the stale finally, no `lastError` is set on the new room, and no toast is raised. New store-level unit tests cover each `IfExists` mutator (state, error, messages, recoveryInFlight) for the "no entry" and "existing entry" branches.

The deferred lifecycle follow-up is part of R11a (not a separate sprint). It adds no new files, no new runtime surface, no new migrations, no new WebSocket events, no new REST routes, and no changes to the user-visible chat behavior. The full frontend test suite (305 tests) is verified; `git diff --check` is clean; the frontend build is clean. R11b+, R10f+, R12, R13, and R14 are unchanged.

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
5. **Validation and sanitisation.**  R11a trims leading/trailing whitespace, normalizes CRLF/CR to LF, enforces a 500 Unicode code point max (server-side via `utf8.RuneCountInString`, client-side via `Array.from(str).length`), and rejects empty content.  No HTML/rich text processing; the wire carries plain text only.
6. **Tests.**  R11a covers use case (send success, empty content, too-long content, non-member, archived room, list recent bounded oldest-to-newest), HTTP (GET/POST success shape and 400/401/403/404/409 mappings, no email leak), WebSocket (`room_chat_message_created` broadcast after successful persistence; no broadcast on failure), frontend (API endpoint/payload, history fetch after the first per-room `room_queue_sync` and re-seeds on slug change, send button disabled states, successful send clears input, WS chat event appends a message, message content rendered as plain text not HTML, 500 Unicode code-point input enforcement).
7. **Documentation.**  This file documents the R11a contract and the R11b+ deferred scope.  `PROJECT_STATE.md`, `SPRINTS/active.md`, and `ROOM_EPIC_SPRINT_SEQUENCE.md` are updated narrowly to mark R11a closed/accepted, R11b+ and R10f+ still planned and NOT active, and R12/R13/R14 still planned and NOT active.

## Out of scope (R11a)

All of the items listed under "Deferred R11b+ scope" above; see also the R10f+ bucket.

## R11a closure (2026-07-15)

* **Closed on `dev`:** 2026-07-15.
* **Accepted by the Product Owner:** 2026-07-15.
* **Next active sprint:** none.  R11b+ and R10f+ remain planned and are NOT marked active.  R12, R13, and R14 remain planned and are NOT marked active.
* **Final verification count:** 305 frontend tests (see `frontend/src/views/__tests__/RoomView.spec.js`, `frontend/src/store/__tests__/roomStore.spec.js`, `frontend/src/services/__tests__/roomApi.spec.js`, and the rest of the frontend test suite).  `cd frontend && npm run test:unit -- --run` → 305/305 pass.  `cd frontend && npm run build` → clean.  `git diff --check` → clean.  Backend R11a test surface (the use case + HTTP + WebSocket + persistence + cmd/server suites) is unchanged from the previous corrective pass and remains green.
* **Runtime contracts preserved:** the accepted R11a REST contract, the additive per-room `room_chat_message_created` WebSocket envelope, the `room_chat_messages` table and migration `0008_room_chat_messages` (schema version 8), the `utf8.RuneCountInString` 500-Unicode-code-point cap, the sender identity resolution (bearer / session, never from the request body), the `display_name` fallback to `user #<id>`, and the chat panel UI (no `maxlength` attribute, `canEditChat` / `canSendChat` split, plain-text rendering, history seed after the first per-room `room_queue_sync`) are all unchanged by the closure pass.  R11a is closed exactly as it stood after the deferred lifecycle follow-up.
* **No runtime code was changed by this closure pass.**  The closure pass updates project-management documentation only.
