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

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **local-music-queue** (5473 symbols, 16625 relationships, 300 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> Index stale? Run `node .gitnexus/run.cjs analyze` from the project root — it auto-selects an available runner. No `.gitnexus/run.cjs` yet? `npx gitnexus analyze` (npm 11 crash → `npm i -g gitnexus`; #1939).

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows. For regression review, compare against the default branch: `detect_changes({scope: "compare", base_ref: "main"})`.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `query({search_query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `context({name: "symbolName"})`.
- For security review, `explain({target: "fileOrSymbol"})` lists taint findings (source→sink flows; needs `analyze --pdg`).

## Never Do

- NEVER edit a function, class, or method without first running `impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `rename` which understands the call graph.
- NEVER commit changes without running `detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/local-music-queue/context` | Codebase overview, check index freshness |
| `gitnexus://repo/local-music-queue/clusters` | All functional areas |
| `gitnexus://repo/local-music-queue/processes` | All execution flows |
| `gitnexus://repo/local-music-queue/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
