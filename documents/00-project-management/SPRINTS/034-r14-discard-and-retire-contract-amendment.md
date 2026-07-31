# Sprint 034 — R14 Discard-and-Retire Contract Amendment

**Status:** Closed and accepted — PR #32, accepted head `6686852445d3cf46bf6c46f3b76d94ba9b932098`, squash-merged into `dev` as `e65eef9954b2f4ba24f4866b2b084910e76e090c` on 2026-07-31, following Architect acceptance and Product Owner merge authorization; lifecycle transition recorded by Sprint 035 ([`035-r14-discard-retire-lifecycle-transition.md`](./035-r14-discard-retire-lifecycle-transition.md))
**Branch:** `sprint/r14-discard-retire-contract-amendment`
**Base:** `dev` at `96c7d0f8a59bf511eb58f5f7432a4283bca409ef`
**Parent epic:** Issue #17
**Risk:** Low (documentation and architecture amendment only — no code, schema, deployment, or production change)
**Scope:** Exactly the 17 approved documentation files (Issue #17 comment #5129061033). No other file may change.

## Authorization record

- Product Owner decision — discard-and-retire legacy global state: Issue #17 comment #5128855220 (2026-07-30).
- Sprint intent and boundary approval: Issue #17 comment #5128872419 (2026-07-30).
- Approved 17-file document boundary: Issue #17 comment #5129061033 (2026-07-30).
- Acceptance criteria: Issue #17 comment #5129187382 (2026-07-30).
- Builder authorization: Issue #17 comment #5129335141 (2026-07-30).

## Goal

Replace the accepted migrate-and-retire cutover contract with the **discard-and-retire** contract as the approved product direction, recorded as ADR 004 ([`../ADRS/004-discard-legacy-global-state-at-room-cutover.md`](../ADRS/004-discard-legacy-global-state-at-room-cutover.md)): no migrated room, no copy of the legacy global queue state, activities, auto-queue configuration, or play history; room-authoritative operation begins with no inherited playback state; users create or join ordinary rooms through the accepted room flows.

## Locked direction

- Replace the accepted migrate-and-retire cutover contract with discard-and-retire.
- Create no migrated room and carry forward no legacy global queue, activity, auto-queue, or play-history state.
- Begin room-authoritative operation with no inherited playback state — "no inherited playback state" means no state copied from the legacy global tables; nothing in this sprint or ADR 004 authorizes deleting or modifying any pre-existing room-scoped room, membership, queue, activity, auto-queue, play-history, lease, invite, or chat data.
- Preserve mandatory backup and false/false rollback artifacts.
- Retain legacy global tables unchanged throughout the rollback window.
- Pause Sprint 031 while Sprint 034 is active — Sprint 031 is neither closed nor superseded.
- Record the current activation proof (`room_cutover_marker`) as **unresolved** and assign its definition and implementation — together with the revised contract, readiness model, rollback model, and preflight surfaces — to the later implementation-correction sprint (ADR 004 Sections 5–6); revised Gate 2 preflight may not resume before that sprint is accepted and integrated. Sprint 034 does not define the revised activation proof.

## Deliverables (the approved 17-file boundary)

1. `documents/00-project-management/ADRS/004-discard-legacy-global-state-at-room-cutover.md` (new)
2. `documents/00-project-management/SPRINTS/034-r14-discard-and-retire-contract-amendment.md` (new — this record)
3. `documents/00-project-management/SPRINTS/active.md` (Sprint 034 sole active; Sprint 031 paused)
4. `documents/00-project-management/SPRINTS/README.md` (index rows)
5. `documents/00-project-management/PROJECT_STATE.md` (direction change recorded)
6. `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md` (direction change recorded)
7. `documents/00-project-management/SPRINTS/031-r14c-gate2-preflight.md` (paused; documents non-executable)
8. `documents/00-project-management/ADRS/003-legacy-global-state-migration-and-contract-retirement.md` (supersession note; historical)
9. `documents/00-project-management/SPRINTS/026-room-cutover-mechanism.md` (historical; copy mechanism unsuitable for revised cutover)
10. `documents/00-project-management/SPRINTS/029-coordinated-authoritative-room-cutover.md` (historical; production assumptions superseded)
11. `documents/07-deployment/room-cutover-runbook.md` (marked non-executable)
12. `documents/07-deployment/room-cutover-gate2-readiness.md` (marked non-executable)
13. `documents/07-deployment/room-cutover-gate2-inputs.template.md` (marked non-executable)
14. `documents/07-deployment/room-cutover-gate2-preflight-ledger.md` (marked non-executable)
15. `documents/07-deployment/room-cutover-gate2-evidence-return.template.md` (marked non-executable)
16. `documents/07-deployment/room-cutover-gate2-discrepancy-register.md` (marked non-executable)
17. `documents/07-deployment/room-cutover-gate2-go-no-go-packet.md` (marked non-executable)

## What this sprint changes

- **ADR 004 created** as the new authority for the discard-and-retire direction.
- **ADR 003 remains historical** with an explicit supersession note identifying the migrate-and-retire portions superseded by ADR 004; its body is otherwise preserved.
- **Sprint 034 becomes the sole active sprint. Sprint 031 is paused** — not closed, not completed, not superseded; it must not proceed under the current documents.
- **Accepted R14b work remains historical**, while the existing room-cutover copy mechanism is declared unsuitable for the revised production cutover.
- **The current activation proof is explicitly unresolved:** the startup guard depends on `room_cutover_marker`, which currently proves a migrated-room copy; a later implementation sprint must define and implement a revised activation proof (ADR 004 Section 5).
- **The runbook and all Gate 2 documents are marked non-executable** until the implementation-correction sprint is accepted and integrated.
- **The required next implementation sprint is identified** (ADR 004 Section 6) without prescribing Builder-level implementation details.

## What this sprint preserves

- All B1–B6, PF-01–PF-15, and E1–E10 ledger rows remain `Not started`; the B7 GO record remains absent; the evidence inventory and discrepancy register remain empty; all decision fields remain unsigned; the recommendation remains `DEFER / NOT READY`.
- Mandatory backups, rollback artifacts, closed-traffic execution, paired server/SPA deployment, and fail-closed behavior remain mandatory.
- The Operator evidence → Architect review → Product Owner decision sequence (Sprint 032 governance) remains mandatory.
- Legacy global tables remain untouched throughout the rollback window; no R14e cleanup is authorized.
- Accepted R14b and R14c Gate 1 work remains on `dev` as historical record; nothing is reverted.

## Out of scope

- Any code, migration, schema, test, Docker, frontend, runtime, preflight, production, credential, backup, host, database, cutover, rollback, or R14e action.
- Shaping, activating, or designing the implementation-correction sprint beyond identifying that it is required (ADR 004 Section 6).
- Advancing any readiness row, creating any revised GO path, resolving any blocker, or signing any decision.
- Closing, completing, or superseding Sprint 031.
- Marking the delivery PR ready, approving it, merging it, or advancing Sprint 034 status — those remain Product Owner actions.

## Verification

Documentation-only change set; the required evidence commands (Issue #17 comment #5129335141):

```bash
git diff --check dev...HEAD
git diff --name-only dev...HEAD
git diff --name-only dev...HEAD | wc -l
git diff --name-only dev...HEAD | grep -Ev '^documents/' || true
git status --short
```

Expected: clean diff, exactly 17 changed files, every changed file under `documents/`, and a clean worktree.

## Record

- Prepared from `dev` at `96c7d0f8a59bf511eb58f5f7432a4283bca409ef` on branch `sprint/r14-discard-retire-contract-amendment` for Architect review and Product Owner decision.
- Delivered as the unmerged draft PR `docs(r14): replace migration with discard-and-retire contract`; the Builder does not mark it ready, approve it, merge it, or advance sprint status.
- No production system, snapshot, deployment host, container, connection bundle, evidence directory, credential, or identity was accessed in preparing this sprint. No runbook step was executed. No readiness item was advanced and no GO was declared.
- Gate 2 remains pending and unauthorized; production remains untouched; the `true` pair remains undeployed; R14e remains inactive. Room epic Issue #17 remains open.

## Closure record

- **Sprint 034 is closed and accepted.** PR #32, accepted head `6686852445d3cf46bf6c46f3b76d94ba9b932098` (including the corrective pass addressing Architect findings F1–F4, PR #32 review #5129688424), was Architect-accepted and, under Product Owner merge authorization, squash-merged into `dev` as `e65eef9954b2f4ba24f4866b2b084910e76e090c` on 2026-07-31.
- ADR 004 is accepted architecture authority on `dev` — no longer draft or pending authority. Its product direction is unchanged.
- The post-merge lifecycle transition is recorded by Sprint 035 — R14 Discard-and-Retire Lifecycle Transition ([`035-r14-discard-retire-lifecycle-transition.md`](./035-r14-discard-retire-lifecycle-transition.md)). After that transition, no sprint is active.
- The closure resolves nothing beyond the lifecycle: Sprint 031 remains paused (not closed, not completed, not superseded, not resumed); the runbook and all Gate 2 documents remain non-executable; all B1–B6, PF-01–PF-15, and E1–E10 rows remain `Not started`; the B7 GO record remains absent; the evidence and discrepancy inventories remain empty; all decision fields remain unsigned; the recommendation remains `DEFER / NOT READY`; the implementation-correction sprint (ADR 004 Section 6) remains required but unshaped, inactive, and unauthorized; Gate 2 remains pending and unauthorized; production remains untouched; R14e remains inactive. Room epic Issue #17 remains open.
