# Sprint 031 — R14c Gate 2 Preflight and Go/No-Go Preparation

**Status:** In progress — awaiting Architect review and Product Owner approval (documentation-only; unmerged PR #28 submitted for review)
**Branch:** `sprint/r14c-gate2-preflight`
**Base:** `dev` at `8ae823e8dc7b337a70bd6d420745595463680666`
**Parent epic:** Issue #17
**Risk:** Low (documentation only — no code, schema, deployment, or production change)
**Scope:** Project-management and deployment documentation only. No production, snapshot, identity, credential, evidence-storage, deployment-host, container, or infrastructure access of any kind.

## Goal

Assemble the preflight-tracking and go/no-go preparation layer that sits on top of the accepted Sprint 030 readiness package, so the Product Owner can convene an evidence-based GO / NO-GO / DEFER review for the R14c production cutover window. This sprint produces the tracking scaffolding only. It resolves nothing: every blocker, preflight check, and entry criterion is recorded as unmet/open pending redacted human-Operator evidence, and no GO is declared.

## Current behavior

At the approved base (`dev` at `8ae823e8dc7b337a70bd6d420745595463680666`):

- R14c Gate 1 is integrated: PR #26 was squash-merged into `dev` as `c8ab4af029d10dda889d1165464e16068a5be573` (2026-07-29); the implementation lifecycle is closed.
- Sprint 030 — R14c Gate 2 Readiness Package — is closed and accepted: PR #27 merged into `dev` as `d27c56ff85a1c655e9c9a1bc38354f13fcfd7ccd` (2026-07-29). The authoritative readiness overlay [`room-cutover-gate2-readiness.md`](../../07-deployment/room-cutover-gate2-readiness.md), the operator-local input template [`room-cutover-gate2-inputs.template.md`](../../07-deployment/room-cutover-gate2-inputs.template.md), and the security-amended runbook [`room-cutover-runbook.md`](../../07-deployment/room-cutover-runbook.md) are authoritative on `dev`.
- Blockers B1–B7, preflight checks 1–15, and entry criteria E1–E10 exist as static prose inside the readiness package, but there is no live tracking ledger of their status, no structured way for a human Operator to return redacted evidence, no register for operational discrepancies encountered while resolving blockers, and no compact Product Owner-facing go/no-go review packet that aggregates the current state into a single decision surface.
- Gate 2 — production execution — remains explicitly pending and unauthorized; no GO record, role assignments, approved operator inputs, or scheduled maintenance window exist. Production remains untouched; R14e remains inactive.

## Desired behavior

- A redacted preflight status ledger [`room-cutover-gate2-preflight-ledger.md`](../../07-deployment/room-cutover-gate2-preflight-ledger.md) tracks the live status of every blocker B1–B6, every preflight check 1–15, and every entry criterion E1–E10, each keyed back to its source in the readiness package and runbook, with an evidence-reference column that only ever holds redacted operator evidence references — never raw values, DSNs, identities, hostnames, or the live Google account. Every row is initialized `Not started` under the ledger's deterministic status model (`Not started` / `In progress` / `Resolved` / `Failed` / `Blocked` / `Not applicable` only where explicitly permitted); nothing moves to `Resolved` without redacted human-Operator evidence accepted by the Product Owner.
- An operator evidence-return template [`room-cutover-gate2-evidence-return.template.md`](../../07-deployment/room-cutover-gate2-evidence-return.template.md) gives the human Operator a placeholder-only structure to return redacted attestations for each preflight check and blocker back to the Product Owner, enforcing the same no-secrets rules as the readiness package (no DSNs, credentials, hostnames, live account address, or raw report contents; redacted references and boolean/attestation outcomes only).
- An operational discrepancy register [`room-cutover-gate2-discrepancy-register.md`](../../07-deployment/room-cutover-gate2-discrepancy-register.md) provides a structured, initially-empty log for any discrepancy, deviation, or anomaly the Operator encounters while resolving blockers and running preflight verification, with severity, affected item, and disposition columns, so nothing is silently resolved and every abort-relevant finding is visible at the go/no-go review.
- A Product Owner go/no-go review packet [`room-cutover-gate2-go-no-go-packet.md`](../../07-deployment/room-cutover-gate2-go-no-go-packet.md) aggregates the ledger, evidence-return, and discrepancy-register state into one compact decision surface: how to read the ledger, the decision rule (GO / NO-GO / DEFER) restated from the readiness package, the separation-of-duties and Builder-prohibition reminders, and an unsigned decision block. It declares no GO and pre-checks nothing.
- Project-management trackers (`active.md`, `README.md`, `PROJECT_STATE.md`) reflect Sprint 031 accurately and continue to state that Gate 2 remains pending and unauthorized.

These four deployment documents are tracking and preparation overlays. They do not modify, replace, or reorder any runbook step or any readiness-package section. On any conflict about *what to execute or in what order*, the runbook prevails; on any conflict about *whether execution is authorized*, the readiness package and the Product Owner decision prevail; this sprint's documents only *track* readiness and never grant it.

## Deliverables

- `documents/07-deployment/room-cutover-gate2-preflight-ledger.md` (new)
- `documents/07-deployment/room-cutover-gate2-evidence-return.template.md` (new)
- `documents/07-deployment/room-cutover-gate2-discrepancy-register.md` (new)
- `documents/07-deployment/room-cutover-gate2-go-no-go-packet.md` (new)
- `documents/00-project-management/SPRINTS/031-r14c-gate2-preflight.md` (this record, new)
- `documents/00-project-management/SPRINTS/active.md` (updated)
- `documents/00-project-management/SPRINTS/README.md` (index row added)
- `documents/00-project-management/PROJECT_STATE.md` (updated)

## Out of scope

- Any production access: SQL, lookups, backups, migrations, deployments, traffic control, true-mode start, rollback actions.
- Any access to production snapshots, the deployment host, containers, the protected connection bundle, the evidence directory, real identities, credentials, DSNs, or the live Google account.
- Executing any runbook step, including "read-only" ones (snapshot plan, isolated `up --dry-run` rehearsal, pairing extraction, backup verification).
- Marking any blocker, preflight check, or entry criterion resolved/met without redacted human-Operator evidence.
- Signing or declaring GO, assigning roles, scheduling the window, supplying real input values, or recording a go/no-go decision — those remain Product Owner (and Operator-evidence) actions taken through the readiness package and this sprint's ledger/packet later.
- Beginning the maintenance-window procedure, the production cutover, any traffic change, true-mode deployment, or rollback.
- Any R14e action: migration 0010, schema version 10, legacy-table deletion, marker edits.
- Modifying any code, schema, Docker/Compose configuration, frontend asset, the runbook, or the Sprint 030 readiness package/input template.
- Merging the PR or advancing sprint status.

## Verification

Documentation-only change set; the relevant checks are documentation-integrity checks:

```bash
git diff --check                 # no whitespace errors
git diff --name-only dev         # docs-only file list
git status --short
# no real identities/DSNs in the new/changed deployment docs
grep -RInE "(@gmail|@googlemail|postgres://|postgresql://[^<])" documents/07-deployment/
# no command supplies a DSN/credential via argv (no --postgres value, no DSN/$DATABASE_URL as a psql/pg_dump argument)
grep -RInE -- '--postgres[ =]"?[<$]|(psql|pg_dump) +"?[<$]' documents/07-deployment/
# no retained full Compose render and no auto-export sourcing of the connection env file
grep -RInE 'compose config +>|set -a' documents/07-deployment/
```

No Go, frontend, or Docker verification applies — no code or configuration file is touched.

## Record

- Prepared from `dev` at `8ae823e8dc7b337a70bd6d420745595463680666` on branch `sprint/r14c-gate2-preflight` as an unmerged PR for Architect review and Product Owner decision.
- Delivered as PR #28, base `dev`, open and non-draft; it must remain unmerged pending Architect review and Product Owner decision. Initial delivery head `bb4a29d564fa47bc11450ecd631d7e1f097e4b31`; the current review head is recorded in PR #28.
- A focused documentation-only corrective pass was applied on PR #28 following Architect review, within the same approved base, branch, and eight-file scope: self-referencing PR-head claims replaced with the initial-delivery-head formulation above; the preflight ledger moved to the deterministic status model (`Not started` / `In progress` / `Resolved` / `Failed` / `Blocked` / `Not applicable` only where explicitly permitted) with per-row owner, pass condition, consequence, and safe checked/updated date; the evidence-return template aligned to that status model with protected-reference identifiers instead of infrastructure paths; the discrepancy register expanded with required fields and the Architect + Product Owner classification rule; the go/no-go packet expanded with current summaries and an unsigned `DEFER / NOT READY` recommendation; and stale lifecycle language corrected in the project-state trackers. All Gate 2 prohibitions are unchanged.
- False-mode status: Not observed by Builder — no deployment access. Repository state says false mode remains authoritative; human Operator confirmation is still required before readiness advances.
- No production system, snapshot, deployment host, container, connection bundle, evidence directory, credential, or identity was accessed in preparing this sprint. No runbook step was executed. No operational item was marked resolved and no GO was declared.
- This record does **not** authorize the production cutover. Blockers B1–B7 remain unresolved; Gate 2 remains pending and unauthorized until a GO is recorded in the readiness package's decision block against redacted human-Operator evidence; production remains untouched.
- R14e remains inactive. Room epic Issue #17 remains open.
