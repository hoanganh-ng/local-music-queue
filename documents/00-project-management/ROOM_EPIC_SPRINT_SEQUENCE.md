# Room Epic – Sprint Sequence

This document lays out the high‑level sequence of sprints for the **Local Music Queue** room epic.  Each sprint is scoped narrowly and builds upon the preceding work.  As sprints close, their status will be updated in this document along with a short description of the outcome.  If a sprint has not yet been shaped, its goals and requirements are captured in a corresponding stub in the `SPRINTS` folder.

| Sprint | Name | Status | Description |
|---|---|---|---|
| **R00** | Room architecture contract plan | **Closed** | Established the baseline architecture contract for rooms. Defined key domain concepts, roles (host, member, guest), and the high‑level API surface. Captured as ADR 001 and sprint `006-room-architecture-adr-contract-plan`. |
| **R01** | PostgreSQL migration design | **Closed** | Designed the move from an embedded SQLite database to PostgreSQL. Documented migration strategy, updated schemas and connection configuration. Captured as ADR 002 and sprint `007-postgresql-migration-design`. |
| **R02** | Core domain implementation | **Closed** | Implemented initial room domain entities (room, user, membership) and basic REST endpoints. Laid groundwork for future sprints by scaffolding repository interfaces and service layers. |
| **R03** | SQLite → PostgreSQL data migration | **Closed** | Performed a one‑time migration of all persistent data from SQLite to PostgreSQL. Added migration scripts, validated data integrity and adjusted CI pipelines accordingly. |
| **R04** | Room invites and membership lifecycle | **Closed** | Added support for inviting users to a room, accepting/declining invites, leaving a room and transferring ownership. Introduced roles (owner vs. member) and access control for invite and join operations. |
| **R05** | Session token authentication | **Closed** | Introduced per‑user session tokens for authenticating both REST and WebSocket calls. Added middleware to resolve a user from a session token, updated the invite/join flows to issue tokens, and tightened CORS. |
| **R06** | Player lease and host departure semantics | **Planned** | Introduce a `PlayerLease` concept to formalize who controls playback. Add claim, heartbeat and release endpoints, enforce a single active lease per room and implement graceful host departure handling. Documented in `011-player-lease-and-host-departure-semantics.md`. |
| **R07** | Room‑scoped playback queue | **Planned** | Persist the playback queue per room, including track order, votes and ownership. Provide endpoints to add, remove and reorder tracks, emit events on changes and handle concurrent modifications. See sprint `012-room-scoped-playback-queue.md`. |
| **R08** | Public read‑only API and CORS | **Planned** | Expose read‑only endpoints for external clients to fetch room state (queue, player status) without requiring full authentication. Implement strict CORS policies and caching headers. See sprint `013-public-read-only-api-and-cors.md`. |
| **R09** | Player control semantics | **Planned** | Define and implement server‑side semantics for play/pause, skip and volume control. Introduce vote‑to‑skip rules and ensure only the active lease holder (or majority of members) can control playback. See sprint `014-player-control-semantics.md`. |
| **R10** | Room deletion and membership removal | **Planned** | Add the ability for owners to delete rooms and for hosts to remove members. Ensure cascading deletion of queues, leases and invites. Provide confirmation flows and audit logging. See sprint `015-room-deletion-and-membership-removal.md`. |
| **R11** | Room chat feature | **Planned** | Introduce a lightweight chat system within each room. Persist chat messages, broadcast them via WebSocket and enforce simple moderation rules. See sprint `016-room-chat-feature.md`. |
| **R12** | Search and room discovery | **Planned** | Add search endpoints to find available tracks (via external providers) and discover public rooms. Implement pagination, filtering and result caching. See sprint `017-search-and-room-discovery.md`. |
| **R13** | Authentication and authorization hardening | **Planned** | Strengthen the authentication subsystem with refresh tokens or JWT, refine role‑based authorization and lock down all endpoints. See sprint `018-authentication-and-authorization-hardening.md`. |
| **R14** | Global contract cleanup and documentation | **Planned** | Perform a final pass over all contracts, code and documentation. Remove deprecated behaviours, update ADRs, publish OpenAPI definitions and ensure test coverage. See sprint `019-global-contract-cleanup-and-documentation.md`. |

## How to use this document

* When shaping a new sprint, refer to the stub file listed in the **Description** column.  Each stub provides a goal, describes the baseline behaviour, outlines desired changes, lists the affected context and gives implementation guidance.
* Once a sprint completes, update the **Status** in the table to **Closed** and summarise the outcome.  If any decisions change the scope of later sprints, reflect that here.
* If you add a new sprint, append it to the bottom of the table with a short description and create a matching stub file under `documents/00-project-management/SPRINTS`.
