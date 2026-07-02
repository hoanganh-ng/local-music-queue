# Room Epic Roadmap

**Last synced:** 2026-07-02  
**Branch:** `dev`  
**Source priority:** current Product Owner decisions, accepted sprint records, implementation/tests, then older roadmap docs.

This document tracks the remaining work for the room feature/epic after **R09g — Room auto-queue frontend integration** was accepted. It is intentionally narrower and more current than the legacy generic roadmap files in this folder.

## Current completion estimate

The room feature is estimated at:

- **~65% complete for the full documented room epic.**
- **~75% complete for the core user-facing room experience**: join a room, use a room-scoped queue, control playback through a lease holder, vote to skip, and use room auto-queue/radio mode.

The estimate is intentionally conservative because several planned or deferred areas still affect lifecycle completeness, public access, discovery, hardening, and final contract cleanup.

## Completed / accepted room slices

- **R00–R05:** room architecture planning, PostgreSQL migration design/runtime, core room domain, invite/membership lifecycle, session-token authentication.
- **R06:** player lease and host-departure semantics are implemented on `dev`; Product Owner acceptance remains recorded as pending in the room epic sequence.
- **R07a–R07d:** room-scoped queue persistence, REST queue operations, per-room WebSocket sync/deltas, frontend room route/wiring, and room prioritize.
- **R09a:** lease-aware room playback controls: status, sync, skip, ended.
- **R09b:** room vote-to-skip backend contract/runtime.
- **R09c:** room-scoped volume command.
- **R09d:** room-scoped previous playback command.
- **R09e:** room auto-queue contract design.
- **R09f:** room auto-queue backend runtime.
- **R09g:** room auto-queue frontend integration.

## Remaining sprint estimate

Plan around **10 more sprints** from the current point. The likely range is:

- **Best-case:** 8 sprints, if deferred architecture work is explicitly excluded from “room feature complete.”
- **Realistic planning number:** 10 sprints.
- **Architecture-complete / hardening-complete:** 12+ sprints, especially if authentication hardening, discovery, or deferred queue architecture are split into contract + runtime + frontend slices.

## Recommended remaining sequence

| Order | Sprint | Purpose | Notes |
|---|---|---|---|
| 1 | **R10a — Room lifecycle removal contract design** | Documentation-only contract for room soft-archive and member removal. | Already handed to Builder. No runtime code, migrations, routes, WebSocket constants, or Vue code. |
| 2 | **R10b — Room lifecycle removal backend runtime** | Implement host-only room soft-archive and member removal. | Should settle lease cleanup, membership checks, vote-session effects, WebSocket notification/close behavior, and audit persistence per R10a. |
| 3 | **R10c — Room lifecycle removal frontend** | Add UI for room archive/delete and host member removal. | Backend remains authoritative; frontend gates are convenience only. |
| 4 | **R08 — Public read-only room API and CORS** | Expose safe public read-only room state, if still desired. | Must preserve `ALLOWED_ORIGINS` / WebSocket origin policy and avoid weakening auth posture. |
| 5 | **R11a — Room chat backend / contract** | Persist room chat messages and broadcast them on the per-room WebSocket. | Should be split from frontend to keep persistence + real-time review narrow. |
| 6 | **R11b — Room chat frontend** | Add chat UI and per-room chat store/listeners. | Keep moderation/retention behavior aligned with R11a. |
| 7 | **R12a — Search and room discovery backend** | Add discovery/search APIs. | Scope must distinguish track search, room discovery, public/private visibility, filtering, and caching. |
| 8 | **R12b — Search and room discovery frontend** | Add room discovery/search UI and any track-search UX integration. | Must not trust client-side visibility/permission checks. |
| 9 | **R13 — Authentication and authorization hardening** | Tighten session/auth behavior and route authorization. | May split into R13a contract + R13b runtime if scope expands. |
| 10 | **R14 — Final contract cleanup and documentation** | Reconcile docs, API/WebSocket inventories, tests, and remaining compatibility notes. | Should close the room epic only after implementation/tests/docs agree. |

## Known deferred / decision-dependent work

These items should be explicitly included, deferred, or dropped before declaring the room feature complete:

- Remaining **R07** deferred scope: relational room queue rows, arbitrary drag-and-drop reorder, cross-process safety, and migration of the legacy global `queue_state` into a room.
- Full media-player device integration from the remaining R09 bucket.
- Whether R08 public read-only access is still required after the current session/origin posture.
- Whether R13 auth hardening is required for room feature completion or can become a separate security epic.
- Whether “complete” means **user-facing complete** or **architecture/security complete**.

## Planning rule

Use **10 remaining sprints** for sprint-capacity planning until R10a is reviewed. After R10a acceptance, re-estimate based on the final deletion/member-removal contract and whether R08/R13 remain in the room epic closure criteria.
