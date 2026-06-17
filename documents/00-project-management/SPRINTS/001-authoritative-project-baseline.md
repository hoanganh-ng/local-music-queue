# Sprint 001: Establish Authoritative Project Baseline

## Goal
Record the true state of the project architecture, features, and deficiencies by directly inspecting the code of the `dev` branch. Document actual backend validations, voting thresholds, database schema, and test statuses to create a reliable foundation for future feature work.

## Current behavior
The project lacks an authoritative baseline. Documentation makes claims about features, security, and tests that conflict with the actual implemented code on the `dev` branch.

## Desired behavior
An accurate, documented baseline of the codebase's current state that serves as the source of truth for planning future sprints. The baseline must accurately reflect exactly what is implemented, including flaws, test failures, and missing features. Coding agents also have one stable root entry point that directs them to the authoritative project-state and active-sprint documents without duplicating volatile implementation facts.

## Required context
- This is a documentation-only sprint.
- The baseline is established from commit `0131b44ff1ac6b263cebef6d2526196042c5560f` on the `dev` branch.
- On 2026-06-17, the Product Owner explicitly approved adding a root `AGENTS.md` as the stable entry point for coding agents.

## Requirements
- Create `PROJECT_STATE.md` baseline document.
- Correct `README.md` mapping actual state to documentation claims.
- Identify testing gaps, security deficiencies (e.g. lack of JWT, client-supplied roles), and topology configuration.
- Add a concise root `AGENTS.md` that points agents to `PROJECT_STATE.md`, `SPRINTS/active.md`, the approved sprint, affected implementation files, and nearby tests.
- Keep volatile endpoint counts, schema details, and sprint status out of `AGENTS.md`; those remain owned by the implementation and project-management documents.

## Out of scope
- Modifying runtime code, tests, configuration, Docker/Compose/Nginx files, workflows, dependency files, generated files, CLAUDE.md, .claudeignore, .claude/, or local-music-queue-project-instruction.md.
- Running tests other than to validate a documentation statement.
- Committing, pushing, merging, or advancing the sprint by the Builder.

## Implementation guidance
- Ensure `TestLoadDefaults` is described as expecting the historical HostPIN default 6666, while current configuration defaults it to 9512. Do not describe this as deprecated PIN authentication.
- Ensure the seven project-management/README documentation paths plus the approved root `AGENTS.md` are the only cumulative Sprint 001 additions or modifications.
- `AGENTS.md` must remain a stable navigation and guardrail document, not a duplicate project-state inventory.

## Verification points
- All eight approved paths are updated correctly.
- No runtime, configuration, dependency, workflow, generated, local-agent-memory, or private-instruction files are changed.
- `git status --short --untracked-files=all` and `git diff --check` run cleanly.

## Risks and review focus
- Focus on ensuring the documentation precisely reflects the codebase without implying approval of any current flawed implementations.
- Ensure `AGENTS.md` stays aligned with source priority, architecture boundaries, security expectations, sprint gates, and verification requirements without becoming a stale second source of implementation truth.

## Builder reasoning effort
Low — documentation only, no runtime logic changes.

## Handoff prompt for Builder
Implement the documentation revisions exactly as specified in the sprint requirements. Do not commit, push, merge, modify unrelated files, or advance the sprint.

## Status
Closed — approved by Product Owner.
