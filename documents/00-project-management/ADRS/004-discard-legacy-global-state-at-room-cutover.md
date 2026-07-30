# ADR 004 — Discard legacy global state at room cutover

- Status: Approved product direction — Product Owner decision 2026-07-30 (Issue #17 comment #5128855220); documented through Sprint 034 — R14 Discard-and-Retire Contract Amendment ([`../SPRINTS/034-r14-discard-and-retire-contract-amendment.md`](../SPRINTS/034-r14-discard-and-retire-contract-amendment.md)), delivered as an unmerged draft PR pending Architect review and Product Owner acceptance
- Date: 2026-07-30
- Scope: Replaces the migrate-and-retire cutover contract (ADR 003 and its R14b/R14c production assumptions) with a **discard-and-retire** contract for the R14 production cutover. ADR 003 remains a historical record; only the portions listed in Section 4 are superseded. ADR 004 is documentation-only: it authorizes no code, schema, migration, Docker, frontend, runtime, preflight, production, cutover, rollback, or R14e action.

## 1. Context

The accepted migrate-and-retire contract (ADR 003, R14a; implemented offline by R14b; production-coordinated by R14c) planned to convert the legacy global state — the singleton `queue_state`, append-only `activities`, singleton `auto_queue_config`, and `play_history` — into exactly one Product-Owner-named room via `cmd/room-cutover up`, record the `room_cutover_marker` row as durable activation proof, and then retire the legacy global REST/WS contracts with `410 Gone` tombstones.

R14c Gate 1 (implementation, isolated rehearsal, runbook) is integrated and closed on `dev`. Gate 2 (production execution) was being prepared through the Sprint 030 readiness package and Sprint 031 preflight scaffold; every blocker, preflight check, and entry criterion remains `Not started`, and the unsigned recommendation remains `DEFER / NOT READY`. No production cutover has been executed.

On 2026-07-30 the Product Owner decided (Issue #17 comment #5128855220) that migrating the legacy global playback state into a room is no longer the desired product outcome. The room-authoritative product should activate clean, without inherited playback state.

## 2. Decision

The R14 production cutover follows **discard-and-retire**:

1. **No migrated room.** No migrated, default, or hidden room is created for legacy state (consistent with ADR 001 §3 Decision 2 and Epic #17).
2. **No legacy state copy.** Legacy global queue state, activities, auto-queue configuration, and play history are **not** copied into any room.
3. **Clean activation.** Room-authoritative operation begins with no inherited playback state. Users create or join ordinary rooms through the accepted room flows (R04/R05b room entry, invites, membership). "No inherited playback state" means no state copied from the legacy global tables. ADR 004 does not authorize deleting or modifying any pre-existing room-scoped room, membership, queue, activity, auto-queue, play-history, lease, invite, or chat data.
4. **Retirement unchanged in intent.** The legacy global REST/WS contracts are still retired at cutover (the `410 Gone` tombstone model of ADR 003 §3.1/§3.4 remains the retirement shape); what changes is that no data migration precedes the retirement.
5. **Rollback preserved.** The mandatory pre-cutover `pg_dump` (retained ≥ 30 days) and the recorded false/false server/SPA rollback pair remain required for rollback and audit throughout the rollback window.
6. **Legacy tables untouched.** The legacy global tables remain unchanged throughout the rollback window. Destructive cleanup (R14e — migration 0010, schema version 10, legacy-table deletion) remains a later, separately approved action and is NOT authorized by this ADR.

## 3. What is preserved unchanged

- Mandatory pre-cutover database backup and verified `<BACKUP_LOCATION>` retention.
- The recorded false/false rollback pair (`--room-cutover-authoritative=false` server + `VITE_ROOM_CUTOVER_AUTHORITATIVE=false` SPA), protected rollback tags, and durable rollback archives.
- Closed-traffic maintenance-window execution and paired server/SPA deployment (no mixed-mode deployment).
- Fail-closed controls: the true server must refuse to serve unless its activation preconditions are proven (see Section 5 for the unresolved proof).
- The mandatory Operator evidence → Architect review → Product Owner decision sequence (Sprint 032 governance, including the named `hoanganh-ng` combined-role exception for R14c Gate 2 only).
- The prohibition on any AI Builder performing production, preflight, credential, or deployment actions.
- Accepted R14b and R14c Gate 1 work as **historical records**: the merged code, tests, rehearsal evidence, and documentation are not reverted or rewritten.

## 4. Supersession

ADR 004 supersedes the **migrate-and-retire production assumptions** of the following, while each remains a historical record:

- **ADR 003** — the product assumption that the legacy global state is converted into exactly one Product-Owner-named room (§5 migration identity contract, §6-onward copy semantics, and every statement that the production cutover executes the `cmd/room-cutover` copy). ADR 003's supersession of ADR 001/ADR 002 paragraphs, its `410 Gone` retirement shape, its migration-file ownership rules, and its historical analysis remain valid history.
- **R14b (Sprint 026)** — the accepted `cmd/room-cutover` copy mechanism is declared **unsuitable for the revised production cutover**: its purpose was the legacy-state copy that is no longer wanted. The merged mechanism, its offline verification, and its schema work (migration 0009, `room_activities`, `room_cutover_marker`) remain accepted historical artifacts on `dev`.
- **R14c (Sprint 029)** — every production-window step that runs the `cmd/room-cutover` copy against the live database, and every readiness statement premised on that copy. R14c Gate 1's runtime flag, startup-guard concept, activity-writer composition, tombstone handlers, and deployment pairing remain integrated and are expected to be **reused where still valid** by the implementation-correction sprint (Section 6).
- **The Gate 2 operational surface** — the runbook (`documents/07-deployment/room-cutover-runbook.md`) and the Gate 2 package (readiness, inputs template, preflight ledger, evidence-return template, discrepancy register, go/no-go packet) are marked **non-executable** under the current migrate-and-retire implementation. They must not be executed and no readiness row may advance until a later implementation-correction sprint is accepted and integrated and the revised Gate 2 preflight is separately resumed.

## 5. Unresolved activation proof

The current fail-closed startup guard requires schema version ≥ 9 **and** `room_cutover_marker.id = 1` before a `--room-cutover-authoritative=true` server opens listeners. Under ADR 003, that marker row was inserted in the same transaction as the migrated-room copy — the marker **proves a migrated-room copy that will no longer happen**.

This is explicitly unresolved by ADR 004. A later implementation-correction sprint must define and implement a revised activation proof for discard-and-retire (and the matching revised guard semantics) before any revised Gate 2 preflight can resume. ADR 004 intentionally does not design that proof.

## 6. Required next implementation sprint

Before any revised Gate 2 preflight may resume, a dedicated **discard-and-retire implementation-correction sprint** must be shaped, approved, implemented, reviewed, and accepted. Its required outcomes (identification only — no Builder-level design is prescribed here):

1. a revised activation proof and startup-guard semantics that do not depend on a migrated-room copy (Section 5);
2. a revised production cutover procedure with no legacy-state copy step;
3. revised runbook, readiness, and preflight surfaces replacing the non-executable migrate-and-retire versions;
4. an explicit decision on the disposition of the now-unsuitable `cmd/room-cutover` copy path and the `room_cutover_marker` contract;
5. preservation of every control in Section 3.

That sprint is NOT active, NOT shaped, and NOT authorized by ADR 004; it requires its own Product Owner approval.

## 7. Consequences

- Sprint 034 is the sole active sprint; Sprint 031 — R14c Gate 2 Preflight — is **paused** (not closed, not superseded) and must not proceed under the current documents.
- All B1–B6, PF-01–PF-15, and E1–E10 ledger rows remain `Not started`; the B7 GO record remains absent; the evidence and discrepancy inventories remain empty; all decision fields remain unsigned; the recommendation remains `DEFER / NOT READY`.
- Gate 2 remains pending and unauthorized. Production remains untouched. The `true` server/SPA pair remains undeployed. Legacy global tables remain untouched. R14e remains inactive.
- No code, migration, schema, test, Docker, frontend, runtime, preflight, production, credential, backup, host, database, cutover, or rollback action is authorized by this ADR.
