# Sprint 004: Authoritative Playback Advancement

## Status
Implementation complete pending Architect review. Sprint not advanced or closed.

**GitHub Issue #8:** Implementation applied; awaiting verification + Product Owner closure.

## Approved Goal
The goal of Sprint 004 is to solve GitHub Issue #8 by making the queue interactor the sole consistency owner for song-addition results and conditional auto-queue insertion. Song-addition mutations return the authoritative post-mutation playback snapshot (`current_index`, `current_song`, `status`, `elapsed`) from inside the same lock as the queue write, so neither the HTTP delivery layer nor any frontend client needs to re-load or infer state. The auto-queue interactor must revalidate its candidate after the slow yt-dlp fetch and atomically reject stale candidates with zero side effects. The host YouTube player must suppress the transient state callbacks the IFrame API emits during programmatic video loads while preserving the existing synchronization for genuine host play/pause interactions.

## Pre-Sprint vs. Desired Behavior
- **Pre-Sprint Behavior:**
  - `POST /api/queue/add` locked, mutated the queue, released the lock, then separately called `GetState` to infer the new song's position. A concurrent skip / song-ended / auto-queue / remove between unlock and reload could ship a stale position or playback state on both the `song_added` WebSocket broadcast and the HTTP response.
  - On `auto_queue_added`, the frontend forcibly promoted the newest auto-added song to `current_index` when the local store happened to show `status === 'paused'`. The frontend was inventing playback state the backend already owned.
  - The auto-queue interactor ran the slow `FetchRelated` outside any queue lock and committed the returned candidate even when the queue had advanced or grown upcoming songs in the interim. The stale insertion produced spurious queue saves, play-history entries, activity entries, and `auto_queue_added` broadcasts.
  - The host YouTube IFrame fired transient `PAUSED`/`BUFFERING`/`PLAYING` state-change events while `loadVideoById` swapped videos. The existing 300 ms debounce passed those straight through to `POST /api/queue/status`, racing a real backend pause against active playback.
- **Desired Behavior:**
  - The queue interactor returns an `AddSongResult` struct from `AddSong`, `AddSongDirect`, and the new `AddAutoQueueSong`, with `Song`, `Position`, `CurrentIndex`, `CurrentSong`, `Status`, `Elapsed`, and `Activity` populated under the mutation lock. Delivery passes those fields straight into the broadcast.
  - `song_added` and `auto_queue_added` carry the authoritative `current_index`, `current_song`, `status`, and `elapsed` fields additively. The frontend applies them and no longer infers playback state. The paused-state heuristic is removed; the store's `addSong` becomes a pure splice + queue recalc.
  - `AddAutoQueueSong` atomically rejects a candidate with the new sentinel `ErrAutoQueueStale` when the source song is no longer current, when an upcoming song now exists, or when the candidate is already in the queue. A stale rejection performs zero `Save`, zero activity, zero history append, and zero broadcast.
  - `NowPlaying.vue` arms an `isLoadingVideo` guard before calling `loadVideoById` / `stopVideo` and clears it on a stable matching `PLAYING` event (with a 1500 ms safety timeout). While engaged, the guard suppresses backend `setStatus` calls for transient `PAUSED`/`BUFFERING`/`PLAYING` callbacks; `ENDED` still routes to `api.songEnded()`. Genuine host play/pause still synchronizes via the existing `isUpdatingFromProp` prop watcher.

## Required Context
- `entity.Queue` already exposes `Songs`, `CurrentIndex`, `Status`, and `Elapsed`. No schema, persistence, or migration change is required to build the authoritative snapshot.
- `queue.Interactor.mu` is the single mutation lock for the queue. The autoqueue interactor's `mu` is unrelated and protects only the single-flight `triggering` flag.
- `autoqueue.Interactor` was already calling the queue interactor's `AddSongDirect` through an injected callback when configured. Sprint 004 replaces that callback with a stricter `AddAutoQueueSongFunc` that also performs revalidation. The legacy "direct repo access" fallback path is removed because it could not enforce the staleness rules atomically.
- The yt-dlp `FetchRelated` call is the only slow operation in the auto-queue path and remains outside the queue lock. The WebSocket broadcaster also remains outside the lock.

## REST Mappings
- `POST /api/queue/add`
  - Request body unchanged.
  - Response body unchanged: the single `entity.Song` object the previous implementation returned.
  - Response codes unchanged.

## WebSocket Compatibility
- Event names (`song_added`, `auto_queue_added`) and the envelope fields (`type`, `data`, `seq_num`, `timestamp`) are unchanged.
- `SongAddedData` adds `current_index`, `current_song`, `status`, and `elapsed`. Pre-existing `song`, `position`, and `activity` fields and their JSON tags are unchanged.
- `AutoQueueAddedData` adds `current_index`, `current_song`, `status`, and `elapsed`. Pre-existing `song`, `source_song_title`, and `activity` fields and their JSON tags are unchanged.
- The frontend WebSocket handler tolerates legacy backends that omit the new fields: each authoritative field is applied only when present (numeric type check for `current_index` / `elapsed`, truthiness check for `status`).
- No new events are emitted. Stale auto-queue rejections emit no WebSocket message.

## Persistence Compatibility
- No SQLite schema changes or migrations.
- Stale auto-queue rejection writes no row to `play_history`, no row to `activities`, and no update to `queue_state`.

## Frontend Behavior
- `addSong` in the store no longer infers `current_index`, `current_song`, or `status`. Insertion is a pure splice + `recalculateQueue`.
- `song_added` and `auto_queue_added` WebSocket handlers call `globalStore.addSong` / `addActivity` and then apply the authoritative snapshot via a shared `applyAuthoritativeSnapshot` helper. The paused-state heuristic that previously promoted the auto-queued song is removed.
- `NowPlaying.vue` adds the `isLoadingVideo` guard around the `videoId` watcher and consults the guard in `onPlayerStateChange`. The 300 ms debounce window for genuine pause/play interactions is preserved.

## Tests
- **Backend (`internal/usecase/queue/interactor_test.go`):**
  - `TestAddSong_AuthoritativeResult` covers a multi-song queue and asserts the result snapshot fields.
  - `TestAddSong_AuthoritativeResult_FirstSong` covers the auto-promotion behavior of `queue.Add` for an empty queue.
  - `TestAddAutoQueueSong_Success` covers the happy path.
  - `TestAddAutoQueueSong_StaleSource` covers source-song advancement during the fetch.
  - `TestAddAutoQueueSong_StaleUpcomingExists` covers a concurrent manual add making the trigger condition false.
  - `TestAddAutoQueueSong_StaleDuplicate` covers a duplicate candidate ID in the upcoming queue.
- **Backend (`internal/usecase/autoqueue/interactor_test.go`):**
  - `TestCheckAndTrigger_StaleDuringBlockedFetch` uses a blocking fetcher to deterministically reproduce the Issue #8 case where a queue mutation lands while `FetchRelated` is in flight, and asserts no save, no history, no activity, no broadcast.
  - `TestCheckAndTrigger_SuccessCarriesAuthoritativeBroadcast` asserts the broadcast payload carries `current_index`, `current_song`, `status`, and `elapsed`.
- **Backend (`internal/delivery/http/handlers_test.go`):**
  - `TestHandleAddSong_BroadcastCarriesAuthoritativeFields` decodes the captured `SongAddedData` from the mock broadcaster and asserts the additive fields.
- **Frontend (`frontend/src/services/__tests__/websocket.spec.js`):**
  - `song_added` applies `current_index`, `current_song`, `status`, and `elapsed`.
  - `auto_queue_added` with `status: 'paused'` from the backend does not promote the new song to current (paused-state heuristic regression).
  - `song_added` without authoritative fields does not call the authoritative setters (legacy-backend compatibility).
- **Frontend (`frontend/src/components/dashboard/__tests__/NowPlaying.spec.js`, new):**
  - Programmatic load with transient `PAUSED`/`BUFFERING` callbacks does not call `api.setStatus`.
  - Genuine host pause (no preceding load) still reaches `api.setStatus` after the 300 ms debounce.
  - `ENDED` still routes to `api.songEnded` even while the guard is engaged.

## Exclusions
- No changes to authentication, voting, priority, SQLite schemas, yt-dlp infrastructure, deployment, styling, or any frontend component outside `services/websocket.js`, `store/index.js`, and `components/dashboard/NowPlaying.vue`.
- No new REST endpoints, no removed REST endpoints, no renamed WebSocket events, no removed WebSocket fields.
- No commit, push, merge, PR, sprint advancement, or sprint closure performed by the implementation pass.

## Risks
- **Frontend tolerance of legacy backends:** the frontend treats the new fields as optional. If a future change makes them required, this must be documented and the helper updated.
- **Single-flight guard:** the autoqueue interactor's existing single-flight only blocks parallel `CheckAndTrigger` calls. The new staleness sentinel is the second layer of defence; both are needed because the fetch happens outside any queue lock.
- **YT IFrame timing:** the `LOADING_GUARD_MS = 1500` safety reset is the maximum window during which transient PAUSED/BUFFERING events are suppressed. If a load genuinely fails to start playing within 1.5 s, the guard releases and subsequent transient events would reach the backend; this matches the pre-Sprint behavior for that edge case.

## Verification Results
- `go test -count=1 ./internal/usecase/queue ./internal/usecase/autoqueue ./internal/delivery/http ./internal/delivery/ws` — PASS
- `go test -race -count=1 ./internal/usecase/queue ./internal/usecase/autoqueue ./internal/delivery/http ./internal/delivery/ws` — PASS
- `go test ./...` — BLOCKED (Pre-existing environmental blocker: `open letsencrypt-backend/accounts: permission denied`)
- `go test -race ./...` — BLOCKED (Pre-existing environmental blocker: `open letsencrypt-backend/accounts: permission denied`)
- `go vet ./...` — BLOCKED (Pre-existing environmental blocker: `open letsencrypt-backend/accounts: permission denied`)
- `cd frontend && npm run test:unit -- --run` — PASS (9 test files, 34 tests)
- `cd frontend && npm run build` — PASS (production build successful)
- `docker compose config` — PASS
- `git diff --check` — PASS
- `git status --short --untracked-files=all` — PASS (12 modified, 1 untracked: the new `NowPlaying.spec.js`)

**Sprint 004-Specific Failures:** None. All Sprint 004 tests pass.
