# R14c Gate 2 — Operator Evidence-Return (TEMPLATE, Redacted)

**This tracked file is a template and contains placeholders only.**

This template is how the human **Operator** returns *redacted* attestations of preflight and blocker work back to the **Product Owner** for the Gate 2 go/no-go review. It feeds the [preflight status ledger](./room-cutover-gate2-preflight-ledger.md); the Product Owner accepts a returned attestation before any ledger row moves to `MET`.

Usage (see [`room-cutover-gate2-readiness.md`](./room-cutover-gate2-readiness.md) Sections 6–9, and [`room-cutover-gate2-preflight-ledger.md`](./room-cutover-gate2-preflight-ledger.md)):

1. Copy this file to an operator-controlled location **outside the repository working tree and outside any web-served path** (for example next to `<EVIDENCE_DIR>`).
2. `chmod 0600` the copy.
3. Fill in **redacted** outcomes only. **Never commit the filled copy, never paste it into an issue/PR/chat, and never add real values to this tracked template.**
4. Return the redacted attestation to the Product Owner by the agreed secure channel; the Product Owner records acceptance in the readiness package and the ledger.
5. Destroy or archive the filled copy per Product Owner instruction after the rollback window closes.

## Redaction rules (mandatory)

- Record **only** attestation outcomes: `t` / boolean pass-fail, "yes, with evidence", "verified", counts as order-of-magnitude bands (e.g. "hundreds"), or a redacted evidence pointer such as `<EVIDENCE_DIR>/<report>.json reviewed, PII-free`.
- **Never** write: any DSN, password, or connection credential; the live Google account address; a raw email; a production hostname; an absolute host path that reveals infrastructure; an image ID/digest; a commit SHA; the target room slug; the host user id; or raw report contents.
- Connection data lives only in the protected connection bundle (readiness §2.4); the input file records bundle file **paths** only; this evidence-return records that the bundle and its checks passed, not what is in it.
- `<LIVE_ALLOWED_GOOGLE_ACCOUNT>` is never written here — attest only "login confirmed on false deployment; maps to approved host user id (`t`)" with a confirmation reference.

## A. Return header

| Field | Value |
|---|---|
| Operator (name/handle) | `<OPERATOR>` |
| Product Owner of record | `<PRODUCT_OWNER>` |
| Return date | `<RETURN_DATE>` |
| Operator-local input file used | `<INPUT_FILE_PATH>` (path only; not committed) |
| Evidence directory reference | `<EVIDENCE_DIR>` (path only; not committed) |

## B. Blocker attestations — B1–B6

For each, record `RESOLVED` / `NOT RESOLVED` and a redacted reference. Do not mark anything resolved without real evidence.

| ID | Attestation (redacted) | Outcome | Reference |
|---|---|---|---|
| B1 | Named Operator and Product Owner-of-record assigned; Operator ≠ Product Owner | `<NOT RESOLVED>` | `<REF>` |
| B2 | All six operator inputs approved and recorded in the input file; live account holder availability confirmed | `<NOT RESOLVED>` | `<REF>` |
| B3 | Maintenance window agreed; closure/reopen announcement channels identified | `<NOT RESOLVED>` | `<REF>` |
| B4 | Backup destination writability, ≥ 30-day retention, and restore-readability demonstrated | `<NOT RESOLVED>` | `<REF>` |
| B5 | Snapshot + isolated snapshot provisioned; protected connection bundle created (paths recorded in input file) | `<NOT RESOLVED>` | `<REF>` |
| B6 | Deployment-host `git rev-parse HEAD` equals the declared reviewed commit (attest boolean equality only) | `<NOT RESOLVED>` | `<REF>` |

## C. Preflight attestations — PF-01…PF-15

Record `PASS` / `FAIL` / `PENDING` and a redacted reference for each. Any FAIL or anomaly is also logged in the [operational discrepancy register](./room-cutover-gate2-discrepancy-register.md).

| # | Attestation (redacted; outcome only) | Outcome | Reference |
|---|---|---|---|
| PF-01 | Host HEAD equals reviewed commit (boolean) | `<PENDING>` | `<REF>` |
| PF-02 | False/false rollback pair captured, pinned, archived, re-verified before any rebuild/prune | `<PENDING>` | `<REF>` |
| PF-03 | Evidence dir exists, mode `0700`, outside repo/web paths | `<PENDING>` | `<REF>` |
| PF-04 | Target slug verified unused and non-reserved | `<PENDING>` | `<REF>` |
| PF-05 | Host-user-id existence check returned exactly one row (`t`); no DSN in argv | `<PENDING>` | `<REF>` |
| PF-06 | Live account → host-user-id equality confirmed (`t`) via `\getenv`/`:'email'`; address never on a command line or in a file | `<PENDING>` | `<REF>` |
| PF-07 | Live login confirmed on false deployment; login eligibility, account role, and room-host membership distinguished | `<PENDING>` | `<REF>` |
| PF-08 | Allowlisted pairing evidence reviewed; same-mode pairing; no env block/credential/full render | `<PENDING>` | `<REF>` |
| PF-09 | Both pairs built from reviewed commit; backend has `/app/server` + `/app/room-cutover`; `:true` IDs recorded (in input file) | `<PENDING>` | `<REF>` |
| PF-10 | Snapshot `plan` and isolated `up --dry-run` completed via `:true` image; reports durable, `0600`, PII-free; counts plausible/consistent; R14b first-cutover readiness proven | `<PENDING>` | `<REF>` |
| PF-11 | Gate 1 isolated end-to-end rehearsal evidence accepted | `<PENDING>` | `<REF>` |
| PF-12 | Backup location writable, verified, ≥ 30-day retention | `<PENDING>` | `<REF>` |
| PF-13 | Migration state clean, schema version exactly 9; identity client supports `\getenv` | `<PENDING>` | `<REF>` |
| PF-14 | Rollback/abort rules read aloud and acknowledged by Operator and Product Owner | `<PENDING>` | `<REF>` |
| PF-15 | Connection bundle in place, each `0600`, outside repo/web paths; non-`export` assignments only; no DSN/credential in any planned argv | `<PENDING>` | `<REF>` |

## D. Discrepancies raised this return

List discrepancy register IDs raised or updated in support of this return (or "none").

| Discrepancy ID | Summary (redacted) | Severity | Disposition |
|---|---|---|---|
| `<D-00>` | `<none>` | `<n/a>` | `<n/a>` |

## E. Operator sign-off

| Item | Value |
|---|---|
| Operator attests all outcomes above are truthful and redacted | `<OPERATOR_INITIALS>` / `<DATE>` |
| No production action was taken outside the approved window (this return is preflight only) | `<CONFIRM>` |
| No credential, DSN, live account address, or raw identity is recorded in this file | `<CONFIRM>` |

This evidence-return does not authorize Gate 2. It supplies redacted inputs to the Product Owner, who alone records GO / NO-GO / DEFER in the readiness package Section 9.
