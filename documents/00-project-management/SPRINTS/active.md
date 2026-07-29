# Active Sprint

**Active sprint: R14c — Coordinated authoritative room cutover** ([`029-coordinated-authoritative-room-cutover.md`](./029-coordinated-authoritative-room-cutover.md)).

R14c was approved by the Product Owner and activated on 2026-07-28 on branch `sprint/r14c-coordinated-production-cutover`, cut from `dev` at approved base commit `13c09549fc89febac2c79085afcf7249a43f62a4`.

Gate 1 implementation is open as **PR #26** (base `dev`, changed-file count per GitHub PR #26) with review-head sequence `1502c3a243b3350b6a18ff2454118981f4032357` (initial Gate 1) → `0daa8fbe22b896c6def5f2e75af129792ee41229` (first corrective pass, F1–F6) → the new corrective head from this pass (exact SHA posted to PR #26 at push). Architect review returned focused findings; the corrective changes are prepared for re-review and Gate 1 remains **pending re-review — not accepted**. The Builder does not commit, push, or advance the sprint.

Activation authorizes **Gate 1 only**: implementation, automated verification, Docker/Compose artifact pairing, packaging of the existing `room-cutover` CLI, an isolated production-like rehearsal, and completion of the redacted operational runbook.

Activation does **not** authorize Gate 2. The Builder must not execute production migration commands, deploy the `true` server/SPA pair, close or reopen public traffic, modify production data or secrets, or perform the live maintenance window. Production execution requires a separate Product Owner go/no-go after Architect review and Gate 1 acceptance.

R14c owns the runtime server flag `--room-cutover-authoritative`, the fail-closed schema/marker startup guard, no-op versus PostgreSQL room-activity writer composition, atomic repository-free `410 Gone` tombstones for all approved legacy global REST routes and `/ws`, deployment pairing with R14d's `VITE_ROOM_CUTOVER_AUTHORITATIVE` bundle, and the production cutover/rollback runbook.

R14d remains closed and accepted. Its `false` bundle remains the pre-cutover and rollback-compatible frontend; its `true` bundle has not been deployed. Production cutover has **not** been executed.

R14e remains inactive. Migration 0010, schema version 10, and legacy-table deletion are not authorized. Room epic Issue #17 remains open.
