# Sprint 030 — R14c Gate 2 Readiness Package

**Status:** In progress — documentation-only; prepared as an unmerged PR awaiting Architect review and Product Owner approval  
**Branch:** `sprint/r14c-gate2-readiness`  
**Base:** `dev` at `6d550ee0db3b64678147d69c9d04333587f20eb3`  
**Parent epic:** Issue #17  
**Risk:** Low (documentation only — no code, schema, deployment, or production change)  
**Scope:** Project-management and deployment documentation only. No production access of any kind.

## Goal

Assemble one authoritative Gate 2 readiness package that overlays the accepted R14c production runbook, so the Product Owner can make an evidence-based GO / NO-GO / DEFER decision for the production cutover window. Following the Architect reviews of PR #27, the sprint also carries focused documentation-only security corrections to the runbook itself: replacing every command that expanded a DSN or credential into process arguments with the approved non-argv connection-supply model, keeping the snapshot DSN variables un-exported in the operator shell, and replacing retained full Compose renders with allowlisted pairing evidence (command order and behavior unchanged).

## Current behavior

At the approved base:

- R14c Gate 1 is integrated: PR #26 was squash-merged into `dev` as `c8ab4af029d10dda889d1165464e16068a5be573` (2026-07-29); the implementation lifecycle is closed.
- The accepted runbook `documents/07-deployment/room-cutover-runbook.md` exists, placeholders only, not executed.
- Gate 2 — production execution — remains explicitly pending; no GO record, role assignments, approved operator inputs, or scheduled maintenance window exist.
- Readiness, custody, blocker, and decision material exists only inline in the runbook and sprint 029; there is no single Product Owner-facing readiness document, no operator-local input template, and no recorded GO / NO-GO / DEFER checklist.

## Desired behavior

- `documents/07-deployment/room-cutover-gate2-readiness.md` is the single authoritative Gate 2 readiness overlay, containing:
  - the complete placeholder/input inventory (all six Gate 2 operator inputs, the supporting placeholders including the protected connection bundle, and every environment variable and fixed identity the runbook references), each with owner and secure supply method;
  - the single approved non-argv connection-supply model (Section 2.4): all connection data lives in the operator-local protected connection bundle (`<PGSERVICE_FILE>`, `<PGPASS_FILE>`, `<CONNECTION_ENV_FILE>`, each mode `0600`); host `psql`/`pg_dump` connect only via `PGSERVICEFILE`/`PGSERVICE`/`PGPASSFILE`; every `room-cutover` invocation omits `--postgres` and consumes `DATABASE_URL` from the environment (name-only `-e DATABASE_URL` passthrough in rehearsals, Compose service environment in the live window); the snapshot DSN variables are plain non-`export` assignments that stay private to the operator shell; no DSN, password, or credential ever appears in argv;
  - maintenance roles and separation-of-duties rules;
  - artifact custody rules for evidence, backups, and the protected rollback pair, including that retained Compose evidence is limited to allowlisted pairing lines (`pairing-false.txt` / `pairing-true.txt`: image references and authoritative flags only) because the full `docker compose config` render interpolates deployment secrets and is never written to disk;
  - the current blocker list (B1–B7);
  - the Product Owner-facing preflight verification checklist mapped to the runbook's pre-window steps, including R14b first-cutover readiness (target slug absent, `room_activities` empty, exactly one `queue_state` row `id = 1` and exactly one `auto_queue_config` row `id = 1`);
  - abort rules for before-window, pre-`up`, and at/after-`up` phases, with the hard prohibitions (no `abort`/`force`/`reset`/hash-edit tooling, no marker deletion or edit, no migrated-room deletion, no cutover rerun);
  - Gate 2 entry criteria E1–E10 and a compact GO / NO-GO / DEFER decision checklist with a recorded decision block.
- `documents/07-deployment/room-cutover-gate2-inputs.template.md` is a tracked, placeholder-only operator-local input template that is copied outside the repository, filled at mode `0600`, and never committed; the live Google account address and all DSNs/passwords are never written into it (connection data lives only in the connection bundle; the input file records bundle file paths only).
- Project-management trackers (`PROJECT_STATE.md`, `SPRINTS/active.md`, `SPRINTS/README.md`) reflect this sprint accurately.
- The accepted runbook carries only the Architect-required amendments from this sprint — the non-argv connection-supply correction (a "Connection supply" section plus the reworked step 5/6/11/12 and backup commands), the un-exported private sourcing of `<CONNECTION_ENV_FILE>`, and the allowlisted pairing-evidence extraction replacing retained full Compose renders (step 9 and the evidence list); its step order, gates, and behavior are otherwise unchanged. On conflict about what to execute, the runbook prevails; on conflict about whether execution is authorized, the readiness package and the Product Owner decision prevail.

## Deliverables

- `documents/07-deployment/room-cutover-gate2-readiness.md` (new)
- `documents/07-deployment/room-cutover-gate2-inputs.template.md` (new)
- `documents/00-project-management/SPRINTS/030-r14c-gate2-readiness-package.md` (this record, new)
- `documents/00-project-management/SPRINTS/active.md` (updated)
- `documents/00-project-management/SPRINTS/README.md` (index row added)
- `documents/00-project-management/PROJECT_STATE.md` (updated)
- `documents/07-deployment/room-cutover-runbook.md` (corrective passes only: the non-argv connection-supply, private DSN-variable, and allowlisted pairing-evidence amendments required by the PR #27 Architect reviews)

## Out of scope

- Any production access: SQL, lookups, backups, migrations, deployments, traffic control, true-mode start, rollback actions.
- Executing any runbook step, including "read-only" ones (snapshot plan, dry-run rehearsal).
- Any R14e action: migration 0010, schema version 10, legacy-table deletion.
- Modifying any code, schema, Docker/Compose configuration, or frontend asset. Runbook edits are limited strictly to the Architect-required security corrections (non-argv connection supply, un-exported DSN variables, allowlisted pairing evidence); no step reordering or behavior change.
- Approving Gate 2, assigning roles, scheduling the window, or supplying real input values — those remain Product Owner decisions recorded through the readiness package itself.
- Merging the PR or advancing sprint status.

## Verification

Documentation-only change set; the relevant checks are documentation-integrity checks:

```bash
git diff --check          # no whitespace errors
git diff --name-only dev  # docs-only file list
git status --short
grep -RInE "(@gmail|@googlemail|postgres://|postgresql://[^<])" documents/07-deployment/  # no real identities/DSNs
# no command supplies a DSN/credential via argv (no --postgres value, no DSN placeholder or $DATABASE_URL as a psql/pg_dump argument)
grep -RInE -- '--postgres[ =]"?[<$]|(psql|pg_dump) +"?[<$]' documents/07-deployment/
# no retained full Compose render and no auto-export sourcing of the connection env file
grep -RInE 'compose config +>|set -a' documents/07-deployment/
```

No Go, frontend, or Docker verification applies — no code or configuration file is touched.

## Record

- Prepared from `dev` at `6d550ee0db3b64678147d69c9d04333587f20eb3` on branch `sprint/r14c-gate2-readiness` as an unmerged PR for Product Owner review.
- Delivered as PR #27 (head `f56d3276bb8b12d742612b530000b10467239dca`). The Architect review returned one Blocking documentation/security finding: snapshot `psql`, `--postgres`, and `pg_dump "$DATABASE_URL"` commands expanded real connection values into argv. A corrective pass on the same branch (documentation-only, unmerged, head `eb68393caebda64673666f00c39c58fce6047b46`) adopted the protected non-argv connection-supply model across the runbook, readiness package, and input template; no runtime, schema, Docker/Compose, frontend, or production change.
- The Architect re-review of head `eb68393` resolved the argv finding but returned one further Blocking finding: retained `docker compose config` renders (`compose-false.yml` / `compose-true.yml`) interpolate deployment secrets (database credentials, `DATABASE_URL`, Google and DuckDNS values) despite being classified PII-free, and the `set -a` sourcing exported both snapshot DSNs to every child process contrary to the documented per-command scope. A second corrective pass on the same branch (documentation-only, unmerged) replaced the retained renders with allowlisted pairing evidence (`pairing-false.txt` / `pairing-true.txt`: image references and authoritative flags only, full render never written to disk) and switched `<CONNECTION_ENV_FILE>` to plain non-`export` assignments sourced privately, with artifact names, custody rules, preflight checks, and verification scans aligned.
- No production system was accessed and no runbook step was executed in preparing this sprint or its corrective passes.
- Gate 2 remains pending until a GO is recorded in the readiness package's decision block.
- R14e remains inactive. Room epic Issue #17 remains open.
