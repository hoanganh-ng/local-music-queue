# R14c Gate 2 — Operational Discrepancy Register (Redacted)

**Sprint:** 031 (R14c Gate 2 Preflight and Go/No-Go Preparation) — `documents/00-project-management/SPRINTS/031-r14c-gate2-preflight.md`
**Overlays:** the accepted readiness package [`room-cutover-gate2-readiness.md`](./room-cutover-gate2-readiness.md), the runbook [`room-cutover-runbook.md`](./room-cutover-runbook.md), and the [preflight status ledger](./room-cutover-gate2-preflight-ledger.md).
**Status:** Tracking document. **Empty at creation — no discrepancy has been recorded because no preflight or operational step has been executed.** This register does not authorize anything.

## Purpose

Any discrepancy, deviation, anomaly, or unexpected result the human **Operator** encounters while resolving blockers B1–B6 or running preflight checks PF-01…PF-15 is logged here — so nothing is silently worked around, every abort-relevant finding is visible at the Product Owner go/no-go review, and the disposition of each is traceable. Discrepancies referenced by an evidence-return are cross-linked from the [evidence-return](./room-cutover-gate2-evidence-return.template.md) Section D and the ledger.

## When to raise a discrepancy

Raise one for, at minimum:

- A preflight check that reaches `Failed` or cannot be resolved against evidence (e.g. host HEAD ≠ reviewed commit; slug already in use; host-user-id existence check ≠ exactly one row; live account does not map to the approved host user id; migration state not clean or schema version ≠ 9; backup not writable/retained; snapshot rehearsal counts implausible or inconsistent between `plan` and `up --dry-run`; R14b singleton-source rows missing or duplicated; pairing extract mixing modes or leaking an environment block/credential).
- Any deviation from the runbook's approved commands or the readiness package's non-argv connection-supply model (readiness §2.4) — for example a command that would place a DSN, password, or the live account address in argv.
- Any custody or permission anomaly (evidence dir not `0700`; a report not `0600`; connection-bundle file not `0600` or found inside the repo/web-served path; a rollback tag or archive at risk of prune/deletion).
- Any environmental surprise (image missing `/app/server` or `/app/room-cutover`; `\getenv` unsupported by the available client; snapshot infrastructure unavailable).

## Required fields per discrepancy

Every discrepancy entry must record **all** of the following:

1. **Expected condition** — what the readiness package, runbook, or ledger row says should be true (cite the source item: B/PF/E ref, readiness §, or runbook step).
2. **Observed condition (redacted)** — what was actually found, described in redacted terms only.
3. **Affected readiness/runbook step** — the ledger row(s) and runbook step(s) the discrepancy affects.
4. **Safety impact** — effect on production safety, rollback readiness, isolation, credential custody, or identity protection.
5. **Work stopped?** — whether the Operator stopped work on the affected item (yes/no, with safe date).
6. **Required decision** — whose explicit decision is required to proceed: `Product Owner` or `Architect + Product Owner` (see the classification rule below).
7. **Evidence reference (redacted)** — a protected-reference identifier for the supporting evidence; never a raw value or path.

Plus the bookkeeping columns: ID, safe date raised, severity, raised by, and disposition.

## Redaction rules (mandatory)

- Describe discrepancies in **redacted** terms only. **Never** record a DSN, password, credential, the live Google account address, a raw email, a production hostname, an absolute infrastructure path, an image ID/digest, a commit SHA, the target slug, or the host user id.
- Refer to values by their placeholder (`<REVIEWED_COMMIT_SHA>`, `<TARGET_ROOM_SLUG>`, etc.) and describe the *nature* of the discrepancy, not the secret value behind it.

## Severity vocabulary

- **Blocking** — Gate 2 cannot proceed until resolved; maps to a NO-GO if unresolved at review.
- **Important** — must be resolved or explicitly accepted before GO, subject to the classification rule below.
- **Minor** — noted; does not by itself prevent GO.
- **Observation** — informational; no action required.

## Classification rule (mandatory escalation)

A discrepancy that contradicts a **procedure**, an **isolation guarantee**, a **credential or identity-protection rule**, a **production-safety rule**, or the **accepted runbook** cannot be dispositioned as `ACCEPTED-RISK` by the Product Owner alone. For these classes:

- work on the affected item **remains stopped**, and
- the discrepancy remains open **until the Architect and the Product Owner both explicitly classify it** and record the agreed disposition here.

All other discrepancies may be dispositioned by the Product Owner against redacted evidence.

## Disposition vocabulary

`OPEN` → `IN PROGRESS` → `RESOLVED` (with redacted evidence reference) or `ACCEPTED-RISK` (explicitly accepted per the classification rule — Product Owner alone only where permitted; Architect + Product Owner for the reserved classes) or `DEFERRED` (moved out of this window). The Builder records none of these; the Operator, Product Owner, and — where required — the Architect do, against real evidence.

## Register

_No discrepancies recorded. The table below is the structure to use; it contains no entries because no preflight or operational step has been executed under this documentation-only sprint._

| ID | Date raised (safe) | Affected item (B/PF/E ref) | Expected condition (source cited) | Observed condition (redacted) | Affected readiness/runbook step | Safety impact | Work stopped? | Required decision (PO / Architect + PO) | Severity | Raised by | Disposition | Evidence reference (redacted) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| — | — | — | *(none)* | *(none)* | — | — | — | — | — | — | — | — |

## Roll-up

| Severity | Open | In progress | Resolved / Accepted / Deferred |
|---|---|---|---|
| Blocking | 0 | 0 | 0 |
| Important | 0 | 0 | 0 |
| Minor | 0 | 0 | 0 |
| Observation | 0 | 0 | 0 |

**No open Blocking or Important discrepancy exists — because none has been recorded.** This reflects that no operational step has run, not that readiness has been demonstrated. Gate 2 remains pending and unauthorized.
