# R14c Gate 2 — Operational Discrepancy Register (Redacted)

**Sprint:** 031 (R14c Gate 2 Preflight and Go/No-Go Preparation) — `documents/00-project-management/SPRINTS/031-r14c-gate2-preflight.md`
**Overlays:** the accepted readiness package [`room-cutover-gate2-readiness.md`](./room-cutover-gate2-readiness.md), the runbook [`room-cutover-runbook.md`](./room-cutover-runbook.md), and the [preflight status ledger](./room-cutover-gate2-preflight-ledger.md).
**Status:** Tracking document. **Empty at creation — no discrepancy has been recorded because no preflight or operational step has been executed.** This register does not authorize anything.

## Purpose

Any discrepancy, deviation, anomaly, or unexpected result the human **Operator** encounters while resolving blockers B1–B6 or running preflight checks PF-01…PF-15 is logged here — so nothing is silently worked around, every abort-relevant finding is visible at the Product Owner go/no-go review, and the disposition of each is traceable. Discrepancies referenced by an evidence-return are cross-linked from the [evidence-return](./room-cutover-gate2-evidence-return.template.md) Section D and the ledger.

## When to raise a discrepancy

Raise one for, at minimum:

- A preflight check that FAILs or cannot be answered "yes, with evidence" (e.g. host HEAD ≠ reviewed commit; slug already in use; host-user-id existence check ≠ exactly one row; live account does not map to the approved host user id; migration state not clean or schema version ≠ 9; backup not writable/retained; snapshot rehearsal counts implausible or inconsistent between `plan` and `up --dry-run`; R14b singleton-source rows missing or duplicated; pairing extract mixing modes or leaking an environment block/credential).
- Any deviation from the runbook's approved commands or the readiness package's non-argv connection-supply model (readiness §2.4) — for example a command that would place a DSN, password, or the live account address in argv.
- Any custody or permission anomaly (evidence dir not `0700`; a report not `0600`; connection-bundle file not `0600` or found inside the repo/web-served path; a rollback tag or archive at risk of prune/deletion).
- Any environmental surprise (image missing `/app/server` or `/app/room-cutover`; `\getenv` unsupported by the available client; snapshot infrastructure unavailable).

## Redaction rules (mandatory)

- Describe discrepancies in **redacted** terms only. **Never** record a DSN, password, credential, the live Google account address, a raw email, a production hostname, an absolute infrastructure path, an image ID/digest, a commit SHA, the target slug, or the host user id.
- Refer to values by their placeholder (`<REVIEWED_COMMIT_SHA>`, `<TARGET_ROOM_SLUG>`, etc.) and describe the *nature* of the discrepancy, not the secret value behind it.

## Severity vocabulary

- **Blocking** — Gate 2 cannot proceed until resolved; maps to a NO-GO if unresolved at review.
- **Important** — must be resolved or explicitly accepted by the Product Owner before GO.
- **Minor** — noted; does not by itself prevent GO.
- **Observation** — informational; no action required.

## Disposition vocabulary

`OPEN` → `IN PROGRESS` → `RESOLVED` (with redacted evidence reference) or `ACCEPTED-RISK` (Product Owner explicitly accepts) or `DEFERRED` (moved out of this window). The Builder records none of these; the Operator and Product Owner do, against real evidence.

## Register

_No discrepancies recorded. The table below is the structure to use; it contains no entries because no preflight or operational step has been executed under this documentation-only sprint._

| ID | Date | Affected item (B/PF/E ref) | Description (redacted) | Severity | Raised by | Disposition | Evidence reference (redacted) |
|---|---|---|---|---|---|---|---|
| — | — | — | *(none)* | — | — | — | — |

## Roll-up

| Severity | Open | In progress | Resolved / Accepted / Deferred |
|---|---|---|---|
| Blocking | 0 | 0 | 0 |
| Important | 0 | 0 | 0 |
| Minor | 0 | 0 | 0 |
| Observation | 0 | 0 | 0 |

**No open Blocking or Important discrepancy exists — because none has been recorded.** This reflects that no operational step has run, not that readiness has been demonstrated. Gate 2 remains pending and unauthorized.
