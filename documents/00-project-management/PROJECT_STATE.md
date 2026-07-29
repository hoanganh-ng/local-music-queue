# Project State Baseline

**Last refreshed:** 2026-07-28  
**Branch:** `sprint/r14c-coordinated-production-cutover`  
**Current active sprint:** R14c — Coordinated authoritative room cutover ([`SPRINTS/029-coordinated-authoritative-room-cutover.md`](./SPRINTS/029-coordinated-authoritative-room-cutover.md))

## Current state

R14c was approved by the Product Owner and activated on 2026-07-28 from `dev` commit `13c09549fc89febac2c79085afcf7249a43f62a4`.

R14c activation authorizes **Gate 1 only**: implementation, automated verification, packaging/deployment pairing, an isolated production-like rehearsal, and completion of the operational cutover/rollback runbook. Production execution is a separate Gate 2 and requires another Product Owner go/no-go after Architect review and Gate 1 acceptance.

Gate 1 implementation is open as **PR #26** (base `dev` `13c09549fc89febac2c79085afcf7249a43f62a4`; changed-file count per GitHub PR #26). Its review-head sequence is `1502c3a243b3350b6a18ff2454118981f4032357` (initial Gate 1) → `0daa8fbe22b896c6def5f2e75af129792ee41229` (first corrective pass, findings F1–F6) → the new corrective head from this pass (exact SHA posted to PR #26 at push; a commit cannot embed its own hash). The Architect returned focused findings and Gate 1 is **pending re-review — not accepted**. The latest corrective changes (packaged-image pre-window plan/`up --dry-run` with bind-mounted evidence, parameterized identity SQL, immutable rollback-image capture from the running containers, Docker-gated Compose pairing test that fails on render errors) preserve the accepted runtime flag, startup guard, activity-writer selection, and tombstone handler. The Builder does not commit, push, merge, or advance the sprint.

Production cutover has not been executed. The true server mode and R14d true frontend bundle have not been deployed. The existing false frontend artifact and current legacy-global server behavior remain the authoritative pre-cutover and rollback-compatible state.

R14c owns:

- strict runtime parsing for `--room-cutover-authoritative=true|false`;
- a fail-closed true-mode guard requiring a clean schema version at least 9 and `room_cutover_marker.id = 1` before listeners open;
- one no-op room-activity writer in false mode and one real PostgreSQL room-activity writer in true mode;
- atomic repository-free `410 Gone` tombstones for the approved legacy global queue, vote, auto-queue, and `/ws` entry points;
- preservation of auth, account utility, room REST, and room WebSocket contracts;
- one Compose-level deployment input that produces matching false/false or true/true server/SPA artifacts;
- packaging of the accepted `cmd/room-cutover` CLI in the backend image;
- isolated rehearsal evidence and the redacted production runbook.

The Builder is not authorized to run production migration commands, deploy the true pair, modify production data/secrets, close or reopen traffic, close R14c, activate R14e, or advance the epic.

R14d — Frontend global-path retirement behind the cutover build gate — remains closed and accepted. PR #25 was merged into `dev` at merge commit `67bd57a8abef92d42cdfefb90069563340147d10`; the final reviewed feature head was `491770ed90e3940307df3933d4236508fac838f3`.

R09i — Room activity runtime parity — remains closed and accepted. PR #24 was merged into `dev` at merge commit `c112000d2a70d105419a57f4059bafd4051e423d`; normal pre-cutover composition still injects the explicit no-op room-activity writer.

R14e remains inactive. Migration 0010, schema version 10, and removal of legacy global tables remain out of scope until after the separately approved production cutover and verified rollback window.

Room epic Issue #17 remains open.

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

R14c is active at the implementation/rehearsal gate. Its production execution gate is not approved. R14e remains inactive.

## Accepted room capability baseline

Accepted room behavior includes room lifecycle, invites, membership and role enforcement, player-lease flows, room-scoped queue and playback controls, room-scoped voting, room-scoped auto-queue, activity-production parity, per-room WebSocket synchronization and deltas, room archival/member removal, existing frontend room flows, and R14d's dual-artifact frontend cutover preparation.

Deferred scope remains deferred: R10f+ lifecycle hardening, R11b+ chat expansion, R12 discovery/search work, R13 broad session/authentication hardening, R14e destructive cleanup, and any later room slices not explicitly activated.

## Completion estimate

The Room epic remains approximately **85% accepted**. This is an implementation-completion estimate, not production-cutover readiness. R14c carries the remaining extra-high-risk runtime switch, contract retirement, deployment pairing, data-cutover execution, activity continuity, rollback, and live-auth verification work. R14e remains a separate irreversible cleanup sprint.

## Authoritative references

- [`SPRINTS/029-coordinated-authoritative-room-cutover.md`](./SPRINTS/029-coordinated-authoritative-room-cutover.md)
- [`SPRINTS/active.md`](./SPRINTS/active.md)
- [`SPRINTS/028-frontend-global-path-retirement.md`](./SPRINTS/028-frontend-global-path-retirement.md)
- [PR #25](https://github.com/hoanganh-ng/local-music-queue/pull/25)
- [`SPRINTS/027-room-activity-runtime-parity.md`](./SPRINTS/027-room-activity-runtime-parity.md)
- [PR #24](https://github.com/hoanganh-ng/local-music-queue/pull/24)
- Room epic Issue #17
- [`ROOM_EPIC_SPRINT_SEQUENCE.md`](./ROOM_EPIC_SPRINT_SEQUENCE.md)
- [`ADRS/003-legacy-global-state-migration-and-contract-retirement.md`](./ADRS/003-legacy-global-state-migration-and-contract-retirement.md)
