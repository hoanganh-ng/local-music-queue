# Project State Baseline

**Last refreshed:** 2026-07-29  
**Branch:** `dev`  
**Current active sprint:** Sprint 031 — R14c Gate 2 Preflight and Go/No-Go Preparation ([`SPRINTS/031-r14c-gate2-preflight.md`](./SPRINTS/031-r14c-gate2-preflight.md)) — documentation-only; unmerged PR from `dev` at `8ae823e8dc7b337a70bd6d420745595463680666` on branch `sprint/r14c-gate2-preflight`. Sprint 030 ([`SPRINTS/030-r14c-gate2-readiness-package.md`](./SPRINTS/030-r14c-gate2-readiness-package.md)) is closed and accepted (PR #27 merged into `dev` as `d27c56ff` on 2026-07-29). R14c ([`SPRINTS/029-coordinated-authoritative-room-cutover.md`](./SPRINTS/029-coordinated-authoritative-room-cutover.md)) remains Gate 1 integrated; Gate 2 pending and unauthorized

## Current state

R14c was approved by the Product Owner and activated on 2026-07-28 from `dev` commit `13c09549fc89febac2c79085afcf7249a43f62a4`.

R14c activation authorizes **Gate 1 only**: implementation, automated verification, packaging/deployment pairing, an isolated production-like rehearsal, and completion of the operational cutover/rollback runbook. Production execution is a separate Gate 2 and requires another Product Owner go/no-go after Architect review and Gate 1 acceptance.

R14c **Gate 1 is integrated**. The Product Owner accepted the re-review and squash-merged **PR #26** (base `dev` `13c09549fc89febac2c79085afcf7249a43f62a4`) into `dev` as commit `c8ab4af029d10dda889d1165464e16068a5be573` on 2026-07-29. The review-head sequence was `1502c3a243b3350b6a18ff2454118981f4032357` (initial Gate 1) → `0daa8fbe22b896c6def5f2e75af129792ee41229` (first corrective pass, findings F1–F6) → `328aba41db44ca1fc41e02ee052d2d6c2c63ca36` (second corrective pass, the reviewed head) → the final documentation-only corrective head, whose exact SHA remains recorded only in the PR #26 lifecycle ledger. R14c's **implementation lifecycle is closed** — no further Gate 1 work is authorized. **Gate 2 — production execution — remains explicitly pending**: the merge does not authorize it; it requires a separate Product Owner go/no-go against the merged runbook.

Production cutover has not been executed. The true server mode and R14d true frontend bundle have not been deployed. The existing false frontend artifact and current legacy-global server behavior remain the authoritative pre-cutover and rollback-compatible state.

Sprint 030 — **R14c Gate 2 Readiness Package** — is **closed and accepted**: PR #27 was merged into `dev` as `d27c56ff85a1c655e9c9a1bc38354f13fcfd7ccd` on 2026-07-29, making its deliverables authoritative on `dev`. It was cut from `dev` at `6d550ee0db3b64678147d69c9d04333587f20eb3` on branch `sprint/r14c-gate2-readiness` and adds the authoritative readiness overlay [`documents/07-deployment/room-cutover-gate2-readiness.md`](../07-deployment/room-cutover-gate2-readiness.md) (placeholder/input inventory with owners and secure supply methods, the approved non-argv connection-supply model, maintenance roles, artifact custody, blockers B1–B7, preflight checks, abort rules, entry criteria E1–E10, and the Product Owner GO / NO-GO / DEFER checklist) plus the placeholder-only operator-local input template [`documents/07-deployment/room-cutover-gate2-inputs.template.md`](../07-deployment/room-cutover-gate2-inputs.template.md). Following the Architect reviews of the sprint's PR (#27), the corrective passes also applied focused documentation-only security amendments to the accepted runbook: all `psql`, `pg_dump`, and `room-cutover` connection data is supplied through protected non-argv channels (libpq service/passfile bundle and name-only environment passthrough; `--postgres` is never used, and the snapshot DSN variables stay un-exported private shell variables), so no DSN or credential ever appears in a command line; and retained Compose evidence is limited to allowlisted pairing lines (`pairing-false.txt` / `pairing-true.txt`) because the full `docker compose config` render interpolates deployment secrets and is never written to disk. Runbook command order and behavior are unchanged. Sprint 030 executed no production action and its closure does **not** authorize the production cutover: blockers B1–B7 remain unresolved, Gate 2 remains pending and unauthorized, production remains untouched, and R14e remains inactive.

Sprint 031 — **R14c Gate 2 Preflight and Go/No-Go Preparation** — is **active and documentation-only**. It was cut from `dev` at `8ae823e8dc7b337a70bd6d420745595463680666` on branch `sprint/r14c-gate2-preflight` and delivered as an **unmerged** PR for Architect review and Product Owner decision. It adds the preflight-tracking and go/no-go preparation layer over the accepted Sprint 030 readiness package: a redacted B1–B6 / preflight 1–15 / E1–E10 status ledger [`documents/07-deployment/room-cutover-gate2-preflight-ledger.md`](../07-deployment/room-cutover-gate2-preflight-ledger.md), an operator evidence-return template [`documents/07-deployment/room-cutover-gate2-evidence-return.template.md`](../07-deployment/room-cutover-gate2-evidence-return.template.md), an operational discrepancy register [`documents/07-deployment/room-cutover-gate2-discrepancy-register.md`](../07-deployment/room-cutover-gate2-discrepancy-register.md), and a Product Owner go/no-go review packet [`documents/07-deployment/room-cutover-gate2-go-no-go-packet.md`](../07-deployment/room-cutover-gate2-go-no-go-packet.md). Every ledger row is initialized OPEN / NOT MET, the discrepancy register is empty, and no GO is declared. These documents only *track* readiness and never grant it; the runbook still prevails on what/how to execute and the readiness package plus the Product Owner decision still prevail on whether execution is authorized. Sprint 031 accessed no production, snapshot, deployment host, container, connection bundle, evidence directory, credential, or identity, executed no runbook step, marked no operational item resolved, and declares no GO. Its existence does **not** authorize the production cutover: blockers B1–B7 remain unresolved, Gate 2 remains pending and unauthorized, production remains untouched, and R14e remains inactive.

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

- [`SPRINTS/031-r14c-gate2-preflight.md`](./SPRINTS/031-r14c-gate2-preflight.md)
- [`../07-deployment/room-cutover-gate2-preflight-ledger.md`](../07-deployment/room-cutover-gate2-preflight-ledger.md)
- [`../07-deployment/room-cutover-gate2-evidence-return.template.md`](../07-deployment/room-cutover-gate2-evidence-return.template.md)
- [`../07-deployment/room-cutover-gate2-discrepancy-register.md`](../07-deployment/room-cutover-gate2-discrepancy-register.md)
- [`../07-deployment/room-cutover-gate2-go-no-go-packet.md`](../07-deployment/room-cutover-gate2-go-no-go-packet.md)
- [`SPRINTS/030-r14c-gate2-readiness-package.md`](./SPRINTS/030-r14c-gate2-readiness-package.md)
- [`../07-deployment/room-cutover-gate2-readiness.md`](../07-deployment/room-cutover-gate2-readiness.md)
- [`../07-deployment/room-cutover-runbook.md`](../07-deployment/room-cutover-runbook.md)
- [`SPRINTS/029-coordinated-authoritative-room-cutover.md`](./SPRINTS/029-coordinated-authoritative-room-cutover.md)
- [`SPRINTS/active.md`](./SPRINTS/active.md)
- [`SPRINTS/028-frontend-global-path-retirement.md`](./SPRINTS/028-frontend-global-path-retirement.md)
- [PR #25](https://github.com/hoanganh-ng/local-music-queue/pull/25)
- [`SPRINTS/027-room-activity-runtime-parity.md`](./SPRINTS/027-room-activity-runtime-parity.md)
- [PR #24](https://github.com/hoanganh-ng/local-music-queue/pull/24)
- Room epic Issue #17
- [`ROOM_EPIC_SPRINT_SEQUENCE.md`](./ROOM_EPIC_SPRINT_SEQUENCE.md)
- [`ADRS/003-legacy-global-state-migration-and-contract-retirement.md`](./ADRS/003-legacy-global-state-migration-and-contract-retirement.md)
