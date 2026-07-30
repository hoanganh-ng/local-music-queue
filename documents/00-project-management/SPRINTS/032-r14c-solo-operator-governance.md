# Sprint 032 — R14c Solo-Operator Governance Amendment

**Status:** In progress — documentation-only governance amendment; no readiness item resolved; no GO declared
**Branch:** `sprint/r14c-solo-operator-governance`
**Base:** `dev` at `c2ca389475174ad165391085a4e98b65d70f3455`
**Parent epic:** Issue #17
**Risk:** Low (documentation only — no code, schema, deployment, or production change)
**Scope:** Project-management and deployment documentation only. No production, snapshot, identity, credential, evidence-storage, deployment-host, container, or infrastructure access of any kind.

## Goal

Amend the R14c Gate 2 governance surfaces so that the project's named sole maintainer, `hoanganh-ng`, may act as both Product Owner-of-record and human Operator **for R14c Gate 2 only**, replacing the mandatory human role separation ("Operator ≠ Product Owner") with a mandatory three-pass procedural sequence, while preserving every fail-closed readiness, evidence, rollback, discrepancy, and production-safety control unchanged. This sprint changes governance wording only. It resolves nothing: every blocker, preflight check, and entry criterion remains `Not started`, the evidence and discrepancy inventories remain empty, the unsigned recommendation remains `DEFER / NOT READY`, and no GO is declared.

## Current behavior

At the approved base (`dev` at `c2ca389475174ad165391085a4e98b65d70f3455`):

- The Gate 2 decision surfaces — readiness package Section 3 and E3, the preflight ledger B1 and E3 rows, the evidence-return B1 row, and the go/no-go packet Sections 4 and 6 — all require an unconditional `Operator ≠ Product Owner` human role separation, with the reopen-traffic GO justified as "a second pair of eyes".
- The project has a single named maintainer, `hoanganh-ng`. The unconditional separation requirement therefore makes B1 and E3 permanently unsatisfiable and blocks the Gate 2 go/no-go review from ever being convened, without adding any additional safety over the existing fail-closed evidence, discrepancy, rollback, and abort controls.
- Sprint 031 — R14c Gate 2 Preflight and Go/No-Go Preparation — is active; its tracking scaffold is integrated and all rows are `Not started` with recommendation `DEFER / NOT READY`.

## Desired behavior

- **Named exception, Gate 2 only.** `hoanganh-ng` is recorded as the approved combined Product Owner-of-record and human Operator for R14c Gate 2 only. Separate-role governance remains the stated normal preference for any other person, gate, or sprint; the exception extends to no one else and to no other scope.
- **Three sequential passes replace the second-pair-of-eyes control:**
  1. **Operator evidence pass** — the Operator resolves blockers and preflight checks and returns redacted evidence through the evidence-return, exactly as already defined.
  2. **Architect review pass** — the Architect reviews the completed ledger, evidence-return, and discrepancy register. Architect outcomes are limited to exactly: `READY FOR PO DECISION`, `DEFER — EVIDENCE INCOMPLETE`, or `NO-GO RECOMMENDED`. The Architect review grants no production authority and does not replace the Product Owner decision.
  3. **Product Owner decision pass** — the Product Owner records GO / NO-GO / DEFER in the readiness package Section 9. **A GO recorded before the Architect review pass has returned `READY FOR PO DECISION` is invalid.**
- **B1 and E3 pass conditions amended** to accept either separate named persons or the named `hoanganh-ng` exception with the three-pass sequence acknowledged; both rows remain `Not started`.
- **Sprint lifecycle:** Sprint 031 is **paused** — not closed, not superseded; its scope, deliverables, and prohibitions remain intact and it resumes after this amendment is integrated. Sprint 032 is the sole active sprint while it is in progress.
- **Everything else is preserved without change:** all B1–B6, PF-01–PF-15, and E1–E10 statuses remain `Not started`; evidence and discrepancy inventories remain empty; the unsigned recommendation remains `DEFER / NOT READY`; Gate 2 remains pending and unauthorized; production remains untouched; R14e remains inactive; the runbook procedures and command order are unchanged; no AI agent (Builder) may execute any Gate 2 operation.

## Deliverables

- `documents/00-project-management/SPRINTS/032-r14c-solo-operator-governance.md` (this record, new)
- `documents/00-project-management/SPRINTS/active.md` (updated — Sprint 032 sole active; Sprint 031 paused)
- `documents/00-project-management/SPRINTS/README.md` (index rows updated)
- `documents/00-project-management/PROJECT_STATE.md` (updated)
- `documents/00-project-management/SPRINTS/031-r14c-gate2-preflight.md` (status set to paused; record note added)
- `documents/07-deployment/room-cutover-gate2-readiness.md` (Section 3 governance, E3 wording, decision-rule sequencing)
- `documents/07-deployment/room-cutover-gate2-preflight-ledger.md` (B1 and E3 pass conditions; governance note)
- `documents/07-deployment/room-cutover-gate2-evidence-return.template.md` (B1 attestation wording; pass-sequence note)
- `documents/07-deployment/room-cutover-gate2-go-no-go-packet.md` (Sections 4–7 governance and sequencing)

## Out of scope

- Application code, tests, schema, migrations, Docker/Compose, frontend, runtime configuration, or dependencies.
- Any runbook command change; the runbook procedures and command order remain unchanged.
- Any production-host, database, snapshot, backup, credential, protected-evidence, or infrastructure access.
- Resolving any readiness row (B1–B6, PF-01–PF-15, E1–E10), returning evidence, or recording a discrepancy.
- Declaring GO, signing any decision block, scheduling the window, or supplying real input values.
- Executing the maintenance window, cutover, rollback, or any R14e action.
- Closing or superseding Sprint 031, or advancing sprint status beyond the changes explicitly required here.

## Verification

Documentation-only change set; the relevant checks are documentation-integrity checks:

```bash
git diff --check                 # no whitespace errors
git diff --name-only dev         # exactly the nine documentation files
git status --short
# no contradictory unconditional separation requirement remains on the Gate 2 decision surfaces
grep -RIn "Operator ≠ Product Owner" documents/07-deployment/
# no readiness status changed from Not started; no GO declared
grep -RInE "\| *(Resolved|In progress|Failed|Blocked) *\|" documents/07-deployment/room-cutover-gate2-preflight-ledger.md
# no identities/DSNs/secrets introduced
grep -RInE "(@gmail|@googlemail|postgres://|postgresql://[^<])" documents/07-deployment/
```

No Go, frontend, or Docker verification applies — no code or configuration file is touched.

## Record

- Prepared from `dev` at `c2ca389475174ad165391085a4e98b65d70f3455` on branch `sprint/r14c-solo-operator-governance` for Architect review and Product Owner decision, delivered as an unmerged draft pull request.
- The only named public handle introduced by this sprint is `hoanganh-ng`, the repository's public maintainer handle. No DSN, credential, email address, hostname, infrastructure path, secret, or other real identity is introduced.
- No production system, snapshot, deployment host, container, connection bundle, evidence directory, credential, or identity was accessed in preparing this sprint. No runbook step was executed. No readiness row left `Not started`, no evidence was recorded, no discrepancy was raised, and no GO was declared.
- This amendment grants no execution authority. Gate 2 remains pending and unauthorized until the three-pass sequence completes and a valid GO is recorded in the readiness package Section 9; production remains untouched.
- R14e remains inactive. Room epic Issue #17 remains open.
