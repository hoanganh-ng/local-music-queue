# R14c Gate 2 — Operator Evidence-Return (TEMPLATE, Redacted)

**This tracked file is a template and contains placeholders only.**

This template is how the human **Operator** returns *redacted* attestations of preflight and blocker work back to the **Product Owner** for the Gate 2 go/no-go review. It feeds the [preflight status ledger](./room-cutover-gate2-preflight-ledger.md); the Product Owner accepts a returned attestation before any ledger row moves to `Resolved`.

Per Sprint 032 — R14c Solo-Operator Governance Amendment (`documents/00-project-management/SPRINTS/032-r14c-solo-operator-governance.md`), this return is **pass 1 (Operator evidence pass)** of the mandatory three-pass Gate 2 sequence: the completed return goes to **pass 2 (Architect review pass** — outcome limited to `READY FOR PO DECISION`, `DEFER — EVIDENCE INCOMPLETE`, or `NO-GO RECOMMENDED`; grants no production authority**)** before **pass 3 (Product Owner GO / NO-GO / DEFER decision pass)**. A GO recorded before the Architect review pass is invalid. This sequence applies whether the Operator and Product Owner roles are held separately (the normal preference) or combined under the approved named exception (`hoanganh-ng`, R14c Gate 2 only).

Usage (see [`room-cutover-gate2-readiness.md`](./room-cutover-gate2-readiness.md) Sections 6–9, and [`room-cutover-gate2-preflight-ledger.md`](./room-cutover-gate2-preflight-ledger.md)):

1. Copy this file to an operator-controlled location **outside the repository working tree and outside any web-served path**.
2. `chmod 0600` the copy.
3. Fill in **redacted** outcomes only. **Never commit the filled copy, never paste it into an issue/PR/chat, and never add real values to this tracked template.**
4. Return the redacted attestation to the Product Owner by the agreed secure channel; the Product Owner records acceptance in the readiness package and the ledger.
5. Destroy or archive the filled copy per Product Owner instruction after the rollback window closes.

## Status model (aligned with the ledger)

Each returned item carries one of the ledger statuses: `Not started`, `In progress`, `Resolved` (recommend-resolve — becomes `Resolved` in the ledger only after Product Owner acceptance), `Failed` (substantive failed condition; discrepancy raised), `Blocked` (required person, input, environment, or dependency unavailable; named), or `Not applicable` (only where the ledger explicitly permits it — currently no row does).

## Redaction rules (mandatory)

- Record **only** attestation outcomes: `t` / boolean pass-fail, "verified", counts as order-of-magnitude bands (e.g. "hundreds"), or a redacted **protected-reference identifier** such as `EVID-REF-07 (rehearsal plan report, reviewed, PII-free)`.
- **Protected-reference identifiers, never paths.** This return never records an actual infrastructure path, directory, or filename layout. Assign each piece of protected evidence an opaque identifier (e.g. `EVID-REF-01`, `EVID-REF-02`, …) in an operator-kept mapping stored with the protected evidence itself; only the identifiers appear in this return.
- **Safe timestamps only.** Record dates as YYYY-MM-DD (optionally a coarse part of day). Never record precise times, timezones, or anything that reveals the unannounced maintenance window.
- **Never** write: any DSN, password, or connection credential; the live Google account address; a raw email; a production hostname; an absolute host path that reveals infrastructure; an image ID/digest; a commit SHA; the target room slug; the host user id; or raw report contents.
- Connection data lives only in the protected connection bundle (readiness §2.4); this evidence-return records that the bundle and its checks passed, not what is in it and not where it is.
- `<LIVE_ALLOWED_GOOGLE_ACCOUNT>` is never written here — attest only "login confirmed on false deployment; maps to approved host user id (`t`)" with a confirmation reference.

## A. Return header

| Field | Value |
|---|---|
| Operator (name or approved handle) | `<OPERATOR>` |
| Product Owner of record | `<PRODUCT_OWNER>` |
| Return date (safe, YYYY-MM-DD) | `<RETURN_DATE>` |
| Operator-local input file | `<INPUT-FILE-REF>` (protected-reference identifier only; never a path) |
| Evidence store | `<EVIDENCE-STORE-REF>` (protected-reference identifier only; never a path) |

## B. Blocker attestations — B1–B6

For each item record every column. Status uses the ledger model (`Not started` / `In progress` / `Resolved` / `Failed` / `Blocked`). Nothing is marked `Resolved` without real evidence, and `Resolved` here is a recommendation until the Product Owner accepts it. Any `Failed` or `Blocked` item must reference a discrepancy-register entry and state the required follow-up.

| ID | Redacted result (attestation) | Status / conclusion | Protected evidence reference | Safe timestamp | Discrepancy ref | Required follow-up | Operator initials/handle |
|---|---|---|---|---|---|---|---|
| B1 | Named Operator and Product Owner-of-record assigned — either separate persons (normal preference) or the approved Sprint 032 named exception (`hoanganh-ng` holds both roles for R14c Gate 2 only); three-pass sequence acknowledged | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| B2 | All six operator inputs approved and recorded in the input file; live account holder availability confirmed | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| B3 | Maintenance window agreed; closure/reopen announcement channels identified | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| B4 | Backup destination writability, ≥ 30-day retention, and restore-readability demonstrated | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| B5 | Snapshot + isolated snapshot provisioned; protected connection bundle created (referenced only by identifier) | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| B6 | Deployment-host `git rev-parse HEAD` equals the declared reviewed commit (attest boolean equality only) | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |

## C. Preflight attestations — PF-01…PF-15

Same columns and status model as Section B. Any `Failed` outcome, and any `Blocked` state, is also logged in the [operational discrepancy register](./room-cutover-gate2-discrepancy-register.md) and cross-referenced in the `Discrepancy ref` column.

| # | Redacted result (attestation; outcome only) | Status / conclusion | Protected evidence reference | Safe timestamp | Discrepancy ref | Required follow-up | Operator initials/handle |
|---|---|---|---|---|---|---|---|
| PF-01 | Host HEAD equals reviewed commit (boolean) | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| PF-02 | False/false rollback pair captured, pinned, archived, re-verified before any rebuild/prune | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| PF-03 | Evidence dir exists, mode `0700`, outside repo/web paths (attested; no path recorded) | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| PF-04 | Target slug verified unused and non-reserved | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| PF-05 | Host-user-id existence check returned exactly one row (`t`); no DSN in argv | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| PF-06 | Live account → host-user-id equality confirmed (`t`) via `\getenv`/`:'email'`; address never on a command line or in a file | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| PF-07 | Live login confirmed on false deployment; login eligibility, account role, and room-host membership distinguished | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| PF-08 | Allowlisted pairing evidence reviewed; same-mode pairing; no env block/credential/full render | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| PF-09 | Both pairs built from reviewed commit; backend has `/app/server` + `/app/room-cutover`; `:true` IDs recorded (in input file) | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| PF-10 | Snapshot `plan` and isolated `up --dry-run` completed via `:true` image; reports durable, `0600`, PII-free; counts plausible/consistent; R14b first-cutover readiness proven | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| PF-11 | Gate 1 isolated end-to-end rehearsal evidence supplied/referenced for Product Owner acceptance | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| PF-12 | Backup location writable, verified, ≥ 30-day retention | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| PF-13 | Migration state clean, schema version exactly 9; identity client supports `\getenv` | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| PF-14 | Rollback/abort rules read aloud and acknowledged by Operator and Product Owner | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |
| PF-15 | Connection bundle in place, each `0600`, outside repo/web paths; non-`export` assignments only; no DSN/credential in any planned argv | `<Not started>` | `<EVID-REF>` | `<YYYY-MM-DD>` | `<D-xx or none>` | `<follow-up or none>` | `<INITIALS>` |

## D. Discrepancies raised this return

List discrepancy register IDs raised or updated in support of this return (or "none"). Every `Failed` or `Blocked` item in Sections B–C must appear here.

| Discrepancy ID | Summary (redacted) | Severity | Work stopped? | Required decision (Product Owner / Architect + Product Owner) |
|---|---|---|---|---|
| `<D-00>` | `<none>` | `<n/a>` | `<n/a>` | `<n/a>` |

## E. Operator sign-off

| Item | Value |
|---|---|
| Operator attests all outcomes above are truthful and redacted | `<OPERATOR_INITIALS>` / `<YYYY-MM-DD>` |
| No production action was taken outside the approved window (this return is preflight only) | `<CONFIRM>` |
| No credential, DSN, live account address, raw identity, or infrastructure path is recorded in this file | `<CONFIRM>` |

This evidence-return does not authorize Gate 2. It supplies redacted inputs for the Architect review pass and then the Product Owner, who alone records GO / NO-GO / DEFER in the readiness package Section 9 — and only after the Architect review pass has returned `READY FOR PO DECISION`; a GO recorded before the Architect review pass is invalid. A `Resolved` conclusion in this return becomes `Resolved` in the ledger only after the Product Owner accepts the referenced evidence.
