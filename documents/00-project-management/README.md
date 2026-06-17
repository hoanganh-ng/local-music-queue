# Project Management

## Purpose
This directory contains project management documentation, establishing the authoritative baseline state, architectural direction, and the active/historical sprint plans. It governs the workflow and ensures alignment between planned work and actual codebase state.

## Source Priority
The `PROJECT_STATE.md` document is the authoritative documentation snapshot for the inspected commit. If other documentation contradicts it, `PROJECT_STATE.md` takes precedence. However, if conflicts are discovered between `PROJECT_STATE.md` and the actual implementation/tests, the implementation/tests themselves remain the ultimate source of truth.

## Current-State vs Desired-State
- **Current-State:** Documented precisely in `PROJECT_STATE.md`. It reflects reality, including technical debt, security flaws, and failing tests.
- **Desired-State:** Defined in individual Sprint documents (under `SPRINTS/`) and the project roadmap. 

## Approval Gates
No runtime code changes or sprint advancements can occur without explicit review and approval gates being met:
- **Architect Review:** Ensures technical approach aligns with architecture and resolves security/structural concerns.
- **Product Owner Approval:** Authorizes the feature scope, priorities, and final acceptance of work.

## Structure
- [`PROJECT_STATE.md`](PROJECT_STATE.md): Authoritative baseline state of the application.
- [`SPRINTS/`](SPRINTS/README.md): Active and completed sprint documentation.
- [`SPRINTS/active.md`](SPRINTS/active.md): Pointer to the currently active sprint.
