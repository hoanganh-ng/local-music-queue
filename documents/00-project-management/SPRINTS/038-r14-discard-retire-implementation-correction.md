# Sprint 038 — R14 Discard-and-Retire Implementation Correction

**Status:** Active — sole authorized sprint; implementation and operational-document correction only; no preflight or production authority
**Branch:** `sprint/r14-discard-retire-implementation-correction`
**Base:** `dev` at `e4b94c0aef74ef90a104a9437caeae99e05c1da3`
**Parent epic:** Issue #17
**Risk:** High — schema, startup safety, operator tooling, packaging, and operational-contract changes; production execution remains prohibited
**Architecture authority:** ADR 004 plus `docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md`

## Authorization record

- Intent/boundary: https://github.com/hoanganh-ng/local-music-queue/issues/17#issuecomment-5189935491
- Acceptance criteria: https://github.com/hoanganh-ng/local-music-queue/issues/17#issuecomment-5190095970
- Builder authorization: https://github.com/hoanganh-ng/local-music-queue/issues/17#issuecomment-5190130138

## Intent

Implement the ADR 004 Section 6 discard-and-retire correction contract and replace copy-based operational guidance with activation-proof-based procedures, without executing preflight or production action.

## Expected outcome

- Authoritative true startup is fail-closed on a dedicated discard-and-retire activation proof instead of a migrated-room copy marker.
- Operators get `room-activation prepare` and `room-activation verify` for PostgreSQL activation proof management.
- Packaging separates runtime server and operator commands.
- Current executable procedures use `room-discard-cutover-*` documents and preserve old Sprint 031 Gate 2 documents unchanged as non-executable historical records.

## Locked architecture decisions

1. Migration `0010` creates dedicated `room_authoritative_activation`; `room_cutover_marker` is not repurposed or synthesized.
2. R14e cleanup moves to migration `0011`; the activation proof remains while startup depends on it.
3. Activation record: one row `id=1`, UUID activation ID, mode `discard-and-retire`, contract version `1`, preparation schema `10`, immutable timestamp, non-empty build SHA, no room/copy fields.
4. One shared PostgreSQL preparation/verification component serves the CLI and startup guard.
5. CLI exposes only `room-activation prepare` and `room-activation verify`; DSN comes only from `DATABASE_URL` or `MIGRATE_DATABASE_URL`; exact repeats retain timestamp; conflicts fail; no force/reset/overwrite/delete/bypass/repair.
6. True startup requires clean migrations, schema at least `10`, valid proof, and no historical copy marker before listeners; false mode remains rollback-compatible without proof.
7. Backend image contains `server` and excludes both operator commands; operator image contains `migrate-schema` and `room-activation`; default remains schema migration.
8. Historical `cmd/room-cutover` source/tests remain but are not packaged or referenced by current executable procedures.
9. Create the seven approved `room-discard-cutover-*` operational documents; preserve the old seven documents unchanged and non-executable.
10. New procedure includes backup, migration `0010`, activation prepare/verify, paired true/true deploy, smoke checks, false/false rollback, and legacy-table preservation; no copy step.
11. Require real PostgreSQL migration/component/startup/non-destructive tests, CLI and secret-redaction tests, artifact inspection, Compose pairing, Go/vet/race/Docker evidence, GitNexus impact analysis, and `detect_changes()`.
12. Prohibit production preflight, credentials, deployment, traffic changes, cutover, rollback execution, readiness advancement, GO, R14e cleanup, extra product scope, and Issue #17 closure.
13. Sprint 031 stays paused throughout Sprint 038 implementation.
14. Builder delivers a short-lived branch and unmerged draft PR with exact evidence; Builder does not mark ready, approve, merge, or activate later work.

## Included scope

- Add PostgreSQL migration `0010` for `room_authoritative_activation` with durable activation-proof constraints.
- Add shared PostgreSQL preparation/verification logic used by both CLI and startup guard.
- Add `room-activation prepare` and `room-activation verify` operator CLI behavior with DSN source limited to `DATABASE_URL` or `MIGRATE_DATABASE_URL`.
- Update true-mode startup guard to require clean migrations, schema at least `10`, valid activation proof, and no historical copy marker before opening listeners.
- Keep false-mode startup rollback-compatible without requiring activation proof.
- Update Docker/operator packaging so backend image contains `server` only, and operator image contains `migrate-schema` and `room-activation` with default schema migration behavior retained.
- Keep historical `cmd/room-cutover` source/tests in repository while excluding it from current executable procedures and packaging.
- Create seven approved `room-discard-cutover-*` operational documents and preserve the old seven Sprint 031 Gate 2 documents unchanged and non-executable.

The seven approved `room-discard-cutover-*` operational documents are:

1. `documents/07-deployment/room-discard-cutover-runbook.md`
2. `documents/07-deployment/room-discard-cutover-gate2-readiness.md`
3. `documents/07-deployment/room-discard-cutover-gate2-inputs.template.md`
4. `documents/07-deployment/room-discard-cutover-gate2-preflight-ledger.md`
5. `documents/07-deployment/room-discard-cutover-gate2-evidence-return.template.md`
6. `documents/07-deployment/room-discard-cutover-gate2-discrepancy-register.md`
7. `documents/07-deployment/room-discard-cutover-gate2-go-no-go-packet.md`

## Explicit exclusions

- Production preflight, production credentials, deployment, traffic changes, cutover, rollback execution, readiness advancement, GO, R14e cleanup, extra product scope, and Issue #17 closure.
- `room_cutover_marker` repurpose, synthesis, reset, or deletion as activation proof.
- Any `room-activation` force, reset, overwrite, delete, bypass, or repair command or flag.
- Any copy step in the new discard-and-retire procedure.
- Closing Sprint 031 as superseded during Sprint 038 implementation.
- Packaging or referencing historical `cmd/room-cutover` in current executable procedures.

## Implementation boundaries

- All database behavior must target real PostgreSQL where the acceptance criteria require migration, activation component, startup, and non-destructive evidence.
- CLI and startup guard must share one preparation/verification component rather than duplicating business rules.
- Secrets and DSNs must be redacted in CLI output, logs, errors, and tests.
- Startup guard must complete before listeners open.
- Activation prepare must be idempotent only for exact repeats and must retain the original immutable timestamp.
- Conflicting activation state must fail closed and must not offer force/reset/overwrite/delete/bypass/repair behavior.
- Legacy room tables and historical markers must be preserved unless a later authorized cleanup sprint changes that contract.

## Acceptance criteria

- Migration `0010` creates `room_authoritative_activation` and does not repurpose or synthesize `room_cutover_marker`.
- Activation row `id=1` contains UUID activation ID, mode `discard-and-retire`, contract version `1`, preparation schema `10`, immutable timestamp, non-empty build SHA, and no room/copy fields.
- R14e cleanup is deferred to migration `0011`, with activation proof retained while startup depends on it.
- CLI exposes only `room-activation prepare` and `room-activation verify`, reads DSN only from `DATABASE_URL` or `MIGRATE_DATABASE_URL`, retains timestamp on exact repeats, fails conflicts, and provides no force/reset/overwrite/delete/bypass/repair path.
- True startup refuses to serve unless migrations are clean, schema is at least `10`, activation proof is valid, and no historical copy marker exists before listeners open.
- False mode remains rollback-compatible without activation proof.
- Backend image contains `server` only; operator image contains `migrate-schema` and `room-activation`; default remains schema migration.
- Historical `cmd/room-cutover` source/tests remain but are not packaged or referenced by current executable procedures.
- Seven approved `room-discard-cutover-*` operational documents exist, and old seven Gate 2 documents remain unchanged and non-executable.
- New procedure covers backup, migration `0010`, activation prepare/verify, paired true/true deploy, smoke checks, false/false rollback, and legacy-table preservation; it contains no copy step.
- Sprint 031 remains paused throughout Sprint 038.
- Builder delivers a short-lived branch and unmerged draft PR with exact evidence and does not mark ready, approve, merge, or activate later work.

## Required evidence

- Real PostgreSQL migration tests for migration `0010` and preservation of existing legacy/copy-marker data.
- Real PostgreSQL shared activation component tests for prepare, verify, exact-repeat idempotence, conflict failure, timestamp immutability, build-SHA requirement, and secret redaction.
- Startup guard tests proving true-mode fail-closed behavior before listeners and false-mode rollback compatibility without proof.
- CLI tests proving command surface contains only `room-activation prepare` and `room-activation verify`, DSN source policy, exact-repeat timestamp retention, conflict failure, no force/reset/overwrite/delete/bypass/repair behavior, and secret redaction.
- Artifact inspection proving backend image contains `server` only and operator image contains `migrate-schema` plus `room-activation` with default schema migration retained.
- Compose pairing evidence for paired true/true deploy and false/false rollback configuration.
- Operational-document evidence proving seven `room-discard-cutover-*` documents exist, old seven documents are unchanged/non-executable, and new procedure contains no copy step.
- Command evidence for `go test ./...`, `go test -race ./...`, `go vet ./...`, Docker/artifact checks, GitNexus impact analysis, and `detect_changes()`.

## Builder delivery contract

- Builder works on a short-lived branch for Sprint 038 implementation only.
- Builder opens an unmerged draft PR with exact evidence for tests, race/vet checks, Docker/artifact inspection, Compose pairing, GitNexus impact analysis, and `detect_changes()`.
- Builder does not mark the PR ready, approve it, merge it, activate successor work, close Issue #17, execute preflight, access production credentials, deploy, change traffic, run cutover, run rollback, advance readiness, declare GO, or perform R14e cleanup.

## Blockers and stop conditions

- Stop if implementation requires production credentials, production access, deployment, traffic changes, cutover execution, rollback execution, readiness advancement, GO, R14e cleanup, or Issue #17 closure.
- Stop if the activation proof cannot be implemented without repurposing or synthesizing `room_cutover_marker`.
- Stop if startup guard cannot verify clean migrations, schema at least `10`, valid proof, and no historical copy marker before listeners open.
- Stop if packaging cannot keep backend image to `server` only while operator image carries `migrate-schema` and `room-activation`.
- Stop if seven old Gate 2 documents would need edits to satisfy this sprint.

## References

- ADR 004: `documents/00-project-management/ADRS/004-discard-legacy-global-state-at-room-cutover.md`
- Approved success design: `docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md`
- Sprint 036 closure: `documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md`
- Sprint 037 activation bridge: `documents/00-project-management/SPRINTS/037-r14-discard-retire-correction-activation.md`
- Issue #17 intent/boundary: https://github.com/hoanganh-ng/local-music-queue/issues/17#issuecomment-5189935491
- Issue #17 acceptance criteria: https://github.com/hoanganh-ng/local-music-queue/issues/17#issuecomment-5190095970
- Issue #17 Builder authorization: https://github.com/hoanganh-ng/local-music-queue/issues/17#issuecomment-5190130138
