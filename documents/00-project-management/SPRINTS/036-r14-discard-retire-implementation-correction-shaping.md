# Sprint 036 — R14 Discard-and-Retire Implementation-Correction Shaping

**Status:** In progress — awaiting Architect review and Product Owner approval
**Branch:** `sprint/r14-discard-retire-implementation-correction-shaping`
**Base:** `dev` at `11c30c4eeccdbc1a2719deb1afed4a9f0273e750`
**Parent epic:** Issue #17
**Risk:** Low (documentation only — no code, schema, deployment, or production change)
**Scope:** Exactly the approved five project-management documentation files (Issue #17 comment #5140909749). No deployment document is touched. No ADR is touched. No production, snapshot, identity, credential, evidence-storage, deployment-host, container, or infrastructure access of any kind.

## Authorization record

- Sprint intent and boundary approval: Issue #17 comment #5140909519 (2026-07-31).
- Approved five-file document boundary: Issue #17 comment #5140909749 (2026-07-31).
- Acceptance criteria: Issue #17 comment #5140910084 (2026-07-31).
- Builder authorization: Issue #17 comment #5140914484 (2026-07-31).

## Goal

Shape, scope, and define the acceptance criteria for the **discard-and-retire implementation-correction sprint** required by ADR 004 Section 6, and — upon Product Owner acceptance of this sprint's PR — authorize it. This sprint is documentation-only shaping: it changes no code, resolves nothing operationally, and does **not** itself activate the correction sprint. The shaped contract below is written as outcome/evidence contracts only — ADR 004 §6 is identification only, and this sprint prescribes no Builder-level design.

## Current behavior

At the approved base (`dev` at `11c30c4eeccdbc1a2719deb1afed4a9f0273e750`):

- **No sprint is active.** Sprint 035 — the completed documentation-only lifecycle bridge — recorded Sprint 034's closure and ADR 004's acceptance as architecture authority on `dev`, leaving no active sprint.
- The implementation-correction sprint required by ADR 004 Section 6 is **unshaped, inactive, and unauthorized** (per `active.md` and `PROJECT_STATE.md`).
- Sprint 031 — R14c Gate 2 Preflight — remains **paused** (not closed, not superseded, not resumed).
- All B1–B6, PF-01–PF-15, and E1–E10 readiness rows remain `Not started`; the B7 GO record remains absent; the evidence and discrepancy inventories remain empty; all decision fields remain unsigned; the unsigned recommendation remains `DEFER / NOT READY`.
- Gate 2 remains pending and unauthorized; the runbook and all Gate 2 documents remain non-executable; production remains untouched; R14e remains inactive.

## Shaped contract — the implementation-correction sprint

The five subsections below map 1:1 to ADR 004 §6 outcomes 1–5. Each is an **outcome/evidence contract** for the downstream sprint: it states what must exist and how acceptance is evidenced, never how to build it.

### 6.1 Revised activation proof and startup-guard semantics

**Outcome.** The downstream sprint must define and implement a revised durable activation proof for discard-and-retire, and matching revised startup-guard semantics, such that a `--room-cutover-authoritative=true` server proves its activation preconditions **without depending on a migrated-room copy** (ADR 004 §5). Fail-closed behavior is preserved: the true server must still refuse to serve unless its revised preconditions are proven.

**Affected surfaces (named for the downstream sprint; no replacement is designed here).**

- The guard contract in `internal/infrastructure/persistence/room_cutover_guard.go`, whose current preconditions are clean migration state, schema version ≥ 9, and `room_cutover_marker.id = 1` — a marker inserted in the same transaction as the migrated-room copy that will no longer happen.
- The authoritative-mode wiring in `cmd/server/main.go` that invokes the guard before listeners open.
- The guard's marker-absent error text, which currently instructs the operator to ``run `room-cutover up` and `room-cutover verify` before starting in authoritative mode`` — an instruction to execute the copy that discard-and-retire forbids.

**Acceptance evidence.** A recorded definition of the revised activation proof; guard tests demonstrating fail-closed refusal when the revised preconditions are absent and successful startup when present; no revised precondition references a migrated-room copy; operator-facing guard messaging no longer instructs running the copy.

### 6.2 Revised production cutover procedure

**Outcome.** The downstream sprint must produce a revised production cutover procedure that contains **no legacy-state copy step** and retains, unchanged in intent: the `410 Gone` retirement shape for the legacy global REST/WS contracts (ADR 004 §2.4), the mandatory pre-cutover `pg_dump` backup with verified retention, the recorded false/false server/SPA rollback pair, and closed-traffic maintenance-window execution with paired server/SPA deployment (no mixed-mode deployment).

**Acceptance evidence.** A reviewable revised procedure in which no step executes `cmd/room-cutover up` (or any equivalent legacy-state copy) against the live database; each retained control above is present and traceable to its ADR 004 §3 origin.

### 6.3 Revised runbook, readiness, and preflight surfaces

**Outcome.** The downstream sprint must deliver revised operational surfaces replacing the seven non-executable migrate-and-retire documents: the runbook (`documents/07-deployment/room-cutover-runbook.md`), the readiness package, the inputs template, the preflight ledger, the evidence-return template, the discrepancy register, and the go/no-go packet. The revised versions are **new documents**; the originals are preserved as historical records with their non-executable banners intact. Whether the revised preflight resumes Sprint 031 or opens a successor sprint is a decision belonging to that revision and the Product Owner — Sprint 036 does not commit it.

**Acceptance evidence.** The seven revised surfaces exist as new documents consistent with the revised procedure (6.2) and revised activation proof (6.1); the seven originals are preserved without edits and their non-executable banners remain; no readiness row is advanced by the delivery itself.

### 6.4 Disposition of the copy path and marker contract

**Outcome.** The downstream sprint must include an explicit, recorded disposition decision for the now-unsuitable `cmd/room-cutover` copy path and the `room_cutover_marker` contract. The allowed decision space is: for the copy path — freeze-as-historical, repurpose, or schedule removal; for the marker contract — replaced, retired, or reinterpreted. The decision is a **Product Owner call on Architect recommendation**; the downstream sprint may not decide it unilaterally.

**Acceptance evidence.** A recorded disposition decision naming the chosen option for each of the two artifacts, the Architect recommendation it rests on, and the Product Owner sign-off reference.

### 6.5 Preservation checklist — ADR 004 §3 controls

**Outcome.** The downstream sprint's acceptance must attest, item-by-item, that every ADR 004 §3 control is preserved:

1. Mandatory pre-cutover database backup and verified `<BACKUP_LOCATION>` retention (≥ 30 days).
2. The recorded false/false rollback pair (`--room-cutover-authoritative=false` server + `VITE_ROOM_CUTOVER_AUTHORITATIVE=false` SPA).
3. Protected rollback tags and durable rollback archives.
4. Closed-traffic maintenance-window execution and paired server/SPA deployment (no mixed-mode deployment).
5. Fail-closed controls: the true server must refuse to serve unless its activation preconditions are proven (as revised under 6.1).
6. The mandatory Operator evidence → Architect review → Product Owner decision sequence (Sprint 032 governance, including the named `hoanganh-ng` combined-role exception for R14c Gate 2 only).
7. The prohibition on any AI Builder performing production, preflight, credential, or deployment actions.
8. Accepted R14b and R14c Gate 1 work as historical records: the merged code, tests, rehearsal evidence, and documentation are not reverted or rewritten.

**Acceptance evidence.** An item-by-item attestation covering all eight controls in the downstream sprint's acceptance record.

### Traceability

| ADR 004 §6 outcome | Shaped-scope section | Acceptance criterion (Issue #17 comment #5140910084) |
|---|---|---|
| 1 — revised activation proof and startup-guard semantics | §6.1 above | Criterion 2, §6.1 bullet |
| 2 — revised cutover procedure with no copy step | §6.2 above | Criterion 2, §6.2 bullet |
| 3 — revised runbook/readiness/preflight surfaces | §6.3 above | Criterion 2, §6.3 bullet |
| 4 — disposition of copy path and marker contract | §6.4 above | Criterion 2, §6.4 bullet |
| 5 — preservation of every §3 control | §6.5 above | Criterion 2, §6.5 bullet |

## Lifecycle statement

Three distinct verbs govern this work:

1. **Sprint 036 *shapes*** the implementation-correction sprint: the contract above defines its scope and acceptance.
2. **Product Owner acceptance of this sprint's PR *authorizes*** the shaped sprint. Authorization is granted by the Product Owner through this delivery — not by ADR 004, whose §6 statement that the sprint is "NOT authorized by ADR 004" remains literally true.
3. **A later, separate delivery *activates*** the shaped sprint, with its own sprint record, its own base SHA, and its own Builder authorization.

Sprint 036 does NOT resume Sprint 031, does NOT make any Gate 2 document executable, and does NOT commit whether the revised preflight resumes Sprint 031 or opens a successor (that decision belongs to §6.3 and the Product Owner).

## Deliverables (the approved five-file boundary)

1. `documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md` (this record, new)
2. `documents/00-project-management/SPRINTS/active.md` (updated — Sprint 036 sole active)
3. `documents/00-project-management/SPRINTS/README.md` (index row added)
4. `documents/00-project-management/PROJECT_STATE.md` (updated — active-sprint header; correction-sprint phrasing)
5. `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md` (direction-amendment blockquote phrase only; no table row)

## Out of scope

- Application code, tests, schema, migrations, Docker/Compose, frontend, runtime configuration, or dependencies.
- *Implementing* any ADR 004 §6 outcome, or prescribing Builder-level design for any of them.
- Any deployment document (`documents/07-deployment/`), including the runbook, readiness package, inputs template, preflight ledger, evidence-return template, discrepancy register, and go/no-go packet — all remain untouched and non-executable.
- Editing ADR 004 or ADR 003, or any closed sprint record (031, 033, 034, 035).
- Activating the implementation-correction sprint; resuming, closing, completing, or superseding Sprint 031.
- Advancing any B1–B6, PF-01–PF-15, or E1–E10 row; returning evidence; recording a discrepancy; signing any decision; declaring GO.
- Any production-host, database, snapshot, backup, credential, protected-evidence, or infrastructure access; any preflight, production, cutover, rollback, or R14e action.
- Marking the delivery PR ready, approving it, or merging it — those remain Product Owner actions.

## Verification

Documentation-only change set; the required evidence commands (Issue #17 comment #5140914484):

```bash
git diff --check dev...HEAD
git diff --name-only dev...HEAD
git diff --name-only dev...HEAD | wc -l
git diff --name-only dev...HEAD | grep -Ev '^documents/00-project-management/' || true
git status --short
```

Expected: clean diff, exactly five changed files, every changed file under `documents/00-project-management/`, and a clean worktree. No Go, frontend, or Docker verification applies — no code, configuration, or deployment file is touched.

## Record

- Prepared from `dev` at `11c30c4eeccdbc1a2719deb1afed4a9f0273e750` on branch `sprint/r14-discard-retire-implementation-correction-shaping` for Architect review and Product Owner acceptance, delivered as the unmerged draft PR `docs(r14): shape the discard-and-retire implementation-correction sprint`.
- This record shapes the implementation-correction sprint; Product Owner acceptance of this PR authorizes it; it remains NOT active and NOT started until separately activated with its own record, base SHA, and Builder authorization.
- No production system, snapshot, deployment host, container, connection bundle, evidence directory, credential, or identity was accessed in preparing this sprint. No runbook or preflight step was executed. No readiness row left `Not started`, no evidence was recorded, no discrepancy was raised, no decision block was signed, and no GO was declared.
- This record grants no execution authority. Gate 2 remains pending and unauthorized; the unsigned recommendation remains `DEFER / NOT READY`; production remains untouched; the `true` pair remains undeployed.
- Sprint 031 remains paused. R14e remains inactive. Room epic Issue #17 remains open.
