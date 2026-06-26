# Active Sprint

Sprint 010 / R04 — Room Domain, Invite, Membership, and Lifecycle is the currently authorized sprint (in progress — pending Product Owner acceptance of the R04 closure pass). See [010-room-domain-invite-membership-lifecycle.md](./010-room-domain-invite-membership-lifecycle.md) for the sprint record.

Sprint R05 — Session Token Authentication & Authorization is closed. R05 shipped server-issued in-memory session tokens, `Authorization: Bearer <token>` enforcement on privileged REST endpoints, role-gated authorization, `?session_token=<opaque>` WebSocket authentication, an additive `error` event for rejected client-originated requests, and browser-extension session-token handling. See `../PROJECT_STATE.md` for the authoritative R05 state description.

Sprint 009 / R03 — SQLite-to-PostgreSQL Data Migration is closed. See [009-sqlite-to-postgresql-data-migration.md](./009-sqlite-to-postgresql-data-migration.md).

Sprint 008 / R02 — PostgreSQL Foundation with Existing Behavior Preserved is closed. See [008-postgresql-foundation-with-existing-behavior-preserved.md](./008-postgresql-foundation-with-existing-behavior-preserved.md).

Sprint 007 / R01 — PostgreSQL Migration Design is closed. See [007-postgresql-migration-design.md](./007-postgresql-migration-design.md).

Sprint 006 / R00 — Room Architecture ADR / R00 is closed. See [006-room-architecture-adr-contract-plan.md](./006-room-architecture-adr-contract-plan.md).

The next sprint to shape after R04 closes is **Sprint R06 — Player Lease and Host Departure Semantics** per [`../ROOM_EPIC_SPRINT_SEQUENCE.md`](../ROOM_EPIC_SPRINT_SEQUENCE.md). R05 (session token authentication & authorization) was completed out-of-band of the R04 sequence.
