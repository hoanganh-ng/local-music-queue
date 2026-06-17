# Sprints

This directory contains documentation for active and past sprints.

## File Naming
Sprint files must follow the format: `XXX-sprint-name.md` (e.g., `001-authoritative-project-baseline.md`).

## Lifecycle States
Sprints progress through defined lifecycle states:
1. **Draft:** Initial planning.
2. **In progress — awaiting Architect review and Product Owner approval:** Under active development or documentation, but pending final sign-off.
3. **Approved:** Ready for execution.
4. **Completed / Closed:** Work is finished and accepted.

## `active.md` Semantics
The `active.md` file must always point to the single sprint that is currently in progress. It explicitly declares which sprint is authorized for work.

## Review/Approval Gates
Sprints cannot be advanced, and code cannot be committed, pushed, or merged without passing the Architect review and Product Owner approval gates.

## Builder Prohibitions
Builders (AI Agents) are strictly prohibited from:
- Committing code to git.
- Pushing to remote repositories.
- Merging branches or opening pull requests.
- Advancing sprint statuses on their own authority.

## Historical Retention
All completed sprint documents are retained in this directory indefinitely to provide a historical record of architectural decisions and feature delivery.
