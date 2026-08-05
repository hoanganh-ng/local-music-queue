# Project State Baseline

**Last refreshed:** 2026-08-05\
**Branch:** `dev`\
**Current active sprint:** **Sprint 038 — R14 Discard-and-Retire Implementation Correction** ([`SPRINTS/038-r14-discard-retire-implementation-correction.md`](./SPRINTS/038-r14-discard-retire-implementation-correction.md)) — the sole active sprint from `dev` at `e4b94c0aef74ef90a104a9437caeae99e05c1da3`. Sprint 038 is authorized by ADR 004, the approved success design, and Sprint 037 Builder authorization (Issue #17 comments #5189935491, #5190095970, #5190130138). It authorizes implementation and revised operational documents only: migration `0010` for dedicated `room_authoritative_activation`, shared activation prepare/verify logic, true-mode startup guard, `room-activation` operator CLI, backend-image/operator-image separation, and seven new `room-discard-cutover-*` operational documents. It grants no preflight, production, credential, deployment, traffic, cutover, rollback, cleanup, readiness, GO, R14e, or Issue #17 closure authority. Sprint 036 is closed and accepted; Sprint 037 is completed as the documentation-only lifecycle transition. Sprint 031 remains paused (not closed, not superseded, not resumed) until correction integration is accepted and a later transition closes it as superseded. Nothing operational is resolved: all readiness rows remain `Not started`, the unsigned recommendation remains `DEFER / NOT READY`, Gate 2 remains pending and unauthorized, production remains untouched, and R14e remains inactive.

## Product direction — discard-and-retire (ADR 004, 2026-07-30)

The Product Owner decided on 2026-07-30 (Issue #17 comment #5128855220) that the R14 production cutover follows **discard-and-retire** instead of migrate-and-retire. ADR 004 is **accepted architecture authority on `dev`** as of 2026-07-31 (Sprint 034 closure via PR #32; recorded by Sprint 035):

- no legacy global queue state, activities, auto-queue configuration, or play history is migrated into a room;
- no migrated, default, or hidden room is created for legacy state;
- room-authoritative operation activates with no inherited playback state — "no inherited playback state" means no state copied from the legacy global tables, and ADR 004 does not authorize deleting or modifying any pre-existing room-scoped room, membership, queue, activity, auto-queue, play-history, lease, invite, or chat data; users create or join ordinary rooms through the accepted room flows;
- the pre-cutover database backup and the false/false rollback pair remain mandatory for rollback and audit during the rollback window;
- the legacy global tables remain untouched during the rollback window; destructive cleanup (R14e) remains a later, separately approved action.

Consequences: the accepted `cmd/room-cutover` copy mechanism (R14b) is declared unsuitable for the revised production cutover and remains an accepted historical artifact, not executable current direction. Sprint 038 now owns the correction: migration `0010` creates dedicated `room_authoritative_activation`, migration `0011` is reserved for later R14e cleanup, backend and operator images separate runtime server from operator commands, and the new `room-discard-cutover-*` operational documents remain non-executable until Sprint 038 is accepted and integrated and a successor preflight is separately authorized.

## Current state

R14c was approved by the Product Owner and activated on 2026-07-28 from `dev` commit `13c09549fc89febac2c79085afcf7249a43f62a4`.

R14c activation authorizes **Gate 1 only**: implementation, automated verification, packaging/deployment pairing, an isolated production-like rehearsal, and completion of the operational cutover/rollback runbook. Production execution is a separate Gate 2 and requires another Product Owner go/no-go after Architect review and Gate 1 acceptance.

R14c **Gate 1 is integrated**. The Product Owner accepted the re-review and squash-merged **PR #26** (base `dev` `13c09549fc89febac2c79085afcf7249a43f62a4`) into `dev` as commit `c8ab4af029d10dda889d1165464e16068a5be573` on 2026-07-29. The review-head sequence was `1502c3a243b3350b6a18ff2454118981f4032357` (initial Gate 1) → `0daa8fbe22b896c6def5f2e75af129792ee41229` (first corrective pass, findings F1–F6) → `328aba41db44ca1fc41e02ee052d2d6c2c63ca36` (second corrective pass, the reviewed head) → the final documentation-only corrective head, whose exact SHA remains recorded only in the PR #26 lifecycle ledger. R14c's **implementation lifecycle is closed** — no further Gate 1 work is authorized. **Gate 2 — production execution — remains explicitly pending**: the merge does not authorize it; it requires a separate Product Owner go/no-go against the merged runbook.

Production cutover has not been executed. The true server mode and R14d true frontend bundle have not been deployed. The existing false frontend artifact and current legacy-global server behavior remain the authoritative pre-cutover and rollback-compatible state.

Sprint 030 — **R14c Gate 2 Readiness Package** — is **closed and accepted**: PR #27 was merged into `dev` as `d27c56ff85a1c655e9c9a1bc38354f13fcfd7ccd` on 2026-07-29, making its deliverables authoritative on `dev`. It was cut from `dev` at `6d550ee0db3b64678147d69c9d04333587f20eb3` on branch `sprint/r14c-gate2-readiness` and adds the authoritative readiness overlay [`documents/07-deployment/room-cutover-gate2-readiness.md`](../07-deployment/room-cutover-gate2-readiness.md) (placeholder/input inventory with owners and secure supply methods, the approved non-argv connection-supply model, maintenance roles, artifact custody, blockers B1–B7, preflight checks, abort rules, entry criteria E1–E10, and the Product Owner GO / NO-GO / DEFER checklist) plus the placeholder-only operator-local input template [`documents/07-deployment/room-cutover-gate2-inputs.template.md`](../07-deployment/room-cutover-gate2-inputs.template.md). Following the Architect reviews of the sprint's PR (#27), the corrective passes also applied focused documentation-only security amendments to the accepted runbook: all `psql`, `pg_dump`, and `room-cutover` connection data is supplied through protected non-argv channels (libpq service/passfile bundle and name-only environment passthrough; `--postgres` is never used, and the snapshot DSN variables stay un-exported private shell variables), so no DSN or credential ever appears in a command line; and retained Compose evidence is limited to allowlisted pairing lines (`pairing-false.txt` / `pairing-true.txt`) because the full `docker compose config` render interpolates deployment secrets and is never written to disk. Runbook command order and behavior are unchanged. Sprint 030 executed no production action and its closure does **not** authorize the production cutover: blockers B1–B7 remain unresolved, Gate 2 remains pending and unauthorized, production remains untouched, and R14e remains inactive.

Sprint 031 — **R14c Gate 2 Preflight and Go/No-Go Preparation** — is **paused as of 2026-07-30 — not closed, not completed, not superseded**; it remains paused until accepted correction integration, after which a later transition may close it as superseded — per the Product Owner's discard-and-retire decision (Issue #17 comment #5128855220) and Sprint 034: Gate 2 preflight must not proceed under the current migrate-and-retire documents. It is documentation-only and was cut from `dev` at `8ae823e8dc7b337a70bd6d420745595463680666` on branch `sprint/r14c-gate2-preflight`; the final accepted head `effc27f92f7b2cbee3c2357fe4dba97f67872a11` was squash-merged through PR #28 into `dev` as `e7e057b6b35244cc5625368570ed3f6a26cabc40` on 2026-07-29. The integrated tracking scaffold consists of a redacted B1–B6 / preflight 1–15 / E1–E10 status ledger [`documents/07-deployment/room-cutover-gate2-preflight-ledger.md`](../07-deployment/room-cutover-gate2-preflight-ledger.md), an operator evidence-return template [`documents/07-deployment/room-cutover-gate2-evidence-return.template.md`](../07-deployment/room-cutover-gate2-evidence-return.template.md), an operational discrepancy register [`documents/07-deployment/room-cutover-gate2-discrepancy-register.md`](../07-deployment/room-cutover-gate2-discrepancy-register.md), and a Product Owner go/no-go review packet [`documents/07-deployment/room-cutover-gate2-go-no-go-packet.md`](../07-deployment/room-cutover-gate2-go-no-go-packet.md) — all now marked **non-executable** under ADR 004 pending Sprint 038 acceptance and later successor preflight authorization. Every ledger row remains `Not started` under the deterministic status model, the evidence inventory and discrepancy register remain empty, and the packet's unsigned recommendation remains `DEFER / NOT READY`. The pause resolves nothing and closes nothing: blockers B1–B7 remain unresolved, Gate 2 remains pending and unauthorized, production remains untouched, and R14e remains inactive.

Sprint 032 — **R14c Solo-Operator Governance Amendment** — is **closed and accepted**: PR #30, accepted head `55b4133ba42a3a880a42e553ea3ed8f0192dd294`, was squash-merged into `dev` as `3ed430b3b753e35a00d3127b782537a38008ca56` on 2026-07-30, making its governance amendment authoritative on `dev`. It was cut from `dev` at `c2ca389475174ad165391085a4e98b65d70f3455` on branch `sprint/r14c-solo-operator-governance`. It records `hoanganh-ng` as the approved combined Product Owner-of-record and human Operator **for R14c Gate 2 only**, preserving separate-role governance as the normal preference outside this named exception, and replaces the unconditional `Operator ≠ Product Owner` requirement on the Gate 2 decision surfaces with a mandatory three-pass sequence: (1) Operator evidence pass, (2) Architect review pass — outcomes limited to `READY FOR PO DECISION`, `DEFER — EVIDENCE INCOMPLETE`, or `NO-GO RECOMMENDED`; the review grants no production authority and does not replace the Product Owner decision — and (3) Product Owner GO / NO-GO / DEFER decision pass; a GO recorded before the Architect review pass is invalid. The B1 and E3 pass conditions are amended accordingly while both rows remain `Not started`. Every fail-closed readiness, evidence, rollback, discrepancy, and production-safety control is preserved: all B1–B6, PF-01–PF-15, and E1–E10 statuses remain `Not started`, the evidence and discrepancy inventories remain empty, the unsigned recommendation remains `DEFER / NOT READY`, Gate 2 remains pending and unauthorized, production remains untouched, R14e remains inactive, the runbook procedures and command order are unchanged, and no AI agent may execute Gate 2 operations.

Sprint 033 — **R14c Governance Lifecycle Transition** ([`SPRINTS/033-r14c-governance-lifecycle-transition.md`](./SPRINTS/033-r14c-governance-lifecycle-transition.md)) — is the **completed documentation-only lifecycle bridge** that recorded Sprint 032's closure and had restored Sprint 031 as the sole active sprint (a state since superseded by Sprint 034's activation and Sprint 031's pause). It changed no governance rule, touched no deployment document, and resolved nothing: all readiness rows remain `Not started`, the evidence and discrepancy inventories remain empty, all decisions remain unsigned, the unsigned recommendation remains `DEFER / NOT READY`, Gate 2 remains pending and unauthorized, production remains untouched, and R14e remains inactive.

Sprint 034 — **R14 Discard-and-Retire Contract Amendment** ([`SPRINTS/034-r14-discard-and-retire-contract-amendment.md`](./SPRINTS/034-r14-discard-and-retire-contract-amendment.md)) — is **closed and accepted**: PR #32 (branch `sprint/r14-discard-retire-contract-amendment`, cut from `dev` at `96c7d0f8a59bf511eb58f5f7432a4283bca409ef`), accepted head `6686852445d3cf46bf6c46f3b76d94ba9b932098`, was Architect-accepted and, under Product Owner merge authorization, squash-merged into `dev` as `e65eef9954b2f4ba24f4866b2b084910e76e090c` on 2026-07-31. Its 17-file documentation-and-architecture amendment is authoritative on `dev`: ADR 004 is **accepted architecture authority** (no longer draft or pending), the migrate-and-retire production assumptions of ADR 003, R14b, and R14c are superseded while preserved as historical records, Sprint 031 is paused, and the runbook and all Gate 2 documents are marked non-executable. The closure resolves nothing operationally: blockers B1–B7 remain unresolved, Gate 2 remains pending and unauthorized, production remains untouched, and R14e remains inactive.

Sprint 035 — **R14 Discard-and-Retire Lifecycle Transition** ([`SPRINTS/035-r14-discard-retire-lifecycle-transition.md`](./SPRINTS/035-r14-discard-retire-lifecycle-transition.md)) — is the **completed documentation-only lifecycle bridge** (Issue #17 comments #5138919618, #5138932156, #5138980052, #5138989366) that reconciled the trackers with `dev` after PR #32's acceptance: Sprint 034 is closed and accepted, ADR 004 is accepted architecture authority, and no sprint remained active after that transition. It changed no governance rule, altered no product direction, touched no deployment document, and resolved nothing.

Sprint 036 — **R14 Discard-and-Retire Implementation-Correction Shaping** ([`SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md`](./SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md)) — is **closed and accepted**. PR #34, accepted head `65d17f7955d0dd9e782c6cb4298467a26613b892`, was squash-merged into `dev` as `723023fd296678bac8aec0ace784ff2e94bb119f` on 2026-07-31. Its accepted delivery shaped and authorized the ADR 004 Section 6 implementation-correction sprint; it did not execute that sprint.

Sprint 037 — **R14 Discard-and-Retire Correction Activation** ([`SPRINTS/037-r14-discard-retire-correction-activation.md`](./SPRINTS/037-r14-discard-retire-correction-activation.md)) — is completed as the documentation-only lifecycle bridge from `e4b94c0aef74ef90a104a9437caeae99e05c1da3`. It records Sprint 036 closure, records Sprint 038 activation, grants no continuing authority, and changes no readiness, production, rollback, cleanup, or R14e state.

R14c owns:

- strict runtime parsing for `--room-cutover-authoritative=true|false`;
- a historical fail-closed true-mode guard requiring a clean schema version at least 9 and `room_cutover_marker.id = 1` before listeners open; Sprint 038 replaces current startup authority with clean migrations, schema at least `10`, valid `room_authoritative_activation`, and no historical copy marker;
- one no-op room-activity writer in false mode and one real PostgreSQL room-activity writer in true mode;
- atomic repository-free `410 Gone` tombstones for the approved legacy global queue, vote, auto-queue, and `/ws` entry points;
- preservation of auth, account utility, room REST, and room WebSocket contracts;
- one Compose-level deployment input that produces matching false/false or true/true server/SPA artifacts;
- historical packaging of the accepted `cmd/room-cutover` CLI in the backend image; Sprint 038 changes current packaging so the backend image contains `server` only and the operator image contains `migrate-schema` plus `room-activation`;
- isolated rehearsal evidence and the redacted production runbook.

The Builder is not authorized to run production migration commands, deploy the true pair, modify production data/secrets, close or reopen traffic, close R14c, activate R14e, or advance the epic.

R14d — Frontend global-path retirement behind the cutover build gate — remains closed and accepted. PR #25 was merged into `dev` at merge commit `67bd57a8abef92d42cdfefb90069563340147d10`; the final reviewed feature head was `491770ed90e3940307df3933d4236508fac838f3`.

R09i — Room activity runtime parity — remains closed and accepted. PR #24 was merged into `dev` at merge commit `c112000d2a70d105419a57f4059bafd4051e423d`; normal pre-cutover composition still injects the explicit no-op room-activity writer.

R14e remains inactive. Migration `0011`, schema version 11, and removal of legacy global tables remain out of scope until after the separately approved production cutover and verified rollback window. Migration `0010` is Sprint 038 activation-proof work and must preserve legacy global tables while startup depends on `room_authoritative_activation`.

Room epic Issue #17 remains open.

## Accepted cutover sequence

The historically approved order was:

```text
R05b → R09h → R14b → R09i → R14d → R14c → R14e
```

Per ADR 004 (2026-07-30), the R14c production-execution step of this sequence follows discard-and-retire and requires Sprint 038 acceptance before any successor Gate 2 preflight; the accepted prerequisite history below is unchanged.

Completed and accepted prerequisites:

- R05b — room entry and player-lease UI slices
- R09h — room vote-to-prioritize parity
- R14b — schema and offline room-cutover mechanism
- R09i — room activity runtime parity
- R14d — frontend global-path retirement behind the cutover build gate

R14c Gate 1 implementation and rehearsal are integrated and closed on `dev`. Per ADR 004 (Sprint 034, 2026-07-30), the production direction is now **discard-and-retire**: the migrate-and-retire production execution that R14c Gate 2 was preparing **will not run**. Sprint 031's Gate 2 preflight is paused (not closed, not superseded) and its documents are non-executable. Sprint 038 must land the revised activation proof, cutover procedure, and operational surfaces before any successor Gate 2 preflight may resume. R14e remains inactive.

## Accepted room capability baseline

Accepted room behavior includes room lifecycle, invites, membership and role enforcement, player-lease flows, room-scoped queue and playback controls, room-scoped voting, room-scoped auto-queue, activity-production parity, per-room WebSocket synchronization and deltas, room archival/member removal, existing frontend room flows, and R14d's dual-artifact frontend cutover preparation.

Deferred scope remains deferred: R10f+ lifecycle hardening, R11b+ chat expansion, R12 discovery/search work, R13 broad session/authentication hardening, R14e destructive cleanup, and any later room slices not explicitly activated.

## Completion estimate

The Room epic remains approximately **85% accepted**. This is an implementation-completion estimate, not production-cutover readiness. R14c's Gate 1 implementation — including the runtime switch, contract retirement, and deployment pairing — is integrated and closed; per ADR 004 the remaining work is: Sprint 038 implementation-correction acceptance and integration, a later revised Gate 2 preflight, the Product Owner go/no-go decision, the separately authorized production execution, live verification, rollback-window governance, and eventual R14e eligibility. R14e remains a separate irreversible cleanup sprint.

## Authoritative references

- [`ADRS/004-discard-legacy-global-state-at-room-cutover.md`](./ADRS/004-discard-legacy-global-state-at-room-cutover.md)
- [`SPRINTS/035-r14-discard-retire-lifecycle-transition.md`](./SPRINTS/035-r14-discard-retire-lifecycle-transition.md)
- [`SPRINTS/034-r14-discard-and-retire-contract-amendment.md`](./SPRINTS/034-r14-discard-and-retire-contract-amendment.md)
- [`SPRINTS/033-r14c-governance-lifecycle-transition.md`](./SPRINTS/033-r14c-governance-lifecycle-transition.md)
- [`SPRINTS/032-r14c-solo-operator-governance.md`](./SPRINTS/032-r14c-solo-operator-governance.md)
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
