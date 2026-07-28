# Active Sprint

**Active sprint: R14d — Frontend global-path retirement behind the cutover build gate** ([`028-frontend-global-path-retirement.md`](./028-frontend-global-path-retirement.md)).

R14d was activated on 2026-07-28 on branch `sprint/r14d-frontend-global-path-retirement`, cut from `dev` at the approved base commit `f9760693e174344dba4bccd92fde22c281b3a4e5`. R14d is frontend-only: it prepares the two compatible SPA artifacts behind the single Vite build-time setting `VITE_ROOM_CUTOVER_AUTHORITATIVE` (false = pre-cutover/rollback artifact; true = post-R14c artifact). The true artifact must **not** be deployed during R14d; deployment remains R14c scope.

R14c and R14e remain inactive. Production cutover has **not** been executed. Room epic Issue #17 remains open.

R09i — Room activity runtime parity — was accepted by the Product Owner and closed on 2026-07-28. PR #24 was merged into `dev` with merge commit `c112000d2a70d105419a57f4059bafd4051e423d`; the final reviewed feature head was `3e2542ef0e3ffcb81ffdf3024cc6658e6107a7f4`.

R09i delivered backend room-activity production parity for qualifying room queue, playback, voting, and auto-queue mutations. Normal pre-R14c server composition continues to inject the explicit no-op room-activity repository, so runtime `room_activities` persistence remains disabled until the separately approved R14c cutover work.

The focused, race-focused, persistence, vet, formatting, and diff checks reported for the accepted head passed. Full-suite runs retained the documented pre-existing PostgreSQL connection-exhaustion and advisory-lock environmental failures; this closure does not represent a fully green project-wide suite.

All unrelated planned work remains inactive.

See [`027-room-activity-runtime-parity.md`](./027-room-activity-runtime-parity.md) and [PR #24](https://github.com/hoanganh-ng/local-music-queue/pull/24) for the complete contract, implementation history, corrective passes, verification evidence, and review record.
