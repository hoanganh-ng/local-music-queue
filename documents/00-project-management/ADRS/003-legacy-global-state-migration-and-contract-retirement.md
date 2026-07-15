# ADR 003 — Legacy global-state migration and contract retirement

- Status: Proposed — R14a contract; pending Architect review and Product Owner acceptance
- Date: 2026-07-15
- Scope: Reconciles several paragraphs of ADR 001 (and one paragraph cluster of ADR 002) that have become inaccurate as the room epic landed in the renumbered R06–R11 sequence. Records the new R14a implementation-ready contract for converting the legacy global state into exactly one Product-Owner-named room and for retiring the parallel global REST/WS contracts. ADR 003 does **not** replace ADR 001's room architecture in general; it supersedes only the specific paragraphs listed below. ADR 003 is documentation-only: no runtime change is authorized by it.

## 1. Context

The verified `dev` baseline after accepted R11a has two parallel product models:

- PostgreSQL is the only runtime database (ADR 002 — R01/R02/R03).
- Legacy global persistence still exists: singleton `queue_state` (`id = 1`, TEXT JSON), append-only `activities`, singleton `auto_queue_config` (`id = 1`), and `play_history` (50-row cap, enforced in Go).
- Room persistence also exists: `rooms` / `room_members` / `room_invites` (R04, migration 0004); `room_queue_state` (R07a, JSONB per `rooms.id`, migration 0006); `room_auto_queue_config` + `room_play_history` (R09f, migration 0007); `player_leases` (R06, migration 0005); `room_chat_messages` (R11a, migration 0008).
- Global and room-scoped runtime contracts coexist: global `/api/queue/...`, `/api/vote/...`, `/api/autoqueue/...`, `/ws`; room-scoped `/api/rooms/{slug}/...` and `/ws/rooms/{slug}`.
- The frontend is substantially room-aware (`DashboardView.vue` is global-only; `RoomView.vue` reuses `globalStore.currentUser` and `globalStore.roomQueues[slug]`).
- No migration has yet converted the global state into a PO-named room as the sole source of truth.
- No permanent `main` room, `room 0`, or hidden default room is allowed by ADR 001 §3 Decision 2 and Epic #17.

Several ADR 001 paragraphs — and one ADR 002 paragraph cluster (§11/§13) — were written before the room epic was renumbered and before the shim plan was deferred indefinitely. They are now historically inaccurate and must be reconciled before the R14 migration sprint can ship safely.

## 2. Supersession statement

ADR 003 supersedes **only** the following paragraphs of ADR 001 and one paragraph cluster of ADR 002. ADR 003 does NOT replace ADR 001's room architecture in general. ADR 001 remains authoritative for the decisions that ADR 003 does not replace, including: explicit rooms and no permanent default room; room lifecycle (create → active → archived); membership roles (host / admin / guest); one host per active room; player-lease separation; invite-token principles; per-room REST/WebSocket direction; single-instance limitations; account-scoped priority unless separately changed.

Superseded ADR 001 paragraphs:

- §3 Decision 6 — the compatibility shim was never built; old global routes still operate on legacy global repositories.
- §9 transition-strategy table — the R07 row's "compatibility shim that resolves the single migrated room by default … logged at startup" was never implemented; no migrated room exists today.
- §9 final route shape list — uses `{roomId}` (the code uses `{slug}`) and lists room routes that were never built in R07/R08 (`.../queue/skip|status|sync|ended|prev|volume`, `.../user/priority-balance`, `.../youtube/search`, `.../vote/prioritize`); the actual playback route family is `/api/rooms/{slug}/playback/{status,sync,skip,ended,prev,volume}`.
- §10 global `/ws` backward-compatibility paragraph — "`/ws` … serves a synthesized 'default room' view backed by the migrated room." This paragraph was never implemented. `/ws` serves the real global `queue_state` JSON today, with no default room; R14c will retire `/ws` with `410 Gone` rather than add a default-room fallback.
- §11 migration direction paragraph — "global `queue_state` row becomes the migrated room's … R06 converts … global rows are deleted at the end of R06." R06 became Player Lease; the global rows were NOT deleted and remain the live source of truth. The conversion is exactly what R14a now shapes.
- §11 table row — "Room-scoped activities … existing global `activities` table is migrated." There is currently no `room_activities` table; the migration target is a NEW table designed by R14a (see §5 below).
- §15 compatibility table — the R07/R08 shim + "`/ws` deprecated but live / default room" rows are inaccurate for the same reasons.
- §18 consequences mapping — the sprint → scope map (R06/R07/R08/R10/R11/R12) does NOT match the renumbered epic (R06 = Player Lease; R07 = Room queue; R09 = playback/vote/auto-queue; R10 = deletion; R11 = chat; R12 = search).

Superseded ADR 002 paragraph cluster:

- §11 (`setval` / R06 fold attribution) and §13 (R03 migrator reuse) repeatedly attribute the global → room fold to "R06". This attribution was never implemented; R06 became Player Lease. The migrator-reuse shape ADR 002 anticipated (via a `room_id` parameter) is the R14b mechanism, not R06. R14b is the slice that fulfils this attribution; R14a shapes it.

## 3. Per-section supersession detail

### 3.1 ADR 001 §3 Decision 6 — compatibility shim

Old: "Room context is mandatory on every queue, playback, voting, auto-queue, invite, member, and priority REST call after the migration sprint, and on the WebSocket connection. The old global routes transition through a documented compatibility shim to a final `410 Gone`."

Verified current behaviour: no shim exists. `cmd/server/main.go` still registers the legacy global routes (lines 389–409) operating on the legacy `QueueRepository` / `AutoQueueRepository`. Old routes return 200/4xx from legacy repositories; they do NOT resolve a migrated room.

New R14a decision: the retirement path is **explicit `410 Gone`** (Phase B), not a shim. A proxy may only be selected if identity, membership, and room-selection behaviour are unambiguous; R14a does not have that evidence, so the recommended path is `410 Gone` with a stable machine-readable error and documentation pointer. See §6 below.

### 3.2 ADR 001 §9 transition-strategy table — R07 row

Old: "R07 … compatibility shim that resolves the single migrated room by default … logged at startup."

Verified current behaviour: no migrated room exists today; the shim was never built.

New R14a decision: removed. The R07 room-queue slice landed as `POST/GET /api/rooms/{slug}/queue/...` (R07a) + the per-room WebSocket delta family (R07b) + frontend narrow wiring (R07c) + the `queue/prioritize` mutation (R07d). No shim. The migration sprint is R14a → R14b → R14c → R14d → R14e (see §7).

### 3.3 ADR 001 §9 final route shape list

Old: uses `{roomId}` as the path parameter; lists `.../queue/skip`, `.../queue/status`, `.../queue/sync`, `.../queue/ended`, `.../queue/prev`, `.../queue/volume`, `.../user/priority-balance`, `.../youtube/search`, `.../vote/prioritize` under the room scope.

Verified current behaviour: path parameter is `{slug}` (not `{roomId}`); the queue mutations live under `.../queue/{add,remove,clear,prioritize}`; the playback mutations live under `.../playback/{status,sync,skip,ended,prev,volume}`; `/api/rooms/{slug}/user/priority-balance` was never built (priority remains account-scoped per ADR 001 §3 Decision 9); `/api/rooms/{slug}/youtube/search` was never built (search remains global utility); `/api/rooms/{slug}/vote/prioritize` was never built (the room vote parity sprint is R09h, see §6).

New R14a decision: ADR 001 §9's route list is replaced by the actual room route inventory in R14a `022-…md` §3. R14a records the four `/api/queue/...`, `/api/vote/skip`, `/api/autoqueue/...`, and global `/ws` routes as the explicit retirement set. `/api/vote/prioritize` `410` is BLOCKED on R09h landing and acceptance.

### 3.4 ADR 001 §10 `/ws` default-room synthesis

Old: "`/ws` … serves a synthesized 'default room' view backed by the migrated room."

Verified current behaviour: `/ws` is built by `ws.NewHub(qInteractor.GetState)` at `cmd/server/main.go:232` and registers via `mux.HandleFunc("/ws", hub.RegisterHandler)` at `main.go:588`. The hub reads `qInteractor.GetState`, which serializes the legacy global `*entity.Queue` from `queue_state` (R00–R05 baseline). There is no default room. The 16-event global inventory is unchanged from R00; per-room events live on `/ws/rooms/{slug}` only.

New R14a decision: R14a explicitly rejects the synthesized default-room `/ws` plan. R14c returns `410 Gone` on `/ws`; no synthesized fallback is permitted. See §6 below.

### 3.5 ADR 001 §11 migration direction — R06 fold

Old: "global `queue_state` row becomes the migrated room's … R06 converts … global rows are deleted at the end of R06."

Verified current behaviour: R06 became Player Lease (`011-player-lease-and-host-departure-semantics.md`). The global `queue_state`, `activities`, `auto_queue_config`, and `play_history` rows were NOT deleted; they remain the live source of truth.

New R14a decision: the global → room conversion is the R14a → R14b → R14c → R14d → R14e sprint sequence. See §7.

### 3.6 ADR 001 §11 — Room-scoped activities row

Old: "Room-scoped activities … existing global `activities` table is migrated."

Verified current behaviour: there is no `room_activities` table, migration, or repository. `grep -rn "room_activities\|RoomActivit"` returns zero matches across `*.go`, `*.sql`, `*.js`.

New R14a decision: R14a designs a new `room_activities` table (see §5). R14b builds it. The migrator copies every legacy row losslessly (preserved id/timestamp/type/user/description/count + sequence resync) and uses a dedicated narrow repository contract `AddActivity(ctx, roomID, entity.Activity) error` + `GetActivities(ctx, roomID, limit) ([]entity.Activity, error)` that is NOT coupled to `QueueRepository`.

### 3.7 ADR 001 §15 compatibility table — shim + default `/ws` rows

Old: rows describing the R07/R08 shim and "`/ws` deprecated but live / default room."

Verified current behaviour: same as §3.1, §3.4.

New R14a decision: replaced by the Phase A–D retirement sequence in §6.

### 3.8 ADR 001 §18 consequences mapping — sprint → scope map

Old: maps R06 = Room-Scoped Persistence, R07 = REST + shim, R08 = WS, R10 = Auto-Queue, R11 = Welcome/Create/Invite/Join, R12 = Dashboard.

Verified current behaviour: the renumbered epic is R06 = Player Lease; R07 = Room queue; R09 = playback/vote/auto-queue; R10 = deletion; R11 = chat; R12 = search.

New R14a decision: the §18 map is replaced by the room epic sequence as it actually landed in `ROOM_EPIC_SPRINT_SEQUENCE.md`. R14a is documented in `022-…md` and tracked in the sequence doc.

### 3.9 ADR 002 §11 + §13 — R06 fold attribution

Old: ADR 002 §11 (line ~257) and §13 (line ~333) repeatedly attribute the global → room fold to "R06": "In R06, this row is the source for the migrated room's …"; "R06 reuses it via a `room_id` parameter."; "R06 converts … global rows are deleted at the end of R06."

Verified current behaviour: same as §3.5 — R06 became Player Lease; the fold was never executed.

New R14a decision: this attribution is now fulfilled by R14b (the migrator reuse). ADR 002's anticipatory language ("via a `room_id` parameter") matches the R14b CLI design; R14a documents this alignment but does not edit ADR 002's body. ADR 002's status remains "Proposed — awaiting Architect review and Product Owner approval"; ADR 003 does not move it.

## 4. Why the old plan was not implemented

R06 became Player Lease rather than the global → room fold (the original ADR 001 §11 / ADR 002 §11/§13 attribution). Sprint renumbering placed room scoping in R07a/R07b/R07c/R07d (room queue) and R09a/R09b/R09c/R09d/R09e/R09f (playback / vote / auto-queue). Rooms were addressed by `{slug}` not `{roomId}`. The compatibility shim was deferred indefinitely because the global frontend surface never produced evidence of unambiguous identity, membership, and room-selection behaviour that would let a hidden default-room proxy work safely. The `/ws` synthesized default-room plan was never implemented; the global hub continues to serve real `queue_state`. The migration is now shaped by R14a and split into R14b–R14e (plus R09h as a blocking parity prerequisite for `/api/vote/prioritize` retirement).

## 5. Migration identity contract

### 5.1 Activities target — new `room_activities` table

R14a designs the contract; R14b builds the table.

```text
room_activities
  id          BIGSERIAL PRIMARY KEY
  room_id     BIGINT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE
  timestamp   TIMESTAMPTZ NOT NULL
  type        TEXT NOT NULL
  user        TEXT NOT NULL
  description TEXT NOT NULL
```

Index: `(room_id, timestamp DESC, id DESC)`.

Migration preserves every legacy `activities` row's `id`, `timestamp`, `type`, `user` (display text), `description`, row count, and ordering. After the preserved-id insert, the migrator resynchronises the `room_activities_id_seq` via `setval(seq, MAX(id), is_called=true)`.

No `user_id` requirement during the migration. Legacy rows store display text and may represent users that cannot be resolved reliably or system-generated actions. A nullable actor id is a future-feature concern, not a migration concern.

Dedicated narrow repository contract (do NOT couple to `QueueRepository`):

```go
AddActivity(ctx context.Context, roomID int64, activity entity.Activity) error
GetActivities(ctx context.Context, roomID int64, limit int) ([]entity.Activity, error)
```

After cutover, `room_activities` is the sole writable source of truth. The legacy `activities` table is read-only for the approved rollback window and is dropped in R14e.

### 5.2 Host bootstrap — canonical PostgreSQL `users.id`

Operator flag: `--host-user-id=<positive integer>` (the canonical `users.id` BIGINT, NOT a UUID).

Validation:

- The value is a positive integer.
- Exactly one `users` row exists with that ID. Because it is a primary key, "ambiguous" is not a meaningful outcome.
- The host is inserted into the migrated room as its sole host **inside the same migration transaction** as room creation and data conversion.
- Failure to resolve the ID aborts the migration before any target state is committed.

The migrator does NOT infer the host from: the first user to join, `users.role`, `HOST_EMAILS` / `ADMIN_EMAILS`, an existing player lease, or frontend / request-supplied identity.

**Override of issue #20 §2:** the schema permits one user to be a member of multiple rooms (uniqueness is `(room_id, user_id)` only); the one-host invariant is **per room**, not per user globally. The migrator does NOT reject a host merely because they belong to another active room.

**Idempotent retry with a different `--host-user-id` MUST fail on the room/slug conflict**, not silently change the room's host.

Email may be documented as an operator-side discovery aid (used manually to look up the numeric ID) but is NOT the migration command's canonical identity input.

### 5.3 Migration report — PII posture

The migration report MAY contain:

- the operator-supplied numeric `users.id` for the host;
- numeric row counts, sha256 hashes, sequence names, table names, and durations.

The migration report MUST NOT contain:

- email addresses;
- OAuth tokens / ID token claims;
- any `display_name` / `user` text from activities rows;
- session tokens;
- any credential, cookie, or `Authorization` header value.

DSN redaction uses `config.RedactDSN` (existing R03 helper).

## 6. Source-to-target mapping

| Source (global, R00–R05) | Target (room, R14b builds) | Conversion notes |
| --- | --- | --- |
| `queue_state` (TEXT JSON, singleton `id = 1`) | `room_queue_state` (JSONB, one row per `rooms.id`) | Marshal/Unmarshal round-trip via `(*entity.Queue).MarshalJSON` / `UnmarshalJSON`; SHA256 over canonical bytes; full invariant preservation (`Songs`, `CurrentIndex`, `Status`, `Elapsed`, history fields, first-song/current-song behaviour); `room_queue_state` row PK = `rooms.id`; the migrator writes one row per room (the migrated room, in R14b's case one row). |
| `activities` | new `room_activities` (R14b builds) | Lossless copy of every row with preserved `id`; `room_id` set to the migrated room's id; sequence resync. |
| `auto_queue_config` (singleton `id = 1`) | `room_auto_queue_config` (already exists, migration 0007) | Keyed by `room_id`; write the migrated room's row with the legacy singleton's values. |
| `play_history` (50-row cap) | `room_play_history` (already exists, migration 0007) | Lossless copy of every row with preserved `id`, `played_at`, `video_id`, `title`; per-room 50-row cap preserved (Go-enforced); sequence resync. |

After the copy, the source rows are retained as migration evidence. They are read-only for the rollback window and dropped in R14e. After cutover, the room targets are the sole writable source of truth.

`users`, `user_sessions`, and `priority_transactions` remain account-scoped (per ADR 001 §3 Decision 9) and are NOT migrated into a room.

## 7. Phase A–D retirement sequence

R14a chooses ONE recommended sequence. The four phases MUST be implemented as separate sprints so each can be reviewed independently.

### Phase A — migration / cutover (offline)

- Operator runs `migrate-data up` (an R03-style CLI; see §8) while the server is stopped.
- Global runtime is **disabled** during the migration; the server is not serving traffic.
- On success the operator restarts the server with the room state authoritative. There is NO simultaneous global+room write path; there is NO proxy during the cutover; there is NO startup magic.
- Phase A is the ONLY slice that creates or resolves the migrated room. The room is named by the operator-supplied slug; it is never called "main", "default", "implicit", or "room 0".

### Phase B — `410 Gone` on the legacy global REST and `/ws`

Phase B lands in R14c (backend) and may only return `410 Gone` for these routes:

- `/api/queue`, `/api/queue/add`, `/api/queue/skip`, `/api/queue/status`, `/api/queue/sync`, `/api/queue/ended`, `/api/queue/prev`, `/api/queue/remove`, `/api/queue/clear`, `/api/queue/volume`, `/api/queue/prioritize` (the global `/queue/prioritize` is OUT OF SCOPE for Phase B — see R09h below);
- `/api/vote/skip`;
- `/api/autoqueue/toggle`, `/api/autoqueue/status`;
- the global `/ws` endpoint.

`410 Gone` response shape:

```json
{
  "error": "gone",
  "code": "global_contract_retired",
  "successor": "/api/rooms/{slug}/...",
  "documentation": "documents/00-project-management/SPRINTS/022-legacy-global-state-migration-and-contract-retirement-plan.md"
}
```

Plus an HTTP `Link: </api/rooms/{slug}/...>; rel="successor-version"` header (the `successor` value is operator-supplied at deploy time and points at the migrated room).

`/api/auth/google`, `/api/auth` (Deprecated), `/api/user/priority-balance`, and `/api/youtube/search` are OUT of the retirement set (auth is R13 scope; priority is account-scoped per ADR 001 §3 Decision 9; search is a global utility).

`/api/vote/prioritize` `410` is **BLOCKED on R09h — Room vote-to-prioritize parity** landing and Product Owner acceptance. R09h shape:

- `POST /api/rooms/{slug}/vote/prioritize` with body `{"song_index": <integer>}`; identity from the bearer token only (never from the body).
- No priority-balance debit; no priority transaction.
- In-memory vote session keyed `prioritize:{slug}:{songID}`, 30-second expiry, single-instance only.
- Threshold: `max(2, uniqueConnectedUserIDs(slug) / 2 + 1)` (mirrors R09b's strict-majority + two-voter floor).
- On a passed vote, a queue-owned `roomqueue.Interactor` operation re-resolves the target by song ID (index as hint only), rejects the current song, detects stale removal/movement, runs under the room queue mutation lock, moves the target immediately after the current song, persists once, and emits the existing `room_queue_song_prioritized` event (NO new WS event constant). `room_vote_updated` and `room_vote_resolved` events ride too (mirrors R09b).

### Phase C — frontend cutover

- `RoomView` becomes the only supported view; `DashboardView` is removed or redirects to a Welcome/room entry surface.
- `globalStore.queueState`, `globalStore.voteSessions`, and `globalStore.autoQueueConfig` are cleared.
- `globalStore.currentUser` is preserved (RoomView still reads it).
- `globalStore.roomQueues[slug]` per-slug slices are preserved (RoomView reads them).
- The global `WebSocketClient` is closed and not reconnected.

### Phase D — schema cleanup

- Drop the legacy `queue_state`, `activities`, `auto_queue_config`, and `play_history` tables.
- Remove `0001_initial.up.sql` rows only used by the legacy state (the rest of `0001` — `users`, `user_sessions`, `priority_transactions` — stays).
- Schema version bumps from 8 to 9.
- Phase D is destructive cleanup only and runs **after** a verified rollback window AND after no runtime path references the legacy tables.

## 8. Atomicity & idempotency (R03 reuse)

The future R14b migrator reuses the R03 patterns verbatim, NOT as a startup-magic transform:

- `pg_try_advisory_lock(987654321)` on a pinned connection (`migrator.go:442`); error if held; deferred unlock.
- Single transaction: `conn.BeginTx` → `tx.Commit`; `tx.Rollback` on any error.
- `migration_marker` table (R03, migration 0003) extended with per-table SHA256 hashes for the new `room_activities` and per-room `room_queue_state` target.
- Re-run on identical source = `already migrated; no-op`, exit 0.
- Hash drift on target = rejected (no `--force` / `--reset`).
- `verifyWithinTx` (pre-commit COUNT/MIN/MAX + `queue_state` byte length parity) and `verifyAfterCommit` (post-commit sanity).
- `Options.DryRun` for a report-only run.
- `config.RedactDSN` in the report.
- Sequence resync via `setval(seq, MAX(id), is_called=true)` for each migrated sequence (`room_activities_id_seq`, plus per-room sequence if any).
- The migrator is an offline CLI; it does NOT run inside the server process; it does NOT mutate either model concurrently with runtime writers.

## 9. WebSocket + frontend transition rules

- `/ws` returns `410 Gone` in Phase B (R14c). No synthesized default-room fallback. No broadcast on the retired endpoint.
- Per-room `/ws/rooms/{slug}` reconnect/seq logic is unchanged.
- The first per-room `room_queue_sync` after migration lands carries the migrated room's queue state under the migrated room's `seq_num` allocation (per-room sequence numbers restart from the per-room hub's own counter, which is independent of the global hub's counter; this is the existing R07b behaviour and is not changed by R14a).
- Frontend: preserve `globalStore.currentUser`; clear `globalStore.queueState`, `globalStore.voteSessions`, `globalStore.autoQueueConfig`; close global `WebSocketClient`; per-slug `roomQueues[slug]` slices kept.

## 10. Future sprint sequence

| Sprint | Scope | Notes |
| --- | --- | --- |
| **R09h — Room vote-to-prioritize parity** | Backend room-vote-prioritize parity. Adds `POST /api/rooms/{slug}/vote/prioritize` per §7 Phase B. **BLOCKING PREREQUISITE for R14c.** | Reuses existing `room_queue_song_prioritized` event. No new WS event constant. |
| **R14b — migration mechanism** | Backend/migration only. New `room_activities` table + repo; host bootstrap via `--host-user-id`; `migration_marker` hashes for new tables; dry-run; report with PII redaction; sequence resync; verifyWithinTx / verifyAfterCommit. **NO endpoint removal. NO frontend changes.** | Reuses R03 advisory-lock / tx / marker patterns verbatim. |
| **R14c — room-authoritative runtime + `410` retirement** | Backend contracts. Returns `410 Gone` on the listed global routes and `/ws` (per §7 Phase B); `/api/vote/prioritize` blocked on R09h. Per-room runtime is the only authoritative path. | NO frontend changes in R14c. |
| **R14d — frontend global-path removal** | Frontend only. DashboardView removed/redirected; clear `globalStore.queueState` / `voteSessions` / `autoQueueConfig`; close global `WebSocketClient`; preserve `currentUser` + `roomQueues[slug]`. | Backend contracts are fixed by R14c. |
| **R14e — schema cleanup** | Backend only. Drop legacy `queue_state`, `activities`, `auto_queue_config`, `play_history`; bump schema to 9. | Runs after a verified rollback window and after no runtime path references the legacy tables. |

Sprint names may be renamed if evidence requires it, but R14a/b/c/d/e MUST NOT be combined; migration, destructive cleanup, frontend rewrite, and R09h parity must remain independently reviewable.

## 11. Deployment, backup, and rollback gates (future R14b/R14c gates)

- Required PostgreSQL `pg_dump` BEFORE R14b migration; retained ≥30 days (mirrors ADR 002 §9).
- Application downtime / maintenance window is required during Phase A.
- `migrate-data --dry-run` MUST be run before any commit.
- Target verification (`verifyWithinTx` + `verifyAfterCommit`) MUST pass before source cleanup is considered.
- A rollback point MUST exist before accepting writes on the room-authoritative version (R14c).
- Migration succeeds but application startup fails: rollback path restores `pg_dump` and the pre-migration binary.
- **Explicit warning:** rollback after new room writes may require forward recovery or data reconciliation.

## 12. Open items requiring PO decision

- Slug-collision resolution at migration time (slug already exists in `rooms`).
- Explicit `410` wording and `successor` field — the operator supplies the migrated room's slug at deploy time.
- Rollback trigger posture — informational-only or hard-stop if any write reaches the legacy tables after cutover.
- Retention window specifics for the migration `pg_dump`.

## 13. Verification

- `git diff --check` clean.
- `git diff --name-only` contains only documents under `documents/00-project-management/`; no `*.go`, `*.vue`, `*.sql`, `Dockerfile*`, `nginx/*`, `*.env*`, `package*.json`, `package-lock.json`, `go.mod`, `go.sum`, anything under `cmd/`, `internal/`, `frontend/src/`, `docker/`, `certs/`, `letsencrypt-*`, `extension/`, `.gitignore`, or `.gitattributes`.
- ADR 001 body of §3–§18 is unchanged byte-for-byte except for the one metadata supersession note near the header.
- ADR 003 is referenced from `022-…md` and `ROOM_EPIC_SPRINT_SEQUENCE.md`.
- No `main`, room `0`, "default room", "implicit room", or synthesized default-room `/ws` language appears in the R14a documents.
