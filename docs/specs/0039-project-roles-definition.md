# Project Roles Definition

Source: GitHub Issue #39

## Purpose

Define short, direct role instructions for Architect and builder while keeping `docs/brain/GOVERNANCE.md` as the detailed workflow authority.

## Locked Requirements

### Architect

Provide two equivalent role definitions:

1. ChatGPT Project Instruction.
2. Root `ARCHITECT.md`.

Both must:
- identify the role as **Architect**
- state that PO has final product authority
- state that builder owns implementation
- summarize Architect responsibilities only
- point to `docs/brain/GOVERNANCE.md` for detailed workflow
- remain short, simple, and direct

The Architect role must cover:
- analyzing PO requests and the current codebase
- discussing findings, blockers, risks, edge cases, and options with PO
- recording only PO-approved **LOCKED decisions**
- writing specifications and implementation plans
- explicitly authorizing builder implementation
- reviewing builder Pull Requests and owning technical approval
- merging accepted work into `dev` after PO E2E acceptance
- maintaining Project Brain/current authoritative truth

Architect must not:
- implement feature code as the normal workflow
- make product decisions for PO

### builder

Replace the root `AGENTS.md` with a short builder role definition.

It must:
- identify the coding agent as **builder**
- state that PO owns product decisions
- state that Architect owns specification, implementation planning, and technical review
- point to `docs/brain/GOVERNANCE.md`
- remain short, simple, and direct

The builder role must require:
- explicit Architect authorization before implementation
- following the locked specification and implementation plan
- no independent product-behavior or architecture changes
- reporting blockers or required deviations
- adding/updating tests and running required verification
- committing implementation work
- opening a Pull Request into `dev`
- including verification evidence
- fixing required Architect review findings
- never merging its own work
- never committing secrets or credentials

## Out of Scope

- Changing `docs/brain/GOVERNANCE.md`
- Defining product features or architecture
- Implementing application code
- Defining release/deployment workflow

## Acceptance Criteria

- Root `ARCHITECT.md` exists and matches the approved Architect intent.
- Root `AGENTS.md` exists and matches the approved builder intent.
- Both are concise and link to `docs/brain/GOVERNANCE.md`.
- No unrelated project files or behavior are changed.
