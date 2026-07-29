# Active Sprint

**Active sprint: Sprint 030 — R14c Gate 2 Readiness Package** ([`030-r14c-gate2-readiness-package.md`](./030-r14c-gate2-readiness-package.md)) — **documentation-only**, prepared on branch `sprint/r14c-gate2-readiness` from `dev` at `6d550ee0db3b64678147d69c9d04333587f20eb3` as an unmerged PR awaiting Architect review and Product Owner approval. It delivers the authoritative Gate 2 readiness package (`documents/07-deployment/room-cutover-gate2-readiness.md`) and the placeholder-only operator-local input template overlaying the accepted runbook; the Architect-review corrective pass additionally applied one focused documentation-only security amendment to the runbook itself (non-argv connection supply; command order and behavior unchanged). Sprint 030 authorizes no production action of any kind.

The underlying R14c operational state is unchanged: **Gate 1 integrated; Gate 2 production execution pending** ([`029-coordinated-authoritative-room-cutover.md`](./029-coordinated-authoritative-room-cutover.md)).

R14c was approved by the Product Owner and activated on 2026-07-28 on branch `sprint/r14c-coordinated-production-cutover`, cut from `dev` at approved base commit `13c09549fc89febac2c79085afcf7249a43f62a4`.

**Gate 1 is integrated.** PR #26 was accepted and squash-merged into `dev` as commit `c8ab4af029d10dda889d1165464e16068a5be573` on 2026-07-29, closing the review-head sequence `1502c3a243b3350b6a18ff2454118981f4032357` (initial Gate 1) → `0daa8fbe22b896c6def5f2e75af129792ee41229` (first corrective pass, F1–F6) → `328aba41db44ca1fc41e02ee052d2d6c2c63ca36` (second corrective pass, the reviewed head) → the final corrective head (exact SHA recorded only in the PR #26 lifecycle ledger). R14c's implementation lifecycle is closed; no further Gate 1 work is authorized.

Activation authorizes **Gate 1 only**: implementation, automated verification, Docker/Compose artifact pairing, packaging of the existing `room-cutover` CLI, an isolated production-like rehearsal, and completion of the redacted operational runbook.

Integration does **not** authorize Gate 2. **Gate 2 — production execution — remains explicitly pending.** The Builder must not execute production migration commands, deploy the `true` server/SPA pair, close or reopen public traffic, modify production data or secrets, or perform the live maintenance window. Production execution requires a separate Product Owner go/no-go against the merged runbook.

R14c owns the runtime server flag `--room-cutover-authoritative`, the fail-closed schema/marker startup guard, no-op versus PostgreSQL room-activity writer composition, atomic repository-free `410 Gone` tombstones for all approved legacy global REST routes and `/ws`, deployment pairing with R14d's `VITE_ROOM_CUTOVER_AUTHORITATIVE` bundle, and the production cutover/rollback runbook.

R14d remains closed and accepted. Its `false` bundle remains the pre-cutover and rollback-compatible frontend; its `true` bundle has not been deployed. Production cutover has **not** been executed.

R14e remains inactive. Migration 0010, schema version 10, and legacy-table deletion are not authorized. Room epic Issue #17 remains open.
