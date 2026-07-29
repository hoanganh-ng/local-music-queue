# R14c Gate 2 — Preflight Status Ledger (Redacted)

**Sprint:** 031 (R14c Gate 2 Preflight and Go/No-Go Preparation) — `documents/00-project-management/SPRINTS/031-r14c-gate2-preflight.md`
**Overlays:** the accepted readiness package [`room-cutover-gate2-readiness.md`](./room-cutover-gate2-readiness.md) and runbook [`room-cutover-runbook.md`](./room-cutover-runbook.md); it tracks their status but changes neither.
**Status:** Tracking document. **Nothing here is executed and nothing is resolved.** Every blocker, preflight check, and entry criterion below is initialized OPEN / NOT MET. Gate 2 production execution remains explicitly pending and unauthorized; a row moves off OPEN / NOT MET only when a redacted human-Operator evidence reference is recorded and the Product Owner accepts it — never by the Builder.

## How to use this ledger

- This is the live status surface the Product Owner and Operator consult when preparing the Gate 2 go/no-go review. It restates each tracked item from the readiness package and runbook and adds status, owner, evidence-reference, and updated-on columns.
- **Redaction rule (mandatory).** The `Evidence reference` column holds **only** a redacted pointer to where the human Operator recorded the evidence (e.g. "evidence-return §PF-05, attested t, 2026-__-__") plus a boolean/attestation outcome. It must **never** contain a raw value, DSN, password, hostname, file path on the deployment host, image ID/digest, commit SHA, the target slug, the host user id, or the live Google account address. All such values live only in the operator-local input file and the protected connection bundle, off-repository.
- **Status vocabulary.** `OPEN` / `NOT MET` (default), `IN PROGRESS` (Operator working it, no accepted evidence yet), `EVIDENCE RETURNED` (redacted evidence submitted, awaiting Product Owner acceptance), `MET` (Product Owner accepted the redacted evidence). The Builder may only ever record `OPEN` / `NOT MET`.
- **No self-clearing.** No item is marked `MET` without a redacted human-Operator evidence reference accepted by the Product Owner. The Builder does not mark any item resolved and does not declare GO.
- Any discrepancy found while working an item is logged in the [operational discrepancy register](./room-cutover-gate2-discrepancy-register.md) and cross-referenced here.

## 1. Blockers — B1–B6

Source: readiness package Section 5. B7 (GO record absent) is the review outcome itself and is tracked in the [go/no-go packet](./room-cutover-gate2-go-no-go-packet.md), not here. Resolving B1–B6 is the precondition for convening the GO / NO-GO review.

| ID | Blocker (readiness §5) | Status | Owner | Evidence reference (redacted only) | Updated |
|---|---|---|---|---|---|
| B1 | Roles unassigned — no named Operator or Product Owner-of-record; Scribe optional/unassigned | OPEN | Product Owner | *(none)* | — |
| B2 | Operator inputs unapproved — none of the six Section 2.1 inputs has an approved value; live account holder availability unconfirmed | OPEN | Product Owner | *(none)* | — |
| B3 | Window not scheduled — `<MAINTENANCE_WINDOW>` not agreed; closure/announcement channels not identified | OPEN | Product Owner | *(none)* | — |
| B4 | Backup posture unverified — `<BACKUP_LOCATION>` writability, ≥ 30-day retention, restore-readability not demonstrated | OPEN | Operator | *(none)* | — |
| B5 | Snapshot infrastructure not provisioned — snapshot + isolated DSNs and the protected connection bundle do not exist | OPEN | Operator | *(none)* | — |
| B6 | Deployment-host commit not confirmed — `<REVIEWED_COMMIT_SHA>` not declared or verified on `<PROD_HOST>` | OPEN | Operator (verifies), Product Owner (declares) | *(none)* | — |

## 2. Preflight checks — 1–15

Source: readiness package Section 6 (the Product Owner verification view) mapped to the runbook pre-window checklist steps 1–14. Every line must be answerable "yes, with evidence" before the window opens. All rows default NOT MET.

| # | Preflight check (readiness §6) | Runbook step | Status | Owner | Evidence reference (redacted only) | Updated |
|---|---|---|---|---|---|---|
| PF-01 | Deployment host `git rev-parse HEAD` equals `<REVIEWED_COMMIT_SHA>` | 1 | NOT MET | Operator | *(none)* | — |
| PF-02 | False/false rollback pair captured from running containers by immutable ID+digest, pinned under `rollback-r14c` tags, archived, re-verified — before any rebuild/prune | 2 | NOT MET | Operator | *(none)* | — |
| PF-03 | `<EVIDENCE_DIR>` exists, mode `0700`, outside repo and web-served paths | 3 | NOT MET | Operator | *(none)* | — |
| PF-04 | `<TARGET_ROOM_SLUG>` verified unused and non-reserved | 4 | NOT MET | Operator | *(none)* | — |
| PF-05 | `<HOST_USER_ID>` existence check returns exactly one row, boolean output only, via `PGSERVICE=r14c-snapshot` — no DSN in argv | 5 | NOT MET | Operator | *(none)* | — |
| PF-06 | `<LIVE_ALLOWED_GOOGLE_ACCOUNT>` → `<HOST_USER_ID>` equality confirmed via `read -rs`/`\getenv`/`:'email'`; result `t`; neither address nor DSN on any command line or file | 6 | NOT MET | Operator | *(none)* | — |
| PF-07 | Login for the live account confirmed on the current false deployment, distinguishing login eligibility, account-level `users.role`, and room-host membership | 8 | NOT MET | Operator | *(none)* | — |
| PF-08 | Allowlisted pairing evidence (`pairing-false.txt` / `pairing-true.txt`) extracted and reviewed; each extract pairs backend+frontend in the same mode; only image-reference and authoritative-flag lines — no env block, credential, or full render | 9 | NOT MET | Operator | *(none)* | — |
| PF-09 | Both artifact pairs built from `<REVIEWED_COMMIT_SHA>`; backend image contains `/app/server` and `/app/room-cutover`; `:true` pair IDs recorded | 10 | NOT MET | Operator | *(none)* | — |
| PF-10 | Snapshot `plan` and isolated `up --dry-run` rehearsals completed through the packaged `:true` backend image with the inline `ROOM_CUTOVER_AUTHORITATIVE=true` prefix, DSNs supplied per readiness §2.4 (per-command `DATABASE_URL`, name-only `-e DATABASE_URL`, `--postgres` never used); reports durable, `0600`, PII-free, counts plausible and mutually consistent; R14b first-cutover readiness proven (target slug absent, `room_activities` empty, exactly one `queue_state` row `id=1` and one `auto_queue_config` row `id=1`, missing singleton fails up front) | 11–12 | NOT MET | Operator | *(none)* | — |
| PF-11 | Gate 1 isolated end-to-end rehearsal evidence accepted | 13 | NOT MET | Operator | *(none)* | — |
| PF-12 | `<BACKUP_LOCATION>` writable, verified, ≥ 30-day retention | 14 | NOT MET | Operator | *(none)* | — |
| PF-13 | PostgreSQL migration state clean and schema version exactly 9; identity-check client supports `\getenv` (psql ≥ 15; deployment runs PostgreSQL 16) | — | NOT MET | Operator | *(none)* | — |
| PF-14 | Rollback and abort rules (readiness §8; runbook Rollback) read aloud and acknowledged by Operator and Product Owner | — | NOT MET | Operator + Product Owner | *(none)* | — |
| PF-15 | Protected connection bundle in place (`<PGSERVICE_FILE>`, `<PGPASS_FILE>`, `<CONNECTION_ENV_FILE>`, each `0600`, outside repo/web paths); `<CONNECTION_ENV_FILE>` holds plain non-`export` assignments only; spot-check confirms no planned command carries a DSN/credential in argv (`--postgres` unused everywhere) | 5–12 | NOT MET | Operator | *(none)* | — |

## 3. Entry criteria — E1–E10

Source: readiness package Section 9. These are the Product Owner GO / NO-GO / DEFER checklist criteria; every one must be met before Gate 2 may be entered. All rows default NOT MET. The decision itself is recorded in the readiness package Section 9 and mirrored in the [go/no-go packet](./room-cutover-gate2-go-no-go-packet.md).

| # | Entry criterion (readiness §9) | Depends on | Status | Evidence reference (redacted only) | Updated |
|---|---|---|---|---|---|
| E1 | Gate 1 integrated: PR #26 squash-merged into `dev` as `c8ab4af029d10dda889d1165464e16068a5be573`; runbook accepted | (satisfied on `dev`) | NOT MET | Confirmed on `dev` per PROJECT_STATE.md; Product Owner records acceptance at review | — |
| E2 | All six operator inputs approved, supplied securely, recorded in the operator-local input file; protected connection bundle in place; no command supplies a DSN/credential via argv | B2, B5, PF-15 | NOT MET | *(none)* | — |
| E3 | Roles assigned: named Operator and Product Owner-of-record; Operator ≠ Product Owner | B1 | NOT MET | *(none)* | — |
| E4 | `<MAINTENANCE_WINDOW>` scheduled with announcement plan for closing and reopening traffic | B3 | NOT MET | *(none)* | — |
| E5 | Backup posture verified: writable, ≥ 30-day retention, restore-readability demonstrated | B4, PF-12 | NOT MET | *(none)* | — |
| E6 | Rollback pair capture-and-preserve plan understood; protected-tag / no-prune custody rules acknowledged | PF-02, PF-14 | NOT MET | *(none)* | — |
| E7 | All Section 6 preflight lines answerable "yes, with evidence" (snapshot rehearsal reports reviewed) | PF-01…PF-15 | NOT MET | *(none)* | — |
| E8 | Smoke matrix reviewed; live account holder available during the window | B2, PF-07 | NOT MET | *(none)* | — |
| E9 | Abort rules acknowledged by Operator and Product Owner | PF-14 | NOT MET | *(none)* | — |
| E10 | Confirmed out of scope: no R14e action, no migration 0010, no legacy-table deletion, no marker edits | — | NOT MET | *(none)* | — |

## 4. Roll-up

| Group | Total | MET | Remaining |
|---|---|---|---|
| Blockers B1–B6 | 6 | 0 | 6 |
| Preflight PF-01…PF-15 | 15 | 0 | 15 |
| Entry criteria E1–E10 | 10 | 0 | 10 |

**Gate 2 readiness: NOT READY.** No blocker is resolved, no preflight check is met, and no entry criterion is met. No GO may be recorded until B1–B6 are resolved, PF-01…PF-15 are each answerable "yes, with evidence," and E1–E10 are all met against accepted redacted human-Operator evidence. The Builder recorded only the default statuses above and performed no operational action.
