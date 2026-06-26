# Sprint 020 / A01 — Allowed Origins and WebSocket Origin Policy

## Status

Closed (2026-06-26). Verification gate passed; pending Product Owner acceptance.

## Sprint name

Sprint 020 / A01 — Allowed Origins and WebSocket Origin Policy

## Goal

Introduce a single shared allowed-origin policy that gates both HTTP CORS and WebSocket upgrade requests, with an explicit, non-empty `ALLOWED_ORIGINS` configuration requirement outside `APP_ENV=local`. Remove the legacy `?user_id=...` WebSocket query parameter as an identity or daily-priority attribution signal.

## Commit range

`c6615cb^..HEAD` — 11 commits across 17 files (per `git diff --name-only c6615cb^..HEAD`).

## Current behavior (pre-A01 baseline)

- HTTP CORS middleware had no shared origin policy; either permissive defaults or unconfigured.
- WebSocket upgrades did not consult any origin allow-list; the legacy `?user_id=...` query parameter was used for identity and daily-priority attribution.
- No failure-fast behavior on missing origin configuration outside development.

## Desired behavior (post-A01)

- `ALLOWED_ORIGINS` is a comma-separated list shared by HTTP CORS and WebSocket upgrades, exposed via `internal/infrastructure/config` (`AllowedOrigins`).
- In `APP_ENV=local` the server falls back to loopback defaults.
- In any other environment the server refuses to start without a non-empty `ALLOWED_ORIGINS`.
- HTTP CORS reflects the allowed origin with `Vary: Origin`; server-to-server requests (empty `Origin`) receive no `Access-Control-Allow-Origin` header.
- WebSocket upgrades are gated by the same configured allow list. Empty `Origin` is permitted for non-browser / server-to-server traffic but never implies authentication.
- The legacy `?user_id=...` WebSocket query parameter is parsed only for diagnostic logging; it no longer affects auth, attribution, or daily-priority attribution. Identity requires `?session_token=<opaque>` (R05).
- Frontend `frontend/src/services/websocket.js` no longer sends the legacy `user_id` on connect; matching test updated.

## Required context

- R05 session-token posture (`?session_token=<opaque>`, bearer-token REST) — preserved unchanged.
- Existing `internal/delivery/http` CORS middleware shape and `internal/delivery/ws` Hub accept path.

## Requirements

1. **Shared policy.** A single `internal/delivery/origin` package exposes `Allowed(origin string) bool` plus helpers used by both HTTP CORS and WebSocket upgrader.
2. **Configuration.** `internal/infrastructure/config` exposes `AllowedOrigins`; `Validate` fails fast with a clear error when `APP_ENV != "local"` and the list is empty.
3. **HTTP CORS.** Middleware in `cmd/server/middleware.go` echoes the matching origin with `Vary: Origin`; methods/headers are gated; disallowed origins are not echoed.
4. **WebSocket upgrade.** `internal/delivery/ws/hub.go` consults the same policy on `CheckOrigin`; the legacy `?user_id=...` query is parsed only for diagnostic logging.
5. **Frontend.** `frontend/src/services/websocket.js` stops sending `user_id`; unit test updated.
6. **Testing.** Origin-package unit tests, config Validate hermetic subtests, CORS response tests, WebSocket accept tests, and the Postgres lifecycle test (with `APP_ENV=test` plus explicit `ALLOWED_ORIGINS`).

## Out of scope

- Changing the R05 session-token issuance, lifetime, or wire format.
- Adding per-route origin overrides.
- Rate limiting on WebSocket connections.
- New public read-only endpoints (those live in the planned R08).

## Verification (2026-06-26)

- `go test -count=1 ./internal/delivery/origin ./internal/delivery/ws ./internal/infrastructure/config ./cmd/server` — PASS.
- `go test -race -count=1 ./internal/delivery/ws ./cmd/server` — PASS.
- `cd frontend && npm run test:unit -- --run` (Node v20.20.2) — 11 files / 149 tests pass.
- `cd frontend && npm run build` (Node v20.20.2) — clean production build.
- The `TestSetupApp_PostgresDBStaysOpen` test in `cmd/server/main_pglifecycle_test.go` was patched to set `t.Setenv("ALLOWED_ORIGINS", "http://localhost:1111")` alongside `APP_ENV=test`, so it continues to exercise non-local setup with explicit allowed origins.

## Execution note

The A01 sprint scope is contained entirely within the commit range `c6615cb^..HEAD` (17 files). No scope split was required. The `documents/00-project-management/SPRINTS/README.md` index and `active.md` are updated to reflect closure, pending Product Owner acceptance.