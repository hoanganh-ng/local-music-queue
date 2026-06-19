# Sprint 003: Trusted Owned-Song Removal

## Status
In progress, awaiting Architect review and Product Owner approval.

## Approved Goal
The goal of Sprint 003 is to solve GitHub Issue #7 by allowing authenticated guest users to remove their own upcoming songs from the queue, while ensuring hosts and admins retain complete administrative removal authority.
Security and permissions must be enforced strictly on the backend using server-issued session identity rather than trusting client-supplied metadata or frontend states.

## Pre-Sprint vs. Desired Behavior
- **Pre-Sprint Behavior:** The `POST /api/queue/remove` endpoint is completely unprotected. It accepts a payload containing `"index"` and `"requested_by"` and performs removal without verifying authentication, ownership, or authority. Client-supplied role and user parameters are trusted blindly.
- **Desired Behavior:** Guest users can only remove upcoming songs they added themselves. Host and admin users can remove any song in the queue. Authenticated session tokens must verify the caller's identity.

## Required Context
Song ownership is established using `AddedByID`, which is written when a user adds a song to the queue. For auto-queue (radio mode) and system songs, `AddedByID` is set to `0` (system). Guests are forbidden from removing these system-added songs.

## Permission Matrix
| Role  | Song Status | Owner (AddedByID matches actor) | Other Guest's Song | Auto-Queue Song (AddedByID = 0) |
| :---  | :---        | :---                            | :---               | :---                            |
| Guest | Upcoming    | Allowed                         | Forbidden          | Forbidden                       |
| Guest | Playing/Past| Forbidden                       | Forbidden          | Forbidden                       |
| Host  | Any Status  | Allowed                         | Allowed            | Allowed                         |
| Admin | Any Status  | Allowed                         | Allowed            | Allowed                         |

## Session Ownership & Single-Process Assumptions
- Sessions are issued upon successful Google authentication (`POST /api/auth/google`).
- Session tokens are cryptographically random 32-byte opaque strings, base64 URL-safe encoded.
- Sessions are transient and stored strictly in-memory. They are lost upon server restart.
- Designed for single-instance deployment; session state is not shared across cluster nodes or persisted to database storage.
- Session expiry is set to 12 hours from creation. A token is treated as expired if the current server time is equal to or later than the expiration timestamp (`now >= expiresAt`).

## REST Mappings
The endpoint `POST /api/queue/remove` requires:
- Header: `Authorization: Bearer <opaque-session-token>`
- Response codes:
  - **204 No Content:** Successful removal.
  - **400 Bad Request:** Malformed JSON, missing index, or invalid index.
  - **401 Unauthorized:** Missing, malformed, invalid, or expired session token.
  - **403 Forbidden:** The authenticated actor lacks permission to remove the song (e.g. guest trying to remove another user's song or currently playing song).
  - **500 Internal Server Error:** Database, persistence, or unexpected server failures.

## WebSocket Compatibility
- The existing event type `song_removed` and payload fields (`removed_index`, `new_index`, `status`, `activity`) are fully preserved.
- Delta updates are broadcast exactly once upon successful database persistence.
- No broadcasts are emitted on failed actions (400, 401, 403, 500).

## Persistence Compatibility
- No SQLite schema changes or database migrations are required.
- Transient sessions are managed in memory.

## Frontend Behavior
- Session tokens and expiry are kept in `sessionStorage` and attached to the `Authorization` header on all API calls.
- Persisted `localStorage` user profile is stripped of session tokens.
- Navigation guards intercept unauthenticated users or legacy users without active sessions, redirecting them to `/auth`.
- Removing a song sends a request to the backend and waits for the WebSocket delta event. No optimistic local state mutation occurs.
- Handles `401 Unauthorized` by clearing the session/user and redirecting to login, and `403 Forbidden` by displaying permission error toast.

## Tests
- **Backend Usecase/Repository/HTTP Tests:** Cover guest ownership permission rules, host/admin admin override, index bounds checks, concurrent session Create/Resolve, token expiry, reload failures, and database failures.
- **Frontend Vitest Tests:** Cover Google login session extraction, sessionStorage cleanup, legacy user guard redirect, remove button visibility, correct full index calculation, 401/403 routing, and non-optimistic mutation.

## Exclusions
- Redesigning OAuth or Google Authentication.
- Protecting endpoints other than `POST /api/queue/remove`.
- Persistent session storage in SQLite or redis.
- WebSocket-level authentication/authorization.

## Risks
- **In-Memory Session Loss:** Server restarts invalidate all active guest sessions, requiring clients to re-authenticate.
- **Other Endpoints Unprotected:** Backend security relies on per-request session tokens only for queue removal. Other playback controls (e.g., skip, play/pause) remain client-trusted.

## Verification Results
- `go test -count=1 ./internal/usecase/auth ./internal/usecase/queue ./internal/delivery/http ./internal/infrastructure/session` - PASS
- `go test ./...` - BLOCKED (Pre-existing environmental blocker: `open letsencrypt-backend/accounts: permission denied`)
- `go test -race ./...` - BLOCKED (Pre-existing environmental blocker: `open letsencrypt-backend/accounts: permission denied`)
- `go vet ./...` - BLOCKED (Pre-existing environmental blocker: `open letsencrypt-backend/accounts: permission denied`)
- `cd frontend && npm run test:unit -- --run` - BLOCKED (Environmental: `npm: command not found`)
- `cd frontend && npm run build` - BLOCKED (Environmental: `npm: command not found`)
- `docker compose config` - PASS
- `git diff --check` - PASS
- `git status --short --untracked-files=all` - PASS
