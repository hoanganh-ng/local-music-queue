# AGENTS.md

This file is the stable entry point for coding agents working on **Local Music Queue**.

It is intentionally concise. Do not copy volatile endpoint counts, schema details, or sprint status into this file. Read the authoritative project documents and implementation instead.

## Start Here

Before changing anything, read in this order:

1. The Product Owner's current instruction.
2. [`documents/00-project-management/PROJECT_STATE.md`](documents/00-project-management/PROJECT_STATE.md) for the documented implementation baseline and known risks.
3. [`documents/00-project-management/SPRINTS/active.md`](documents/00-project-management/SPRINTS/active.md) for the only currently authorized sprint.
4. The sprint document linked from `active.md`.
5. The current diff, affected implementation files, and nearby tests.

When sources conflict, use this priority:

1. Current Product Owner decision
2. Approved sprint and current diff
3. Implementation and tests
4. `PROJECT_STATE.md` and current documentation
5. Architecture/API documents
6. Roadmap and historical documents

Implementation and tests prevail over stale documentation when current behavior is being established.

## Repository Map

- Branch: `dev`
- Composition root: `cmd/server/`
- Backend: `internal/`
- Frontend: `frontend/src/`
- Documentation: `README.md`, `documents/`
- Backend stack: Go 1.22, `net/http`, Gorilla WebSocket, SQLite
- Frontend stack: Vue 3, Vite, Vitest

Do not read or refactor the whole repository by default. Start with the active sprint, current diff, affected files, and nearby tests.

## Working Agreement

- Implement only the approved active sprint.
- Preserve established REST payloads, WebSocket events, queue JSON, SQLite schema, frontend mappings, and deployment assumptions unless the sprint explicitly changes them.
- Do not modify unrelated files or perform broad restructuring.
- Do not commit, push, merge, open pull requests, rewrite history, or advance sprint status. The Product Owner handles repository history and sprint advancement.
- Do not expose credentials, personal data, OAuth tokens, environment secrets, or local machine paths in code, tests, logs, fixtures, screenshots, or documentation.
- Report the actual changed files, decisions, risks, and verification output. Never claim a command passed without showing evidence.

## Architecture Boundaries

Use Clean Architecture and keep dependencies pointing inward:

- Domain: entities, invariants, errors, repository/service contracts
- Use cases: application behavior through interfaces
- Infrastructure: SQLite, configuration, Google/YouTube, external adapters
- Delivery: HTTP/WebSocket validation, error mapping, responses, broadcasts
- `cmd/server/`: composition and dependency wiring
- Frontend: presentation and client state, not authorization authority

Prefer small interfaces and constructor injection. Avoid circular dependencies, duplicated business rules in delivery, and transport concerns in domain/use-case packages.

## Compatibility and Consistency

Protect these established assumptions unless an approved sprint changes them:

- Queue state is stored as one JSON document in the single-row `queue_state` record.
- `Songs`, `CurrentIndex`, `Status`, and `Elapsed` must remain consistent.
- The first song starts playback; duplicate detection concerns upcoming songs.
- Clear keeps the current song; prioritize moves a non-current song immediately after it.
- Priority balance, queue mutation, and transaction records must not end in partial contradiction.
- Vote sessions are in memory and expire; thresholds depend on connected clients.
- Auto-queue is asynchronous and single-instance; prevent duplicates, stale actions, races, deadlocks, and blocked broadcasts.
- Clients receive an initial full sync followed by sequenced deltas.

For queue, SQLite, voting, WebSocket, or goroutine changes, identify the consistency owner and examine race behavior. Avoid slow work while holding locks and detached goroutines without clear ownership or error handling.

## Security Boundary

Local-network deployment is not a security boundary.

Treat client-supplied IDs, roles, names, `localStorage`, and WebSocket query parameters as untrusted. Do not rely only on frontend role checks for privileged operations. Authentication or authorization redesigns require their own approved security sprint.

Treat `yt-dlp` as an external-process boundary. Validate inputs, avoid command injection, bound execution, and prevent sensitive output leakage.

## Verification

Use the checks relevant to the approved change and include exact results:

```bash
go test ./...
go test -race ./...   # concurrency-related work, when feasible
go vet ./...          # when relevant
cd frontend && npm run test:unit -- --run
cd frontend && npm run build
docker compose config # deployment changes
```

Do not claim frontend E2E coverage unless it is confirmed in the repository.

## Delivery Format

For implementation work, return:

- Changed files and purpose
- Behavior and contract effects
- Validation, authorization, persistence, and concurrency considerations
- Tests and commands actually run, with exact outcomes
- Remaining risks or live checks

For review work, separate verified facts, agent claims, and unverified behavior. Classify findings as Blocking, Important, Minor, or Observation.
