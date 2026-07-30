# Sprint 029 — Coordinated authoritative room cutover (R14c)

**Status:** Gate 1 integrated — PR #26 accepted and squash-merged into `dev` as `c8ab4af029d10dda889d1165464e16068a5be573` (2026-07-29); implementation lifecycle closed. Gate 2 production execution explicitly pending (separate Product Owner go/no-go)  
**Branch:** `sprint/r14c-coordinated-production-cutover`  
**Base:** `dev` at `13c09549fc89febac2c79085afcf7249a43f62a4`  
**Parent epic:** Issue #17  
**Risk:** Extra high  
**Scope:** Backend runtime composition, legacy-contract tombstones, deployment pairing, packaging, tests, isolated rehearsal, and production runbook. No production execution is authorized by activation.

> **Direction amendment — discard-and-retire (ADR 004, Sprint 034, 2026-07-30).** This sprint record is preserved as the historical record of the accepted R14c Gate 1 work, which remains integrated on `dev` and is not reverted. However, the Product Owner has replaced the migrate-and-retire cutover contract with the **discard-and-retire** contract (Issue #17 comment #5128855220; ADR 004): the Gate 2 **production assumptions in this document are superseded** — no `cmd/room-cutover up` production copy will run, no migrated room will be created, and the maintenance-window sequence below must not be executed. The current activation proof is explicitly unresolved: the fail-closed startup guard depends on `room_cutover_marker`, which proves a migrated-room copy that will no longer happen (ADR 004 Section 5). The runbook and all Gate 2 documents are marked non-executable, and Sprint 031 (Gate 2 preflight) is paused, until the implementation-correction sprint (ADR 004 Section 6) is accepted and integrated. The runtime flag, fail-closed startup behavior, tombstone retirement intent, mandatory backups, false/false rollback pair, paired server/SPA deployment, and Sprint 032 governance sequence remain in force.

## Goal

Deliver a fail-closed transition from the legacy global runtime to the room-authoritative runtime through one runtime server flag, the accepted R14d frontend build gate, repository-free retirement handlers, and a reviewed operational runbook.

R14c has two separate gates:

1. **Implementation gate:** code, tests, packaging, deployment configuration, isolated rehearsal, and runbook are reviewed and accepted.
2. **Execution gate:** the Product Owner separately authorizes the real maintenance window after reviewing the implementation, rehearsal evidence, operator inputs, backup posture, rollback pair, and smoke matrix.

The Builder implements Gate 1 only. Activation does not authorize Gate 2.

## Current behavior

At the approved base:

- R05b, R09h, R14b, R09i, and R14d are closed and accepted.
- Production cutover has not occurred.
- The R14d `false` SPA remains the authoritative pre-cutover/rollback behavior; the `true` SPA has not been deployed.
- `cmd/server` has no `--room-cutover-authoritative` argument.
- Server startup runs embedded migrations defensively.
- Normal composition injects one explicit no-op room-activity writer into roomqueue, roomvote, and roomautoqueue.
- All legacy global queue, vote, auto-queue, and `/ws` entry points remain registered.
- The repository already contains schema version 9 support, `room_cutover_marker`, `room_activities`, `cmd/room-cutover plan|up|verify`, and the real PostgreSQL room-activity repository.
- The backend production image does not currently package `room-cutover`.
- Compose does not currently pair the server runtime mode with the R14d frontend build mode.

## Desired behavior

### False pair — pre-cutover and rollback

```text
--room-cutover-authoritative=false
VITE_ROOM_CUTOVER_AUTHORITATIVE=false
```

The false pair preserves current behavior:

- real legacy global handlers and global `/ws` remain registered;
- room routes remain registered;
- the no-op room-activity writer is selected;
- no marker is required;
- normal defensive startup migration behavior remains available;
- the Dashboard remains the frontend landing surface.

### True pair — authoritative room runtime

```text
--room-cutover-authoritative=true
VITE_ROOM_CUTOVER_AUTHORITATIVE=true
```

The true server must refuse to serve unless:

- the migration state is clean;
- schema version is at least 9; and
- `room_cutover_marker.id = 1` exists.

The guard runs before listeners open and before real room-activity writes can receive traffic. It checks marker and schema presence only; it does not revalidate changing target hashes on every startup.

After the guard passes:

- one real `PostgresRoomActivityRepository` is injected into all three activity-producing room interactors;
- all approved legacy global queue, vote, auto-queue, and `/ws` entry points return repository-free `410 Gone` tombstones;
- room REST routes and `/ws/rooms/{slug}` remain unchanged;
- auth, priority-balance, and YouTube-search routes remain live;
- the R14d true SPA is deployed as the matching client.

## Runtime flag

Add:

```text
--room-cutover-authoritative=true|false
```

Rules:

- omitted → `false`;
- explicit `false` → false mode;
- explicit `true` → true mode;
- invalid value, unknown flag, or positional argument → startup error;
- parse before database composition and before listeners open;
- read once and pass through a narrow immutable setup option;
- no mutable package-level mode state;
- the Go server never reads `VITE_ROOM_CUTOVER_AUTHORITATIVE`.

## Fail-closed startup guard

When true mode is requested:

1. open and ping PostgreSQL;
2. inspect the existing schema version and dirty flag before running defensive startup migrations;
3. reject a dirty state;
4. reject schema version below 9;
5. require `room_cutover_marker.id = 1`;
6. only then continue composition.

The guard must not create schema, create/edit a marker, invoke the cutover CLI, repair a dirty migration, validate mutable target hashes, or expose DSNs, credentials, marker hashes, email addresses, or sessions.

False mode retains the existing defensive migration behavior.

## Activity-writer composition

Select exactly one `repository.RoomActivityRepository` during setup:

- false → `NoopRoomActivityRepository`;
- true → `PostgresRoomActivityRepository`.

Inject the same selected instance into:

- `roomqueue.Interactor`;
- `roomvote.Interactor`;
- `roomautoqueue.Interactor`.

Do not switch implementations after startup.

## Atomic legacy contract retirement

In true mode, retire exactly:

```text
GET  /api/queue
POST /api/queue/add
POST /api/queue/skip
POST /api/queue/status
POST /api/queue/sync
POST /api/queue/ended
POST /api/queue/prev
POST /api/queue/remove
POST /api/queue/clear
POST /api/queue/volume
POST /api/queue/prioritize
POST /api/vote/skip
POST /api/vote/prioritize
POST /api/autoqueue/toggle
GET  /api/autoqueue/status
/ws
```

Every retired REST method and the global `/ws` HTTP-phase request return:

```http
HTTP/1.1 410 Gone
Content-Type: application/json
Link: </api/rooms>; rel="successor-version"
```

```json
{
  "error": "gone",
  "code": "global_contract_retired",
  "documentation": "documents/00-project-management/SPRINTS/022-legacy-global-state-migration-and-contract-retirement-plan.md"
}
```

The tombstone is delivery-owned and repository-free. It runs before authentication, does not resolve sessions, does not invoke legacy use cases or repositories, does not upgrade `/ws`, and does not expose a migrated-room slug or synthesize a default room.

Use one reusable handler and one central all-or-nothing registration decision. There is no per-route runtime configuration.

## Contracts that remain live

Do not retire or change:

```text
POST /api/auth/google
POST /api/auth
GET  /api/user/priority-balance
GET  /api/youtube/search
/api/rooms/...
/api/invites/...
/ws/rooms/{slug}
```

Room authorization, business rules, WebSocket sequencing, event inventories, payloads, and recovery behavior remain unchanged.

## Deployment pairing

Provide one Compose-level operator input, defaulting to false, that produces a matching pair:

- backend command argument `--room-cutover-authoritative=false|true`;
- frontend build argument `VITE_ROOM_CUTOVER_AUTHORITATIVE=false|true`.

This shared Compose interpolation is deployment wiring, not a third application flag. Each application still consumes only its approved setting.

Required deployment outcomes:

- default build/run is false/false;
- backend image contains `/app/server` and `/app/room-cutover`;
- frontend image accepts the R14d build argument;
- documented/configured paths cannot accidentally produce true server + false SPA;
- both rendered Compose configurations are reviewable before deployment;
- the true pair may be built and rehearsed but must not be deployed to production during Builder work.

## Gate 1 — implementation and isolated rehearsal

The Builder may:

- implement the runtime flag and guard;
- implement writer selection and tombstones;
- add focused tests;
- update Docker/Compose pairing and package `room-cutover`;
- create a redacted production runbook;
- run an isolated production-like rehearsal using non-production data and placeholder identities.

The Builder must not:

- execute commands against production;
- deploy the true pair;
- open or close production traffic;
- use real production secrets in reports;
- alter the production marker or migrated data;
- activate or implement R14e.

## Gate 2 — production execution

Gate 2 requires a separate Product Owner go/no-go after Gate 1 is reviewed and accepted.

Required operator inputs before go/no-go:

```text
<TARGET_ROOM_SLUG>
<TARGET_ROOM_NAME>
<HOST_USER_ID>
<MAINTENANCE_WINDOW>
<BACKUP_LOCATION>
<LIVE_ALLOWED_GOOGLE_ACCOUNT>
```

A real allow-listed Google account is mandatory because R14d's isolated review could not complete real-login and server-authenticated room flows.

## Production runbook requirements

Create a focused runbook using placeholders only.

### Pre-window

- confirm reviewed commit and image identities;
- record the exact false/false rollback pair;
- approve an unused target slug and room name;
- validate the positive canonical PostgreSQL host user ID;
- confirm a real allow-listed Google account;
- render and review false and true Compose configurations;
- build/tag both artifact pairs;
- run `room-cutover plan` against a production snapshot;
- run `room-cutover up --dry-run` against an isolated copy;
- complete the isolated end-to-end rehearsal;
- confirm a verified backup destination and at least 30-day retention.

### Maintenance window

1. close public traffic;
2. take and verify a timestamped `pg_dump`;
3. stop frontend and backend while retaining PostgreSQL;
4. run `room-cutover plan` against the live database;
5. run `room-cutover up` with approved identity values;
6. run `room-cutover verify` before room writes;
7. deploy the true server and true SPA as one pair;
8. run the mandatory smoke matrix;
9. reopen traffic only after every mandatory check passes.

### Rollback before reopening traffic

Any failed mandatory check requires:

- stop the true pair;
- restore the exact false/false rollback pair;
- do not delete or edit `room_cutover_marker`;
- do not use force/reset/hash edits;
- retain the dump and redacted reports;
- record any true-mode room writes made during smoke testing;
- defer forward recovery versus database restoration to a separate Product Owner decision.

Rollback is not deleting the migrated room and is not rerunning cutover.

## Mandatory production smoke matrix

While public traffic is closed, verify:

### Server and migration

- true server cannot start before marker creation;
- schema is clean and version ≥9;
- marker exists;
- `room-cutover verify` succeeds before room mutations;
- reports are PII-free;
- target room and host membership exist;
- queue, activity, auto-queue config, and play-history counts match the report.

### Retired contracts

- all 15 listed legacy REST methods return exact `410`, envelope, and `Link` header;
- global `/ws` returns `410` during HTTP and never upgrades/registers a client.

### Preserved contracts

- real Google login succeeds;
- authenticated room list succeeds;
- migrated room opens with matching queue state;
- room WebSocket full sync succeeds;
- direct room URL works;
- create/list/open/invite redemption work;
- player lease claim/heartbeat/release work;
- room queue/playback mutations work;
- skip and prioritize votes work;
- room auto-queue status/toggle work;
- priority balance and YouTube search remain reachable.

### Activity continuity

After `room-cutover verify`:

- perform one approved qualifying room mutation;
- confirm exactly the expected room activity is appended;
- confirm its ID is greater than the copied legacy maximum;
- confirm no legacy `activities` row is appended by the room mutation.

### Frontend

- login and `/` land on RoomEntry;
- no Dashboard route is reachable;
- sign-out works;
- no global `/ws` attempt or reconnect occurs;
- room flows use room-scoped APIs.

## Required context

Read first:

- `documents/00-project-management/PROJECT_STATE.md`
- `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md`
- `documents/00-project-management/SPRINTS/active.md`
- `documents/00-project-management/SPRINTS/022-legacy-global-state-migration-and-contract-retirement-plan.md`
- `documents/00-project-management/SPRINTS/026-room-cutover-mechanism.md`
- `documents/00-project-management/SPRINTS/027-room-activity-runtime-parity.md`
- `documents/00-project-management/SPRINTS/028-frontend-global-path-retirement.md`
- `documents/00-project-management/ADRS/003-legacy-global-state-migration-and-contract-retirement.md`
- `cmd/server/main.go` and nearby composition/route tests
- `internal/delivery/http/` focused handlers/tests
- `internal/delivery/ws/hub.go`, `room_hub.go`, and `events.go`
- `internal/infrastructure/persistence/migrator.go`
- no-op and PostgreSQL room-activity repositories/tests
- migration `0009_room_cutover_support.{up,down}.sql`
- `cmd/room-cutover/` and `internal/infrastructure/persistence/roomcutover/`
- `Dockerfile`, `Dockerfile.migrate`, `docker-compose.yml`, `.env.example`
- `frontend/Dockerfile`, `frontend/.env.example`
- focused deployment docs.

Do not scan/refactor unrelated Vue source, room business rules, OAuth/session architecture, yt-dlp, migrations 0001–0009, R13, R14e/0010, R10f+, R11b+, or R12.

## Out of scope

- production execution by the Builder;
- migration 0010 or schema version 10;
- dropping/editing legacy tables;
- changing migration 0009;
- deleting legacy repositories or Dashboard source;
- frontend behavior changes or new frontend runtime flags;
- OAuth/session/authorization redesign;
- activity read panel;
- new REST or WebSocket events;
- room business-rule changes;
- multi-instance guarantees;
- activating R14e.

## Verification

```bash
go test -count=1 ./cmd/server/... \
  ./internal/delivery/http/... \
  ./internal/infrastructure/persistence/...

go test -race -count=1 ./cmd/server/... \
  ./internal/delivery/http/... \
  ./internal/usecase/roomqueue/... \
  ./internal/usecase/roomvote/... \
  ./internal/usecase/roomautoqueue/...

go test ./...
go vet ./...

cd frontend
VITE_ROOM_CUTOVER_AUTHORITATIVE=false npm run test:unit -- --run
VITE_ROOM_CUTOVER_AUTHORITATIVE=true npm run test:unit -- --run
VITE_ROOM_CUTOVER_AUTHORITATIVE=false npm run build
VITE_ROOM_CUTOVER_AUTHORITATIVE=true npm run build
cd ..

ROOM_CUTOVER_AUTHORITATIVE=false docker compose config
ROOM_CUTOVER_AUTHORITATIVE=true docker compose config

docker compose build backend frontend

git diff --check
git diff --name-only
git status --short
```

Report environmental skips and pre-existing PostgreSQL connection-exhaustion failures separately. Never claim a command passed without evidence.

## Review focus

Blocking risks include:

- true startup silently migrates an older database before evaluating the guard;
- true startup succeeds without schema 9, clean state, or marker;
- false mode selects the real activity writer;
- any legacy mutation route remains live in true mode;
- tombstones authenticate or touch repositories;
- global `/ws` upgrades;
- server and SPA can be deployed in mismatched modes;
- production image cannot execute `room-cutover`;
- a room mutation occurs before `room-cutover verify`;
- activity sequence continuity is not proven;
- rollback edits migration evidence;
- real-login checks are falsely marked passed;
- Builder performs production work;
- R14e is mixed into R14c.

## Builder handoff

Implement Gate 1 only from the approved base. Keep the work narrow and self-contained.

Return:

- exact changed files;
- flag/guard design;
- full retired-route inventory;
- activity-writer composition evidence;
- Docker/Compose pairing evidence;
- isolated rehearsal report;
- exact automated command results;
- environmental limitations;
- completed but unexecuted production runbook;
- confirmation that no production data/deployment, R14e migration, frontend behavior, or authentication architecture was changed.

## Gate 1 implementation record

Gate 1 is **integrated**: the Product Owner accepted the re-review and squash-merged PR #26 into `dev`, closing R14c's implementation lifecycle. Gate 2 (production execution) is **not** authorized by the merge and remains explicitly pending.

- **PR:** #26 — base `dev` (`13c09549fc89febac2c79085afcf7249a43f62a4`), branch `sprint/r14c-coordinated-production-cutover`; **squash-merged into `dev` as `c8ab4af029d10dda889d1165464e16068a5be573` on 2026-07-29**.
- **Review-head sequence:** `1502c3a243b3350b6a18ff2454118981f4032357` (initial Gate 1) → `0daa8fbe22b896c6def5f2e75af129792ee41229` (first corrective pass, findings F1–F6) → `328aba41db44ca1fc41e02ee052d2d6c2c63ca36` (second corrective pass, the reviewed head) → the final documentation-only corrective head, whose exact SHA remains recorded only in the PR #26 lifecycle ledger (a commit cannot embed its own hash). The changed-file count is the GitHub PR #26 file count for the merged head.
- **Status:** Gate 1 implementation **integrated and closed**. The reviewed head (`328aba41…`) delivered the packaged-image pre-window rehearsals with bind-mounted durable reports, quoted-variable identity SQL, rollback image-ID capture from the running containers, and the Docker-gated Compose pairing test. The final corrective pass was runbook/tracker documentation only: the captured false-pair images are preserved under protected `rollback-r14c` tags plus durable archives in `<BACKUP_LOCATION>`, verified to resolve to the captured IDs, with deletion and pruning prohibited until the rollback window closes; both pre-window rehearsal commands carry an inline `ROOM_CUTOVER_AUTHORITATIVE=true` prefix so each unambiguously selects the prebuilt `:true` backend image; the live email reaches `psql` through the environment and `\getenv` into the quoted `:'email'` variable, so it never appears in process arguments; and the review-head history above records `328aba41…` as the reviewed head. None altered the accepted runtime flag, startup guard, activity-writer selection, or tombstone handler.
- **Corrective head:** the Builder submitted each corrective commit unmerged on the PR branch and posted its exact SHA to PR #26 (the PR lifecycle ledger); the Product Owner performed the merge. Repository history and sprint advancement remain with the Product Owner.
- **Verification evidence:** recorded from this Gate 1 branch — `go test` and `go vet` across the cutover packages; frontend unit suites and builds in both `VITE_ROOM_CUTOVER_AUTHORITATIVE=false` and `=true`; `docker compose config` and `docker compose build backend frontend` in both modes; the mode-qualified false/true image references and their local selectability; the isolated rollback-pair rehearsal; `git diff --check`, `git diff --name-only`, `git status --short`. Exact outputs and any environmental skips are reported in the handoff, never claimed without evidence.
- **Not executed:** Gate 2 production execution is not authorized; the `true` pair is not deployed; no production migration command was run; no production data or secrets were touched.
- **R14e** remains inactive (migration 0010, schema version 10, and legacy-table deletion are not authorized).

Do not commit, push, merge, open/close pull requests, execute production commands, close the sprint, activate R14e, or advance the epic.