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

## Follow-up: Optional browser notifications (accepted before sprint close)

### Follow-up scope

- Add an opt-in browser/system `Notification` API for live vote events as a
  frontend-only enhancement.
- Preserve existing in-app toast behavior, `initial_sync` suppression, and
  vote event deduplication unchanged.
- Detect `Notification` API support and current permission on Dashboard load.
- Auto-enable browser notifications when permission is already `'granted'`
  (no persistent user toggle).
- Show a one-time dismissible CTA banner only when permission is `'default'`.
- Never re-prompt when permission is `'denied'`.
- Persist a local "dismissed/requested" flag under
  `lmq_browser_notifications_dismissed` so the CTA does not repeatedly
  reappear.
- Do not change backend REST or WebSocket payload contracts.
- Do not introduce service workers or push notifications in this sprint.

### Follow-up behavior

- A new composable `useVoteBrowserNotifications` owns all `Notification` API
  interaction (feature detection, permission, dismissed state, click-to-focus).
  `services/websocket.js` is not modified and remains UI-agnostic.
- `DashboardView.vue` calls `notifyVote(...)` from inside `handleVoteEvent`,
  after the existing dedupe `Set` write. The toast path below is unchanged.
- The composable's `canNotify` gate requires: supported, permission
  `'granted'`, and `document.hidden === true`. There is no user opt-out
  toggle — granting permission IS opting in.
- `showCta` is true only when: supported, permission `'default'`, and the
  dismissed flag is not set. Once the user clicks Enable (requesting
  permission) or dismiss (×), the flag is written and the banner never
  reappears.
- Title is short and derived from event type/outcome (`"Vote update"`,
  `"Vote passed"`, `"Vote expired"`). Body is `activity.description`
  truncated to 117 characters plus `...`.
- Notification `tag` reuses the existing `voteNotificationKey()` so the
  operating system dedupes repeated notifications.
- Notification click handler calls `window.focus()` then `notification.close()`.
- Permission is re-synced on `document.visibilitychange` so revocation in
  browser settings is picked up automatically.

### Follow-up implementation notes

- Composable module-level refs follow the existing `useToast`/`useConfirm`
  pattern.
- A single `document.visibilitychange` listener is installed at module scope
  with an `installed` guard for HMR safety.
- The `notifyVote(tag, title, body)` API is event-shape-agnostic; the caller
  passes the already-computed dedupe key.

## Verification (initial sprint close)

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

  ```text
  ## dev...origin/dev
   M documents/00-project-management/SPRINTS/005-in-app-vote-event-notifications.md
   M frontend/src/services/__tests__/websocket.spec.js
   M frontend/src/services/websocket.js
   M frontend/src/views/__tests__/DashboardView.spec.js
  ```

### Follow-up verification

- `cd frontend && npm run test:unit -- --run src/composables/__tests__/useVoteBrowserNotifications.spec.js src/views/__tests__/DashboardView.spec.js src/services/__tests__/websocket.spec.js` — PASS
- `cd frontend && npm run test:unit -- --run` — PASS
  - full Vitest suite, no regressions
- `cd frontend && npm run build` — PASS
  - Vite production build completed successfully.
- `grep -n "Notification" frontend/src/services/websocket.js` — empty
  - UI-agnostic invariant preserved.
- `git diff --check` — PASS
  - No output.
- `git status --short --branch` — recorded before commit (see Expected Output
  in the follow-up plan).
- Backend contracts unchanged: no files under `internal/`, `cmd/`, or other
  backend directories were modified.
- Browser API limitation: when `Notification` is not exposed by the browser
  (or by jsdom during tests), the toggle button is hidden via `v-if` and
  `unsupported.value === true` is honored. No fallback path is rendered.
