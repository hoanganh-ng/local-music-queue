# R08 – Public read‑only API and CORS

**Status:** planned (stub – work not yet started)

**Sprint name:** Public read‑only API and CORS

## Goal

Allow external clients to query room state without full authentication and lock down cross‑origin requests with a clear CORS policy.  Provide read‑only endpoints (e.g. `GET /rooms/{id}/state`) that return the current playback queue, player status and basic room metadata.  These endpoints should be accessible from whitelisted origins only and never expose sensitive user information.  Implement proper caching headers to minimise load on the server.

## Current behaviour (pre‑sprint baseline)

Currently, all HTTP endpoints require a valid session token.  There is no way for unauthenticated or third‑party clients to fetch the state of a room.  The CORS middleware either allows all origins (`*`) or has not been tightened to restrict cross‑origin requests.  There are no caching directives on API responses, so repeated polling leads to unnecessary database hits.

## Desired behaviour (post‑sprint)

* Introduce a new public endpoint such as `GET /public/rooms/{room_code}/state` that returns:
  * Room metadata (name, owner display name, current player mode).
  * The active playback queue (ordered list of tracks with current votes).
  * The currently playing track and its elapsed time.
* Public endpoints should not return any personally identifying information or session data.  User IDs should be replaced with anonymised nicknames or omitted entirely.
* Add configuration for allowed origins (e.g. `PUBLIC_API_ALLOWED_ORIGINS`) and update CORS middleware to reflect the specific origin on responses, adding `Vary: Origin`.  Reject requests from disallowed origins with a 403 status.
* Apply appropriate caching headers (e.g. `Cache-Control: public, max-age=5`) on public responses to reduce load while keeping data reasonably fresh.
* Document the public API, including rate limits and expected response format.  Provide examples and clarify that write operations still require authentication.
* Ensure that read‑only WebSocket connections, if supported, respect the same origin policy and do not allow state‑changing commands.

## Required context

* Review the existing CORS middleware and session token authentication (R05) to understand how origins are currently handled.
* The planned origin configuration and lease semantics from sprint R06 may influence which requests are considered trusted.
* Understand the shape of the queue and player state from previous sprints (particularly R07) to structure the public response payload.

## Requirements

1. **Endpoint design.**  Define and document a public REST endpoint (or endpoints) for fetching room state.  Use room codes rather than internal IDs to avoid leaking database identifiers.
2. **CORS restrictions.**  Introduce environment configuration for allowed origins.  Middleware must echo back the specific origin header only if it matches the allowed list and set `Vary: Origin`.
3. **Response shaping.**  Sanitize sensitive fields (user IDs, emails) and return only what is necessary for display in an embeddable widget or external dashboard.  Include timestamps for cache invalidation if appropriate.
4. **Caching.**  Use HTTP caching headers on public endpoints.  Add unit tests to verify headers are present and correct.
5. **Documentation.**  Extend API documentation to cover the public endpoint.  Make clear that these endpoints are read‑only and do not require session tokens.
6. **Testing.**  Add integration tests verifying that requests from allowed origins succeed, disallowed origins fail, and the payload contains no sensitive data.

## Out of scope

* UI components or embeddable widgets that consume the new API – those will be addressed later.
* Changing existing authenticated endpoints or the session token mechanism.
* Rate limiting – although recommended, it may be implemented in a separate sprint.
* Implementing analytics or metrics for public API usage.

## Implementation guidance

* Keep public controllers separate from authenticated controllers to avoid accidental leakage of sensitive data.
* Provide a clear configuration key (e.g. `PUBLIC_API_ALLOWED_ORIGINS`) and parse it as a comma‑separated list.  Fall back to a safe default that disallows all external origins in non‑development environments.
* When shaping the response, consider representing users who added tracks by a hashed or anonymised value rather than their actual user ID.  This can be as simple as omitting the `added_by` field or using a sequential alias (e.g. “User 1”).
* Consider using ETags or `Last‑Modified` headers if the public data seldom changes and clients benefit from conditional requests.

## Execution note

This stub records the intent for the **public read‑only API and CORS** sprint.  When the sprint is executed, revisit these requirements to refine the scope, then update this document with the completed implementation details and mark its status as **closed** in both this file and in `ROOM_EPIC_SPRINT_SEQUENCE.md`.
