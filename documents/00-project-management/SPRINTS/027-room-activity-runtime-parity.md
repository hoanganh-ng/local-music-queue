# R09i — Room activity runtime parity

**Status:** Active — approved by the Product Owner on 2026-07-27  
**Parent epic:** Issue #17  
**Sequence:** `R05b → R09h → R14b → R09i → R14d → R14c → R14e`  
**Builder branch:** `sprint/r09i-room-activity-runtime-parity`  
**Pull-request base:** `dev`

## Goal

Implement backend room-activity parity for room queue, playback, voting, and auto-queue mutations through the existing room-activity repository contract.

The complete activity-production behavior must be implemented and tested in R09i, while normal pre-R14c server composition must continue to inject an explicit no-op room-activity repository. R14c remains responsible for selecting the real PostgreSQL writer only after the production cutover guard is satisfied.

## Current behavior

- Migration `0009_room_cutover_support` and the PostgreSQL `room_activities` table exist.
- `domain/repository.RoomActivityRepository` already defines `AddActivity` and `GetActivities`.
- `PostgresRoomActivityRepository` already implements that contract.
- Room queue, room vote, and room auto-queue interactors do not currently produce room-scoped activity records.
- `cmd/server` does not compose the PostgreSQL room-activity repository.
- Global queue, priority, and voting flows already provide the behavioral reference for activity types and descriptions.
- Production cutover has not been executed.
- No room-activity read API, WebSocket event, or frontend panel exists.

## Desired behavior

- Every qualifying successful room mutation produces the exact approved activity record.
- Rejected, failed, stale, and non-mutating operations do not produce activity, except that an accepted vote ballot is recorded before a later stale pass resolution.
- Activity persistence is best-effort and secondary to the already-successful primary mutation.
- `AddActivity` is never called while a room queue, room vote, or room auto-queue mutex is held.
- Normal pre-R14c server startup injects an explicit no-op repository, so R09i cannot write production `room_activities` rows yet.
- The real PostgreSQL repository remains directly usable by tests and ready for R14c composition.
- No REST, WebSocket, schema, frontend, deployment, or cutover contract changes occur.

## Required context

Read only the following context before implementation.

### Project and sprint context

- `documents/00-project-management/PROJECT_STATE.md`
- `documents/00-project-management/ROOM_EPIC_SPRINT_SEQUENCE.md`
- `documents/00-project-management/SPRINTS/active.md`
- `documents/00-project-management/SPRINTS/022-room-activity-persistence-foundation.md`
- `documents/00-project-management/SPRINTS/026-room-cutover-mechanism.md`
- `documents/02-architecture/ADR-003-room-activity-cutover.md`
- Room epic Issue #17, limited to the approved R09i and R14 sequencing decisions
- Closed R14b Issue #23, limited to its final accepted state and deferred runtime-composition boundary

### Existing activity contracts and persistence

- `internal/domain/entity/activity.go`
- `internal/domain/repository/room_activity_repository.go`
- `internal/infrastructure/persistence/postgres_room_activity_repository.go`
- nearby room-activity repository tests
- migration `0009_room_cutover_support.up.sql` and its down migration

### Mutation owners

- `internal/usecase/roomqueue/`
- `internal/usecase/roomvote/`
- `internal/usecase/roomautoqueue/`
- their existing tests
- `cmd/server/main.go`
- nearby server-composition tests

### Reference behavior only

Inspect only the activity-writing sections of the existing global queue, priority, and vote interactors. Do not refactor those global paths.

### Unrelated areas not to scan or refactor

- unrelated HTTP routes or WebSocket handlers
- Vue views, stores, composables, and frontend tests
- player leases and room lifecycle
- room chat
- authentication or session redesign
- unrelated migrations or cutover CLI code
- Docker, Nginx, TLS, and deployment files
- R14d, R14c, R14e, or later room slices

## Requirements

### 1. Narrow dependency injection

Add a small activity-writer dependency to each mutation owner that needs it. Prefer a local interface containing only:

```go
AddActivity(ctx context.Context, roomID int64, activity entity.Activity) error
```

The existing `RoomActivityRepository` and `PostgresRoomActivityRepository` must satisfy the dependency without broadening domain contracts.

Inject the dependency through constructors. Do not use globals, service locators, handler-owned business rules, or package-level mutable state.

### 2. Explicit no-op repository

Add an explicit `NoopRoomActivityRepository` implementing the complete existing `RoomActivityRepository` contract:

- `AddActivity` returns `nil`.
- `GetActivities` returns a non-nil empty slice and `nil` error.
- It performs no SQL.
- It starts no goroutine.
- It keeps no mutable state.
- Its comment must state that it is the intentional pre-R14c collision guard and not a production persistence implementation.

### 3. Pre-R14c server composition

In `cmd/server`:

- instantiate one explicit no-op room-activity repository;
- inject it into roomqueue, roomvote, and roomautoqueue composition;
- do not instantiate or select `PostgresRoomActivityRepository` for normal runtime mutation paths;
- do not add a server startup flag;
- do not read any `VITE_*` value in Go;
- do not inspect `room_cutover_marker` for runtime writer selection;
- do not execute or imply production cutover.

R14c owns the guarded switch to the real writer.

### 4. Actor identity

Human-authored activity names must come from backend-authenticated actor context, never from client-supplied IDs, roles, or names.

Pass the authenticated display name alongside the existing actor ID through internal call boundaries where required. Do not add wire fields.

When the authenticated display name is blank, use:

```text
user #<id>
```

System-authored activity uses the canonical actor name:

```text
System
```

### 5. Exact activity matrix

Use only the existing activity types.

| Trigger | Type | Actor | Description |
|---|---|---|---|
| Successful manual song add | `song_added` | authenticated actor | `added "<title>"` |
| Successful queue removal | `playback_changed` | authenticated actor | `removed "<title>" from queue` |
| Successful queue clear | `playback_changed` | authenticated actor | `cleared the queue` |
| Successful direct prioritize | `playback_changed` | authenticated actor | `prioritized "<title>"` |
| Successful playback status change | `playback_changed` | authenticated actor | `changed status to <status>` |
| Successful direct skip | `song_skipped` | authenticated actor | `skipped the current song` |
| Natural end advances to another song | `playback_changed` | `System` | `song finished playing` |
| Natural end finishes the queue | `playback_changed` | `System` | `queue finished playing` |
| Successful previous-song action | `playback_changed` | authenticated actor | `went to the previous song` |
| Successful auto-queue insertion | `song_added` | `System` | `added "<title>"` |
| Accepted vote ballot | `vote_cast` | authenticated actor | `voted to <type> "<title>" (<count>/<threshold>)` |
| Vote passes | `vote_passed` | `System` | `vote to <type> "<title>" passed` |
| Successful skip vote action | `song_skipped` | decisive ballot actor | `vote skipped "<title>"` |
| Successful prioritize vote action | `playback_changed` | decisive ballot actor | `vote prioritized "<title>"` |
| Vote expires | `vote_expired` | `System` | `vote to <type> "<title>" expired` |

`<type>` is the existing canonical vote type, such as `skip` or `prioritize`.

Do not introduce new activity types.

### 6. Explicit non-events

Do not append room activities for:

- reads or initial synchronization;
- elapsed-time synchronization;
- volume changes;
- auto-queue status reads or enable/disable configuration changes;
- rejected authorization or validation attempts;
- duplicate, stale, missing, or failed queue mutations;
- failed YouTube searches or metadata fetches;
- failed primary persistence;
- direct internal `roomqueue.SkipVote` or `roomqueue.PrioritizeVote` calls, because `roomvote` owns vote-resolution activity;
- room lifecycle, membership, invite, lease, or chat events;
- any global queue, global vote, or global auto-queue action.

### 7. Vote ordering and edge cases

Preserve the existing single room-vote session map, mutex, expiry loop, and single-instance semantics.

- Every accepted ballot produces exactly one `vote_cast` activity.
- A rejected ballot produces no activity.
- `vote_passed` and the resulting action activity are produced only when the queue action succeeds.
- When a ballot reaches threshold but the underlying queue target has become stale, record only the accepted `vote_cast`; do not record `vote_passed` or an action activity.
- When a new ballot encounters an already-expired session, record the old session's `vote_expired` before recording the replacement session's `vote_cast`.
- When the replacement ballot immediately passes, preserve this order:
  1. `vote_expired`
  2. `vote_cast`
  3. `vote_passed`
  4. successful vote action activity
- The shared expiry sweep emits at most one `vote_expired` activity for each expired session.
- Do not add a second vote map, mutex, ticker, worker, or detached activity goroutine.

### 8. Failure semantics

Room activity persistence is synchronous and best-effort after primary success.

- An `AddActivity` failure must not change a successful primary operation into an HTTP or WebSocket failure.
- Do not attempt to roll back an already-committed queue, vote, or auto-queue mutation.
- Continue writing subsequent ordered activities after one activity append fails.
- Log only safe context: room ID or slug, activity type, and error.
- Do not log OAuth data, tokens, invite values, full external-process output, or unnecessary personal data.

### 9. Locking and concurrency

No `AddActivity` call may occur while holding:

- the roomqueue mutation mutex;
- the roomvote session mutex;
- the roomautoqueue coordinator mutex.

Snapshot the immutable activity payload while the owner still has authoritative state, release the lock, then append activities sequentially.

Do not use detached goroutines to avoid lock ownership. Preserve existing room isolation, stale-target checks, and retry idempotency.

### 10. Persistence and compatibility

- Keep schema version 9.
- Add no migration.
- Do not modify the `room_activities` schema.
- Do not copy, delete, or retire global activities.
- Do not modify `room_cutover_marker`.
- Do not execute the cutover CLI.
- Do not claim cross-process ordering or safety; room vote state remains in-memory and single-instance.
- Preserve queue JSON, SQLite-history assumptions already superseded by the current PostgreSQL implementation, and all established PostgreSQL deployment behavior.

### 11. REST and WebSocket contracts

- Add no REST endpoint.
- Add no WebSocket event type.
- Change no request or response payload.
- Change no existing room event sequence or payload.
- Keep display-name propagation internal to authenticated delivery and use-case calls.

### 12. Frontend, configuration, and deployment

- No frontend changes.
- No `VITE_*` changes.
- No Docker, Nginx, TLS, Compose, or environment-file changes.
- No runtime cutover flag.

### 13. Documentation

This file is the authoritative R09i handoff.

During implementation, update only focused project-management state required to record:

- R09i implementation status;
- the short-lived branch and PR number;
- verification evidence;
- Architect findings and corrective commits;
- final Product Owner acceptance or rejection.

Do not rewrite ADR history or completed R14b history. Keep R14d, R14c, and R14e inactive.

## Branch and pull-request workflow

R09i must be developed through one short-lived feature branch and reviewed entirely through one pull request.

### Branch ownership

Create the branch from the latest `dev` head:

```text
sprint/r09i-room-activity-runtime-parity
```

The branch is dedicated exclusively to R09i.

- Do not include unrelated cleanup, formatting, dependency updates, or later-sprint work.
- Do not reuse a previous sprint branch.
- Do not branch from another unreviewed feature branch.

### Builder permissions

For R09i only, the Builder is authorized to:

- create the short-lived R09i branch;
- make focused commits on that branch;
- push that branch;
- open a pull request targeting `dev`;
- push corrective commits to the same branch after Architect review.

The Builder is not authorized to:

- commit directly to `dev`;
- merge, squash-merge, rebase-merge, or close the pull request;
- force-push or rewrite reviewed commit history unless the Product Owner explicitly approves it;
- change the pull-request base branch;
- execute production cutover;
- close room epic Issue #17;
- delete the branch before Product Owner acceptance and merge;
- activate or begin another sprint.

### Pull-request creation

After implementation and required verification, open one pull request:

```text
Base: dev
Head: sprint/r09i-room-activity-runtime-parity
```

Recommended title:

```text
R09i: Room activity runtime parity
```

The pull-request description must include:

1. The approved R09i goal.
2. Exact changed-file list grouped by domain/infrastructure, roomqueue, roomvote, roomautoqueue, server composition, tests, and project-management documentation.
3. Activity events implemented.
4. Explicit confirmation that pre-R14c composition uses the no-op writer.
5. Explicit confirmation that normal server startup does not select the real PostgreSQL room-activity repository.
6. REST, WebSocket, schema, frontend, deployment, and cutover contracts that remain unchanged.
7. Commands executed with exact pass/fail summaries.
8. PostgreSQL test environment used and any skipped verification.
9. Known limitations and remaining live checks.
10. Confirmation that R14d, R14c, R14e, and unrelated work remain inactive.

Builder summaries are not proof. The actual diff, commits, tests, and PR discussion are authoritative.

### Review and correction workflow

- The Architect reviews the actual PR diff and changed files.
- Findings are posted against the same PR and classified as Blocking, Important, Minor, or Observation.
- The Builder addresses approved findings through new corrective commits on the same branch.
- Do not create a replacement PR for normal corrections.
- Do not hide corrective history through force-pushing or squashing during review.
- Each corrective pass must report:
  - the response to every finding;
  - corrective commit SHA;
  - exact changed files;
  - focused verification evidence;
  - tests not rerun and why.
- The Architect re-reviews the updated PR.
- The review/correction cycle continues on the same PR until the Architect returns an acceptable verdict.

### Acceptance and merge ownership

Architect acceptance means the PR is ready for the Product Owner's decision. It does not merge or advance the sprint automatically.

Only the Product Owner may:

- formally accept R09i;
- choose the final merge strategy;
- merge the PR into `dev`;
- close the R09i tracking issue;
- delete the short-lived branch;
- approve activation of R14d or another sprint.

Until the Product Owner explicitly accepts and merges the PR:

- R09i remains active and pending acceptance;
- the PR remains open;
- R14d, R14c, R14e, and unrelated planned work remain inactive.

## Out of scope

- Room-activity read endpoint, WebSocket event, or frontend panel
- Selecting the real PostgreSQL writer during server startup
- Production cutover or marker-based runtime guard
- Runtime cutover startup flag
- Copying or retiring global activity data
- Global activity behavior changes
- Transactional outbox or coupling activity writes to primary mutations
- New activity types
- Room lifecycle, membership, invite, lease, or chat activities
- Schema, frontend, deployment, OAuth, or session redesign
- R14d, R14c, R14e, or later work

## Implementation guidance

1. Add the explicit no-op implementation and focused tests first.
2. Add narrow constructor-injected activity-writer dependencies to roomqueue, roomvote, and roomautoqueue.
3. Update only the authenticated delivery/use-case call sites that require actor display names.
4. Produce roomqueue activities from authoritative successful mutation results.
5. Produce ordered roomvote activity batches after releasing the vote mutex; keep roomqueue vote-action methods activity-free.
6. Produce the system `song_added` activity only after a successful auto-queue insertion.
7. Keep all activity writes synchronous, sequential, best-effort, and outside locks.
8. Compose the no-op writer in `cmd/server`.
9. Add positive, negative, ordering, failure, cross-room, and lock-release tests.
10. Update focused sprint state and PR references only.

Avoid broad constructor restructuring. Do not add an activity event dispatcher, async worker, secondary vote coordinator, or transport-owned activity rules.

## Verification points

### Behavior

Verify exact type, actor, description, room ID, and order for every positive activity case.

Verify no activity for every explicit non-event and failed/stale operation.

Verify:

- direct skip versus vote skip produces no duplicate activity;
- direct prioritize versus vote prioritize produces no duplicate activity;
- auto-queue activity occurs only after successful insertion;
- a stale threshold-reaching ballot records only `vote_cast`;
- expiry-on-entry ordering is exact;
- one failed activity write does not prevent later ordered writes;
- activity failure does not alter the successful primary result.

### Concurrency

Use blocking or asserting fakes to demonstrate:

- roomqueue releases its mutation mutex before activity persistence;
- roomvote releases its session mutex before activity persistence;
- a blocked activity write for one room does not prevent a vote operation in another room;
- roomautoqueue releases its coordinator mutex before activity persistence;
- no detached activity goroutine is created.

### Composition

Verify:

- the no-op implementation performs no SQL;
- normal `cmd/server` composition injects the no-op writer;
- normal startup does not instantiate or select the PostgreSQL room-activity repository for mutation owners;
- no runtime cutover flag, `VITE_*` read, or marker guard is introduced;
- the PostgreSQL repository remains directly usable in persistence tests.

### Commands

Run and report exact results:

```bash
go test -count=1 ./internal/usecase/roomqueue/... ./internal/usecase/roomvote/... ./internal/usecase/roomautoqueue/... ./cmd/server/...
go test -race -count=1 ./internal/usecase/roomqueue/... ./internal/usecase/roomvote/... ./internal/usecase/roomautoqueue/...
go test -count=1 ./internal/infrastructure/persistence/...
go test ./...
go test -race ./...
go vet ./...
git diff --check
git diff --name-only
git status --short
```

Use `LMQ_TEST_DATABASE_URL` for PostgreSQL-backed tests. Report exact skips and their reasons. No frontend command is required because frontend changes are out of scope.

## Risks and review focus

### Blocking-risk areas

- accidentally selecting the real PostgreSQL writer before R14c;
- holding queue, vote, or auto-queue locks during activity persistence;
- turning secondary activity failure into a false primary-operation failure;
- duplicate activity ownership between roomvote and roomqueue;
- activity records for stale or rejected actions;
- trusting client-supplied actor names;
- unsafe logging of sensitive values;
- REST or WebSocket contract drift;
- early runtime flags or cutover-marker behavior;
- scope expansion into read UI, deployment, lifecycle, or later slices.

### Review focus

The Architect must inspect the actual diff for:

- dependency direction and interface size;
- exact activity matrix and ordering;
- actor identity source;
- mutation ownership and duplicate prevention;
- lock-release evidence;
- best-effort failure behavior;
- explicit no-op server composition;
- unchanged REST/WebSocket/schema/frontend/deployment contracts;
- focused tests and no unrelated changes.

## Builder reasoning effort

**High.**

This sprint crosses use-case ownership, in-memory vote state, queue persistence, asynchronous auto-queue behavior, server composition, concurrency, and future cutover compatibility. The implementation must remain narrow despite the cross-package surface.

## Handoff prompt for Builder

You are implementing **R09i — Room activity runtime parity** for `hoanganh-ng/local-music-queue`.

The Product Owner has approved this sprint. Work only within the contract in this file. Do not advance or implement R14d, R14c, R14e, or unrelated work.

Start from the latest accepted `dev` head. Read only the required context listed above and the nearby tests for the files you change. Do not scan or refactor unrelated routes, Vue code, lifecycle, chat, authentication, migrations, cutover CLI, or deployment files.

Implement complete backend room-activity production parity for qualifying room queue, playback, vote, and auto-queue mutations. Use the existing activity types and exact actor/description matrix in this handoff. Add narrow constructor-injected activity-writer dependencies to the mutation owners. Human actor names must come from backend-authenticated context, with `user #<id>` as the blank-name fallback; system activity uses `System`.

Add an explicit `NoopRoomActivityRepository` that implements the full existing room-activity repository contract, performs no SQL, starts no goroutine, and returns a non-nil empty result for reads. In normal pre-R14c `cmd/server` composition, inject this no-op writer into roomqueue, roomvote, and roomautoqueue. Do not instantiate or select the real PostgreSQL room-activity repository for those runtime paths. Do not add a startup flag, read a `VITE_*` value in Go, inspect the cutover marker, or execute production cutover.

Activity persistence must be synchronous, sequential, best-effort, and outside all roomqueue, roomvote, and roomautoqueue mutexes. Do not create detached goroutines. An activity failure must not turn an already-successful primary mutation into a failure. Preserve existing stale-target rules, room isolation, vote-session ownership, expiry loop, REST payloads, WebSocket event names and payloads, schema version 9, frontend behavior, and deployment assumptions.

Roomvote owns vote-cast, pass, expiry, and vote-action activity. The direct internal roomqueue vote mutation methods must not append duplicate activity. Preserve the exact expiry/cast/pass/action ordering and the stale-threshold ballot behavior described in this handoff.

Add focused tests covering every positive activity, every explicit non-event, exact vote ordering, stale and failed operations, cross-room behavior, activity-write failure, duplicate prevention, and proof that every owner releases its mutex before calling the writer. Verify normal server composition uses the no-op writer and introduces no early cutover selection mechanism.

Run the required Go, race, vet, diff, and status commands. Use `LMQ_TEST_DATABASE_URL` for PostgreSQL-backed tests and report exact skips. Do not claim a command passed without evidence.

Work only on the short-lived branch:

```text
sprint/r09i-room-activity-runtime-parity
```

Create it from the latest `dev` head. For R09i only, focused commits and pushing this branch are authorized. Do not commit directly to `dev`.

After implementation and verification, open one pull request:

```text
Base: dev
Head: sprint/r09i-room-activity-runtime-parity
Title: R09i: Room activity runtime parity
```

Include the exact scope, grouped changed-file list, implemented activity matrix, contract-preservation statements, verification evidence, skips, limitations, and explicit confirmation that pre-R14c server composition still selects the no-op writer and cannot write runtime room activities.

Architect review and all Builder corrective passes will occur on that same pull request. Address review findings with additional focused commits on the existing branch and report each corrective commit and its verification. Do not replace the PR, force-push reviewed history, squash during review, merge, close the PR, delete the branch, execute production cutover, close Issue #17, or advance another sprint.

The Product Owner retains final acceptance, merge strategy, merge, issue closure, branch deletion, and sprint-advancement authority.
