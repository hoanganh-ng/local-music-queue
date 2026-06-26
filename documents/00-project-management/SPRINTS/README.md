# Sprints

This directory contains documentation for active and past sprints.

## Sprint Index

| Sprint | Title                                                | Status |
|--------|------------------------------------------------------|--------|
| 001    | Authoritative Project Baseline                       | Closed |
| 002    | Trustworthy Verification Baseline                    | Closed |
| 003    | Trusted Owned-Song Removal                           | Closed |
| 004    | Authoritative Playback Advancement                   | Closed |
| 005    | In-App Vote Event Notifications                      | Closed |
| 006    | Room Architecture ADR / R00                          | Closed |
| 007    | PostgreSQL Migration Design / R01                    | Closed |
| 008    | PostgreSQL Foundation (Behavior Preserved) / R02     | Closed |
| 009    | SQLite-to-PostgreSQL Data Migration / R03            | Closed |
| 010    | Room Domain, Invite, Membership, and Lifecycle / R04 | In progress — awaiting Architect review and Product Owner approval |
| 020    | Allowed Origins and WebSocket Origin Policy / A01    | Closed (2026-06-26) — pending Product Owner acceptance             |

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
