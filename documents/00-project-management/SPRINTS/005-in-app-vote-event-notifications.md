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

## Deferred Future Work

- Browser/device push notifications.
- User notification preferences.
- Optional notification sounds and vibration.

## Verification

- Branch: `dev`
- Review-fix base commit: `31706d7d0e973a6627194529cd188baa0a509f8e`
- Current HEAD during verification: `31706d7d0e973a6627194529cd188baa0a509f8e`
- `go test -count=1 ./internal/delivery/ws ./internal/delivery/http ./internal/usecase/vote` — PASS
  - `ok local-music-queue/internal/delivery/ws 0.867s`
  - `ok local-music-queue/internal/delivery/http 1.099s`
  - `ok local-music-queue/internal/usecase/vote 0.004s`
- `go test -race -count=1 ./internal/delivery/ws ./internal/delivery/http ./internal/usecase/vote` — PASS
  - `ok local-music-queue/internal/delivery/ws 1.888s`
  - `ok local-music-queue/internal/delivery/http 3.204s`
  - `ok local-music-queue/internal/usecase/vote 1.019s`
- `cd frontend && npm run test:unit -- --run src/services/__tests__/websocket.spec.js src/views/__tests__/DashboardView.spec.js` — PASS
  - 2 test files passed, 21 tests passed
- `cd frontend && npm run test:unit -- --run` — PASS
  - 9 test files passed, 64 tests passed
- `cd frontend && npm run build` — PASS
  - Vite production build completed successfully.
- `go vet ./...` — PASS
- `git diff --check` — PASS
  - No output.
- `git status --short --branch` — PASS
  - `## dev...origin/dev`
  - ` M documents/00-project-management/SPRINTS/005-in-app-vote-event-notifications.md`
  - ` M frontend/src/services/__tests__/websocket.spec.js`
  - ` M frontend/src/services/websocket.js`
  - ` M frontend/src/views/__tests__/DashboardView.spec.js`
