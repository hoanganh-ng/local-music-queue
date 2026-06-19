# Sprint 003: Trusted Owned-Song Removal

## Goal
Fix GitHub Issue #7 by allowing authenticated guests to remove their own upcoming songs while preserving host/admin removal authority.

The permission rule must be enforced by the backend using a trusted server-issued session identity.

## Status
In progress.

## Proposed Changes
- Introduce minimal in-memory session management for Google Login using URL-safe base64 opaque tokens.
- Add `session_token` and `session_expires_at` to `POST /api/auth/google` response.
- Protect `POST /api/queue/remove` with `Authorization: Bearer <token>` header, ignoring client-supplied user metadata.
- Enforce removal authorization on the backend queue use case under the existing mutation lock.
- Return a transport-neutral result from the queue use case.
- Map errors to 400, 401, 403, and 500 HTTP statuses.
- Update frontend to store session token/expiry in `sessionStorage` and send it in the Authorization header.
- Render the remove button only for songs the guest added or if they are host/admin.
- Handle auth expiration (401) and forbidden (403) states in the frontend.
