# R14c Gate 2 Readiness Package

> **NON-EXECUTABLE — direction amendment (ADR 004, Sprint 034, 2026-07-30).** The Product Owner replaced the migrate-and-retire cutover contract with the **discard-and-retire** contract (Issue #17 comment #5128855220; `documents/00-project-management/ADRS/004-discard-legacy-global-state-at-room-cutover.md`). This readiness package governs a Gate 2 whose migrate-and-retire production assumptions are superseded: no migrated room will be created and no legacy global state will be copied. **This package is non-executable** — no blocker resolution, preflight check, entry-criterion verification, role assignment, scheduling, or GO decision may proceed under it — until the implementation-correction sprint (ADR 004 Section 6) is accepted and integrated and the Product Owner authorizes a revised Gate 2. Sprint 031 is paused. The body below is preserved unchanged as the historical record; its backup, rollback, fail-closed, no-secrets, and Sprint 032 governance controls carry forward into any revised Gate 2.

**Sprint:** 030 (R14c Gate 2 Readiness Package) — `documents/00-project-management/SPRINTS/030-r14c-gate2-readiness-package.md`
**Overlays:** the accepted runbook [`room-cutover-runbook.md`](./room-cutover-runbook.md) (written and reviewed under R14c Gate 1, PR #26, integrated into `dev` as `c8ab4af029d10dda889d1165464e16068a5be573`)
**Amended by:** Sprint 032 — R14c Solo-Operator Governance Amendment (`documents/00-project-management/SPRINTS/032-r14c-solo-operator-governance.md`) — role governance in Section 3, entry criterion E3, and the Section 9 decision-rule sequencing only; every readiness, evidence, rollback, discrepancy, and production-safety control, and every runbook procedure and command, is unchanged.
**Status:** Documentation only. **Nothing in this package has been executed.** Gate 2 production execution remains explicitly pending and requires a Product Owner GO recorded against this package and the runbook.

This package is the single authoritative readiness and governance overlay for Gate 2. Apart from the Architect-required security corrections applied to the runbook in this same PR (focused documentation-only amendments: non-argv connection supply with un-exported private DSN variables, and allowlisted pairing evidence instead of retained full Compose renders; command order and behavior unchanged), it does **not** modify, replace, or reorder any step of the runbook. On any conflict about *what to execute or in what order*, the runbook prevails; on any conflict about *whether execution is authorized*, this package and the Product Owner decision prevail. Placeholders only — no production hostname, credential, DSN, email address, token, or account identity appears here or may ever be committed.

## 1. Purpose and authority

- Gate 1 (implementation, isolated rehearsal, runbook) is integrated and closed. Gate 2 (production execution) is a separate authorization owned solely by the Product Owner.
- This package assembles everything the Product Owner needs to decide GO / NO-GO / DEFER: the complete input inventory, role assignments, artifact custody rules, current blockers, preflight checks, abort rules, and entry criteria.
- No AI agent (Builder) may perform any Gate 2 action: no production SQL, lookup, backup, migration, deployment, traffic control, true-mode start, rollback, or R14e action. Gate 2 steps are executed only by the assigned human Operator inside the approved window.

## 2. Complete placeholder / input inventory

Every placeholder used by the runbook, with its owner and the only approved secure supply method. Constraint column repeats the runbook constraint verbatim where one exists.

### 2.1 Operator inputs approved at the GO / NO-GO decision

| Placeholder | Meaning | Constraint | Owner | Secure supply method |
|---|---|---|---|---|
| `<TARGET_ROOM_SLUG>` | Slug the global state is migrated into | Valid, non-reserved, unused slug | Product Owner (approves), Operator (verifies unused) | Written into the operator-local input file (Section 7); never committed |
| `<TARGET_ROOM_NAME>` | Display name of the target room | Non-empty | Product Owner | Operator-local input file |
| `<HOST_USER_ID>` | Canonical PostgreSQL `users.id` made sole room host | Positive integer, must exist in `users` | Product Owner (designates), Operator (validates via runbook step 5) | Operator-local input file; validated as boolean existence check only, row never printed |
| `<MAINTENANCE_WINDOW>` | Start/end of the closed-traffic window | Product Owner approved | Product Owner | Operator-local input file plus the GO record |
| `<BACKUP_LOCATION>` | Verified destination for the pre-cutover `pg_dump` | ≥ 30-day retention, verified writable | Operator (provisions), Product Owner (accepts) | Operator-local input file; path only, no credentials embedded |
| `<LIVE_ALLOWED_GOOGLE_ACCOUNT>` | Real allow-listed Google account for the smoke matrix | Mandatory (R14d's isolated review could not exercise real login) | Product Owner (nominates), account holder (consents) | **Never written to any file, log, or command line.** Supplied only at execution time via `read -rs` into `LIVE_EMAIL`, passed to `psql` through the environment and `\getenv` into the quoted `:'email'` variable, then `unset` (runbook pre-window step 6). The input file records only a confirmation reference (e.g. "confirmed with PO on <date>"), never the address |

### 2.2 Supporting placeholders

| Placeholder | Meaning | Owner | Secure supply method |
|---|---|---|---|
| `<REVIEWED_COMMIT_SHA>` | The accepted R14c review commit deployed on the host | Product Owner (declares), Operator (verifies with `git rev-parse HEAD`) | Operator-local input file; public SHA, not secret, still never guessed |
| `<FALSE_PAIR_IMAGE_TAGS>` | Immutable IDs/digests of the live false backend+frontend images | Operator (captures per runbook pre-window step 2, before any rebuild/prune) | Recorded in the operator-local input file and in `<EVIDENCE_DIR>`; pinned under `rollback-r14c` tags and archived to `<BACKUP_LOCATION>` |
| `<TRUE_PAIR_IMAGE_TAGS>` | IDs/digests of the built `:true` pair | Operator (captures per runbook pre-window step 10) | Operator-local input file and `<EVIDENCE_DIR>` |
| `<PROD_HOST>` | Production deployment host | Operator | Operator-local input file; hostname only, no credentials |
| `<DUMP_FILE>` | Timestamped pre-cutover `pg_dump` archive | Operator | Created inside the window; copied to `<BACKUP_LOCATION>`; mode `0600` |
| `<EVIDENCE_DIR>` | Operator-controlled host directory for all reports | Operator | Created before the window, mode `0700`, outside the repository working tree and any web-served path; every report `0600`; bind-mounted into one-off containers; contents never committed |
| `<SNAPSHOT_DSN>` | Read-only production snapshot DSN | Operator (provisions) | Exists **only** inside the protected connection bundle (Section 2.4) — as the `r14c-snapshot` service in `<PGSERVICE_FILE>` for host `psql`, and as the un-exported `SNAPSHOT_DSN` assignment in `<CONNECTION_ENV_FILE>` for the containerized rehearsal. Never in argv, the input file, retained logs, or any committed file |
| `<ISOLATED_SNAPSHOT_DSN>` | Throwaway snapshot copy for the `up --dry-run` rehearsal only | Operator (provisions) | Same handling as `<SNAPSHOT_DSN>` (`r14c-snapshot-isolated` service / un-exported `ISOLATED_SNAPSHOT_DSN` assignment); destroyed after rehearsal |
| `<PGSERVICE_FILE>` | libpq connection service file (services `r14c-snapshot`, `r14c-snapshot-isolated`, `r14c-live`) | Operator (creates) | Part of the connection bundle: operator-local, mode `0600`, outside the repo and web-served paths; selected via `PGSERVICEFILE`/`PGSERVICE` environment variables; holds no passwords; never committed |
| `<PGPASS_FILE>` | libpq password file for the bundle services | Operator (creates) | Part of the connection bundle: mode `0600`, referenced only via `PGPASSFILE`; the sole place passwords live; never committed |
| `<CONNECTION_ENV_FILE>` | Shell variable file with plain (non-`export`) assignments of `SNAPSHOT_DSN` / `ISOLATED_SNAPSHOT_DSN` for containerized `room-cutover` | Operator (creates) | Part of the connection bundle: mode `0600`; sourced with `. <CONNECTION_ENV_FILE>` in the private shell so both DSNs stay un-exported private shell variables invisible to child processes; only the selected value reaches only the selected command via a per-command `DATABASE_URL="$…"` prefix and name-only `-e DATABASE_URL`; never committed |

### 2.3 Environment variables and fixed identities referenced by the runbook

| Token | Role | Owner | Handling |
|---|---|---|---|
| `ROOM_CUTOVER_AUTHORITATIVE` | The single Compose pairing switch in the deployment `.env`; also used as an inline per-command prefix to pin rehearsal commands to the prebuilt `:true` backend image | Operator | Set in the deployment `.env` (untracked) only at runbook step 7; rehearsal commands always carry the inline prefix |
| `VITE_ROOM_CUTOVER_AUTHORITATIVE` | Frontend build argument baked at build time | Operator (via Compose build) | Never set independently of the backend half |
| `DATABASE_URL` | The only approved connection channel into `room-cutover` (its documented fallback when `--postgres` is omitted); also the backend service's own connection source | Operator | Live window: consumed from the Compose backend service environment (deployment `.env`). Rehearsals: per-command `DATABASE_URL="$SNAPSHOT_DSN"` / `"$ISOLATED_SNAPSHOT_DSN"` prefix from the connection bundle, forwarded by name only with `-e DATABASE_URL`. Never committed, echoed, or placed in argv |
| `PGSERVICEFILE` / `PGSERVICE` / `PGPASSFILE` | libpq environment selectors for host `psql` / `pg_dump` | Operator | Point at the connection bundle files; the values placed in argv-visible positions are only file paths and service names, never DSNs or passwords |
| `LIVE_EMAIL` | Transient carrier for the live account address | Operator | `read -rs`, per-command environment scope, `unset` afterwards; never in process arguments, files, or history |
| `HOST_EMAILS` / `ADMIN_EMAILS` | Account-level role allow-lists | Product Owner (content), Operator (deployment env) | Explanatory only in the runbook — they do **not** drive login eligibility or room-host membership; do not modify for the cutover |
| `local-music-queue-{backend,frontend}:{false,true}` | Mode-qualified image identities | Operator | Built from `<REVIEWED_COMMIT_SHA>`; the `true` build never overwrites the `false` pair |
| `local-music-queue-{backend,frontend}:rollback-r14c` | Protected rollback tags | Operator | Pin the captured false-pair IDs; no `rmi`/prune of these until the Product Owner closes the rollback window |
| `rollback-{backend,frontend}-r14c.tar` | Durable rollback archives | Operator | Stored in `<BACKUP_LOCATION>`, mode `0600`, retained until the rollback window closes |
| `plan-report.json`, `up-dry-run.json`, `live-plan.json`, `live-up.json`, `live-verify.json`, `pairing-false.txt`, `pairing-true.txt` | Evidence artifacts | Operator | All under `<EVIDENCE_DIR>`, mode `0600`, PII-free and secret-free, never committed. The pairing files hold only allowlisted lines (image references and authoritative flags); the full `docker compose config` render is never written to disk because it interpolates deployment secrets |

### 2.4 Approved connection-supply model (no DSN or credential in argv)

This is the **single approved model** for supplying database connection data during Gate 2; no alternative that expands a DSN or credential into a command line is permitted. Rationale: argv is visible through `ps`/`/proc/*/cmdline` and can persist in shell history and retained logs.

- All connection data lives in one operator-local **protected connection bundle** — `<PGSERVICE_FILE>`, `<PGPASS_FILE>`, `<CONNECTION_ENV_FILE>` — each mode `0600`, outside the repository working tree and any web-served path (see the runbook's "Connection supply" section).
- Host `psql` and `pg_dump` connect **only** via `PGSERVICEFILE`/`PGSERVICE`/`PGPASSFILE` environment selectors (`r14c-snapshot`, `r14c-snapshot-isolated`, `r14c-live`).
- Every `room-cutover` invocation **omits `--postgres`** and consumes its documented `DATABASE_URL` fallback: in rehearsals via a per-command `DATABASE_URL="$…"` environment prefix plus name-only `docker compose run -e DATABASE_URL` passthrough; in the live window from the Compose backend service environment (deployment `.env`). A value is never typed after `-e`. The DSN variables sourced from `<CONNECTION_ENV_FILE>` are plain (non-`export`) assignments, so they remain private shell variables — only the selected value is exposed, only to the selected command, through its `DATABASE_URL` prefix.
- **No-argv rule:** no DSN, password, or credential may appear as a positional argument or flag value of `psql`, `pg_dump`, `pg_restore`, or any `room-cutover` invocation, at any Gate 2 step.

## 3. Maintenance roles

| Role | Assignee | Responsibilities |
|---|---|---|
| Product Owner | *(unassigned — blocker B1)* | Approves the six operator inputs; records GO / NO-GO / DEFER (Section 9); owns the reopen-traffic decision after the smoke matrix; closes the rollback window; decides forward recovery vs database restoration after any rollback |
| Operator | *(unassigned — blocker B1)* | The only person who executes runbook commands; provisions `<SNAPSHOT_DSN>`, `<ISOLATED_SNAPSHOT_DSN>`, the protected connection bundle (Section 2.4), `<BACKUP_LOCATION>`, `<EVIDENCE_DIR>`; custodian of all evidence artifacts; performs rollback on any failed mandatory check |
| Scribe / witness (optional but recommended) | *(unassigned)* | Timestamps each runbook step, records smoke-matrix initials, keeps the abort/rollback log; must never handle credentials |
| Builder (AI agent) | n/a | **Prohibited from all Gate 2 actions.** May only produce/update documentation before the window on Product Owner instruction |

Separate-role governance remains the normal preference: where more than one person is available, the Operator and Product Owner roles should be held by different people, and one person may hold Product Owner and Scribe. By named exception recorded under Sprint 032 — R14c Solo-Operator Governance Amendment, the sole maintainer **`hoanganh-ng`** is approved to act as both Product Owner-of-record and human Operator **for R14c Gate 2 only**; the exception extends to no other person, gate, or sprint. For R14c Gate 2 the former second-pair-of-eyes control is replaced by a mandatory three-pass procedural sequence, which applies whether the roles are held separately or combined:

1. **Operator evidence pass** — the Operator works blockers B1–B6 and preflight checks PF-01…PF-15 and returns redacted evidence through the [evidence-return](./room-cutover-gate2-evidence-return.template.md).
2. **Architect review pass** — the Architect reviews the completed ledger, evidence-return, and discrepancy register and records exactly one outcome: `READY FOR PO DECISION`, `DEFER — EVIDENCE INCOMPLETE`, or `NO-GO RECOMMENDED`. The outcome must be attributable: it is recorded in the Section 9 decision block together with the Architect reviewer's name or approved handle, the review date, and a durable review reference (e.g. a GitHub PR/issue review-comment link), and it may not be entered by the combined Product Owner/Operator on the Architect's behalf. The Architect review grants no production authority and does not replace the Product Owner decision.
3. **Product Owner decision pass** — the Product Owner records GO / NO-GO / DEFER in Section 9. **A GO recorded before the Architect review pass has returned `READY FOR PO DECISION` is invalid.**

## 4. Artifact custody

- `<EVIDENCE_DIR>` (mode `0700`) holds every report and the allowlisted pairing evidence (`pairing-false.txt` / `pairing-true.txt`), each `0600`, written through bind mounts so they survive `docker compose run --rm`. The full `docker compose config` render is **never** retained on disk — it interpolates deployment secrets (database credentials, the constructed `DATABASE_URL`, Google and DuckDNS values); only the allowlisted image-reference and authoritative-flag lines are extracted into the pairing files. Custodian: Operator. Retention: until the Product Owner closes the rollback window, then per Product Owner instruction.
- `<BACKUP_LOCATION>` holds `<DUMP_FILE>` (≥ 30 days) and the two rollback image archives (until the rollback window closes). Custodian: Operator.
- Protected `rollback-r14c` image tags and the captured IDs/digests: no `docker rmi`, no `docker image prune -a`, no archive deletion until the Product Owner closes the rollback window.
- The completed smoke-matrix checklist with operator initials and timestamps is retained in `<EVIDENCE_DIR>`.
- Nothing from `<EVIDENCE_DIR>` or `<BACKUP_LOCATION>` is ever committed to the repository; these artifacts can contain real identities.
- The operator-local input file (Section 7) lives outside the repository, mode `0600`, and is destroyed or archived per Product Owner instruction after the rollback window closes.
- The protected connection bundle (`<PGSERVICE_FILE>`, `<PGPASS_FILE>`, `<CONNECTION_ENV_FILE>`, each mode `0600`, outside the repository and web-served paths) is created and held solely by the Operator; the snapshot entries are removed when the snapshots are destroyed, and the whole bundle is destroyed or rotated per Product Owner instruction after the rollback window closes. It is never committed, copied into `<EVIDENCE_DIR>`, or shared.

## 5. Current blockers

All of the following block Gate 2 entry today:

- **B1 — Roles unassigned.** No named Operator or Product Owner-of-record for the window; the Scribe is optional but unassigned.
- **B2 — Operator inputs unapproved.** None of the six Section 2.1 inputs has an approved value; `<LIVE_ALLOWED_GOOGLE_ACCOUNT>` additionally needs the account holder's availability during the window.
- **B3 — Window not scheduled.** `<MAINTENANCE_WINDOW>` is not agreed; closure/announcement channels are not identified.
- **B4 — Backup posture unverified.** `<BACKUP_LOCATION>` writability, ≥ 30-day retention, and restore-readability have not been demonstrated.
- **B5 — Snapshot infrastructure not provisioned.** `<SNAPSHOT_DSN>` and `<ISOLATED_SNAPSHOT_DSN>` do not exist yet and the protected connection bundle (Section 2.4) has not been created; the pre-window plan and `up --dry-run` rehearsals cannot run without them.
- **B6 — Deployment-host commit not confirmed.** `<REVIEWED_COMMIT_SHA>` has not been declared or verified on `<PROD_HOST>`.
- **B7 — GO record absent.** No Product Owner GO / NO-GO / DEFER decision has been recorded (Section 9).

Resolving B1–B6 is a precondition for convening the GO / NO-GO review; B7 is the review itself.

## 6. Preflight checks

The runbook's pre-window checklist (steps 1–14) is the executable procedure. This section is the *verification* view the Product Owner reviews at GO / NO-GO — every line must be answerable "yes, with evidence" before the window opens:

1. Deployment host `git rev-parse HEAD` equals `<REVIEWED_COMMIT_SHA>` (runbook step 1).
2. False/false rollback pair captured from the **running containers** by immutable ID + digest, pinned under `rollback-r14c` tags, archived to `<BACKUP_LOCATION>`, and re-verified to resolve to the captured IDs — all **before** any rebuild or prune (step 2).
3. `<EVIDENCE_DIR>` exists, mode `0700`, outside the repo and web-served paths (step 3).
4. `<TARGET_ROOM_SLUG>` verified unused and non-reserved (step 4).
5. `<HOST_USER_ID>` existence check returns exactly one row, boolean output only, connecting via `PGSERVICE=r14c-snapshot` from the bundle — no DSN in argv (step 5).
6. `<LIVE_ALLOWED_GOOGLE_ACCOUNT>` → `<HOST_USER_ID>` equality confirmed via the `read -rs` / `\getenv` / `:'email'` procedure with the bundle's `PGSERVICE` connection; result `t`; neither the address nor a DSN ever on a command line or in a file (step 6).
7. Login for the live account confirmed on the current false deployment, distinguishing login eligibility, account-level `users.role`, and room-host membership (step 8).
8. Allowlisted pairing evidence extracted and reviewed in `<EVIDENCE_DIR>` (`pairing-false.txt` / `pairing-true.txt`): each extract pairs backend and frontend in the **same** mode; the false extract references the `:false` pair, the true extract the `:true` pair; each contains only image-reference and authoritative-flag lines — no environment block, credential, or full Compose render (step 9).
9. Both artifact pairs built from `<REVIEWED_COMMIT_SHA>`; backend image contains `/app/server` and `/app/room-cutover`; `:true` pair IDs recorded (step 10).
10. Snapshot `plan` and isolated `up --dry-run` rehearsals completed **through the packaged `:true` backend image with the inline `ROOM_CUTOVER_AUTHORITATIVE=true` prefix**, with the snapshot DSNs supplied per Section 2.4 (per-command `DATABASE_URL` prefix, name-only `-e DATABASE_URL`, `--postgres` never used), reports durable on the host, `0600`, PII-free, counts plausible and mutually consistent (steps 11–12). These runs also prove first-cutover readiness as defined by R14b: target slug absent, `room_activities` empty, and the legacy singleton source rows present — exactly one `queue_state` row (`id = 1`) and exactly one `auto_queue_config` row (`id = 1`) — with a missing singleton failing up front, distinctly from other failures.
11. Gate 1 isolated end-to-end rehearsal evidence accepted (step 13).
12. `<BACKUP_LOCATION>` writable, verified, ≥ 30-day retention (step 14).
13. PostgreSQL migration state clean and schema version exactly 9 (the CLI refuses anything else); the client used for identity checks supports `\getenv` (psql ≥ 15; deployment runs PostgreSQL 16).
14. Rollback and abort rules (Section 8 of this package; runbook Rollback section) read aloud and acknowledged by Operator and Product Owner.
15. Protected connection bundle in place per Section 2.4: `<PGSERVICE_FILE>`, `<PGPASS_FILE>`, `<CONNECTION_ENV_FILE>` exist, each mode `0600`, outside the repo and web-served paths; `<CONNECTION_ENV_FILE>` contains plain (non-`export`) assignments only; spot-check confirms no planned command carries a DSN or credential in argv (`--postgres` unused everywhere).

## 7. Operator-local input template

A placeholder-only template is tracked at [`room-cutover-gate2-inputs.template.md`](./room-cutover-gate2-inputs.template.md). Before the GO / NO-GO review the Operator copies it **outside the repository working tree** (alongside, or under, `<EVIDENCE_DIR>`), sets mode `0600`, and fills it in. The filled copy is never committed, never web-served, and never contains the live Google account address — that value exists only transiently in `LIVE_EMAIL` per Section 2.1. It also never contains a DSN or password: connection data lives only in the protected connection bundle (Section 2.4), and the input file records only the bundle file **paths**.

## 8. Abort rules

- **Before the window opens:** any unmet preflight line in Section 6, any unresolved blocker in Section 5, or a NO-GO/DEFER decision simply means the window does not open. Nothing to undo; no production state has changed.
- **Inside the window, before `room-cutover up` (runbook maintenance steps 1–4):** stop, leave traffic closed, and either fix-and-continue only with explicit Product Owner approval or restart the false pair and reopen traffic (no data was changed; the backup and rollback pair make this a plain redeploy).
- **Inside the window, at or after `room-cutover up` (steps 5–9):** any failure at any step, including any failed mandatory smoke check, goes **directly to the runbook Rollback section**. There is no partial continuation.
- **Hard prohibitions during any abort or rollback:** the CLI has **no** `abort`, `force`, `reset`, or hash-edit subcommand and none may be improvised; never delete or edit `room_cutover_marker`; never delete the migrated room; never rerun the cutover in the same window; retain `<DUMP_FILE>` and every `<EVIDENCE_DIR>` report; record any true-mode room writes made during smoke testing.
- **After rollback:** the choice between forward recovery and database restoration is a separate Product Owner decision — it is not made inside the window.

## 9. Gate 2 entry criteria and Product Owner GO / NO-GO / DEFER checklist

Gate 2 may be entered only when every entry criterion below is met. The checklist is completed and signed by the Product Owner; it is the GO record referenced by blocker B7.

| # | Entry criterion | Met? |
|---|---|---|
| E1 | Gate 1 integrated: PR #26 squash-merged into `dev` as `c8ab4af029d10dda889d1165464e16068a5be573`; runbook accepted | ☐ |
| E2 | All six operator inputs (Section 2.1) approved, supplied via their secure methods, and recorded in the operator-local input file; the protected connection bundle (Section 2.4) is in place and no command supplies a DSN or credential via argv | ☐ |
| E3 | Roles assigned (Section 3): named Operator and Product Owner-of-record recorded — either separate persons (normal preference) or the approved Sprint 032 named exception (`hoanganh-ng` holding both roles for R14c Gate 2 only) — with the Operator → Architect → Product Owner three-pass sequence acknowledged | ☐ |
| E4 | `<MAINTENANCE_WINDOW>` scheduled with announcement plan for closing and reopening traffic | ☐ |
| E5 | Backup posture verified: `<BACKUP_LOCATION>` writable, ≥ 30-day retention, restore-readability demonstrated | ☐ |
| E6 | Rollback pair capture-and-preserve plan understood; protected-tag / no-prune custody rules acknowledged | ☐ |
| E7 | All Section 6 preflight lines answerable "yes, with evidence" (snapshot rehearsal reports reviewed) | ☐ |
| E8 | Smoke matrix reviewed; live account holder available during the window | ☐ |
| E9 | Abort rules (Section 8) acknowledged by Operator and Product Owner | ☐ |
| E10 | Confirmed out of scope: no R14e action, no migration 0010, no legacy-table deletion, no marker edits | ☐ |

**Decision rule:**

- **GO** — all of E1–E10 checked **and** the Architect review pass (Section 3) has returned `READY FOR PO DECISION`. A GO recorded before the Architect review pass is invalid. Record date, window, and signatures; the Operator may then execute the runbook inside `<MAINTENANCE_WINDOW>` only.
- **NO-GO** — any criterion is failed on substance (e.g. rehearsal reports inconsistent, backup unverifiable, rollback custody rejected). Record the failed criteria; Gate 2 stays closed; remediation is planned outside the window.
- **DEFER** — criteria are unmet only for scheduling/assignment reasons (inputs pending, window not agreed, personnel unavailable). Record what is pending and the revisit date; no production action of any kind in the meantime.

| Decision | GO ☐ / NO-GO ☐ / DEFER ☐ |
|---|---|
| Architect review pass outcome (required before GO; Section 3) | READY FOR PO DECISION ☐ / DEFER — EVIDENCE INCOMPLETE ☐ / NO-GO RECOMMENDED ☐ |
| Architect reviewer (name or approved handle — must not be the combined Product Owner/Operator) | |
| Architect review date | |
| Durable Architect review reference (e.g. GitHub PR/issue review-comment link) | |
| Confirmation: `READY FOR PO DECISION` grants no production authority and does not replace the Product Owner decision | ☐ |
| Date | |
| Approved `<MAINTENANCE_WINDOW>` (GO only) | |
| Failed criteria / pending items (NO-GO / DEFER) | |
| Product Owner signature | |
| Operator acknowledgement | |

Until a GO is recorded here, no production SQL, lookup, backup, migration, deployment, traffic control, true-mode start, rollback, or R14e action is authorized for anyone, and none has been performed in preparing this package.
