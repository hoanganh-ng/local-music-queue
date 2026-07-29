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
| 027    | Room activity runtime parity / R09i                  | Closed and accepted (2026-07-28; PR #24; merge `c112000d`)         |
| 028    | Frontend global-path retirement / R14d               | Closed and accepted (2026-07-28; PR #25; merge `67bd57a8`)         |
| 029    | Coordinated authoritative room cutover / R14c        | Gate 1 integrated (PR #26 squash-merged into `dev` as `c8ab4af0` 2026-07-29); Gate 2 production execution pending |
| 030    | R14c Gate 2 Readiness Package                        | Closed and accepted (2026-07-29; PR #27; merge `d27c56ff`) — Gate 2 production execution remains pending and unauthorized |
| 031    | R14c Gate 2 Preflight and Go/No-Go Preparation       | In progress — tracking scaffold integrated (2026-07-29; PR #28; merge `e7e057b6`); all rows `Not started`; `DEFER / NOT READY`; Gate 2 pending and unauthorized |

## File Naming

Sprint files must follow the format: `XXX-sprint-name.md` (e.g., `001-authoritative-project-baseline.md`).

## Lifecycle States

Sprints progress through defined lifecycle states:

1. **Draft:** Initial planning.
2. **In progress — awaiting Architect review and Product Owner approval:** Under active development or documentation, but pending final sign-off.
3. **Approved:** Ready for execution.
4. **Completed / Closed:** Work is finished and accepted.

R14c uses an additional operational gate: Gate 1 implementation and rehearsal are integrated and closed, and that approval does not authorize production execution. The real maintenance window requires a separate Product Owner go/no-go, prepared through Sprint 031's integrated Gate 2 preflight documents.

## `active.md` Semantics

The `active.md` file identifies the single sprint currently authorized for work. When no sprint has been activated, it must explicitly state that no sprint is active and must not imply authorization for the next planned sprint.

## Review/Approval Gates

Sprints cannot be advanced, and code cannot be committed, pushed, or merged without passing the Architect review and Product Owner approval gates.

## Builder Prohibitions

Builders (AI Agents) are strictly prohibited from:

- Committing code to git.
- Pushing to remote repositories.
- Merging branches or opening pull requests.
- Advancing sprint statuses on their own authority.
- Executing R14c production migration or deployment operations.

## Historical Retention

All completed sprint documents are retained in this directory indefinitely to provide a historical record of architectural decisions and feature delivery.
