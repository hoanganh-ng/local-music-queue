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
    observed before insertion. The `addAutoQueueSong` callback runs while
    still holding the interactor's mutex so a `SetEnabled(false)` that
    lands after the post-fetch check has passed is forced to wait for the
    in-flight insertion to complete. The mutex is released IMMEDIATELY
    after the insertion call returns; the error inspection, the history
    append, and the broadcaster call all run OUTSIDE the auto-queue
    mutex so a concurrent `SetEnabled(false)` is not blocked on those
    downstream operations.
  - `NowPlaying.vue` arms a per-load `loadGeneration` counter. The guard
    remains engaged until the shared settlement helper confirms the
    component is still mounted, the expected generation is still current,
    the player's current video still matches the expected videoId,
    `player.getPlayerState()` reports `PLAYING`, and authoritative
    `props.status` still expects `playing`; the 250 ms confirmation and
    the 1.5-second safety path use the same helper. The
    `statusChangeTimeout` debounce scheduled for a previous song is
    cancelled when a new videoId arrives. ENDED is confirmed against the
    current generation and current player state before it advances the
    backend, and at most one `songEnded` request is issued per generation.

## Required Context
- `entity.Queue` already exposes `Songs`, `CurrentIndex`, `Status`, and
  `Elapsed`. No schema, persistence, or migration change is required to build
  the authoritative snapshot.
- `queue.Interactor.mu` is the single mutation lock for the queue. The
  `autoqueue.Interactor.mu` serializes the single-flight `triggering`
  flag, the pre-fetch and post-fetch `cfg.Enabled` checks, AND the
  queue-owned `addAutoQueueSong` insertion call against `SetEnabled`.
  Holding `autoqueue.Interactor.mu` across the `addAutoQueueSong`
  callback does NOT block on the queue mutation itself (which uses a
  different lock inside the callback); it only serializes the
  auto-queue decision against `SetEnabled`. A `SetEnabled(false)` that
  lands after the post-fetch check has passed must wait for the
  insertion to complete before it can run. The mutex is released
  IMMEDIATELY after the insertion call returns; the error inspection,
  the history append, and the broadcaster call all run OUTSIDE the
  auto-queue mutex so a concurrent `SetEnabled(false)` is not blocked
  on those downstream operations.
- The yt-dlp `FetchRelated` call is the only slow operation in the
  auto-queue path and remains outside both the queue lock and the
  auto-queue interactor's mutex. The WebSocket broadcaster and the
  history append run AFTER `autoqueue.Interactor.mu` is released
  immediately after the insertion call returns — they are NOT held
  under the post-fetch defer.

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
  `videoId` watcher. `settledGeneration` only advances through the shared
  generation-keyed settlement helper. At settlement time, whether invoked
  by the normal 250 ms confirmation path or the 1.5-second safety path,
  the component verifies the component is still mounted, the expected
  generation is still current, the player's currently loaded video (read
  from `player.getVideoUrl()`) still matches the expected videoId,
  `player.getPlayerState()` still reports `PLAYING`, and `props.status`
  still expects `playing`. If the safety path fires while the player is
  still buffering, paused, unstarted, or on another video, the generation
  remains guarded and later loading callbacks cannot send a backend pause.
  A `statusChangeTimeout` armed for a previous song is cancelled on every
  accepted PLAYING/PAUSED callback (cleared and nulled before any new
  timeout is scheduled), and again when the watcher arms a new generation.
  ENDED events are confirmed on a timer keyed to the current generation;
  before calling `api.songEnded()`, the component verifies it is still
  mounted, the generation is unchanged and settled, the player's current
  video still matches the expected videoId, and `player.getPlayerState()`
  still reports `ENDED`. Repeated ENDED callbacks while confirmation is
  pending or after a request has already been issued for the same
  generation are ignored. The ENDED confirmation and issued-request guard
  are reset on every new video generation and on unmount. Genuine host
  play/pause still synchronizes via the existing
  `isUpdatingFromProp` prop watcher (whose reset timer handle is now
  tracked, replaced safely on subsequent status changes, and cleared on
  unmount).

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
  - `TestCheckAndTrigger_SetEnabledBlockedDuringInsertion` —
    `addAutoQueueSong` blocks during the locked insertion; a concurrent
    `SetEnabled(false)` is verified to NOT complete until the insertion
    returns; on release, the insertion commits and the disable lands
    after. Linearization asserted deterministically via a 100 ms
    goroutine-blocking check.
  - `TestCheckAndTrigger_SetEnabledUnblockedAfterInsertion` —
    insertion completes, downstream broadcast and history persistence
    are parked; a concurrent `SetEnabled(false)` is verified to
    complete BEFORE the broadcast/history unblock, proving the mutex
    is released immediately after the insertion call returns and the
    downstream work runs outside the auto-queue mutex.
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
  - Settled current video: PAUSED then PLAYING within 300 ms cancels the
    paused `setStatus` before the 300 ms debounce fires; advancing past
    300 ms confirms no `setStatus('paused', ...)` was issued.
  - PAUSED with no recovery still synchronizes `setStatus('paused', ...)`
    exactly once even when repeated PAUSED callbacks fire within the
    debounce window.
  - Loading B then C before B settles: B's late PLAYING does not clear C's
    guard.
  - Stale ENDED during a programmatic load does not call `api.songEnded`.
  - Genuine ENDED for a settled current song calls `api.songEnded`.
  - Genuine host pause after settlement calls `api.setStatus('paused', ...)`.
  - PLAYING matching `props.status` arms a generation-keyed confirmation
    timer that settles the generation at fire time (re-checking mount,
    generation, current video, and player state); subsequent genuine
    PAUSED reaches the backend.
  - Confirmation timer does NOT settle when the player has already moved
    on (identity mismatch at confirmation fire time, e.g. C swapped in
    after B's late PLAYING armed a B-keyed confirmation).
  - Confirmation timer DOES settle when identity and state still match at
    fire time.
  - Safety timeout does NOT settle when the current player is still
    paused; a subsequent PAUSED remains guarded and does not call
    `setStatus('paused', ...)`, then matching confirmed PLAYING settles
    the generation and a later genuine PAUSED synchronizes once.
  - B then C: late PLAYING(B) and transient PAUSED(C) arriving after a B→C
    load (with mismatched videoId identity) do NOT trigger `setStatus` or
    `songEnded`.
  - Matching PLAYING(C) settles the generation; subsequent genuine
    PAUSED(C) reaches the backend via the 300 ms debounce.
  - Stale ENDED reporting a previous video's videoId does NOT call
    `api.songEnded`.
  - Matching ENDED for the current settled videoId calls `api.songEnded`
    after confirmation.
  - ENDED callback does NOT call `api.songEnded` when the same current
    player reports `PLAYING` at confirmation time.
  - ENDED callback does NOT call `api.songEnded` when the generation
    changes before confirmation.
  - Repeated matching ENDED callbacks for one generation issue exactly one
    `songEnded` request.
  - After component unmount, ENDED and PAUSED callbacks trigger no
    `setStatus` and no `songEnded`.

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

  In addition, the guard is keyed on a videoId-identity match read from
  the player's currently loaded video (parsed out of
  `player.getVideoUrl()`, with a documented `getPlayerState()` companion
  seam for tests). `event.target` identifies the player itself, NOT an
  arbitrary historical video event — callbacks are not given a
  fabricated per-event videoId identity. The mock player exposes
  `getVideoUrl()` and `getPlayerState()` and tests mutate the single
  player's actual current video/state rather than supplying a fabricated
  callback identity. A stale callback that passes the initial identity
  check is still rejected at the generation-keyed settlement step unless
  the player is still mounted, the expected generation has not changed,
  the player's current video still matches, `getPlayerState()` still
  reports `PLAYING`, and authoritative props still expect `playing`. The
  normal confirmation timer and 1.5-second safety timer both use this same
  helper, so the safety timer cannot settle a paused, buffering,
  unstarted, or wrong-video load. ENDED callbacks use a separate
  generation-keyed confirmation that re-checks settled generation, current
  video identity, and `getPlayerState() === ENDED` before calling the
  backend, with per-generation dedupe for pending and already-issued
  requests. An `isUnmounted` ref provides a hard kill-switch so
  any callback arriving after `onUnmounted` is a no-op even before the
  identity check. The guard is therefore keyed on BOTH a monotonic
  counter AND an exact-match current-player identity, and the actual
  player state at settlement/confirmation time — not "generation-safe" by
  counter alone.
- **Auto-queue mid-fetch mutex:** the interactor's mutex serializes the
  post-fetch re-check, the queue-owned `addAutoQueueSong` insertion, AND
  `SetEnabled`. The slow `FetchRelated` is held outside the mutex; a
  `SetEnabled` that lands mid-fetch is observed before the post-fetch
  check. A `SetEnabled` that lands AFTER the post-fetch check has
  passed is forced to wait for the in-flight insertion to complete
  (linearized after the insertion). The mutex is released IMMEDIATELY
  after the insertion call returns; the history append and broadcaster
  call run OUTSIDE the auto-queue mutex so a concurrent `SetEnabled`
  is not blocked on those downstream operations. If a future caller
  needs `SetEnabled` to be non-blocking, the serialization must be
  revisited.
- **Load-failure semantics:** callers that previously relied on
  `ErrAutoQueueStale` to absorb load failures must now distinguish the two.
  The HTTP delivery layer does not call auto-queue insertion directly, so
  no HTTP contract change is required.

## Verification Results
- `go test -count=1 ./internal/usecase/queue ./internal/usecase/autoqueue ./internal/delivery/http ./internal/delivery/ws` — PASS (97 tests across 4 packages)
- `go test -race -count=1 ./internal/usecase/queue ./internal/usecase/autoqueue ./internal/delivery/http ./internal/delivery/ws` — PASS (97 tests, no races)
- `go test ./...` — BLOCKED (Pre-existing environmental blocker: `TestAPIIntegration` and `TestSetupApp` in `cmd/` fail with `yt-dlp executable not found at` — the host does not have yt-dlp installed in the search path)
- `go test -race ./...` — BLOCKED (same pre-existing environmental blocker as `go test ./...`)
- `go vet ./...` — PASS (no issues found; the previously-recorded letsencrypt permission blocker is not surfacing in this environment)
- `cd frontend && npm run test:unit -- --run` — PASS (9 test files, 47 tests)
- `cd frontend && npm run build` — PASS
- `docker compose config` — PASS
- `git diff --check` — PASS (no whitespace/indent warnings)
- `git status --short --untracked-files=all` — PASS (6 modified files: 2 backend, 2 frontend, 1 docs, 1 package-lock; no new untracked files)

**Sprint 004-Specific Failures:** None observed in the focused test suites.
The repository-wide `go test ./...` and `go test -race ./...` remain
blocked by a pre-existing environmental issue (host missing the yt-dlp
binary in PATH; `cmd/` setup tests cannot bootstrap the app). `go vet ./...`
passes cleanly. Focused Go and frontend test suites pass under the
supported Node 20 line (host runs Node 22.22.1, compatible with the
toolchain pinned in `frontend/Dockerfile`).

## Outstanding Review Findings — Second Pass
The first review pass left four categories of findings outstanding.
The second implementation pass addresses each:

1. **Debounce cancel-on-every-transition:** the `statusChangeTimeout`
   is now cleared and nulled on every accepted PLAYING/PAUSED callback
   before any new timeout is scheduled. A recovered PAUSED→PLAYING
   within 300 ms no longer leaves a stale `setStatus('paused', ...)`
   pending against the new song. A separate test
   (`PAUSED with no recovery still synchronizes setStatus("paused")
   exactly once`) proves a no-recovery PAUSED still synchronizes
   exactly once.
2. **Player-identity seam and host-player confirmations:** the production code now reads
   `player.getVideoUrl()` (parsed) for identity and
   `player.getPlayerState()` for state, with the mock implementing
   both. `event.target` is the single stable player reference; tests
   mutate the player's actual current video/state, not a fabricated
   per-callback identity. Settling a generation requires a
   shared generation-keyed helper that re-checks mount, generation,
   current video, current `PLAYING` state, and authoritative
   `props.status === 'playing'` at fire time; the 250 ms confirmation
   and 1.5-second safety path both use that helper. ENDED uses a
   separate generation-keyed confirmation that requires the generation to
   remain current and settled, the current player video to match the
   expected videoId, and `getPlayerState() === ENDED` before calling
   `api.songEnded()`. Pending and already-issued ENDED requests are
   deduplicated per generation and reset on new video generations and
   unmount.
3. **Auto-queue mutex release:** `i.mu` is now released IMMEDIATELY
   after the `addAutoQueueSong` callback returns. The insertion error
   is inspected, the history entry is appended, and the broadcaster
   is called — all OUTSIDE the auto-queue mutex. New tests
   `TestCheckAndTrigger_SetEnabledUnblockedAfterInsertion` and
   `TestCheckAndTrigger_SetEnabledUnblockedDuringHistoryPersistence`
   assert this linearization deterministically. The original
   `TestCheckAndTrigger_SetEnabledBlockedDuringInsertion` still passes
   (insertion itself remains serialized with `SetEnabled`).
4. **`isUpdatingFromProp` reset timer:** the reset timeout handle is
   now stored in `isUpdatingFromPropResetTimer`. Subsequent status
   changes replace the handle safely (`clearTimeout` before scheduling
   a new one). The handle is cleared in `onUnmounted` so a pending
   reset cannot fire after the component has been torn down.
