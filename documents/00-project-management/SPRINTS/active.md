# Active Sprint

Sprint 010 / R04 — Room Domain, Invite, Membership, and Lifecycle is the currently authorized sprint (in progress — pending Product Owner acceptance of the R04 closure pass). See [010-room-domain-invite-membership-lifecycle.md](./010-room-domain-invite-membership-lifecycle.md) for the sprint record.

Sprint 020 / A01 — Allowed Origins and WebSocket Origin Policy is closed (2026-06-26, pending Product Owner acceptance). A01 introduced a shared `ALLOWED_ORIGINS` policy shared by HTTP CORS and WebSocket upgrades, with fail-fast validation outside `APP_ENV=local`, and deprecated the legacy `?user_id=...` WebSocket query parameter as an identity or daily-priority attribution signal. See [020-allowed-origins-and-websocket-origin-policy.md](./020-allowed-origins-and-websocket-origin-policy.md).

Sprint R05 — Session Token Authentication & Authorization is closed. R05 shipped server-issued in-memory session tokens, `Authorization: Bearer <token>` enforcement on privileged REST endpoints, role-gated authorization, `?session_token=<opaque>` WebSocket authentication, an additive `error` event for rejected client-originated requests, and browser-extension session-token handling. See `../PROJECT_STATE.md` for the authoritative R05 state description.

Sprint 009 / R03 — SQLite-to-PostgreSQL Data Migration is closed. See [009-sqlite-to-postgresql-data-migration.md](./009-sqlite-to-postgresql-data-migration.md).

Sprint 008 / R02 — PostgreSQL Foundation with Existing Behavior Preserved is closed. See [008-postgresql-foundation-with-existing-behavior-preserved.md](./008-postgresql-foundation-with-existing-behavior-preserved.md).

Sprint 007 / R01 — PostgreSQL Migration Design is closed. See [007-postgresql-migration-design.md](./007-postgresql-migration-design.md).

Sprint 006 / R00 — Room Architecture ADR / R00 is closed. See [006-room-architecture-adr-contract-plan.md](./006-room-architecture-adr-contract-plan.md).

The next sprint to shape after R04 closes is **Sprint R06 — Player Lease and Host Departure Semantics** per [`../ROOM_EPIC_SPRINT_SEQUENCE.md`](../ROOM_EPIC_SPRINT_SEQUENCE.md). R06 implementation is in progress on `dev`; R05 (session token authentication & authorization) and A01 (allowed origins & WebSocket origin policy) were completed out-of-band of the R04 sequence.

Sprint 012 / R07a — Room-scoped playback queue (narrow first slice) is closed (2026-06-29, accepted by the Product Owner). See [012-room-scoped-playback-queue.md](./012-room-scoped-playback-queue.md).

Sprint 012 / R07b — Room-scoped playback queue (per-room WebSocket deltas at `GET /ws/rooms/{slug}`) is closed (2026-06-29, accepted by the Product Owner). See [012-room-scoped-playback-queue.md](./012-room-scoped-playback-queue.md). Full R07 remains split; the remaining slices (relational queue rows, room-scoped reorder/vote endpoints, cross-process safety, and migration of the legacy global `queue_state` into a room) are deferred per the 012 document. The original "R08 placeholder" (public read-only API and CORS) was renumbered away from R07b and remains a future sprint per the sequence doc.

Sprint 012 / R07c — Room-scoped playback queue (frontend narrow wiring for R07a/R07b) is closed (2026-06-29, accepted by the Product Owner). See [012-room-scoped-playback-queue.md](./012-room-scoped-playback-queue.md).

Sprint 012 / R07d — Room-scoped playback queue (one room-scoped mutation: POST /api/rooms/{slug}/queue/prioritize, plus the matching per-room WebSocket delta and a minimal RoomView Prioritize control) is closed (2026-06-30, accepted by the Product Owner). The closure pass tightened three narrow invariants from the original R07d landing: the handler request shape uses `*int` so a missing `song_index` is rejected with `400`; the interactor returns the post-mutation song so the broadcast payload carries `IsPrioritized=true`; and `PROJECT_STATE.md` plus the frontend store fallback reflect the snake-case `is_prioritized` field. R07d does not advance any out-of-scope work (no global route, no priority balances, no relational rows). See the *Implementation summary (R07d)* and *Closure* sections in [012-room-scoped-playback-queue.md](./012-room-scoped-playback-queue.md).
