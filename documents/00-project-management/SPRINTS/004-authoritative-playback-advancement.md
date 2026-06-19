# Sprint 004: Authoritative Playback Advancement

## Status
**Implementation in progress — review findings outstanding.** The first
implementation pass landed but did not satisfy all review findings. The
implementer has applied the review fixes; the sprint has NOT been advanced or
closed.

**GitHub Issue #8:** Implementation applied; awaiting verification + Product
Owner closure.

> **Compatibility and completion claims have been removed from this document
> until Architect review confirms the second-pass implementation is correct.**

## Approved Goal
The goal of Sprint 004 is to solve GitHub Issue #8 by making the queue
interactor the sole consistency owner for song-addition results and
conditional auto-queue insertion. Song-addition mutations return the
authoritative post-mutation playback snapshot
(`current_index`, `current_song`, `status`, `elapsed`) from inside the same
lock as the queue write, so neither the HTTP delivery layer nor any frontend
client needs to re-load or infer state. The auto-queue interactor must
revalidate its candidate after the slow yt-dlp fetch, re-check that
auto-queue is still enabled, and atomically reject stale or disabled-mid-flight
candidates with zero side effects. The host YouTube player must suppress the
transient state callbacks the IFrame API emits during programmatic video
loads while preserving the existing synchronization for genuine host
play/pause interactions.

## Pre-Sprint vs. Desired Behavior
- **Pre-Sprint Behavior:**
  - `POST /api/queue/add` locked, mutated the queue, released the lock, then
    separately called `GetState` to infer the new song's position. A
    concurrent skip / song-ended / auto-queue / remove between unlock and
    reload could ship a stale position or playback state on both the
    `song_added` WebSocket broadcast and the HTTP response.
  - On `auto_queue_added`, the frontend forcibly promoted the newest
    auto-added song to `current_index` when the local store happened to show
    `status === 'paused'`. The frontend was inventing playback state the
    backend already owned.
  - The auto-queue interactor ran the slow `FetchRelated` outside any queue
    lock and committed the returned candidate even when the queue had
    advanced or grown upcoming songs in the interim. The stale insertion
    produced spurious queue saves, play-history entries, activity entries,
    and `auto_queue_added` broadcasts.
  - `SetEnabled(false)` could land between `FetchRelated` start and the
    locked insertion; the candidate was still appended because the auto-queue
    interactor did not re-validate `cfg.Enabled` after the slow fetch.
  - The host YouTube IFrame fired transient `PAUSED`/`BUFFERING`/`PLAYING`
    state-change events while `loadVideoById` swapped videos. A single
    undifferentiated load guard could be cleared by a late PLAYING
    callback belonging to an older video load, racing a real backend pause
    against playback for the newest song.
- **Desired Behavior:**
  - The queue interactor returns an `AddSongResult` struct from `AddSong`,
    `AddSongDirect`, and the new `AddAutoQueueSong`. The snapshot carries
    `Song`, `Position`, `PreviousCurrentIndex`, `CurrentIndex`, `CurrentSong`,
    `Status`, `Elapsed`, `PlaybackAdvanced`, and `Activity` — all populated
    under the same lock as the mutation.
  - `AddAutoQueueSong` distinguishes a repository load failure (wrapped
    operational error) from a stale-but-valid queue state (`ErrAutoQueueStale`).
    The former propagates to the caller so infrastructure failures are not
    silently masked.
  - `song_added` and `auto_queue_added` carry the authoritative `current_index`,
    `current_song`, `status`, and `elapsed` fields additively. The frontend
    applies them and no longer infers playback state. The paused-state
    heuristic is removed; the store's `addSong` becomes a pure splice +
    queue recalc. A legacy payload (no authoritative fields) triggers the
    single approved frontend fallback: if the queue had no current song and
    the just-inserted song is the first song, promote it to current, set
    status to playing, and reset elapsed.
  - `CheckAndTrigger` re-validates `cfg.Enabled` after the slow fetch and
    before queue insertion. The slow fetch is held outside the interactor's
    mutex; both `SetEnabled` and the pre-fetch and post-fetch config checks
    share that mutex so a `SetEnabled(false)` that lands mid-fetch is
    observed before insertion.
  - `NowPlaying.vue` arms a per-load `loadGeneration` counter. The guard
    remains engaged until a stable PLAYING matching `props.status` arrives
    for the current generation; the `statusChangeTimeout` debounce scheduled
    for a previous song is cancelled when a new videoId arrives; an ENDED
    callback for a non-settled generation must not advance the backend.

## Required Context
- `entity.Queue` already exposes `Songs`, `CurrentIndex`, `Status`, and
  `Elapsed`. No schema, persistence, or migration change is required to build
  the authoritative snapshot.
- `queue.Interactor.mu` is the single mutation lock for the queue. The
  `autoqueue.Interactor.mu` serializes the single-flight `triggering` flag
  AND the pre-fetch / post-fetch `cfg.Enabled` checks against `SetEnabled`.
- The yt-dlp `FetchRelated` call is the only slow operation in the
  auto-queue path and remains outside both the queue lock and the
  auto-queue interactor's mutex. The WebSocket broadcaster also remains
  outside both locks.

## REST Mappings
- `POST /api/queue/add`
  - Request body unchanged.
  - Response body unchanged: the single `entity.Song` object the previous
    implementation returned.
  - Response codes unchanged.

## WebSocket Compatibility
- Event names (`song_added`, `auto_queue_added`) and the envelope fields
  (`type`, `data`, `seq_num`, `timestamp`) are unchanged.
- `SongAddedData` adds `current_index`, `current_song`, `status`, and
  `elapsed`. Pre-existing `song`, `position`, and `activity` fields and
  their JSON tags are unchanged.
- `AutoQueueAddedData` adds `current_index`, `current_song`, `status`, and
  `elapsed`. Pre-existing `song`, `source_song_title`, and `activity`
  fields and their JSON tags are unchanged.
- The frontend WebSocket handler tolerates legacy backends that omit the new
  fields: it applies only the single approved first-song fallback (promote
  the inserted song to current if the queue was empty) when the payload
  lacks authoritative fields.
- No new events are emitted. Stale or disabled-mid-flight auto-queue
  rejections emit no WebSocket message.

## Persistence Compatibility
- No SQLite schema changes or migrations.
- Stale or disabled-mid-flight auto-queue rejection writes no row to
  `play_history`, no row to `activities`, and no update to `queue_state`.

## Frontend Behavior
- `addSong` in the store no longer infers `current_index`, `current_song`,
  or `status`. Insertion is a pure splice + `recalculateQueue`.
- `song_added` and `auto_queue_added` WebSocket handlers apply the
  authoritative snapshot via a shared `applyAuthoritativeSnapshot` helper
  when the payload carries authoritative fields; otherwise they apply the
  single approved `applyLegacyFirstSongFallback`.
- `NowPlaying.vue` arms a per-load `loadGeneration` counter in the
  `videoId` watcher. `settledGeneration` only advances on a stable PLAYING
  event matching `props.status`. A `statusChangeTimeout` armed for a
  previous song is cancelled when the watcher arms a new generation. An
  ENDED event is routed to `api.songEnded()` only when
  `loadGeneration === settledGeneration`. Genuine host play/pause still
  synchronizes via the existing `isUpdatingFromProp` prop watcher.

## Tests
- **Backend (`internal/usecase/queue/interactor_test.go`):**
  - `TestAddSong_AuthoritativeResult` — multi-song queue; snapshot fields
    match post-mutation state; `PlaybackAdvanced` is `false`.
  - `TestAddSong_AuthoritativeResult_FirstSong` — empty queue; snapshot
    reflects auto-promotion; `PlaybackAdvanced` is `true`;
    `PreviousCurrentIndex` is `-1`.
  - `TestAddSong_AuthoritativeResult_ExhaustedPaused` — last-song paused
    tail; `PlaybackAdvanced` is `true`.
  - `TestAddAutoQueueSong_Success` — happy path;
    `PreviousCurrentIndex == CurrentIndex == 0`; `PlaybackAdvanced` is
    `false` (auto-queue appends, does not advance).
  - `TestAddAutoQueueSong_StaleSource`, `TestAddAutoQueueSong_StaleUpcomingExists`,
    `TestAddAutoQueueSong_StaleDuplicate` — predicates reject stale
    candidates with zero save / activity / history side effects.
  - `TestAddAutoQueueSong_LoadFailureIsOperational` — repository load
    failure returns a wrapped error, NOT `ErrAutoQueueStale`; no save, no
    activity.
- **Backend (`internal/usecase/autoqueue/interactor_test.go`):**
  - `TestCheckAndTrigger_StaleDuringBlockedFetch` — concurrent queue
    mutation lands while `FetchRelated` is blocked; candidate dropped, no
    history, no activity, no broadcast.
  - `TestCheckAndTrigger_SuccessCarriesAuthoritativeBroadcast` —
    `auto_queue_added` payload carries the additive snapshot fields.
  - `TestCheckAndTrigger_DisabledMidFlight` — `SetEnabled(false)` lands
    while `FetchRelated` is blocked; candidate dropped, no history, no
    activity, no broadcast; the slow fetch is verified to NOT be held
    under the auto-queue interactor's mutex.
  - `TestCheckAndTrigger_LoadFailurePropagates` — load failure surfaced by
    the callback reaches `CheckAndTrigger` and is NOT collapsed into
    `ErrAutoQueueStale`.
- **Backend (`internal/delivery/http/handlers_test.go`):**
  - `TestHandleAddSong_BroadcastCarriesAuthoritativeFields` — broadcast
    payload carries the additive fields captured under the locked
    mutation.
- **Frontend (`frontend/src/services/__tests__/websocket.spec.js`):**
  - `song_added` applies `current_index`, `current_song`, `status`, and
    `elapsed` when authoritative fields are present.
  - `auto_queue_added` with `status: 'paused'` does NOT promote the new
    song to current (paused-state heuristic regression).
  - Legacy `song_added` with empty queue and a first song: promotes the
    inserted song to current, sets status to playing, resets elapsed.
  - Legacy `song_added` with a non-empty queue: does NOT shift
    `current_index` or change status.
- **Frontend (`frontend/src/components/dashboard/__tests__/NowPlaying.spec.js`):**
  - Pause debounce scheduled for song A, then a swap to song B cancels the
    pending `setStatus('paused', ...)`.
  - Loading B then C before B settles: B's late PLAYING does not clear C's
    guard.
  - Stale ENDED during a programmatic load does not call `api.songEnded`.
  - Genuine ENDED for a settled current song calls `api.songEnded`.
  - Genuine host pause after settlement calls `api.setStatus('paused', ...)`.
  - PLAYING matching `props.status` advances `settledGeneration` so
    subsequent genuine PAUSED reaches the backend.

## Exclusions
- No changes to authentication, voting, priority, SQLite schemas, yt-dlp
  infrastructure, deployment, styling, or any frontend component outside
  `services/websocket.js`, `store/index.js`, and
  `components/dashboard/NowPlaying.vue`.
- No new REST endpoints, no removed REST endpoints, no renamed WebSocket
  events, no removed WebSocket fields.
- No commit, push, merge, PR, sprint advancement, or sprint closure
  performed by the implementation pass.

## Risks
- **Generation counter scope:** `loadGeneration` is local to the
  `NowPlaying.vue` component instance. If two `NowPlaying` components
  mount simultaneously (e.g. host + viewer), each owns an independent
  counter; both must hold the same `loadGeneration` semantics in their own
  state. This is acceptable for the Sprint 004 scope but is a known
  locality boundary.
- **Auto-queue mid-fetch mutex:** the interactor's mutex serializes the
  post-fetch re-check and `SetEnabled`. The slow `FetchRelated` is held
  outside the mutex; a `SetEnabled` during the slow window is observed
  before insertion. If a future caller needs `SetEnabled` to be
  non-blocking, the serialization must be revisited.
- **Load-failure semantics:** callers that previously relied on
  `ErrAutoQueueStale` to absorb load failures must now distinguish the two.
  The HTTP delivery layer does not call auto-queue insertion directly, so
  no HTTP contract change is required.

## Verification Results
- `go test -count=1 ./internal/usecase/queue ./internal/usecase/autoqueue ./internal/delivery/http ./internal/delivery/ws` — PASS
- `go test -race -count=1 ./internal/usecase/queue ./internal/usecase/autoqueue ./internal/delivery/http ./internal/delivery/ws` — PASS
- `go test ./...` — BLOCKED (Pre-existing environmental blocker: `open letsencrypt-backend/accounts: permission denied`)
- `go test -race ./...` — BLOCKED (Pre-existing environmental blocker)
- `go vet ./...` — BLOCKED (Pre-existing environmental blocker)
- `cd frontend && npm run test:unit -- --run` — PASS (9 test files, 38 tests)
- `cd frontend && npm run build` — PASS
- `docker compose config` — PASS
- `git diff --check` — PASS
- `git status --short --untracked-files=all` — PASS (8 modified files; no new untracked files beyond the Sprint 004 doc and existing `NowPlaying.spec.js`)

**Sprint 004-Specific Failures:** None observed in the focused test suites.
The repository-wide `go test ./...`, `go test -race ./...`, and `go vet ./...`
remain blocked by the pre-existing `letsencrypt-backend/accounts: permission
denied` environmental issue first recorded in the Sprint 003 baseline.
