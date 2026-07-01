# R09e — Room Auto-Queue Contract Design

**Status:** Active (planning/design — documentation only). No Go runtime code, no Vue runtime code, no migrations, no routes, no WebSocket constants, and no config changes are introduced by this sprint. R09e is the third narrow slice of the legacy R09c+ bucket (volume, prev, room auto-queue, full device integration). R09c (volume) and R09d (prev) are closed and accepted by the Product Owner; R09e shapes the contract for the room auto-queue slice. R09e+ still defers full media-player device integration.

**Sprint name:** Room auto-queue contract design (planning/design only)

## Goal

Produce a narrow, reviewable contract for a future room auto-queue implementation. The contract must settle the following without committing to runtime code: persistence, trigger points, queue ownership, stale-candidate handling, broadcast contracts, recommendation-source behavior, authorization, failure semantics, and a future test strategy. R09e ships no runtime code; it ships this document.

## Out of scope (explicit non-goals for R09e)

- Go runtime code (no new packages, no new methods, no new entity helpers, no new sentinels, no new interactor methods, no new handler methods, no new broadcaster methods, no new repository methods).
- Vue runtime code (no new API methods, no new store mutators, no new RoomView listeners, no new toast mapping).
- PostgreSQL migrations, schema changes, or new tables.
- New HTTP routes, new WebSocket event constants, or new payload structs.
- Configuration changes (env vars, config fields, defaults).
- Global auto-queue (`/api/autoqueue/...`, `/ws` `auto_queue_added`, the `auto_queue_config` and `play_history` tables, the `domain.AutoQueueRepository`, the `domain.RelatedSongFetcher`, the `usecase/autoqueue.Interactor`, and the `PostgresAutoQueueRepository`).
- Docker, Nginx, HTTPS, CORS, session design, OAuth, or any auth/session posture.
- Room deletion, membership removal, chat, search, or discovery.
- Full media-player device integration.
- Relational queue rows (single-row JSONB `room_queue_state` per room stays unchanged).
- Cross-process safety claims (single-instance only).

## Current behavior (pre-R09e baseline)

### Global auto-queue (out of scope for R09e, but the reference contract)

- `domain.AutoQueueConfig` (a single-row config: `enabled`, `strategy`) and `domain.PlayHistoryEntry` are persisted by `PostgresAutoQueueRepository` in the single-row `auto_queue_config` table and the 50-row capped `play_history` table.
- `domain.RelatedSongFetcher` is implemented by `YtDlpRelatedFetcher`; the fetcher takes a `videoID` and an `exclude` list and returns one candidate `*entity.Song`.
- `usecase/autoqueue.Interactor` exposes `CheckAndTrigger(ctx)`, `GetConfig(ctx)`, `SetEnabled(ctx, enabled)` with an `mu`-serialized single-flight `triggering` flag and a serialized `SetEnabled`/post-fetch recheck contract (Sprint 004 hardening).
- `queue.Interactor.AddAutoQueueSong(ctx, song, expectedSourceSongID)` is the queue-owned conditional insertion point: it runs under the queue mutex, revalidates source-song identity / "current is last" / duplicate-guard, and returns `queue.ErrAutoQueueStale` on any violation with zero side effects.
- The `auto_queue_added` event (`AutoQueueAddedData`) rides the global `/ws` endpoint with the authoritative post-mutation snapshot (`current_index`, `current_song`, `status`, `elapsed`).
- A stale candidate (source song no longer current or an upcoming song now exists) produces NO save, NO history append, NO activity, and NO broadcast.

### Room queue (target for R09e)

- Per-room queue state is persisted in `room_queue_state` (single-row JSONB per `rooms.id`).
- `roomqueue.Interactor` exposes room-scoped mutations (`AddSong`, `RemoveSong`, `ClearQueue`, `PrioritizeSong`, `SetPlaybackStatus`, `SyncPlaybackElapsed`, `SkipPlayback`, `PlaybackEnded`, `SkipVote`, `ChangePlaybackVolume`, `PrevPlayback`) under a per-`Interactor`-instance mutex.
- Lease-aware playback mutations route through the existing `room.PlaybackLeaseAuthorizer` seam.
- The `Broadcaster` interface in `roomqueue.Interactor` is satisfied by `*ws.RoomWSHub`; broadcasts ride `GET /ws/rooms/{slug}` only and never appear on the global `/ws` endpoint.
- The per-room hub loop owns the only per-room `seqNum` allocation; per-room sequencing is single-process / in-memory.
- R09d's `prev` command explicitly does NOT trigger auto-queue.

### Gaps the future slice must close

- No per-room auto-queue config exists; only the global `auto_queue_config` does.
- No per-room `play_history` analogue exists; only the global 50-row `play_history` table does.
- No per-room `RelatedSongFetcher` call site exists; only the global `autoqueue.Interactor.CheckAndTrigger` does.
- The existing `roomqueue.Interactor` does not call `CheckAndTrigger` from any `roomqueue` path. The only auto-queue trigger is wired into `queue.Interactor.SkipSong` and `queue.Interactor.SongEnded` for the global queue.
- The `Broadcaster` interface has no `BroadcastRoomAutoQueueAdded` method.
- `internal/delivery/ws/events.go` has no `EventRoomAutoQueueAdded` constant.
- The frontend `RoomView.vue` has no auto-queue listener; the global `room_queue_sync` snapshot has no `autoQueueConfig` field; `globalStore.roomQueues[slug]` has no `autoQueueConfig` slice.

## Desired behavior (post-future-slice, sketched)

- A room auto-queue is a per-room setting, NOT a per-server setting. Toggling the global auto-queue (or vice versa) MUST NOT change the per-room setting. The per-room setting is the authoritative trigger condition for room radio mode; the global setting continues to drive the global queue.
- When the per-room auto-queue is enabled AND the room queue is at "current is last" (one song left), the room's use-case layer MAY fetch a related song and atomically insert it, mirroring the global contract's stale-candidate invariants.
- The room auto-queue broadcast rides the per-room WebSocket only. The global `/ws` 16-event inventory and the global `auto_queue_added` event are unchanged.
- Authorization for the room auto-queue toggle follows the same room-membership rules as the existing room queue mutations. Lease-holder enforcement is NOT required for the toggle itself (toggle is a config edit, not a queue mutation).
- Stale candidates produce zero side effects: no save, no history append, no activity, no broadcast. This is the same contract as the global auto-queue (Sprint 004).
- The recommendation source is the existing `domain.RelatedSongFetcher` abstraction; a per-room fetcher is unnecessary unless a concrete blocker is documented.
- All locking mirrors the global contract: the slow `FetchRelated` call is NEVER held under any lock; the queue-owned conditional insertion takes the roomqueue mutex; broadcasts and history appends run OUTSIDE the roomqueue and roomautoqueue locks.

## Contract decisions (frozen by R09e; future implementation must conform)

### CD-1: Per-room scope

The future room auto-queue is room-scoped, NOT silently controlled by the global `auto_queue_config`. Toggling the global auto-queue toggle (or vice versa) MUST NOT mutate the per-room setting. The two settings are independent; clients that want both must toggle each one explicitly.

### CD-2: Per-room persistence (recommended shape)

Future implementation SHOULD use per-room config and history tables (or documented equivalents). Recommended physical shape (NOT implemented in R09e):

- A per-room config row keyed by `room_id`, with columns `enabled BOOLEAN`, `strategy TEXT` (default `'related'`), and an `updated_at` timestamp. The future migration is named in a future slice; R09e does NOT create the migration.
- A per-room `room_play_history` table keyed by `room_id` with the same columns and 50-row cap as the global `play_history` table. The future migration is named in a future slice; R09e does NOT create the migration.

The single-row `auto_queue_config` table and the 50-row `play_history` table are the global auto-queue's and are NOT replaced by R09e. A future implementation may choose to factor a single repository type that operates on both global and per-room rows, but the recommended direction is two narrow repositories that each own their table.

### CD-3: Reuse of the existing fetcher abstraction

Future implementation MUST reuse `domain.RelatedSongFetcher` for the room auto-queue's recommendation source unless a concrete, documented blocker is found at implementation time. The existing `YtDlpRelatedFetcher` is the production implementation; a per-room fetcher is not introduced.

### CD-4: Use-case layering

A future implementation MAY introduce a new use case (working name: `roomautoqueue.Interactor`) that orchestrates the per-room config read, the recommendation fetch, and the queue-owned conditional insertion. The future use case MUST NOT take a queue lock during `FetchRelated` (the slow yt-dlp call is held outside any lock, mirroring the global contract).

The room queue interactor (`roomqueue.Interactor`) owns conditional queue insertion and stale revalidation, mirroring the global `queue.Interactor.AddAutoQueueSong` contract. The future `roomqueue.Interactor` method (working name: `AddRoomAutoQueueSong`) MUST revalidate the source-song identity, the "current is last" predicate, and the duplicate guard under the roomqueue mutex, and MUST return a stale sentinel with zero side effects on any violation.

The broadcaster for the future per-room event is the existing `*ws.RoomWSHub`. The future `roomqueue.Broadcaster` interface gains a single new method (working name: `BroadcastRoomAutoQueueAdded`) following the same dispatch pattern as the existing room broadcaster methods. The hub loop remains the sole owner of per-room `seqNum` allocation.

### CD-5: Trigger ownership and trigger points

The future room auto-queue trigger fires AFTER a successful room queue mutation that leaves the post-mutation queue at "current is last" (one song remaining) AND the per-room config is enabled. The trigger site is the roomqueue use-case layer, not the handler layer.

The trigger fires after a successful (non-stale, non-error) mutation in any of these paths:

- `roomqueue.Interactor.PlaybackEnded` — final-song case. When the entity layer returns `entity.ErrNoNextSong`, the interactor pauses at end-of-queue (`Status = StatusPaused`, `Elapsed = 0`, `CurrentIndex` unchanged) and persists. The post-mutation queue is "current is last"; the trigger fires AFTER that save if room auto-queue is enabled.
- `roomqueue.Interactor.PlaybackEnded` — successful-advance case. When `AdvanceToNext` succeeds and the new current song is the new last in the queue (i.e. the previous "last" was an upcoming song and the queue was at "last+1" before the advance), the post-mutation queue is "current is last"; the trigger fires.
- `roomqueue.Interactor.SkipPlayback` — successful-advance case. When the lease-holder skip advances to a new current song that becomes the new last in the queue, the post-mutation queue is "current is last"; the trigger fires.
- `roomqueue.Interactor.SkipVote` — successful-advance case. When the room vote skip advances to a new current song that becomes the new last in the queue, the post-mutation queue is "current is last"; the trigger fires.
- `roomqueue.Interactor.RemoveSong` — when the removal leaves the queue at "current is last" (the removed song was an upcoming song and the current song is now last).
- `roomqueue.Interactor.ClearQueue` — when the queue becomes "current is last" after clearing (the kept current song is the only remaining song, or the queue had one upcoming song and clearing left the current as the new last).

The trigger MUST NOT fire on:

- `roomqueue.Interactor.PlaybackEnded` no-mutation error paths (e.g. `entity.ErrNoCurrentSong` from an empty queue, or other non-final-song errors). The trigger only fires after the interactor returns nil — i.e. after a successful save. The `entity.ErrNoNextSong` return from `AdvanceToNext` inside `PlaybackEnded` is NOT a no-mutation path: the interactor handles it by persisting paused end-of-queue state, and that save IS the trigger site.
- `roomqueue.Interactor.SkipPlayback` and `roomqueue.Interactor.SkipVote` no-mutation paths. These methods return `roomqueue.ErrNoNextSong` / `roomqueue.ErrNoCurrentSong` BEFORE any state change (the entity layer refuses to mutate); the queue is byte-for-byte unchanged, so the trigger has nothing to react to.
- `roomqueue.Interactor.PrevPlayback` (R09d: prev is not an auto-queue trigger; the post-mutation queue is a rewind, not an "ended" state).
- `roomqueue.Interactor.PrioritizeSong` (a re-order; the queue tail is unchanged).
- `roomqueue.Interactor.AddSong` (a manual add; the user-driven addition is the auto-queue's source of new content, not a trigger).
- `roomqueue.Interactor.ChangePlaybackVolume` (no queue change at all).
- `roomqueue.Interactor.SyncPlaybackElapsed` / `SetPlaybackStatus` (elapsed/status-only; the queue tail is unchanged).

Trigger invocations are async (non-blocking) and use a per-room single-flight guard (see CD-6). The slow `FetchRelated` call runs in the goroutine; the per-room mutex is NEVER held during the fetch.

### CD-6: Per-room single-flight / concurrency model

- A future `roomautoqueue.Interactor` MUST hold a **coordinator mutex** (working name: `mu`) that guards a **per-room in-flight map** (working name: `inFlight map[roomID|string]bool`). The `mu` is taken only to read or mutate the map; the slow `FetchRelated` call is NEVER held under `mu`. This is the per-room analogue of the global `autoqueue.Interactor.triggering` flag, but keyed per room so the in-flight state is per-room, not global.
- Per-room in-flight semantics: each room's trigger is independent. A slow `FetchRelated` for room A MUST NOT suppress, block, or delay a trigger for room B; the map entry is keyed by room id (or slug) and checked under `mu` only. The single-flight guarantee is "at most one in-flight trigger per room at any time", not "at most one in-flight trigger across all rooms".
- The future `roomqueue.Interactor.AddRoomAutoQueueSong` runs under the existing `roomqueue.Interactor` mutex (it re-uses the same mutex the rest of the roomqueue package already holds). The insertion MUST NOT call out to the slow `FetchRelated` (that already happened under the roomautoqueue coordinator mutex).
- The `FetchRelated` call is held outside BOTH the roomautoqueue coordinator mutex and the roomqueue mutex. The two rooms' fetches (if both have triggers in flight) may run concurrently.
- `AppendHistory`, activity writes, and the broadcaster call run OUTSIDE both locks, mirroring the global contract. A concurrent `SetEnabled` is not blocked on downstream work.
- Stale-candidate, repository-load-failure, and disable-mid-flight paths are serialized exactly as the global contract serializes them — and only with respect to OTHER triggers for the SAME room. Cross-room operations are independent.
- Single-instance only: the per-room in-flight map is in-memory; on restart every room's in-flight state is empty. No cross-process safety claim is made; a future horizontal-scaling redesign would need a different coordinator (e.g. advisory lock per room) and is out of scope.

### CD-7: Stale-candidate handling

A future implementation MUST treat stale candidates as zero-side-effect drops. The future `roomqueue.Interactor.AddRoomAutoQueueSong` runs under the roomqueue mutex, revalidates:

- `queue.CurrentIndex` is in range.
- `queue.Songs[queue.CurrentIndex].ID == expectedSourceSongID` (the song that triggered the fetch is still the current song).
- `queue.CurrentIndex == len(queue.Songs) - 1` (no upcoming song now exists).
- `!queue.ContainsSong(song.ID)` (the candidate is not already queued).

On any failure against a successfully-loaded queue, the method returns a stale sentinel (`roomqueue.ErrRoomAutoQueueStale`, working name) with zero side effects: no `queueRepo.Save`, no `roomRepo.Save` (config), no `roomPlayHistoryRepo.AppendHistory`, no activity, no broadcast.

Repository load failures are wrapped and returned as operational errors (NOT the stale sentinel) so the future `roomautoqueue.Interactor` can distinguish a stale-but-valid state from a database failure.

### CD-8: Broadcast contract

#### CD-8.1: New per-room event (additive)

- Constant: `EventRoomAutoQueueAdded = "room_auto_queue_added"` (working name; final name confirmed by the future slice).
- Emitted on: `GET /ws/rooms/{slug}` only.
- NOT emitted on: the global `/ws` endpoint. The global 16-event inventory is unchanged.
- Triggered by: a successful room auto-queue insertion (the future `roomqueue.Interactor.AddRoomAutoQueueSong` returned a non-stale result).
- NOT triggered by: stale candidates, repository failures, disable-mid-flight, fetcher failures when no fallback candidate is available.

#### CD-8.2: Payload (working shape)

```json
{
  "type": "room_auto_queue_added",
  "data": {
    "room_slug": "<slug>",
    "song": { "id": "...", "title": "...", "thumbnail": "...", "added_by": "system:autoqueue", "added_at": "..." },
    "source_song_title": "...",
    "current_index": 0,
    "current_song": { "id": "...", "title": "..." },
    "status": "playing",
    "elapsed": 12,
    "state": { /* authoritative *entity.Queue snapshot */ }
  },
  "seq_num": 12345,
  "timestamp": "..."
}
```

- The payload MUST carry the post-mutation snapshot (mirroring the global `AutoQueueAddedData` shape and the existing room `room_queue_song_added` shape).
- The payload MUST NOT carry the per-room `autoQueueConfig` (toggle state is delivered via the future per-room config event, not on every auto-queue delta).
- The `added_by` field is the existing `entity.SystemUserID` constant; the frontend renders the `⚡ auto` badge from that field (no new constant).

#### CD-8.3: Sequencing

- Per-room `seqNum` is allocated by the hub loop (same invariant as the existing per-room broadcasters). `dispatch()` MUST NOT allocate a `seqNum`.
- A new client receives the future `room_queue_sync` snapshot with a strictly lower `seq_num` than any subsequent per-room delta (existing R07b interleaving invariant).

#### CD-8.4: Auth

- The future per-room event rides the existing per-room WebSocket auth gate (session_token, active membership, active room). No new auth flow.
- The event is not subject to the player-lease holder rule (the event is informational; the future auto-queue trigger fires regardless of who holds the lease).

### CD-9: Recommendation-source behavior

- The future `roomautoqueue.Interactor` MUST use the `domain.RelatedSongFetcher` abstraction for the primary path. The exclude list is the room's recent history (last 20 entries from `room_play_history`) PLUS the room queue's current contents, mirroring the global exclusion contract.
- The future implementation MAY use a fallback path: pick a random entry from the room's `room_play_history` (last 50) that is not currently in the room queue. Fallback failure is silent (no save, no history append, no activity, no broadcast).
- The future implementation MUST NOT reach into the global `play_history` table for per-room fallback (the rooms are independent).
- Excluding the same source song from the next fetch is required to avoid the "just added by auto-queue, now appended again" loop.

### CD-10: Authorization

- Toggle (per-room config edit): any active room member MAY read the per-room config; only host or admin may toggle it. The toggle itself does NOT require the player-lease holder. This mirrors the global `/api/autoqueue/toggle` rule.
- Future HTTP endpoint shape: `POST /api/rooms/{slug}/autoqueue/toggle` body `{ "enabled": <bool> }` (final shape confirmed by the future slice; R09e does NOT add the route). Response 200 on success with the new per-room config; 401/403/404/409 per the existing room queue mapping.
- The future read endpoint (working name: `GET /api/rooms/{slug}/autoqueue/status`) returns the per-room config; any active member may read. 200 with `{ "enabled": <bool>, "strategy": "related" }`.
- Insertion (the auto-queue trigger): no per-action authorization. The trigger fires regardless of which user holds the lease, mirroring the global auto-queue.
- The future per-room config change event (working name: `room_auto_queue_config_changed`) rides the per-room WebSocket only. Mirror of the global `auto_queue_config_changed` shape (envelope `{room_slug, enabled, strategy}`), but with `room_slug` added.

### CD-11: UI staging

- The future RoomView MAY add a small toggle (e.g. a `📻 Radio` button in the queue panel). The toggle is a copy of the global dashboard's pattern, gated on host/admin.
- The future frontend store slice is `globalStore.roomQueues[slug].autoQueueConfig = { enabled, strategy }` (working name). The slice is updated by the future `room_auto_queue_config_changed` listener.
- The future `applyRoomAutoQueueAdded(slug, payload)` mutator is added to the store; it prefers `payload.state` when present and otherwise applies the snapshot fields, never touching the global `queueState`, `currentUser`, `voteSessions`, or `autoQueueConfig`.
- The future frontend listener renders the existing `⚡ auto` badge on the auto-added song (no new badge constant).

## Failure semantics

| Failure | Mutex held? | Room queue save? | History append? | Activity? | Broadcast? | Config save? | Logged? |
|---|---|---|---|---|---|---|---|
| Disabled mid-flight (post-fetch `enabled=false`) | release before save | No | No | No | No | No (config is saved only by the toggle endpoint) | `auto-queue: disabled mid-flight, dropping candidate` |
| Stale (source song no longer current) | release before save | No | No | No | No | No | `auto-queue: candidate stale, dropping` |
| Stale (upcoming song now exists) | release before save | No | No | No | No | No | `auto-queue: candidate stale, dropping` |
| Stale (candidate already in queue) | release before save | No | No | No | No | No | `auto-queue: candidate stale, dropping` |
| Repository load failure | release before save | No | No | No | No | No | wrapped error propagates; future `roomautoqueue.CheckAndTrigger` returns the wrapped error |
| Fetcher fails AND no fallback candidate | n/a | No | No | No | No | No | `auto-queue: no candidate available` |
| Insertion succeeds | release before downstream | Yes (room queue only — `room_queue_state` JSONB) | Yes (room play history only — `room_play_history`) | Yes (single activity) | Yes (`room_auto_queue_added`) | No (insertion does NOT touch the per-room config; config is saved only by the toggle / status endpoints, not by the trigger path) | success log line |
| `SetEnabled` lands during slow fetch | holds coordinator `mu`; serializes with post-fetch recheck (per-room only) | Same as disabled-mid-flight | Same as disabled-mid-flight | Same as disabled-mid-flight | Same as disabled-mid-flight | No (config save is the toggle endpoint's path, separate from the trigger) | same log line as disabled-mid-flight |

The "downstream work runs outside the lock" invariant is preserved end-to-end. A concurrent `SetEnabled` is not blocked on `AppendHistory` or the broadcaster.

## Future implementation sketch (not a build plan; a contract for the next slice)

The future implementation lands as a single narrow slice, mirroring the R07a/R07b/R07c/R07d, R09a/R09b/R09c/R09d pattern. The slice:

1. Adds a new `internal/usecase/roomautoqueue` package containing a `roomautoqueue.Interactor` with `CheckAndTrigger(ctx, slug)`, `GetConfig(ctx, slug)`, and `SetEnabled(ctx, slug, enabled)`. The interactor holds a coordinator `mu` mutex that guards a per-room `inFlight` map keyed by room id (or slug). The map is the per-room analogue of the global `autoqueue.Interactor.triggering` flag, but is NOT a single global flag: each room's in-flight state is independent, so a slow `FetchRelated` for room A does not suppress or block room B's trigger. The slow `FetchRelated` is held OUTSIDE the coordinator mutex (CD-6). The per-room in-flight map is in-memory, single-instance only.
2. Adds a per-room `RoomAutoQueueRepository` interface in `internal/domain/repository` (or a sibling package) with `GetConfig(ctx, roomID)`, `SaveConfig(ctx, roomID, cfg)`, `AppendHistory(ctx, roomID, entry)`, and `GetRecentHistory(ctx, roomID, limit)`. Backed by a future `PostgresRoomAutoQueueRepository` and a future migration.
3. Adds a new `roomqueue.Interactor.AddRoomAutoQueueSong(ctx, slug, song, expectedSourceSongID)` method that runs under the existing roomqueue mutex, revalidates the stale predicates, persists via `queueRepo.Save`, and returns the post-mutation snapshot + a stale sentinel. This mirrors the global `queue.Interactor.AddAutoQueueSong` contract byte-for-byte.
4. Adds a `roomautoqueue.AddRoomAutoQueueSongFunc` seam (mirror of the global `AddAutoQueueSongFunc` seam) so the future `roomautoqueue.Interactor` does not import `roomqueue` (avoiding an upward dependency from a leaf usecase).
5. Adds `BroadcastRoomAutoQueueAdded(slug, song, sourceSongTitle, state)` to the `roomqueue.Broadcaster` interface; the `*ws.RoomWSHub` gains a matching method that calls `dispatch(slug, EventRoomAutoQueueAdded, RoomAutoQueueAddedData{...})`.
6. Wires the new use case into the roomqueue `PlaybackEnded` / `SkipPlayback` / `SkipVote` / `RemoveSong` / `ClearQueue` paths (each fires a non-blocking `CheckAndTrigger(slug)` when the room's queue ends up at "current is last").
7. Adds `GET /api/rooms/{slug}/autoqueue/status` (any active member) and `POST /api/rooms/{slug}/autoqueue/toggle` (host/admin) behind `roomAuth`. Both are pure REST; no global state is touched.
8. Adds the `room_auto_queue_added` and `room_auto_queue_config_changed` events to `internal/delivery/ws/events.go`. Both ride `/ws/rooms/{slug}` only.
9. Adds a minimal frontend control (a `📻 Radio` toggle) gated on host/admin, an `applyRoomAutoQueueAdded` mutator on `globalStore.roomQueues[slug]`, and a `room_auto_queue_added` WS listener in `RoomView.vue`.
10. The future slice verification mirrors R09a: `go test -count=1 ./internal/usecase/roomautoqueue ./internal/usecase/roomqueue ./internal/delivery/http ./internal/delivery/ws ./cmd/server` plus the `-race` variant, `go vet`, `git diff --check`, frontend vitest + build.

The future slice does NOT touch: the global `/api/autoqueue/...` routes, the global `/ws` 16-event inventory, the global `auto_queue_config` / `play_history` tables, the `domain.AutoQueueRepository`, the `domain.RelatedSongFetcher`, the `usecase/autoqueue.Interactor`, the `PostgresAutoQueueRepository`, the `YtDlpRelatedFetcher`, the global `queue.Interactor.AddAutoQueueSong`, the `frontend/src/views/DashboardView.vue` radio button, the `frontend/src/services/api.js` global auto-queue methods, or the global `globalStore.autoQueueConfig` slice.

## Future test matrix

### Go unit tests

| Test | Layer | What it pins |
|---|---|---|
| `TestRoomAutoQueue_DisabledConfig` | `roomautoqueue.Interactor` | `enabled=false` at pre-fetch; no fetcher call; no insertion |
| `TestRoomAutoQueue_NotLastSong_Skips` | `roomautoqueue.Interactor` | `current != last`; no fetcher call; no insertion |
| `TestRoomAutoQueue_LastSong_Triggers` | `roomautoqueue.Interactor` | `current == last`; primary fetcher returns; insertion callback invoked; broadcast emitted; history appended |
| `TestRoomAutoQueue_FetcherFailsFallbackSuccess` | `roomautoqueue.Interactor` | Fetcher error; fallback returns a history entry; insertion succeeds |
| `TestRoomAutoQueue_BothFail` | `roomautoqueue.Interactor` | No candidate available; no save, no history, no activity, no broadcast |
| `TestRoomAutoQueue_StaleAtInsertion_SourceMismatch` | `roomqueue.Interactor.AddRoomAutoQueueSong` | Source song changed mid-fetch; returns `ErrRoomAutoQueueStale` (working name); no save, no broadcast |
| `TestRoomAutoQueue_StaleAtInsertion_UpcomingSongExists` | `roomqueue.Interactor.AddRoomAutoQueueSong` | Another song was added mid-fetch; returns `ErrRoomAutoQueueStale`; no save, no broadcast |
| `TestRoomAutoQueue_StaleAtInsertion_Duplicate` | `roomqueue.Interactor.AddRoomAutoQueueSong` | Candidate already in queue; returns `ErrRoomAutoQueueStale`; no save, no broadcast |
| `TestRoomAutoQueue_LoadFailurePropagates` | `roomautoqueue.Interactor` | Repository load failure; surfaces as wrapped error, NOT the stale sentinel |
| `TestRoomAutoQueue_DisabledMidFlight` | `roomautoqueue.Interactor` | `SetEnabled(false)` while `FetchRelated` is blocked; candidate dropped on post-fetch recheck; no save, no history, no activity, no broadcast |
| `TestRoomAutoQueue_SetEnabledBlockedDuringInsertion` | `roomautoqueue.Interactor` | Once the post-fetch recheck passes and the insertion begins, `SetEnabled` blocks until the insertion returns |
| `TestRoomAutoQueue_SetEnabledUnblockedAfterInsertion` | `roomautoqueue.Interactor` | After the insertion returns, `mu` is released; a concurrent `SetEnabled` completes even while the broadcaster is parked |
| `TestRoomAutoQueue_HistoryUnblockedAfterInsertion` | `roomautoqueue.Interactor` | `AppendHistory` runs outside `mu`; a concurrent `SetEnabled` completes while history is parked |
| `TestRoomAutoQueue_ExcludedAfterInsert` | `roomautoqueue.Interactor` | The just-added auto song is excluded from the next fetch's exclude list (no infinite self-loop) |
| `TestRoomAutoQueue_ExclusionUsesRoomHistory` | `roomautoqueue.Interactor` | The exclude list is the room's `GetRecentHistory(20)` + room queue contents (NOT the global `play_history`) |
| `TestRoomAutoQueue_FallbackUsesRoomHistory` | `roomautoqueue.Interactor` | Fallback draws from the room's `GetRecentHistory(50)`, not the global one |

### Go integration / repo tests

| Test | Layer | What it pins |
|---|---|---|
| `TestRoomAutoQueueRepo_GetConfig_DefaultDisabled` | `PostgresRoomAutoQueueRepository` | Missing row → default `enabled=false`, `strategy='related'` |
| `TestRoomAutoQueueRepo_SaveConfig_RoundTrip` | `PostgresRoomAutoQueueRepository` | Insert + read back; UPDATE on conflict |
| `TestRoomAutoQueueRepo_AppendHistory_CapEnforced` | `PostgresRoomAutoQueueRepository` | 51st entry triggers a DELETE that preserves the 50 newest |
| `TestRoomAutoQueueRepo_GetRecentHistory_OrderedDesc` | `PostgresRoomAutoQueueRepository` | Returns newest-first; respects `limit` |
| `TestRoomAutoQueueRepo_PerRoomIsolation` | `PostgresRoomAutoQueueRepository` | Two rooms; each has its own config + history; cross-room reads return empty |

### WebSocket / hub tests

| Test | Layer | What it pins |
|---|---|---|
| `TestRoomWSHub_BroadcastRoomAutoQueueAdded_Dispatch` | `ws.RoomWSHub` | Only clients in the matching room receive the event |
| `TestRoomWSHub_BroadcastRoomAutoQueueAdded_DoesNotLeakToGlobal` | `ws.RoomWSHub` | The event does NOT reach clients of the global `/ws` endpoint |
| `TestRoomWSHub_BroadcastRoomAutoQueueAdded_SeqAllocatedByHubLoop` | `ws.RoomWSHub` | `dispatch` does NOT allocate a `seq_num`; the hub loop stamps it on dequeue |
| `TestRoomWSHub_BroadcastRoomAutoQueueAdded_InitialSyncFloor` | `ws.RoomWSHub` | The seq allocated for `room_auto_queue_added` is strictly greater than the seq stamped on `room_queue_sync` for the same client |
| `TestRoomWSHub_BroadcastRoomAutoQueueAdded_PayloadShape` | `ws.RoomWSHub` | Payload carries `room_slug`, `song`, `source_song_title`, `current_index`, `current_song`, `status`, `elapsed`, `state` |
| `TestRoomWSHub_StaleCandidate_DoesNotBroadcast` | `ws.RoomWSHub` | A stale `AddRoomAutoQueueSong` result MUST NOT produce a `room_auto_queue_added` broadcast |

### HTTP handler tests

| Test | Layer | What it pins |
|---|---|---|
| `TestRoomAutoQueue_Toggle_HostSucceeds` | `delivery/http` | Host toggle returns 200 with new config; per-room config event broadcast |
| `TestRoomAutoQueue_Toggle_AdminSucceeds` | `delivery/http` | Admin toggle returns 200 with new config |
| `TestRoomAutoQueue_Toggle_GuestForbidden` | `delivery/http` | Guest toggle returns 403 |
| `TestRoomAutoQueue_Toggle_MissingBody_400` | `delivery/http` | Body `{enabled: <bool>}` is required |
| `TestRoomAutoQueue_Toggle_ArchivedRoom_409` | `delivery/http` | Archived room returns 409 |
| `TestRoomAutoQueue_Toggle_RoomNotFound_404` | `delivery/http` | Unknown slug returns 404 |
| `TestRoomAutoQueue_Status_AnyMember_200` | `delivery/http` | Any active member may read; returns `{enabled, strategy}` |
| `TestRoomAutoQueue_Status_NonMember_403` | `delivery/http` | Non-member returns 403 |
| `TestRoomAutoQueue_Toggle_DoesNotAffectGlobalConfig` | `delivery/http` | Toggling the per-room setting MUST NOT mutate `auto_queue_config.id=1` |

### Frontend tests (Vitest)

| Test | Layer | What it pins |
|---|---|---|
| `applyRoomAutoQueueAdded_PrefersFullState` | `globalStore` | `payload.state` is applied; fallback fields ignored |
| `applyRoomAutoQueueAdded_FallbackPath` | `globalStore` | `current_index`, `current_song`, `status`, `elapsed` applied; global `queueState` untouched |
| `applyRoomAutoQueueAdded_IsolatedSlice` | `globalStore` | The mutator MUST NOT touch global `queueState`, `currentUser`, `voteSessions`, or `autoQueueConfig` |
| `applyRoomAutoQueueConfigChanged` | `globalStore` | Per-room `autoQueueConfig` slice updated; global `autoQueueConfig` untouched |
| `RoomView.RoomAutoQueueAdded_RendersBadge` | `RoomView` | `⚡ auto` badge appears for `added_by === 'system:autoqueue'` |
| `RoomView.RoomAutoQueueAdded_NoGlobalStoreMutate` | `RoomView` | The listener MUST NOT mutate global `queueState` |
| `RoomView.RadioToggle_GatedOnHostAdmin` | `RoomView` | Toggle button visible only for host/admin; disabled for guest |

### Manual / E2E (if a future slice adds it; R09e does not add E2E)

- Two browsers, one room; toggle room auto-queue in browser A; observe `room_auto_queue_config_changed` in browser B.
- Drain the room queue to "current is last" with room auto-queue enabled; observe a related song added with `⚡ auto` badge; confirm `room_auto_queue_added` carries the post-mutation state.
- Toggle the GLOBAL auto-queue in browser A while a room is running; the room's setting MUST remain unchanged.
- Toggle the room auto-queue in browser A; the global auto-queue config MUST remain unchanged.
- Disable the room auto-queue in browser A while a slow fetch is in flight; observe the candidate dropped (no save, no history, no broadcast).
- Two rooms with auto-queue enabled; the rooms MUST NOT exchange candidates (per-room isolation).

## Cross-references

- R09a (lease-aware room playback controls) — `014-player-control-semantics.md` Implementation summary (R09a).
- R09b (room vote-to-skip) — `014-player-control-semantics.md` Implementation summary (R09b).
- R09c (room-scoped volume) — `014-player-control-semantics.md` Implementation summary (R09c).
- R09d (room-scoped previous playback) — `014-player-control-semantics.md` Implementation summary (R09d).
- Global auto-queue (R02 / R04) — `documents/03-features/auto-queue-feature.md`; `internal/domain/auto_queue.go`; `internal/usecase/autoqueue/interactor.go`; `internal/usecase/queue/interactor.go` (the queue-owned `AddAutoQueueSong`); `internal/infrastructure/persistence/postgres_auto_queue_repo.go`; `internal/infrastructure/youtube/yt_related_fetcher.go`.
- Sprint 004 stale-candidate hardening — `documents/03-features/auto-queue-feature.md` Bug Fixes & Improvements section; `internal/usecase/autoqueue/interactor_test.go` (the `TestCheckAndTrigger_StaleDuringBlockedFetch` / `_DisabledMidFlight` / `_SetEnabled*DuringInsertion` cases are the template the future room suite mirrors).
- Per-room queue and broadcaster contracts — `internal/usecase/roomqueue/interactor.go` (the `Broadcaster` interface); `internal/delivery/ws/room_hub.go`; `internal/delivery/ws/events.go` (the `EventRoom*` constants).

## Closure

R09e is a documentation-only planning/design sprint. R09e shapes the contract for the future room auto-queue slice; the runtime code, migrations, routes, WebSocket constants, and config changes are deferred to a future slice that conforms to this contract. R09e does NOT advance the R09 epic to closure; remaining R09 scope (full media-player device integration) is still deferred. When the future implementation slice closes, the implementation summary lands in `014-player-control-semantics.md` and the closure line is updated in `documents/00-project-management/SPRINTS/active.md` and `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md`.
