# R14c Gate 2 — Operator-Local Input File (TEMPLATE)

> **NON-EXECUTABLE — direction amendment (ADR 004, Sprint 034, 2026-07-30).** The Product Owner replaced the migrate-and-retire cutover contract with the **discard-and-retire** contract (Issue #17 comment #5128855220; `documents/00-project-management/ADRS/004-discard-legacy-global-state-at-room-cutover.md`). This template collects inputs for a migrated-room production copy that **will no longer happen** (e.g. the target room slug/name and host user id of the migrated room). **This template is non-executable**: it must not be copied, filled, or used until the implementation-correction sprint (ADR 004 Section 6) is accepted and integrated and the Product Owner authorizes a revised Gate 2 with revised inputs. Sprint 031 is paused. Preserved unchanged below as historical record; its no-secrets handling rules carry forward into any revised template.

**This tracked file is a template and contains placeholders only.**

Usage (see [`room-cutover-gate2-readiness.md`](./room-cutover-gate2-readiness.md), Sections 2 and 7):

1. Copy this file to an operator-controlled location **outside the repository working tree and outside any web-served path** (for example next to `<EVIDENCE_DIR>`).
2. `chmod 0600` the copy.
3. Fill in the values below from the Gate 2 approvals. **Never commit the filled copy, never paste it into an issue/PR/chat, and never add real values to this tracked template.**
4. Destroy or archive the filled copy per Product Owner instruction after the rollback window closes.

Hard rule: `<LIVE_ALLOWED_GOOGLE_ACCOUNT>` is **never written into the filled copy** (or any other file). It is supplied only at execution time via `read -rs` into `LIVE_EMAIL` per the runbook. Record only a confirmation reference here.

Hard rule: **no DSN, password, or other connection credential is ever written into the filled copy either.** Connection data lives only in the protected connection bundle (`<PGSERVICE_FILE>`, `<PGPASS_FILE>`, `<CONNECTION_ENV_FILE>`, each mode `0600` — readiness package Section 2.4), and commands consume it exclusively through environment/service/passfile channels — never through positional arguments and never as a value given to the `--postgres` flag. This file records only the bundle file **paths**.

## Operator inputs (approved at GO / NO-GO)

| Field | Value |
|---|---|
| `TARGET_ROOM_SLUG` | `<TARGET_ROOM_SLUG>` |
| `TARGET_ROOM_NAME` | `<TARGET_ROOM_NAME>` |
| `HOST_USER_ID` | `<HOST_USER_ID>` |
| `MAINTENANCE_WINDOW` (start / end, timezone) | `<MAINTENANCE_WINDOW>` |
| `BACKUP_LOCATION` (path only, no credentials) | `<BACKUP_LOCATION>` |
| Live allow-listed Google account | DO NOT WRITE THE ADDRESS — confirmation reference only: `<CONFIRMATION_REFERENCE>` |

## Supporting values

| Field | Value |
|---|---|
| `REVIEWED_COMMIT_SHA` | `<REVIEWED_COMMIT_SHA>` |
| `PROD_HOST` (hostname only, no credentials) | `<PROD_HOST>` |
| `EVIDENCE_DIR` (host path, mode `0700`) | `<EVIDENCE_DIR>` |
| `SNAPSHOT_DSN` / `ISOLATED_SNAPSHOT_DSN` | DO NOT WRITE DSNs — they live only in the connection bundle below |
| `PGSERVICE_FILE` (path only, mode `0600`) | `<PGSERVICE_FILE>` |
| `PGPASS_FILE` (path only, mode `0600`) | `<PGPASS_FILE>` |
| `CONNECTION_ENV_FILE` (path only, mode `0600`) | `<CONNECTION_ENV_FILE>` |
| `DUMP_FILE` (timestamped, set inside the window) | `<DUMP_FILE>` |
| `FALSE_PAIR_IMAGE_TAGS` (backend ID+digest line) | `<FALSE_PAIR_BACKEND_ID_DIGEST>` |
| `FALSE_PAIR_IMAGE_TAGS` (frontend ID+digest line) | `<FALSE_PAIR_FRONTEND_ID_DIGEST>` |
| `TRUE_PAIR_IMAGE_TAGS` (backend ID+digest line) | `<TRUE_PAIR_BACKEND_ID_DIGEST>` |
| `TRUE_PAIR_IMAGE_TAGS` (frontend ID+digest line) | `<TRUE_PAIR_FRONTEND_ID_DIGEST>` |

## Role assignments

| Role | Name / handle |
|---|---|
| Product Owner (of record for the window) | `<PRODUCT_OWNER>` |
| Operator (must differ from Product Owner) | `<OPERATOR>` |
| Scribe / witness (optional) | `<SCRIBE>` |

## Sign-off references

| Item | Reference |
|---|---|
| GO record (readiness package Section 9) date | `<GO_DATE>` |
| Gate 1 rehearsal evidence acceptance reference | `<GATE1_EVIDENCE_REF>` |
| Backup verification evidence reference | `<BACKUP_VERIFICATION_REF>` |
