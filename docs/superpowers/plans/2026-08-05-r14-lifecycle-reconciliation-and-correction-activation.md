# R14 Lifecycle Reconciliation and Correction Activation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the stale Sprint 036 lifecycle and activate the approved discard-and-retire implementation correction as the sole authorized sprint.

**Architecture:** Use one documentation-only lifecycle bridge, Sprint 037, to reconcile PR #34 and the merged success design, then establish Sprint 038 as the active implementation contract. Change exactly seven project-management files; preserve every Gate 2 prohibition and make no runtime, schema, deployment, preflight, or production change.

**Tech Stack:** Markdown, Git, GitHub Issue #17 lifecycle ledger, GitHub draft pull requests.

## Global Constraints

- Start only after PR #35 is Product Owner-accepted and squash-merged into `dev`.
- The merged `docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md` is Sprint 038 architecture authority.
- Resolve the execution base from current `origin/dev`; record the resulting full SHA in every new sprint record.
- Issue #17 must contain approved Sprint 037 intent, exact file boundary, acceptance criteria, and Builder authorization before work starts.
- Change exactly these seven files:
  - create `documents/00-project-management/SPRINTS/037-r14-discard-retire-correction-activation.md`;
  - create `documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md`;
  - modify `documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md`;
  - modify `documents/00-project-management/SPRINTS/active.md`;
  - modify `documents/00-project-management/SPRINTS/README.md`;
  - modify `documents/00-project-management/PROJECT_STATE.md`;
  - modify `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md`.
- Do not modify the approved design, this plan, ADRs, deployment documents, Go code, tests, migrations, Docker/Compose, frontend code, configuration, dependencies, or infrastructure.
- Sprint 031 remains paused and not yet superseded. Its migrate-and-retire documents remain non-executable.
- All B1–B7, PF-01–PF-15, and E1–E10 state remains unchanged; evidence and discrepancy inventories remain empty; decisions remain unsigned; recommendation remains `DEFER / NOT READY`.
- Gate 2 remains pending and unauthorized; production and legacy tables remain untouched; the true/true pair remains undeployed; R14e remains inactive.
- Sprint 038 grants implementation and documentation authority only. It grants no preflight, credential, deployment, traffic, cutover, rollback, cleanup, or Issue #17 closure authority.
- Use branch `sprint/r14-discard-retire-correction-activation`; deliver an unmerged draft PR against `dev`.
- The Builder does not mark ready, approve, merge, or advance lifecycle/readiness state.

## File Responsibility Map

| File | Responsibility |
|---|---|
| `SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md` | Record closure and PR #34 integration while preserving the accepted shaped contract. |
| `SPRINTS/037-r14-discard-retire-correction-activation.md` | Record the completed lifecycle bridge and exact prohibitions. |
| `SPRINTS/038-r14-discard-retire-implementation-correction.md` | Define the sole active implementation-correction contract. |
| `SPRINTS/active.md` | Point to Sprint 038 as the only authorized assignment. |
| `SPRINTS/README.md` | Reconcile index rows 036–038. |
| `PROJECT_STATE.md` | Record current lifecycle, architecture decisions, and unchanged operational state. |
| `ROOM_EPIC_SPRINT_SEQUENCE.md` | Update current R14 direction and migration sequence without rewriting accepted history. |

## Execution Preconditions — Architect and Product Owner

Before Task 1:

1. PR #35 is merged into `dev`.
2. The Architect posts Sprint 037 intent and the seven-file boundary to Issue #17.
3. The Product Owner approves the boundary and acceptance criteria.
4. The Product Owner authorizes the Builder.
5. The exact Issue #17 comment URLs are available for the sprint records.

---

### Task 1: Establish the Authoritative Base

**Files:**
- Read: `AGENTS.md`
- Read: `documents/00-project-management/PROJECT_STATE.md`
- Read: `documents/00-project-management/SPRINTS/active.md`
- Read: `docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md`
- Change: none

**Interfaces:**
- Consumes: merged PR #35, Issue #17 authorization records, current `origin/dev`.
- Produces: isolated worktree, full `BASE_SHA`, full `SPEC_SHA`, sprint branch.

- [ ] **Step 1: Create an isolated worktree**

Invoke `superpowers:using-git-worktrees`. Do not work in a dirty checkout.

- [ ] **Step 2: Refresh `dev` and capture the base**

Run:

```bash
git fetch origin
git switch dev
git pull --ff-only origin dev
BASE_SHA="$(git rev-parse HEAD)"
printf 'BASE_SHA=%s\n' "$BASE_SHA"
test "${#BASE_SHA}" -eq 40
git status --short
```

Expected: clean worktree and a 40-character `BASE_SHA`.

- [ ] **Step 3: Verify the approved design is merged**

Run:

```bash
test -f docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md
SPEC_SHA="$(git log -1 --format=%H -- docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md)"
printf 'SPEC_SHA=%s\n' "$SPEC_SHA"
test "${#SPEC_SHA}" -eq 40
git show "$SPEC_SHA:docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md" >/dev/null
```

Expected: all commands succeed.

- [ ] **Step 4: Confirm the expected stale lifecycle still exists**

Run:

```bash
grep -F 'Sprint 036 — R14 Discard-and-Retire Implementation-Correction Shaping' documents/00-project-management/SPRINTS/active.md
grep -F 'sole active sprint' documents/00-project-management/SPRINTS/active.md
grep -F 'Sprint 031' documents/00-project-management/SPRINTS/active.md
grep -F 'paused' documents/00-project-management/SPRINTS/active.md
```

Expected: all checks match. If a later transition is already on `dev`, stop and request a plan rebase.

- [ ] **Step 5: Create the sprint branch**

Run:

```bash
git switch -c sprint/r14-discard-retire-correction-activation
```

Expected: branch starts at `BASE_SHA`.

---

### Task 2: Close Sprint 036 and Create Sprint 037/038 Records

**Files:**
- Modify: `documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md`
- Create: `documents/00-project-management/SPRINTS/037-r14-discard-retire-correction-activation.md`
- Create: `documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md`

**Interfaces:**
- Consumes: `BASE_SHA`, `SPEC_SHA`, PR #34 lifecycle facts, approved design, exact Issue #17 references.
- Produces: closed Sprint 036, completed Sprint 037, active Sprint 038.

- [ ] **Step 1: Close Sprint 036**

Set its status to closed and accepted and record:

- PR #34;
- accepted head `65d17f7955d0dd9e782c6cb4298467a26613b892`;
- squash merge `723023fd296678bac8aec0ace784ff2e94bb119f`;
- merge date `2026-07-31`;
- acceptance authorized but did not activate the correction sprint.

Append this exact section:

```markdown
## Closure record

- Sprint 036 is closed and accepted. PR #34, accepted head `65d17f7955d0dd9e782c6cb4298467a26613b892`, was squash-merged into `dev` as `723023fd296678bac8aec0ace784ff2e94bb119f` on 2026-07-31.
- The accepted delivery shaped and authorized the ADR 004 Section 6 implementation-correction sprint; it did not activate that sprint.
- The Product Owner-approved architecture boundary is `docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md`.
- Sprint 037 records lifecycle reconciliation. Sprint 038 is separately activated from the current `dev` base with its own record and Builder authorization.
- Sprint 031 remains paused; Gate 2 documents remain non-executable; no readiness, production, rollback, or R14e state changed through Sprint 036 closure.
```

Preserve the accepted §6.1–§6.5 contract. Rewrite only stale lifecycle wording that presents Sprint 036 as currently active or awaiting acceptance.

- [ ] **Step 2: Create Sprint 037**

Create `037-r14-discard-retire-correction-activation.md` with:

```markdown
# Sprint 037 — R14 Discard-and-Retire Correction Activation

**Status:** Completed — documentation-only lifecycle transition; closes Sprint 036 and activates Sprint 038; no implementation, preflight, production, rollback, or R14e action
**Branch:** `sprint/r14-discard-retire-correction-activation`
**Parent epic:** Issue #17
**Risk:** Low — project-management documentation only
**Scope:** Exactly seven approved project-management files

## Authorization record
## Goal
## Current behavior
## Desired behavior
## Deliverables
## Acceptance criteria
## Out of scope
## Verification
## Record
```

Insert the base line by running:

```bash
printf '**Base:** `dev` at `%s`\n' "$BASE_SHA"
```

Place that output after the branch line. The committed file must contain the actual 40-character SHA and must not contain the text `BASE_SHA`.

Required assertions:

- PR #35 and the approved design are merged authority.
- Sprint 036 is closed with the exact PR #34 head and merge SHAs.
- Sprint 037 completes in this delivery and grants no continuing authority.
- Sprint 038 becomes the sole active sprint.
- Sprint 038 has implementation/documentation authority only.
- Sprint 031 remains paused; its seven old operational documents remain non-executable.
- Sprint 031 is closed as superseded only after correction implementation is accepted and integrated, not now.
- Readiness, evidence, discrepancy, signatures, GO, deployment, cutover, rollback, and cleanup state remain unchanged.
- R10f+, R11b+, R12, and R13 remain outside core Issue #17 closure.
- Deliverables are exactly the seven authorized files.
- Authorization section contains the exact Issue #17 references from the preconditions.

- [ ] **Step 3: Create Sprint 038**

Create `038-r14-discard-retire-implementation-correction.md` with:

```markdown
# Sprint 038 — R14 Discard-and-Retire Implementation Correction

**Status:** Active — sole authorized sprint; implementation and operational-document correction only; no preflight or production authority
**Branch:** `sprint/r14-discard-retire-implementation-correction`
**Parent epic:** Issue #17
**Risk:** High — schema, startup safety, operator tooling, packaging, and operational-contract changes; production execution remains prohibited
**Architecture authority:** ADR 004 plus `docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md`

## Authorization record
## Intent
## Expected outcome
## Locked architecture decisions
## Included scope
## Explicit exclusions
## Implementation boundaries
## Acceptance criteria
## Required evidence
## Builder delivery contract
## Blockers and stop conditions
## References
```

Insert the base line after the branch line using:

```bash
printf '**Base:** `dev` at `%s`\n' "$BASE_SHA"
```

The committed file must contain the actual 40-character SHA and no symbolic base token.

Repeat the complete locked contract:

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

Authorization section must contain exact Sprint 038 activation and Builder authorization references from Issue #17.

- [ ] **Step 4: Validate sprint records**

Run:

```bash
grep -F 'Closed and accepted' documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md
grep -F '65d17f7955d0dd9e782c6cb4298467a26613b892' documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md
grep -F '723023fd296678bac8aec0ace784ff2e94bb119f' documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md
grep -F "$BASE_SHA" documents/00-project-management/SPRINTS/037-r14-discard-retire-correction-activation.md
grep -F "$BASE_SHA" documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md
grep -F 'room_authoritative_activation' documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md
grep -F 'migration `0011`' documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md
if grep -nF 'BASE_SHA' documents/00-project-management/SPRINTS/037-r14-discard-retire-correction-activation.md documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md; then exit 1; fi
```

Expected: required values match; symbolic base check produces no output.

- [ ] **Step 5: Commit sprint records**

Run:

```bash
git add \
  documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md \
  documents/00-project-management/SPRINTS/037-r14-discard-retire-correction-activation.md \
  documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md
git diff --cached --check
git commit -m 'docs(r14): record correction activation lifecycle'
```

Expected: one commit with exactly three sprint-record files.

---

### Task 3: Reconcile Trackers and Canonical Sequence

**Files:**
- Modify: `documents/00-project-management/SPRINTS/active.md`
- Modify: `documents/00-project-management/SPRINTS/README.md`
- Modify: `documents/00-project-management/PROJECT_STATE.md`
- Modify: `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md`

**Interfaces:**
- Consumes: Sprint 036 closure, Sprint 037 completion, Sprint 038 activation, approved design.
- Produces: one consistent current lifecycle with Sprint 038 as sole active sprint.

- [ ] **Step 1: Update `active.md`**

State at the top:

- Sprint 038 is the sole active sprint;
- branch is `sprint/r14-discard-retire-implementation-correction`;
- base is the actual `BASE_SHA`;
- authority is implementation and revised operational documents only;
- no preflight or production authority.

Retain concise summaries that Sprint 037 is completed, Sprint 036 is closed via PR #34, Sprint 031 remains paused, old operational documents remain non-executable, Gate 2 remains unauthorized, production untouched, and R14e inactive.

Remove current-language claims that Sprint 036 is active, awaiting review, or still shaping the correction.

- [ ] **Step 2: Update sprint index**

In `SPRINTS/README.md`:

- mark 036 closed and accepted with PR #34 and merge `723023fd`;
- add 037 as completed lifecycle transition;
- add 038 as sole active implementation correction with no preflight/production authority;
- preserve 031 as paused;
- update lifecycle prose so new Gate 2 work requires Sprint 038 acceptance and a later successor preflight.

Do not rewrite unrelated rows.

- [ ] **Step 3: Update `PROJECT_STATE.md`**

Set the actual execution date and record:

- Sprint 038 as current active sprint from actual `BASE_SHA`;
- Sprint 036 closure and Sprint 037 transition;
- dedicated activation proof;
- migration `0010` for activation and `0011` for R14e cleanup;
- operator-image/backend-image separation;
- Sprint 031 remains paused until correction integration, then a later transition closes it as superseded;
- new operational documents are Sprint 038 deliverables but remain non-executable until successor preflight;
- readiness and production state remain unchanged.

Remove current claims that the correction is merely being shaped or inactive.

- [ ] **Step 4: Update room epic sequence**

Update the direction amendment and current R14 planning so they state:

- Sprint 036 closed; approved design merged; Sprint 038 active;
- Sprint 031 remains paused now and is superseded only after accepted correction integration;
- no Gate 2 work is authorized;
- migration `0010` is activation proof;
- R14e cleanup is migration `0011`;
- activation proof remains while startup uses it;
- accepted ADR 003/R14b/R14c descriptions are historical, not executable current direction;
- R10f+, R11b+, R12, and R13 remain outside core Issue #17 closure.

Do not mark Sprint 031 superseded or R14e active in this delivery.

- [ ] **Step 5: Validate tracker consistency**

Run:

```bash
grep -F 'Sprint 038 — R14 Discard-and-Retire Implementation Correction' documents/00-project-management/SPRINTS/active.md
grep -F "$BASE_SHA" documents/00-project-management/SPRINTS/active.md
grep -F '| 036' documents/00-project-management/SPRINTS/README.md
grep -F '| 037' documents/00-project-management/SPRINTS/README.md
grep -F '| 038' documents/00-project-management/SPRINTS/README.md
grep -F 'room_authoritative_activation' documents/00-project-management/PROJECT_STATE.md
grep -F 'migration `0010`' documents/00-project-management/PROJECT_STATE.md
grep -F 'migration `0011`' documents/00-project-management/PROJECT_STATE.md
grep -F 'Sprint 038' documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md
grep -F 'Sprint 031' documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md
```

Expected: every command matches.

- [ ] **Step 6: Reject stale authority wording**

Run:

```bash
FILES='documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md documents/00-project-management/SPRINTS/037-r14-discard-retire-correction-activation.md documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md documents/00-project-management/SPRINTS/active.md documents/00-project-management/SPRINTS/README.md documents/00-project-management/PROJECT_STATE.md documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md'
if grep -nE 'Sprint 036.*(sole active|awaiting Architect review|pending acceptance)|implementation-correction sprint.*(NOT active|inactive and unauthorized|being shaped by Sprint 036)' $FILES; then
  echo 'stale lifecycle wording found' >&2
  exit 1
fi
```

Expected: no output. Historical base-state prose must be explicitly labeled historical if it would otherwise match.

- [ ] **Step 7: Commit tracker changes**

Run:

```bash
git add \
  documents/00-project-management/SPRINTS/active.md \
  documents/00-project-management/SPRINTS/README.md \
  documents/00-project-management/PROJECT_STATE.md \
  documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md
git diff --cached --check
git commit -m 'docs(r14): activate implementation-correction sprint'
```

Expected: second commit with exactly four tracker files.

---

### Task 4: Verify and Deliver the Lifecycle PR

**Files:**
- Verify: all seven authorized files
- Change: none unless correcting an in-scope finding

**Interfaces:**
- Consumes: two focused documentation commits.
- Produces: exact evidence, draft PR, Issue #17 Builder delivery record.

- [ ] **Step 1: Enforce exact changed-file set**

Run:

```bash
cat > /tmp/r14-expected-files.txt <<'EOF'
documents/00-project-management/PROJECT_STATE.md
documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md
documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md
documents/00-project-management/SPRINTS/037-r14-discard-retire-correction-activation.md
documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md
documents/00-project-management/SPRINTS/README.md
documents/00-project-management/SPRINTS/active.md
EOF
git diff --name-only "$BASE_SHA"...HEAD | sort > /tmp/r14-actual-files.txt
diff -u /tmp/r14-expected-files.txt /tmp/r14-actual-files.txt
git diff --name-only "$BASE_SHA"...HEAD | wc -l
git diff --name-only "$BASE_SHA"...HEAD | grep -Ev '^documents/00-project-management/' || true
```

Expected: no diff, count `7`, no out-of-directory paths.

- [ ] **Step 2: Verify unchanged operational authority**

Run:

```bash
grep -F 'Sprint 031' documents/00-project-management/SPRINTS/active.md
grep -F 'paused' documents/00-project-management/SPRINTS/active.md
grep -F 'DEFER / NOT READY' documents/00-project-management/SPRINTS/active.md
grep -F 'Gate 2 remains pending and unauthorized' documents/00-project-management/PROJECT_STATE.md
grep -F 'production remains untouched' documents/00-project-management/PROJECT_STATE.md
grep -F 'R14e remains inactive' documents/00-project-management/PROJECT_STATE.md
grep -F 'no preflight or production authority' documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md
```

Expected: every command matches.

- [ ] **Step 3: Run documentation checks**

Run:

```bash
git diff --check "$BASE_SHA"...HEAD
git rev-list --count "$BASE_SHA"..HEAD
git status --short
```

Expected: no whitespace errors, commit count `2`, clean worktree. No Go/frontend/Docker command applies.

- [ ] **Step 4: Run GitNexus scope detection**

Invoke:

```text
detect_changes({scope: "compare", base_ref: "dev"})
```

Expected: documentation-only changes and no runtime execution-flow impact. Record exact output.

- [ ] **Step 5: Push and open draft PR**

Run:

```bash
git push -u origin sprint/r14-discard-retire-correction-activation
```

Open a draft PR against `dev` titled:

```text
docs(r14): activate discard-and-retire implementation correction
```

PR body must include base/head SHAs, seven-file list, Sprint 036 closure, Sprint 037 completion, Sprint 038 boundary, no-preflight/no-production statement, command output, GitNexus result, and confirmation the Builder did not mark ready, approve, or merge.

- [ ] **Step 6: Record delivery on Issue #17**

Post draft PR URL, branch/base/head, exact files, exact verification, and confirmation that no runtime, schema, deployment, preflight, production, rollback, or R14e action occurred. State Sprint 031 remains paused and Gate 2 remains `DEFER / NOT READY`.

- [ ] **Step 7: Stop at review gate**

Do not mark ready, approve, merge, or create the Sprint 038 implementation branch. Await Architect review and Product Owner decision.

---

## Self-Review Checklist

- [ ] Sprint 036 closure has exact PR #34 head and merge SHAs.
- [ ] Sprint 037 is completed and grants no continuing authority.
- [ ] Sprint 038 is sole active sprint with actual base SHA.
- [ ] Sprint 038 repeats every locked design boundary required for Builder execution.
- [ ] Sprint 031 remains paused and is not prematurely superseded.
- [ ] Operational/readiness state remains unchanged.
- [ ] Current planning consistently uses `0010` activation and `0011` cleanup.
- [ ] `room_authoritative_activation` retention is explicit.
- [ ] Changed set is exactly seven project-management files.
- [ ] No symbolic base token or ambiguous authority wording remains in committed sprint records.

## Subsequent Plan Boundary

After this lifecycle PR is Architect-accepted, Product Owner-approved, and squash-merged, create a separate Sprint 038 code-level implementation plan from its actual merged `dev` base. That plan covers migration `0010`, shared activation logic, `room-activation`, startup verification, image packaging, revised operational documents, and full PostgreSQL/Docker evidence. Do not write or execute that code-level plan against the pre-transition base.