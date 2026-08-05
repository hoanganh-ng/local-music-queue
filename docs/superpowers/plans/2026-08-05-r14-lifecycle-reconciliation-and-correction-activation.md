# R14 Lifecycle Reconciliation and Correction Activation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reconcile Sprint 036 with its accepted merge, record the approved discard-and-retire success design as repository authority, and activate the implementation-correction sprint as the sole authorized assignment.

**Architecture:** Deliver one documentation-only lifecycle bridge, Sprint 037, that closes the stale Sprint 036 lifecycle and creates Sprint 038 as the active implementation-correction sprint. The delivery changes exactly seven project-management files, preserves every Gate 2 prohibition and readiness state, and performs no runtime, schema, deployment, preflight, or production work.

**Tech Stack:** Markdown project-management records, Git, GitHub Issue #17 lifecycle ledger, GitHub draft pull request workflow.

## Global Constraints

- Execute this plan only after PR #35, `docs(r14): record discard-and-retire success design`, is Product Owner-accepted and squash-merged into `dev`.
- The merged file `docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md` is the architecture boundary for Sprint 038.
- Resolve the execution base from the then-current `origin/dev`; never reuse the design baseline `723023fd296678bac8aec0ace784ff2e94bb119f` unless it is still the actual current head.
- Issue #17 must contain durable Product Owner-approved records for Sprint 037 intent, the exact seven-file boundary, acceptance criteria, and Builder authorization before implementation begins.
- Change exactly these seven files and no others:
  - create `documents/00-project-management/SPRINTS/037-r14-discard-retire-correction-activation.md`;
  - create `documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md`;
  - modify `documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md`;
  - modify `documents/00-project-management/SPRINTS/active.md`;
  - modify `documents/00-project-management/SPRINTS/README.md`;
  - modify `documents/00-project-management/PROJECT_STATE.md`;
  - modify `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md`.
- Do not modify the approved design specification or this implementation plan during Sprint 037 execution.
- Do not touch ADRs, deployment documents, Go code, tests, migrations, Docker/Compose, frontend code, configuration, dependencies, production data, credentials, snapshots, artifacts, or infrastructure.
- Sprint 031 remains paused, not completed, not resumed, and not yet superseded. Its historical migrate-and-retire documents remain non-executable.
- Every B1–B7, PF-01–PF-15, and E1–E10 readiness item remains unresolved or `Not started`; evidence and discrepancy inventories remain empty; decision fields remain unsigned; the recommendation remains `DEFER / NOT READY`.
- Gate 2 remains pending and unauthorized; the true/true pair remains undeployed; production and legacy global tables remain untouched; R14e remains inactive.
- Sprint 038 authorizes repository implementation and documentation only. It grants no preflight, production, credential, deployment, cutover, rollback, or cleanup authority.
- Use the short-lived branch `sprint/r14-discard-retire-correction-activation` and open an unmerged draft pull request against `dev`.
- The Builder may not mark the pull request ready, approve it, merge it, sign a Gate 2 decision, or advance any readiness state.
- Squash merge is the intended integration method after Architect acceptance and explicit Product Owner merge authorization.

## File Responsibility Map

| File | Responsibility in this delivery |
|---|---|
| `SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md` | Close Sprint 036 as accepted and record PR #34 integration without rewriting the shaped contract. |
| `SPRINTS/037-r14-discard-retire-correction-activation.md` | Record the completed documentation-only lifecycle bridge, its authority, exact scope, evidence, and prohibitions. |
| `SPRINTS/038-r14-discard-retire-implementation-correction.md` | Define and activate the sole implementation-correction sprint from the fresh `dev` base. |
| `SPRINTS/active.md` | Identify Sprint 038 as the only authorized sprint and retain the paused/historical lifecycle context. |
| `SPRINTS/README.md` | Reconcile index rows 036–038 and preserve lifecycle semantics. |
| `PROJECT_STATE.md` | Establish the current active sprint, locked design decisions, operational state, and exclusions. |
| `ROOM_EPIC_SPRINT_SEQUENCE.md` | Update the current R14 direction and sequence without rewriting historical accepted implementation records. |

## Execution Preconditions — Architect and Product Owner

Before the Builder starts Task 1:

1. PR #35 is accepted and squash-merged into `dev`.
2. The Architect posts the proposed Sprint 037 intent and exact seven-file boundary to Issue #17.
3. The Product Owner explicitly approves the boundary and acceptance criteria.
4. The Product Owner explicitly authorizes the Builder to execute Sprint 037.
5. The four durable Issue #17 comment URLs are available for the sprint records; the Builder copies those exact references and never writes placeholder references.

---

### Task 1: Establish the Authoritative Execution Base

**Files:**
- Read: `AGENTS.md`
- Read: `documents/00-project-management/PROJECT_STATE.md`
- Read: `documents/00-project-management/SPRINTS/active.md`
- Read: `docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md`
- No file changes in this task.

**Interfaces:**
- Consumes: merged PR #35, the four Issue #17 authorization records, and current `origin/dev`.
- Produces: clean isolated worktree, exact `BASE_SHA`, exact design commit SHA, and branch `sprint/r14-discard-retire-correction-activation`.

- [ ] **Step 1: Create an isolated worktree**

Invoke `superpowers:using-git-worktrees`, then create the worktree from current `origin/dev`. Do not edit in an existing dirty checkout.

- [ ] **Step 2: Refresh and verify `dev`**

Run:

```bash
git fetch origin
git switch dev
git pull --ff-only origin dev
BASE_SHA="$(git rev-parse HEAD)"
printf 'BASE_SHA=%s\n' "$BASE_SHA"
git status --short
```

Expected:

- `git pull` is a fast-forward or reports already up to date;
- `BASE_SHA` is a full 40-character commit SHA;
- `git status --short` produces no output.

- [ ] **Step 3: Verify the approved specification is merged**

Run:

```bash
test -f docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md
SPEC_SHA="$(git log -1 --format=%H -- docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md)"
test -n "$SPEC_SHA"
printf 'SPEC_SHA=%s\n' "$SPEC_SHA"
git show "$SPEC_SHA:docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md" >/dev/null
```

Expected: every command exits `0`, and `SPEC_SHA` is the merged commit containing the approved design.

- [ ] **Step 4: Confirm repository lifecycle is still the expected stale state**

Run:

```bash
grep -F 'Sprint 036 — R14 Discard-and-Retire Implementation-Correction Shaping' documents/00-project-management/SPRINTS/active.md
grep -F 'sole active sprint' documents/00-project-management/SPRINTS/active.md
grep -F 'Sprint 031' documents/00-project-management/SPRINTS/active.md
grep -F 'paused' documents/00-project-management/SPRINTS/active.md
```

Expected: all four checks match. If current `dev` already contains a later lifecycle transition, stop and return to the Architect for a plan rebase; do not force this plan onto changed authority.

- [ ] **Step 5: Create the sprint branch**

Run:

```bash
git switch -c sprint/r14-discard-retire-correction-activation
```

Expected: branch creation succeeds from exactly `BASE_SHA`.

- [ ] **Step 6: Record the baseline in the Builder evidence log**

Record:

```text
Branch: sprint/r14-discard-retire-correction-activation
Base: the full BASE_SHA printed in Step 2
Approved design commit: the full SPEC_SHA printed in Step 3
Scope: exactly seven project-management files
Production/preflight authority: none
```

Do not commit yet.

---

### Task 2: Close Sprint 036 and Create the Transition and Active Sprint Records

**Files:**
- Modify: `documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md`
- Create: `documents/00-project-management/SPRINTS/037-r14-discard-retire-correction-activation.md`
- Create: `documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md`

**Interfaces:**
- Consumes: `BASE_SHA`, `SPEC_SHA`, PR #34 lifecycle facts, approved design decisions, and exact Issue #17 authorization references.
- Produces: one closed shaping record, one completed lifecycle-transition record, and one active implementation-correction contract.

- [ ] **Step 1: Close Sprint 036 without rewriting its shaped contract**

Change the status line to state exactly that Sprint 036 is closed and accepted, including:

- PR #34;
- accepted head `65d17f7955d0dd9e782c6cb4298467a26613b892`;
- squash merge `723023fd296678bac8aec0ace784ff2e94bb119f`;
- merge date `2026-07-31`;
- the fact that acceptance authorized but did not activate the correction sprint.

Append a `## Closure record` section containing these assertions:

```markdown
## Closure record

- Sprint 036 is closed and accepted. PR #34, accepted head `65d17f7955d0dd9e782c6cb4298467a26613b892`, was squash-merged into `dev` as `723023fd296678bac8aec0ace784ff2e94bb119f` on 2026-07-31.
- The accepted delivery shaped and authorized the implementation-correction sprint required by ADR 004 Section 6; it did not activate that sprint.
- The Product Owner-approved success design is recorded at `docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md` and is the architecture boundary used by Sprint 038.
- Sprint 037 records the lifecycle reconciliation. Sprint 038 is separately activated from the current `dev` base with its own record and Builder authorization.
- Sprint 031 remains paused; all Gate 2 documents remain non-executable; no readiness state, production state, rollback state, or R14e state changed through Sprint 036 closure.
```

Do not alter the existing §6.1–§6.5 shaped outcome/evidence contract except where stale lifecycle phrases must change from pending acceptance to closed/accepted history.

- [ ] **Step 2: Create Sprint 037 as a completed lifecycle bridge**

Create `037-r14-discard-retire-correction-activation.md` with these exact headings:

```markdown
# Sprint 037 — R14 Discard-and-Retire Correction Activation

**Status:** Completed — documentation-only lifecycle transition; closes Sprint 036 and activates Sprint 038; no implementation, preflight, production, rollback, or R14e action
**Branch:** `sprint/r14-discard-retire-correction-activation`
**Base:** `dev` at the full BASE_SHA resolved during execution
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

Replace the prose phrase `the full BASE_SHA resolved during execution` with the actual 40-character SHA before committing.

The body must state all of the following, without weakening them:

- PR #35 and the approved design are merged authority on `dev`.
- Sprint 036 is closed and accepted with the exact PR #34 head and merge SHAs.
- Sprint 037 is completed within this same delivery and grants no continuing work authority.
- Sprint 038 becomes the sole active sprint after integration.
- Sprint 038 is implementation-and-documentation only; it grants no Gate 2 preflight or production authority.
- Sprint 031 remains paused and its seven migrate-and-retire documents remain non-executable.
- Sprint 031 is not closed as superseded in this delivery; that happens only after the correction implementation is accepted and integrated.
- Every readiness row remains unchanged; no evidence, discrepancy, signature, GO, deployment, cutover, rollback, or cleanup action occurs.
- R10f+, R11b+, R12, and R13 remain outside the core Issue #17 closure boundary.
- The exact seven deliverables match the Global Constraints list and no other file is authorized.
- The `Authorization record` contains the exact four Issue #17 comment references created before Task 1; do not write generic or missing references.

- [ ] **Step 3: Create Sprint 038 as the sole active implementation-correction contract**

Create `038-r14-discard-retire-implementation-correction.md` with these exact metadata semantics:

```markdown
# Sprint 038 — R14 Discard-and-Retire Implementation Correction

**Status:** Active — sole authorized sprint; implementation and operational-document correction only; no preflight or production authority
**Branch:** `sprint/r14-discard-retire-implementation-correction`
**Base:** `dev` at the full BASE_SHA resolved during execution
**Parent epic:** Issue #17
**Risk:** High — schema, startup safety, operator tooling, packaging, and operational-contract changes; production execution remains prohibited
**Architecture authority:** ADR 004 plus `docs/superpowers/specs/2026-08-05-r14-discard-retire-success-design.md`
```

Replace the prose phrase `the full BASE_SHA resolved during execution` with the actual 40-character SHA before committing.

Use these headings:

```markdown
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

The record must repeat, rather than merely point at, the following locked contract:

1. **Activation proof and migration numbering**
   - migration `0010` creates `room_authoritative_activation`;
   - the proof is dedicated to discard-and-retire and never reuses or synthesizes `room_cutover_marker`;
   - R14e cleanup moves to migration `0011`;
   - `room_authoritative_activation` is preserved while startup depends on it.

2. **Activation record fields and invariants**
   - single row `id = 1`;
   - operator-generated UUID `activation_id`;
   - fixed mode `discard-and-retire`;
   - contract version `1`;
   - preparation schema version `10`;
   - immutable preparation timestamp;
   - non-empty build SHA;
   - no target room, host, copied-state hash, legacy offset, or copied-row concept.

3. **Shared component and command**
   - one shared PostgreSQL-backed preparation/verification component;
   - `room-activation prepare` and `room-activation verify` only;
   - connection data from `DATABASE_URL` or `MIGRATE_DATABASE_URL`, never a DSN argv flag;
   - exact-repeat idempotency retains the original timestamp;
   - conflicting immutable identity fails;
   - no force, reset, overwrite, delete, bypass, or repair operation.

4. **Startup guard**
   - true mode requires clean migration state, schema at least `10`, valid activation proof, and no historical copy marker;
   - verification occurs before listeners and performs no writes;
   - false mode does not require activation proof and remains rollback-compatible at schema `10` and later compatible schemas.

5. **Packaging**
   - backend image contains `server` only and excludes `room-cutover` and `room-activation`;
   - operator image contains `migrate-schema` and `room-activation`;
   - operator-image default remains `migrate-schema up`;
   - historical `cmd/room-cutover` source/tests remain in the repository but are not packaged or referenced by executable current runbooks.

6. **Operational documents**
   - create the seven new `room-discard-cutover-*` documents named in the approved design;
   - preserve the seven old `room-cutover-*` documents unchanged and non-executable;
   - new procedure includes backup, migration `0010`, activation prepare/verify, paired true/true deployment, smoke checks, false/false rollback, and legacy-table preservation;
   - no copy command or equivalent legacy-to-room state migration appears.

7. **Verification**
   - real PostgreSQL migration, component, conflict, startup, and non-destructive tests;
   - CLI exit-code and secret-redaction tests;
   - backend/operator artifact inspection;
   - Compose pairing verification;
   - `go test ./...`, `go vet ./...`, targeted race tests, Docker builds, `docker compose config`, Git diff checks;
   - PostgreSQL-backed suites may not be accepted when skipped;
   - GitNexus impact analysis before symbol edits and `detect_changes()` before commit.

8. **Explicit exclusions**
   - no production preflight, credentials, deployment, traffic change, cutover, rollback execution, readiness advancement, GO, R14e cleanup, product feature expansion, or Issue #17 closure;
   - Sprint 031 remains paused throughout Sprint 038 implementation;
   - R10f+, R11b+, R12, and R13 remain deferred and outside this sprint.

9. **Builder delivery**
   - short-lived branch from the recorded base;
   - unmerged draft PR with exact changed files and command output;
   - Builder does not mark ready, approve, merge, activate later work, or perform operational actions.

The `Authorization record` must contain the exact Sprint 038 activation and Builder authorization references from Issue #17. If those references do not yet exist, the record is not commit-ready.

- [ ] **Step 4: Validate the three sprint records**

Run:

```bash
grep -F 'Closed and accepted' documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md
grep -F '65d17f7955d0dd9e782c6cb4298467a26613b892' documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md
grep -F '723023fd296678bac8aec0ace784ff2e94bb119f' documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md
grep -F 'Completed — documentation-only lifecycle transition' documents/00-project-management/SPRINTS/037-r14-discard-retire-correction-activation.md
grep -F "$BASE_SHA" documents/00-project-management/SPRINTS/037-r14-discard-retire-correction-activation.md
grep -F 'Active — sole authorized sprint' documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md
grep -F "$BASE_SHA" documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md
grep -F 'room_authoritative_activation' documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md
grep -F 'migration `0011`' documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md
grep -F 'no preflight or production authority' documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md
```

Expected: every command returns at least one matching line.

- [ ] **Step 5: Commit the sprint-record unit**

Run:

```bash
git add \
  documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md \
  documents/00-project-management/SPRINTS/037-r14-discard-retire-correction-activation.md \
  documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md
git diff --cached --check
git commit -m 'docs(r14): record correction activation lifecycle'
```

Expected: one commit containing exactly the three sprint-record files.

---

### Task 3: Reconcile the Live Trackers and Canonical Epic Sequence

**Files:**
- Modify: `documents/00-project-management/SPRINTS/active.md`
- Modify: `documents/00-project-management/SPRINTS/README.md`
- Modify: `documents/00-project-management/PROJECT_STATE.md`
- Modify: `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md`

**Interfaces:**
- Consumes: closed Sprint 036, completed Sprint 037, active Sprint 038, `BASE_SHA`, and the approved design contract.
- Produces: one consistent repository-wide lifecycle state with Sprint 038 as the sole active assignment.

- [ ] **Step 1: Replace the active-sprint header**

At the top of `SPRINTS/active.md`, state:

- Sprint 038 is the sole active sprint;
- its branch is `sprint/r14-discard-retire-implementation-correction`;
- its base is the exact `BASE_SHA`;
- it is limited to implementation and revised operational documents;
- it grants no preflight or production authority.

Immediately below, retain concise lifecycle summaries:

- Sprint 037 completed the transition;
- Sprint 036 is closed and accepted through PR #34;
- Sprint 031 remains paused and historical documents remain non-executable;
- production remains untouched, Gate 2 unauthorized, R14e inactive.

Remove stale statements saying Sprint 036 is active, awaiting review, or still shaping the correction sprint.

- [ ] **Step 2: Reconcile the sprint index**

In `SPRINTS/README.md`:

- change Sprint 036 status to closed and accepted with PR #34 and merge `723023fd`;
- add Sprint 037 as completed documentation-only lifecycle transition;
- add Sprint 038 as active, sole authorized implementation-correction sprint, with no preflight or production authority;
- preserve Sprint 031 as paused;
- update the lifecycle note so revised discard-and-retire Gate 2 work requires Sprint 038 acceptance and a later successor preflight, not resumption under the old documents.

Do not rewrite unrelated historical sprint rows.

- [ ] **Step 3: Update `PROJECT_STATE.md`**

Set the refresh date to the actual execution date and replace the active-sprint header with Sprint 038.

The current-state narrative must include:

- Sprint 036 closure facts;
- Sprint 037 lifecycle transition;
- Sprint 038 activation from exact `BASE_SHA`;
- the approved dedicated activation proof and migration numbering (`0010` activation proof, `0011` R14e cleanup);
- the separate operator-image and backend-image boundaries;
- Sprint 031 remains paused until correction integration, then will be closed as superseded through a later lifecycle transition;
- new discard-and-retire Gate 2 documents will be created by Sprint 038, but remain non-executable until the later preflight successor;
- no readiness, evidence, production, rollback, or cleanup state has changed.

Remove current-language claims that the correction sprint is merely being shaped, pending acceptance, inactive, or awaiting activation.

- [ ] **Step 4: Update the canonical room-epic sequence**

In `ROOM_EPIC_SPRINT_SEQUENCE.md`:

1. Update the direction-amendment blockquote to state:
   - Sprint 036 is closed and accepted;
   - the approved success design is merged authority;
   - Sprint 038 is active;
   - Sprint 031 remains paused and will later be closed as superseded after correction integration;
   - no Gate 2 work is currently authorized.

2. Update the current R14/R14e planning language so:
   - migration `0010` belongs to the new activation proof;
   - R14e destructive cleanup is planned for migration `0011`;
   - `room_authoritative_activation` must remain while startup depends on it;
   - historical descriptions of accepted ADR 003/R14b/R14c work remain clearly historical and are not presented as current executable direction.

3. Preserve the core Issue #17 closure boundary:
   - R10f+, R11b+, R12, and R13 do not block the approved core closure sequence.

Do not mark Sprint 031 superseded yet and do not mark R14e active.

- [ ] **Step 5: Run tracker consistency checks**

Run:

```bash
grep -F 'Sprint 038 — R14 Discard-and-Retire Implementation Correction' documents/00-project-management/SPRINTS/active.md
grep -F 'sole active sprint' documents/00-project-management/SPRINTS/active.md
grep -F "$BASE_SHA" documents/00-project-management/SPRINTS/active.md
grep -F '| 036' documents/00-project-management/SPRINTS/README.md
grep -F '| 037' documents/00-project-management/SPRINTS/README.md
grep -F '| 038' documents/00-project-management/SPRINTS/README.md
grep -F 'room_authoritative_activation' documents/00-project-management/PROJECT_STATE.md
grep -F 'migration `0010`' documents/00-project-management/PROJECT_STATE.md
grep -F 'migration `0011`' documents/00-project-management/PROJECT_STATE.md
grep -F 'Sprint 038' documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md
grep -F 'Sprint 031' documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md
grep -F 'superseded after' documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md
```

Expected: every command returns matching text describing the approved lifecycle.

- [ ] **Step 6: Reject stale active-language in the seven-file scope**

Run:

```bash
CHANGED_SCOPE='documents/00-project-management/SPRINTS/036-r14-discard-retire-implementation-correction-shaping.md documents/00-project-management/SPRINTS/037-r14-discard-retire-correction-activation.md documents/00-project-management/SPRINTS/038-r14-discard-retire-implementation-correction.md documents/00-project-management/SPRINTS/active.md documents/00-project-management/SPRINTS/README.md documents/00-project-management/PROJECT_STATE.md documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md'
if grep -nE 'Sprint 036.*(sole active|awaiting Architect review|pending acceptance)|implementation-correction sprint.*(NOT active|inactive and unauthorized|being shaped by Sprint 036)' $CHANGED_SCOPE; then
  echo 'stale lifecycle wording found' >&2
  exit 1
fi
```

Expected: no matches and exit code `0`.

Historical statements inside the closed Sprint 036 `Current behavior` section may describe its original base state. Rewrite those sentences as explicitly historical if the check would otherwise mistake them for current authority.

- [ ] **Step 7: Commit the tracker unit**

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

Expected: a second commit containing exactly the four tracker files.

---

### Task 4: Verify Scope, Authority, and Delivery Evidence

**Files:**
- Verify all seven authorized files.
- No new file changes unless correcting a finding inside the seven-file boundary.

**Interfaces:**
- Consumes: the two documentation commits.
- Produces: review-ready evidence, an unmerged draft PR, and an Issue #17 Builder delivery record.

- [ ] **Step 1: Verify the exact file boundary**

Run:

```bash
git diff --name-only "$BASE_SHA"...HEAD
git diff --name-only "$BASE_SHA"...HEAD | wc -l
git diff --name-only "$BASE_SHA"...HEAD | grep -Ev '^documents/00-project-management/' || true
```

Expected:

- exactly the seven Global Constraints files are listed;
- the count is `7`;
- the final command produces no output.

Then enforce the exact set:

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
```

Expected: `diff` produces no output.

- [ ] **Step 2: Verify no operational authority advanced**

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

- [ ] **Step 3: Run documentation integrity checks**

Run:

```bash
git diff --check "$BASE_SHA"...HEAD
git status --short
```

Expected:

- `git diff --check` produces no output;
- `git status --short` produces no output after committed changes.

No Go, frontend, migration, Docker, Compose, or deployment command applies because this delivery changes project-management Markdown only.

- [ ] **Step 4: Run GitNexus scope detection**

Invoke:

```text
detect_changes({scope: "compare", base_ref: "dev"})
```

Expected: documentation-only changes; no runtime symbol or execution-flow impact. Record the exact output in the delivery evidence.

- [ ] **Step 5: Review commit and branch shape**

Run:

```bash
git log --oneline "$BASE_SHA"..HEAD
git rev-list --count "$BASE_SHA"..HEAD
git status --short
```

Expected:

- two focused documentation commits;
- commit count `2`;
- clean worktree.

- [ ] **Step 6: Push the branch and open a draft pull request**

Run:

```bash
git push -u origin sprint/r14-discard-retire-correction-activation
```

Open a draft PR against `dev` titled:

```text
docs(r14): activate discard-and-retire implementation correction
```

The PR body must contain:

- exact base SHA;
- exact head SHA;
- the seven-file list;
- Sprint 036 closure facts;
- Sprint 037 completion;
- Sprint 038 activation boundary;
- explicit no-preflight/no-production statement;
- exact outputs from all verification commands;
- GitNexus `detect_changes()` result;
- statement that the Builder did not mark ready, approve, or merge.

- [ ] **Step 7: Record Builder delivery on Issue #17**

Post a Builder delivery comment containing:

- draft PR number and URL;
- branch, base SHA, and head SHA;
- exact changed files;
- exact verification output;
- confirmation that no deployment document, runtime code, schema, migration, test, Docker, frontend, preflight, production, rollback, or R14e action occurred;
- confirmation that Sprint 031 remains paused and Gate 2 remains `DEFER / NOT READY`.

- [ ] **Step 8: Stop at the review gate**

Do not mark the PR ready, approve it, merge it, activate implementation work outside Sprint 038, or create the code-level implementation branch. Await Architect review and Product Owner decision.

---

## Plan Self-Review Checklist

Before presenting the delivery:

- [ ] Every locked design decision needed to activate Sprint 038 is repeated in its sprint record.
- [ ] Sprint 036 is closed with exact PR #34 head and merge SHAs.
- [ ] Sprint 037 is completed and grants no continuing authority.
- [ ] Sprint 038 is the sole active sprint and has the exact current base SHA.
- [ ] Sprint 031 remains paused and is not prematurely marked superseded.
- [ ] No readiness or operational state advanced.
- [ ] Migration numbering is consistently `0010` activation proof and `0011` R14e cleanup in current planning surfaces.
- [ ] `room_authoritative_activation` retention is explicit.
- [ ] The changed-file set is exactly seven project-management files.
- [ ] The plan and design files are unchanged by Sprint 037 execution.
- [ ] No placeholder, ambiguous authority phrase, or contradictory lifecycle statement remains.

## Subsequent Plan Boundary

After this lifecycle PR is Architect-accepted, Product Owner-approved, and squash-merged, write a separate implementation plan for Sprint 038 from its actual merged `dev` base. That later plan will cover migration `0010`, the shared activation component, `room-activation`, startup verification, image packaging, revised operational documents, and the full PostgreSQL/Docker evidence contract. Do not write or execute that code-level plan against the pre-transition base.