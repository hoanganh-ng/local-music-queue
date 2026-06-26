# R13 – Authentication and authorization hardening

**Status:** planned (stub – work not yet started)

**Sprint name:** Authentication and authorization hardening

## Goal

Strengthen the authentication and authorization model for the Local Music Queue.  Replace or augment session tokens with a more robust mechanism such as JSON Web Tokens (JWT) or refresh tokens.  Ensure that all endpoints enforce role‑based access control and that tokens are properly rotated and revoked.  The sprint aims to close any remaining security gaps identified in previous sprints.

## Current behaviour (pre‑sprint baseline)

Session tokens issued in sprint R05 provide basic authentication but lack features like refresh, expiration rotation and granular scopes.  Roles are implicitly defined (owner, member, guest), but enforcement is scattered across the codebase.  The system does not implement token revocation lists, and there is no mechanism to handle compromised tokens.  CORS and origin checks may still be permissive in some components.

## Desired behaviour (post‑sprint)

* **Token design**: Adopt JWT or another stateless token format that includes user identity, room context and expiry.  Optionally introduce refresh tokens so that access tokens can be short‑lived without requiring the user to reauthenticate frequently.
* **Scopes and roles**: Define explicit scopes or claims within tokens (e.g. `room:read`, `room:write`, `queue:modify`).  Update endpoint middleware to enforce these scopes.  Clearly document which roles (owner, member, guest) receive which scopes.
* **Token revocation**: Implement a revocation list (e.g. Redis or database) to invalidate tokens before they expire.  When a user leaves a room or is removed (R10), add their tokens to the revocation list.
* **Rotation and expiration**: Set reasonable expiration times on access tokens (e.g. 15 minutes) and refresh tokens (e.g. 24 hours).  Provide an endpoint to rotate tokens and require reauthentication if a refresh token expires or is revoked.
* **Origin enforcement**: Consolidate CORS and origin checks across HTTP and WebSocket protocols.  Apply the same allowed origins for all endpoints and drop any fallback paths.
* **Audit and logging**: Enhance logging around authentication events (login, token issuance, token rotation, revocation).  Ensure logs do not contain sensitive token values but capture enough context for security auditing.

## Required context

* Review the current session token implementation (R05) and any related middleware.
* Understand the outcomes of R06–R12, since these sprints may introduce new endpoints or roles that require authorisation.
* Familiarise with JWT standards, including signature algorithms, key rotation and best practices for token storage on the client.
* Evaluate existing infrastructure for storing revocation lists and secret keys (e.g. a Redis cluster or database).

## Requirements

1. **Token format.**  Choose a token format (JWT or equivalent) and define its payload structure.  Include `sub` (subject), `exp` (expiration), `iat` (issued at), `room_id` and `scope` claims.
2. **Issuance and rotation.**  Implement endpoints for login/authentication that issue access and refresh tokens.  Implement an endpoint to rotate refresh tokens, invalidating the old one.
3. **Middleware update.**  Replace existing session token middleware with JWT verification middleware.  Validate signatures, expiry and revocation.  Populate the request context with user information and scopes.
4. **Revocation mechanism.**  Provide a way to revoke tokens (e.g. storing revoked token IDs in Redis).  Check the revocation list on each request.
5. **Scope enforcement.**  Update endpoints to require appropriate scopes.  For example, queue modification endpoints should require `queue:modify`; room deletion requires `room:delete` and ownership.
6. **Testing.**  Add extensive tests for token creation, verification, rotation, revocation, and scope enforcement.  Include tests for token replay and tampering.
7. **Documentation.**  Update API authentication documentation.  Provide guidance for clients on storing tokens securely, refreshing them and handling token expiry.

## Out of scope

* Implementing multi‑factor authentication – this could be considered in a future enhancement.
* Integration with third‑party identity providers (OAuth login) – focus here is on internal token mechanics.
* UI changes related to login screens or token storage on the client side.

## Implementation guidance

* Use a well‑maintained JWT library that supports your chosen algorithm (e.g. HS256 or RS256).  If using symmetric signing (HS256), keep the secret key secure; for asymmetric signing (RS256), manage private/public keys accordingly.
* Consider splitting tokens by room context to prevent using a token issued for one room in another room.  Alternatively, include the `room_id` claim and validate it on each request.
* For revocation, store a short identifier (e.g. JWT ID or hash) in Redis with the token’s expiry as the TTL.  Check the store on every request.  This avoids having to store full tokens.
* Provide a migration path from existing session tokens.  During transition, accept both token types and gradually phase out the old system.

## Execution note

This stub details the objectives of the **authentication and authorization hardening** sprint.  Prior to implementation, confirm the token format and revocation strategy with the security team.  After the sprint completes, document the final design, summarise any deviations from this plan and mark the sprint as **closed** in this document and `ROOM_EPIC_SPRINT_SEQUENCE.md`.