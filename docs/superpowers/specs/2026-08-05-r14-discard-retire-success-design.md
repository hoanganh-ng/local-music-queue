# R14 Discard-and-Retire Success Design

**Status:** Product Owner-approved design; repository review pending
**Date:** 2026-08-05
**Branch baseline:** `dev` at `723023fd296678bac8aec0ace784ff2e94bb119f`
**Parent epic:** Issue #17
**Architecture authority:** ADR 004 — Discard legacy global state at room cutover

## Purpose

Define the smallest safe path that completes the core room feature in Issue #17 under the accepted discard-and-retire direction. The design replaces the obsolete migrated-room activation proof, preserves rollback controls, creates a clean Gate 2 lifecycle, and leaves deferred product expansions outside the core epic closure boundary.

This document defines architecture and acceptance boundaries. It does not activate a sprint, authorize implementation, authorize preflight, or authorize production work.

## Repository state motivating the design

At the design baseline:

- PR #34 has been squash-merged into `dev` as `723023fd296678bac8aec0ace784ff2e94bb119f`.
- Sprint 036 shaped and authorized a later implementation-correction sprint, but the live project trackers still show Sprint 036 as active and awaiting acceptance.
- Sprint 031 remains paused and targets the non-executable migrate-and-retire operational package.
- The authoritative startup guard requires schema version at least 9 and `room_cutover_marker.id = 1`.
- `room_cutover_marker` proves a copied legacy-state migration into a target room, which conflicts with ADR 004.
- The backend image still packages the historical `room-cutover` command.
- Production cutover has not occurred, the true/true pair is undeployed, legacy global tables remain untouched, and R14e remains inactive.

The first delivery after this design must therefore reconcile sprint authority before implementation begins.

## Locked product and architecture decisions

### Discard-and-retire meaning

Room-authoritative operation begins without inherited legacy playback state:

- no migrated, default, or hidden room is created;
- no legacy global queue, activity, auto-queue, or play-history state is copied into a room;
- existing room-scoped rooms and product data are not deleted, reset, or modified by activation;
- legacy global tables remain untouched throughout the rollback window;
- destructive cleanup remains a later R14e action.

### Dedicated activation proof

Create a new durable discard-and-retire activation record. Do not repurpose or synthesize `room_cutover_marker` data.

`room_cutover_marker` and `cmd/room-cutover` remain accepted historical migrate-and-retire artifacts. They are not reused as current activation proof.

### Migration numbering

- Migration `0010` is assigned to the new discard-and-retire activation proof.
- R14e destructive cleanup moves from migration `0010` to migration `0011`.
- The correction sprint updates all authoritative R14e references accordingly but does not implement R14e.

### Gate 2 lifecycle

Sprint 031 will not resume. After the correction implementation is accepted and integrated, Sprint 031 is closed as **superseded, not completed**, and a new discard-and-retire Gate 2 preflight successor is created with fresh operational documents and readiness state.

## Correction sprint boundary

### Success outcome

The correction sprint succeeds when this statement is true:

> A clean production database can be explicitly prepared for room-authoritative operation without creating a room, copying legacy state, or falsifying migration evidence; the server then fails closed unless that preparation is durably proven.

### Included scope

1. Migration `0010` and the durable activation record.
2. A shared activation preparation and verification component.
3. A thin offline `room-activation` operator command.
4. Startup-guard correction for authoritative mode.
5. Operator-image packaging and backend-image containment.
6. Seven new discard-and-retire operational documents.
7. R14e migration-reference renumbering to `0011`.
8. Automated, PostgreSQL-backed, packaging, Compose, and rehearsal evidence.

### Explicit exclusions

The correction sprint does not:

- execute production preflight or production cutover;
- deploy the true/true pair;
- sign or declare Product Owner GO;
- delete legacy tables or historical marker data;
- implement R14e;
- change room product behavior;
- add chat, discovery, authorization, retention, hard-delete, or lifecycle expansions;
- close Issue #17.

## Durable activation-record contract

Migration `0010` creates a single-row table named `room_authoritative_activation`.

The durable record contains only:

- `id`, constrained to `1`;
- `activation_id`, an operator-generated UUID;
- fixed mode `discard-and-retire`;
- activation contract version, initially `1`;
- schema version at preparation, exactly `10`;
- immutable preparation timestamp;
- non-empty release or build SHA for audit provenance.

The record contains no target room ID, room slug, host ID, source hashes, target hashes, legacy ID offset, copied-row count, or other migrated-room concept.

The database schema should enforce the fixed single-row and fixed-mode invariants where practical. Application verification remains fail-closed even if corruption bypasses constraints.

Applying migration `0010` only makes activation proof possible. It does not activate authoritative mode and does not write the activation row.

## Shared activation component

Create one focused internal component responsible for:

- validating preparation inputs;
- reading clean migration state;
- transactionally preparing the activation record;
- verifying the stored record;
- detecting conflicting repeats;
- rejecting the historical copy marker;
- producing typed, non-sensitive results and failures.

Both `cmd/room-activation` and the server startup guard use this component. Neither maintains independent SQL or validity rules.

The component depends only on PostgreSQL access and migration-state inspection. It must not depend on room repositories, queue services, HTTP handlers, WebSockets, frontend code, or user-session behavior.

## Operator command contract

Create `cmd/room-activation` with two subcommands:

```text
room-activation prepare
room-activation verify
```

### Connection supply

The command reads PostgreSQL connection data only from `DATABASE_URL` or `MIGRATE_DATABASE_URL`.

It must not provide a DSN command-line flag because process arguments can be exposed through process listings and operational evidence.

### Prepare inputs

`prepare` requires:

- `--activation-id <uuid>`;
- `--build-sha <immutable-release-sha>`.

### Prepare semantics

`prepare` must:

1. require a clean migration state at exactly schema version `10`;
2. reject an existing `room_cutover_marker.id = 1`;
3. insert only the activation row inside one transaction;
4. create no room and modify no legacy or room-scoped product data;
5. treat an exact repeat using the same immutable identity as a verified no-op;
6. reject a different activation ID or any conflicting immutable field;
7. provide no reset, force, overwrite, delete, bypass, or ignore-conflict operation.

The server never prepares activation automatically.

### Verify semantics

`verify` is read-only and accepts no expected activation identity. It reads the authoritative stored record and confirms:

- migration state is clean;
- schema version is at least `10`;
- exactly one activation row exists;
- mode is exactly `discard-and-retire`;
- activation contract version is supported;
- activation ID is valid;
- preparation schema version is exactly `10`;
- build provenance is non-empty;
- `room_cutover_marker.id = 1` does not exist.

### Exit codes

- `0`: preparation or verification succeeded, including an exact idempotent repeat;
- `1`: database, migration-state, consistency, conflict, or verification failure;
- `2`: invalid command, malformed or missing input, unknown flag, or unsupported argument.

### Evidence output

Human-readable output may include:

- outcome status;
- activation ID;
- activation mode and contract version;
- current and preparation schema versions;
- preparation timestamp;
- build SHA;
- named verification checks.

Output and errors must not include:

- DSNs or credentials;
- raw environment-variable values;
- user identities, emails, or session data;
- legacy or room queue contents;
- copied-state hashes from the historical marker.

Historical-marker conflicts direct the operator to stop and investigate. No automated repair is offered.

## Startup-guard contract

The existing server-facing startup guard remains the entry point used by authoritative mode, but delegates activation validity to the shared verifier.

With `--room-cutover-authoritative=true`, startup requires:

- clean migration state;
- schema version at least `10`;
- a valid discard-and-retire activation record;
- absence of `room_cutover_marker.id = 1`.

Any failure occurs before HTTP or WebSocket listeners open and exits the process non-zero.

The guard is read-only. It never:

- applies migrations;
- prepares activation;
- repairs data;
- rewrites proof;
- invokes operator commands.

With `--room-cutover-authoritative=false`:

- the activation record is not required;
- schema version `10` remains compatible;
- false/false rollback does not require a down migration.

The stored build SHA is audit provenance, not a permanent binary lock. Later true-mode patch releases do not rewrite the immutable activation record.

## Packaging architecture

### Shared boundary

The command and startup guard share validation behavior but not execution authority:

- the operator command may write the one activation row;
- the server may only read and validate it;
- neither may modify product data.

### Operator image

Extend the existing short-lived migration image so it contains:

- `/app/migrate-schema`;
- `/app/room-activation`.

Its default command remains `/app/migrate-schema up` so ordinary `docker compose up` cannot prepare activation automatically.

Activation requires an explicit operator override such as:

```text
docker compose run --rm db-init /app/room-activation prepare ...
```

### Backend image

The long-running backend image contains only the server and its normal runtime dependencies.

It must not contain:

- `/app/room-cutover`;
- `/app/room-activation`.

The historical `cmd/room-cutover` source and tests remain in the repository but are built into no current production artifact and appear in no executable discard-and-retire runbook.

## Operational-document architecture

Create seven new documents rather than overwriting the migrate-and-retire historical set:

```text
documents/07-deployment/room-discard-cutover-runbook.md
documents/07-deployment/room-discard-cutover-gate2-readiness.md
documents/07-deployment/room-discard-cutover-gate2-inputs.template.md
documents/07-deployment/room-discard-cutover-gate2-preflight-ledger.md
documents/07-deployment/room-discard-cutover-gate2-evidence-return.template.md
documents/07-deployment/room-discard-cutover-gate2-discrepancy-register.md
documents/07-deployment/room-discard-cutover-gate2-go-no-go-packet.md
```

The original `room-cutover-*` documents remain unchanged, visibly non-executable, and preserved as historical migrate-and-retire evidence.

The new procedure contains only:

- protected operator input supply;
- closed traffic;
- mandatory database backup and verified retention;
- schema migration `0010`;
- explicit `room-activation prepare` and `verify`;
- paired true/true deployment;
- smoke and contract verification;
- false/false rollback;
- legacy-table preservation throughout the rollback window.

It contains no legacy-to-room copy step and no invocation of `room-cutover`.

## Gate 2 successor lifecycle

After the correction sprint is accepted and integrated:

1. close the correction sprint through a lifecycle transition;
2. close Sprint 031 as superseded, not completed;
3. preserve Sprint 031 and its seven historical documents unchanged;
4. activate a new discard-and-retire Gate 2 preflight successor from the merged correction base.

The successor begins with:

- every blocker `Not started`;
- every preflight check `Not started`;
- every entry criterion `Not started`;
- empty evidence inventory;
- empty discrepancy register;
- unsigned decision fields;
- recommendation `DEFER / NOT READY`.

No old readiness status carries forward automatically.

Accepted controls that remain relevant are copied with fresh traceability, including backup retention, artifact custody, protected connection supply, closed-traffic execution, paired deployment, rollback preservation, secret-redacted evidence, abort rules, and discrepancy handling.

Copy-specific checks are replaced with activation-specific checks, including:

- migration `0010` applied cleanly;
- `room-activation prepare` succeeded;
- `room-activation verify` succeeded;
- no historical copy marker exists;
- non-destructive data proof is accepted;
- backend artifact excludes both operator commands;
- operator artifact contains `room-activation`;
- true startup succeeds only with valid activation proof;
- false/false rollback remains executable.

### Decision sequence

The accepted three-pass governance remains mandatory:

1. human Operator evidence pass;
2. Architect review pass, with outcome limited to:
   - `READY FOR PO DECISION`;
   - `DEFER — EVIDENCE INCOMPLETE`;
   - `NO-GO RECOMMENDED`;
3. Product Owner `GO`, `NO-GO`, or `DEFER` decision.

A GO recorded before Architect review is invalid. The named Product Owner/Operator combined-role exception remains limited to R14 Gate 2 as already accepted. AI agents and Builders remain prohibited from production, preflight, credential, deployment, cutover, or rollback execution.

## Verification and acceptance evidence

### Migration tests

PostgreSQL-backed tests prove:

- migration `0010` creates only the activation table and constraints;
- applying it to version 9 preserves all existing tables and rows;
- its down migration removes only the activation table;
- up → down → up remains clean;
- migration bookkeeping reports version `10`, `dirty=false`.

### Shared-component tests

Real PostgreSQL tests cover:

- first preparation succeeds;
- exact repeated preparation is a verified no-op;
- different activation ID fails;
- different build SHA fails;
- missing record fails;
- malformed stored UUID fails;
- unsupported mode fails;
- unsupported contract version fails;
- preparation schema other than `10` fails;
- dirty migration state fails;
- schema below `10` fails;
- historical copy marker fails;
- database and transaction failures return non-sensitive errors.

### Non-destructive integration proof

A focused integration test seeds representative legacy global data and existing room-scoped data, runs activation preparation, and proves:

- no room was created;
- no existing room was changed;
- no queue, activity, auto-queue, history, membership, or playback data changed;
- only migration bookkeeping and the single activation record changed.

This proves that discard means no inheritance, not deletion.

### CLI tests

Tests prove:

- `prepare` and `verify` dispatch correctly;
- malformed or missing inputs return `2`;
- operational failures return `1`;
- success and exact repeat return `0`;
- unknown flags and positional arguments are rejected;
- no DSN flag exists;
- output includes required evidence fields;
- output and errors redact connection secrets;
- no force, reset, overwrite, delete, or bypass command exists.

### Startup tests

Tests prove:

- true mode with valid proof proceeds;
- true mode with missing, malformed, or conflicting proof stops before listeners;
- true mode with a historical marker stops;
- false mode without proof remains valid;
- false mode at schema version `10` remains rollback-compatible;
- startup verification performs no writes.

### Packaging and Compose evidence

Artifact inspection proves:

- backend image contains `server`;
- backend image contains neither `room-cutover` nor `room-activation`;
- operator image contains `migrate-schema` and `room-activation`;
- operator-image default remains schema migration;
- the existing single `ROOM_CUTOVER_AUTHORITATIVE` input continues to produce matched true/true and false/false server/SPA pairs.

### Required Builder evidence

The Builder reports exact output for at least:

```text
go test ./...
go vet ./...
go test -race <affected activation, persistence, and server packages>
docker compose config
docker build -t <backend-test-image> -f Dockerfile .
docker build -t <operator-test-image> -f Dockerfile.migrate .
git diff --check <base>...HEAD
git diff --name-only <base>...HEAD
git status --short
```

Affected PostgreSQL-backed tests must run against a reachable disposable PostgreSQL instance. Skipped database tests are not sufficient acceptance evidence.

Repository-required GitNexus impact analysis must be recorded before changing affected symbols. `detect_changes()` evidence must be recorded before Builder commits.

## End-to-end success sequence

### Stage 1 — Lifecycle reconciliation and correction activation

A documentation-only delivery:

- closes Sprint 036 as accepted;
- records PR #34 integration;
- creates the implementation-correction sprint record from the then-current `dev` SHA;
- records this approved design as its architecture boundary;
- activates only that correction sprint;
- grants explicit Builder authorization.

### Stage 2 — Correction implementation

The Builder delivers the approved implementation and new operational documents through an unmerged draft pull request with complete evidence. Acceptance makes the mechanism executable but does not change production.

### Stage 3 — Correction lifecycle transition

After squash merge:

- close the correction sprint;
- close Sprint 031 as superseded, not completed;
- record the accepted implementation SHA;
- preserve historical artifacts;
- activate no operational sprint unless separately authorized.

### Stage 4 — New Gate 2 preflight

Activate the discard-and-retire successor. The human Operator completes all inputs, rehearsals, artifact custody, backup, rollback, activation, pairing, and evidence requirements. The Architect reviews the returned evidence. The Product Owner decides GO, NO-GO, or DEFER.

No production action occurs inside the preflight sprint.

### Stage 5 — Separately authorized production cutover and rollback window

Only after a valid Product Owner GO:

- close traffic;
- create and verify the production backup;
- apply migration `0010`;
- prepare and verify activation;
- deploy the paired true server and true frontend;
- run smoke and contract checks;
- reopen traffic only after all conditions pass.

Throughout the rollback window:

- legacy tables remain untouched;
- false/false artifacts remain protected and deployable;
- discrepancies remain recorded;
- abort conditions trigger immediate rollback.

### Stage 6 — R14e cleanup and Issue #17 closure

After the rollback window is completed and accepted:

- activate R14e;
- use migration `0011` for approved cleanup;
- remove obsolete global implementation and packaging surfaces;
- publish final API and architecture documentation;
- verify no legacy global route can regain authority;
- perform final Issue #17 acceptance review.

Issue #17 closes only when:

- room-scoped operation is the only active product model;
- production runs the accepted true/true pair;
- discard activation is durably proven;
- no legacy state was inherited into a room;
- rollback-window obligations are complete;
- R14e cleanup is accepted;
- final documentation matches implementation;
- no Blocking or Important findings remain.

R10f+, R11b+, R12, and R13 remain outside this core closure boundary unless the Product Owner explicitly reintroduces them.

## Consequences and trade-offs

### Benefits

- Activation evidence remains truthful and auditable.
- The server stays fail-closed without depending on copied-room state.
- The write-capable operator tool is separated from the long-running backend image.
- False/false rollback remains possible at schema version 10.
- Historical accepted work remains available for audit without remaining executable.
- Gate 2 evidence cannot accidentally mix old and new cutover contracts.
- Each lifecycle stage is independently reviewable and reversible until production authorization.

### Costs

- One new migration and one new operator command are required.
- R14e migration references must move to `0011`.
- Seven new operational documents are created rather than editing the old set.
- A separate lifecycle transition is required before implementation and again after merge.

These costs are accepted because they prevent false audit evidence, mixed operational authority, and ambiguous rollback behavior.

## Non-goals

This design does not reopen settled room architecture, redesign authentication, add product features, alter room domain contracts, or authorize production operations. It addresses only the discard-and-retire correction and the shortest safe path to the core Issue #17 completion boundary.
