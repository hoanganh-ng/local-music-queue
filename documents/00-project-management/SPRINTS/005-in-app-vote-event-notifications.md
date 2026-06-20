# Sprint 005 — In-App Vote Event Notifications

## Goal

Add accessible in-app toast notifications for live `vote_updated` and `vote_resolved` WebSocket events while suppressing active vote sessions replayed during initial connection or reconnection.

## Scope

- Add an optional backend `initial_sync` marker to `vote_updated` payloads.
- Mark only active vote sessions replayed by `Hub.RegisterHandler` during WebSocket bootstrap as `initial_sync: true`.
- Preserve existing vote event names, envelope fields, vote session fields, sequence behavior, and live broadcast behavior.
- Add a presentation-neutral frontend `onVoteEvent(callback)` WebSocket subscription.
- Show vote notifications from `DashboardView.vue`, not from `websocket.js`.
- Suppress toast notifications for bootstrap `vote_updated` events while keeping existing store updates.
- Deduplicate logical vote notifications.

## Out of Scope

- Voting rules, thresholds, expiry, persistence, queue behavior, authentication, authorization, auto-queue, priority, deployment, and unrelated UI.
- Commits, pushes, merges, issue closure, history rewrites, or sprint advancement.

## Implementation Notes

- `ws.VoteUpdatedData` now has `InitialSync bool json:"initial_sync,omitempty"`.
- `Hub.RegisterHandler` sets `InitialSync: true` only for active vote sessions replayed to a newly connected client.
- Live HTTP vote broadcasts and expiry resolution broadcasts keep their existing paths and do not set `InitialSync`.
- `WebSocketClient.onVoteEvent(callback)` returns an unsubscribe function and catches callback errors.
- `DashboardView.vue` maps vote notifications as:
  - live `vote_updated`: info
  - `vote_resolved` with `outcome: "passed"`: success
  - `vote_resolved` with `outcome: "expired"`: info
- Vote update dedupe uses event type, session ID, session creation time, and vote count.
- Vote resolution dedupe uses event type, session ID, outcome, and activity timestamp.

## Verification

- `go test -count=1 ./internal/delivery/ws ./internal/delivery/http` — PASS
  - `ok local-music-queue/internal/delivery/ws 0.866s`
  - `ok local-music-queue/internal/delivery/http 0.970s`
- `npm run test:unit -- --run src/services/__tests__/websocket.spec.js src/views/__tests__/DashboardView.spec.js` — PASS
  - 2 test files passed, 18 tests passed
- `go test -count=1 ./...` — BLOCKED by local environment / pre-existing test fixture dependency
  - `cmd/server` failed because `yt-dlp` was unavailable:
    - `TestAPIIntegration`: `yt-dlp executable not found at : exec: "": executable file not found in $PATH`
    - `TestSetupApp`: `yt-dlp executable not found at /home/vi0l3tsc0rpi0n/linux-softwares/yt-dlp`
  - Other packages passed.
- `go test -race -count=1 ./...` — BLOCKED by the same local `yt-dlp` dependency in `cmd/server`
  - Other packages passed.
- `go vet ./...` — PASS
- `go test -race -count=1 ./internal/delivery/ws ./internal/delivery/http` — PASS
  - `ok local-music-queue/internal/delivery/ws 1.889s`
  - `ok local-music-queue/internal/delivery/http 3.379s`
- `npm run test:unit -- --run` — PASS
  - 9 test files passed, 61 tests passed
- `npm run build` — PASS
  - Vite production build completed successfully.
- `git diff --check` — PASS
- `git status --short --branch` — PASS
  - Working tree contains only Sprint 005 implementation/documentation changes; no commits were made.
