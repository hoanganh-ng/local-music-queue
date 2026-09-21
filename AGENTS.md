# AGENTS.md

This file defines the starting rules for coding agents working on Local Music Queue.

## Repository state

- Active development branch: `dev`
- Current implementation: none
- Active sprint: none
- Inherited backlog: none

The repository is intentionally starting fresh. Historical branches, commits, issues, documentation, and implementation are reference material only and are not current requirements.

## Working rules

1. Start from the Product Owner's current instruction.
2. Design non-trivial behavior before implementation.
3. Do not copy or restore historical implementation unless the Product Owner explicitly approves it.
4. Do not assume historical APIs, schemas, architecture, deployment topology, or frontend behavior remain valid.
5. Keep changes scoped to the currently approved work.
6. Never commit credentials, private keys, tokens, production connection data, or other secrets.
7. Treat any credentials found in repository history as compromised/retired.
8. Add tests and verification appropriate to the technology stack once that stack is selected.

## Delivery

For implementation work, report changed files, behavior, verification performed, and remaining risks. Never claim a test or build passed without fresh evidence.
