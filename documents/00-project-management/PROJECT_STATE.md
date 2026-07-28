# Project State Baseline

**Last refreshed:** 2026-07-28  
**Branch:** `dev`  
**Current active sprint:** None

## Current state

R14d — Frontend global-path retirement behind the cutover build gate — is closed and accepted by the Product Owner. PR #25 was merged into `dev` at merge commit `67bd57a8abef92d42cdfefb90069563340147d10`; the final reviewed feature head was `491770ed90e3940307df3933d4236508fac838f3`.

R14d delivered one frontend source that produces two compatible artifacts through the single Vite build-time setting `VITE_ROOM_CUTOVER_AUTHORITATIVE`. The `false` artifact preserves the existing Dashboard, global runtime state, global REST paths, and global `/ws` behavior for the pre-cutover and rollback window. The `true` artifact makes RoomEntry the authenticated landing surface, removes Dashboard from the route graph, retires only the legacy global client slices while preserving `currentUser` and room-local state, and prevents the global WebSocket from connecting or reconnecting.

The true artifact has not been deployed. Production cutover has not been executed. R14c and R14e remain inactive. Room epic Issue #17 remains open.

The accepted R14d verification record reports 443/443 frontend tests and a successful build in each baked mode. Focused local compatibility checks confirmed the false Dashboard/global-WebSocket path and the true RoomEntry/no-global-WebSocket path. Real Google-login and server-authenticated room list/create/open/invite checks remained blocked in the isolated environment and are retained as mandatory pre-R14c or R14c paired-smoke checks.

R09i — Room activity runtime parity — remains closed and accepted. PR #24 was merged into `dev` at merge commit `c112000d2a70d105419a57f4059bafd4051e423d`; normal pre-R14c server composition still injects the explicit no-op room-activity repository, so runtime `room_activities` persistence remains disabled until the separately approved R14c cutover.

## Accepted cutover sequence

The approved order remains:

```text
R05b → R09h → R14b → R09i → R14d → R14c → R14e
```

Completed and accepted prerequisites:

- R05b — room entry and player-lease UI slices
- R09h — room vote-to-prioritize parity
- R14b — schema and offline room-cutover mechanism
- R09i — room activity runtime parity
- R14d — frontend global-path retirement behind the cutover build gate

R14c is the next planned sprint but is not active. R14c retains ownership of the coordinated production cutover, the runtime `--room-cutover-authoritative` guard, validation of schema version 9 plus the durable `room_cutover_marker`, execution and verification of the offline migration, global REST/WebSocket tombstones, activity-copy and sequence resynchronization, real PostgreSQL room-activity-writer selection, and the paired true-server/true-SPA smoke test. The Go backend must not consume the SPA gate.

R14e remains inactive and retains ownership of schema version 10 and removal of the legacy global tables only after the approved rollback window.

## Accepted room capability baseline

Accepted room behavior on `dev` includes room lifecycle, invites, membership and role enforcement, player-lease flows, room-scoped queue and playback controls, room-scoped voting, room-scoped auto-queue, activity-production parity, per-room WebSocket synchronization and deltas, room archival/member removal, existing frontend room flows, and the R14d dual-artifact frontend cutover preparation.

Deferred scope remains deferred: R10f+ lifecycle hardening, R11b+ chat expansion, R12 discovery/search work, R13 broad session/authentication hardening, and any later room slices not explicitly activated.

## Completion estimate

The Room epic is approximately **85% accepted**. This is an implementation-completion estimate, not production-cutover readiness. R14c carries the remaining high-risk migration, runtime-switch, contract-retirement, activity-continuity, deployment, and live-auth verification work; R14e carries irreversible legacy-table cleanup after the rollback window.

## Authoritative references

- [`SPRINTS/028-frontend-global-path-retirement.md`](./SPRINTS/028-frontend-global-path-retirement.md)
- [`SPRINTS/active.md`](./SPRINTS/active.md)
- [PR #25](https://github.com/hoanganh-ng/local-music-queue/pull/25)
- [`SPRINTS/027-room-activity-runtime-parity.md`](./SPRINTS/027-room-activity-runtime-parity.md)
- [PR #24](https://github.com/hoanganh-ng/local-music-queue/pull/24)
- Room epic Issue #17
- [`ROOM_EPIC_SPRINT_SEQUENCE.md`](./ROOM_EPIC_SPRINT_SEQUENCE.md)
- [`ADRS/003-legacy-global-state-migration-and-contract-retirement.md`](./ADRS/003-legacy-global-state-migration-and-contract-retirement.md)
