# Active Sprint

**Sprint 038 — R14 Discard-and-Retire Implementation Correction** ([`038-r14-discard-retire-implementation-correction.md`](./038-r14-discard-retire-implementation-correction.md)) is the **sole active sprint**. Branch: `sprint/r14-discard-retire-implementation-correction`. Base: `dev` at `e4b94c0aef74ef90a104a9437caeae99e05c1da3`. Authority comes from ADR 004, the approved success design (`docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md`), and Sprint 037 Builder authorization (Issue #17 comments #5189935491, #5190095970, #5190130138). It authorizes implementation and revised operational documents only; it grants **no preflight, production, credential, deployment, traffic, cutover, rollback, cleanup, readiness, GO, R14e, or Issue #17 closure authority**.

## Sprint 038 Scope

- Add PostgreSQL migration `0010` for `room_authoritative_activation` with durable activation-proof constraints.
- Add shared PostgreSQL preparation/verification logic used by both CLI and startup guard.
- Add `room-activation prepare` and `room-activation verify` operator CLI behavior with DSN source limited to `DATABASE_URL` or `MIGRATE_DATABASE_URL`.
- Update true-mode startup guard to require clean migrations, schema at least `10`, valid activation proof, and no historical copy marker before opening listeners; keep false mode rollback-compatible without proof.
- Update packaging so the backend image contains `server` only, and the operator image contains `migrate-schema` and `room-activation` with default schema migration behavior retained.
- Keep historical `cmd/room-cutover` source/tests in repository while excluding it from current executable procedures and packaging.
- Create the seven approved `room-discard-cutover-*` operational documents and preserve the old seven Sprint 031 Gate 2 documents unchanged and non-executable.

## Sprint 038 Acceptance

- Migration `0010` creates `room_authoritative_activation`; it does not repurpose or synthesize `room_cutover_marker`.
- Activation row `id=1` contains UUID activation ID, mode `discard-and-retire`, contract version `1`, preparation schema `10`, immutable timestamp, non-empty build SHA, and no room/copy fields.
- R14e cleanup is deferred to migration `0011`, with activation proof retained while startup depends on it.
- CLI exposes only `room-activation prepare` and `room-activation verify`, reads DSN only from `DATABASE_URL` or `MIGRATE_DATABASE_URL`, retains timestamp on exact repeats, fails conflicts, and provides no force/reset/overwrite/delete/bypass/repair path.
- True startup refuses to serve unless migrations are clean, schema is at least `10`, activation proof is valid, and no historical copy marker exists before listeners open; false mode remains rollback-compatible without activation proof.
- Backend image contains `server` only; operator image contains `migrate-schema` and `room-activation`; default remains schema migration.
- Historical `cmd/room-cutover` source/tests remain but are not packaged or referenced by current executable procedures.
- Seven approved `room-discard-cutover-*` operational documents exist, old seven Gate 2 documents remain unchanged and non-executable, and the new procedure contains no copy step.
- Sprint 031 remains paused throughout Sprint 038.
- Builder delivers a short-lived branch and unmerged draft PR with exact evidence and does not mark ready, approve, merge, or activate later work.

## Explicit Exclusions

- Production preflight, production credentials, deployment, traffic changes, cutover, rollback execution, readiness advancement, GO, R14e cleanup, extra product scope, and Issue #17 closure.
- `room_cutover_marker` repurpose, synthesis, reset, or deletion as activation proof.
- Any `room-activation` force, reset, overwrite, delete, bypass, or repair command or flag.
- Any copy step in the new discard-and-retire procedure.
- Closing Sprint 031 as superseded during Sprint 038 implementation.
- Packaging or referencing historical `cmd/room-cutover` in current executable procedures.

## Out of scope

- Operational execution of any new `room-discard-cutover-*` document.
- Changes outside the Sprint 038 implementation/documentation boundary.
- Successor Gate 2 preflight, Product Owner GO / NO-GO / DEFER decision, production cutover, rollback execution, or destructive cleanup.

## Lifecycle Notes

**Sprint 037 — R14 Discard-and-Retire Correction Activation** ([`037-r14-discard-retire-correction-activation.md`](./037-r14-discard-retire-correction-activation.md)) is completed as the documentation-only lifecycle bridge that closed Sprint 036 and activated Sprint 038. It grants no continuing authority.

**Sprint 036 — R14 Discard-and-Retire Implementation-Correction Shaping** ([`036-r14-discard-retire-implementation-correction-shaping.md`](./036-r14-discard-retire-implementation-correction-shaping.md)) is **closed and accepted**: PR #34, accepted head `65d17f7955d0dd9e782c6cb4298467a26613b892`, was squash-merged into `dev` as `723023fd296678bac8aec0ace784ff2e94bb119f` on 2026-07-31. It shaped and authorized the Sprint 038 contract; it did not execute implementation, preflight, production, rollback, or R14e work.

**Sprint 031 — R14c Gate 2 Preflight and Go/No-Go Preparation** ([`031-r14c-gate2-preflight.md`](./031-r14c-gate2-preflight.md)) remains **paused as of 2026-07-30 — not closed, not completed, not superseded, not resumed**. The old runbook and every Gate 2 document remain **non-executable** until Sprint 038 is accepted and integrated and a later successor preflight is separately authorized. All readiness rows remain `Not started`, evidence and discrepancy inventories remain empty, all decisions remain unsigned, and the unsigned recommendation remains `DEFER / NOT READY`.

**Sprint 034** is closed and accepted; ADR 004 remains accepted architecture authority. **Sprint 035** is completed. **Sprint 033**, **Sprint 032**, and **Sprint 030** remain closed historical records.

Gate 2 remains unauthorized. Production and legacy global tables remain untouched. R14e remains inactive; migration `0011` cleanup is not authorized by Sprint 038. Room epic Issue #17 remains open.
