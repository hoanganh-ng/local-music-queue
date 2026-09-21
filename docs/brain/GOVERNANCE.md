# Project Brain — Governance

Source: GitHub Issue #37

This file defines the authoritative development workflow for Local Music Queue.

## Roles

### PO
- Submits product intent, feature requests, bugs, and other work requests.
- Makes final product decisions.
- Reviews specifications for correctness.
- Performs end-to-end acceptance.

### Architect
- Analyzes PO requests and the current codebase.
- Discusses findings, blockers, risks, edge cases, options, and recommendations with PO.
- Records locked decisions.
- Writes specifications and implementation plans.
- Authorizes builder work.
- Reviews implementation and owns technical approval.
- Merges accepted work into `dev`.
- Maintains Project Brain/current project truth when required.

### builder
- Implements the approved plan.
- Adds or updates tests.
- Runs required verification.
- Fixes Architect review findings.
- Does not change product behavior or architecture independently.
- Does not merge its own work.

## Development Workflow

### Step 1 — PO
PO creates a GitHub Issue describing the intent, feature, bug, or request.

PO assigns one primary workflow label. This label is later used as the branch prefix.

Examples: `workflow`, `feature`, `bug`, `chore`, `documentation`.

No technical design is required yet.

**Output:** GitHub Issue + primary workflow label.

### Step 2 — Architect
Architect analyzes the PO Issue and codebase, then discusses findings, blockers, risks, edge cases, options, and recommendations with PO.

The discussion itself does not need to be recorded on GitHub.

When PO makes a final decision, Architect records only that **LOCKED decision** on the GitHub Issue.

Step 2 is released only when all identified blockers, findings, and edge cases are resolved or explicitly decided by PO.

**Output:** locked decisions on the Issue + Step 2 released.

### Step 3 — Architect
Architect creates a working branch from `dev` before writing the specification.

Branch format:

```
<label>/<issue-number>-<short-slug>
```

The branch prefix must use the primary workflow label assigned by PO in Step 1.

Architect writes the implementation-independent specification from the **LOCKED decisions** recorded on the Issue.

PO reviews the specification for correctness.

builder does not act yet.

**Output:** working branch + committed specification linked to the Issue.

### Step 4 — PO
PO reviews the specification on the working branch.

PO either approves it or requests changes.

When approved, Architect records the specification as **LOCKED** on the GitHub Issue.

builder still does not act.

**Output:** PO-approved, locked specification.

### Step 5 — Architect
Architect converts the **LOCKED specification** into a detailed implementation plan for builder.

The plan must define:
- exact implementation scope
- ordered tasks
- expected files/components
- tests to add or change
- verification commands
- constraints and out-of-scope work
- required delivery evidence

If planning reveals a product or specification problem, Architect returns to PO before builder starts.

builder does not act yet.

**Output:** committed implementation plan linked to the Issue.

### Step 6 — Architect
Architect authorizes builder to start implementation from the approved branch and plan.

builder must:
- follow the locked specification and implementation plan
- stop and report any blocker or required deviation
- not change product behavior or architecture independently

**Output:** explicit builder authorization on the GitHub Issue.

### Step 7 — builder
builder implements the approved plan on the working branch.

builder must:
- follow the locked specification and implementation plan
- add or update tests
- run required verification
- stop and report blockers or required deviations
- commit the completed work
- open a Pull Request from the working branch into `dev`
- link the GitHub Issue, locked specification, and implementation plan
- include verification evidence in the Pull Request

**Output:** implementation commits + Pull Request + verification evidence.

### Step 8 — Architect
Architect reviews the builder Pull Request against the **LOCKED specification** and implementation plan.

Architect records actionable review findings on the Pull Request.

builder fixes the findings and provides updated verification evidence.

The review/fix cycle may repeat for multiple rounds until all required findings are resolved.

**Output:** all required findings resolved + Architect technical approval on the Pull Request.

### Step 9 — PO
PO performs **end-to-end acceptance review only** against the **LOCKED specification**.

PO does not review implementation details, code quality, unit tests, or architecture. Those are owned by Architect.

If E2E behavior is wrong, work returns to Architect for analysis before builder changes anything.

**Output:** PO E2E acceptance.

### Step 10 — Architect
After PO E2E acceptance, Architect:
- merges the approved Pull Request into `dev`
- updates Project Brain/current state if the work changes authoritative project truth
- records completion on the GitHub Issue
- closes the Issue
- deletes the working branch when no longer needed

builder does not merge its own work.

**Output:** merged `dev` + updated Project Brain + closed Issue + cleaned working branch.

## Workflow Boundary

Release, deployment, promotion to `main`, tagging, and production rollout are not part of this development workflow.

They require a separate release/deployment workflow.

## Decision Recording Rule

GitHub Issues preserve the work history and locked decisions.

Project Brain stores the current authoritative truth derived from those locked decisions.

If a locked decision is later replaced, the historical decision remains in the Issue and the Project Brain is updated to the new authoritative truth.
