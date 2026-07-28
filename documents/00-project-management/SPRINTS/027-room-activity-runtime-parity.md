# R09i — Room activity runtime parity

**Status:** Closed and accepted by the Product Owner on 2026-07-28  
**Parent epic:** Issue #17 — remains open  
**Sequence:** `R05b → R09h → R14b → R09i → R14d → R14c → R14e`  
**Feature branch:** `sprint/r09i-room-activity-runtime-parity`  
**Pull request:** [#24](https://github.com/hoanganh-ng/local-music-queue/pull/24)  
**Final reviewed feature head:** `3e2542ef0e3ffcb81ffdf3024cc6658e6107a7f4`  
**Merge commit on `dev`:** `c112000d2a70d105419a57f4059bafd4051e423d`

## Closure

R09i was activated by the Product Owner on 2026-07-27. After the implementation, two focused corrective passes, and final Architect acceptance, the Product Owner accepted R09i and merged PR #24 into `dev` on 2026-07-28.

No subsequent sprint was activated by this closure. R14d is next in the approved sequence but remains inactive. R14c, R14e, and unrelated planned work also remain inactive.

## Delivered behavior

R09i delivered backend room-activity production parity for qualifying room queue, playback, voting, and auto-queue mutations:

- an explicit stateless `NoopRoomActivityRepository` implementing the complete existing room-activity repository contract;
- one no-op writer instance injected into roomqueue, roomvote, and roomautoqueue during normal pre-R14c server composition;
- the approved activity matrix for successful manual queue changes, playback changes, vote ballots/resolutions/actions, and auto-queue insertion;
- actor identity from backend-authenticated context, with blank display names mapped to `user #<id>` and system transitions attributed to `System`;
- exact ordered vote batches for expiry, cast, pass, and successful action activity;
- synchronous best-effort activity writes that never turn an already-successful primary mutation into a failure;
- no `AddActivity` call while the roomqueue mutation mutex, roomvote session mutex, or roomautoqueue coordinator mutex is held;
- deterministic proof that a blocked activity append for one room does not stall a vote in another room;
- a real-composition regression proving the same no-op writer is selected for all three activity producers and the PostgreSQL writer is not selected;
- a local observer-parameter composition seam with no package-level mutable composition state.

## Preserved contracts

R09i added no REST route, WebSocket event type, migration, schema change, frontend change, deployment change, OAuth/session redesign, runtime cutover flag, or cutover-marker selection path.

Schema version remains 9. Normal server startup still selects only the no-op room-activity writer. Production `room_activities` persistence remains disabled until R14c performs the coordinated cutover, copies legacy activity data, resynchronizes `room_activities_id_seq`, validates the durable cutover marker, and explicitly selects the real PostgreSQL writer.

Production cutover has not been executed.

## Review history

- Initial implementation: `a42657c4f4880e1d0ee6a6458ba847f5fc11e1f8`
- Initial project-management update: `e38680bef88c9045babd86cce0ec634cd5334283`
- First corrective pass: `43e229b9412a58b2ef5d93e26ae75d8dc4370cf6`
- Second corrective pass: `3e2542ef0e3ffcb81ffdf3024cc6658e6107a7f4`
- Final Architect verdict: **Accepted**
- Product Owner acceptance and merge: `c112000d2a70d105419a57f4059bafd4051e423d`

PR #24 is the authoritative record for the exact changed-file scope, review findings, corrective reports, implementation discussion, and reported verification outcomes.

## Verification record

The accepted head recorded passing focused use-case/server suites, focused race suites, persistence suites, `go vet`, formatting checks on changed files, and `git diff --check`.

Full-suite runs retained documented pre-existing PostgreSQL environmental failures:

- connection exhaustion in `internal/delivery/http` (`SQLSTATE 53300`), reproduced before the second corrective change and passing on isolated affected-test reruns;
- cross-package advisory-lock contention in room-cutover tests under default parallelism, passing in isolation and under serialized execution.

GitHub had no CI status for the accepted head. R09i closure therefore does not claim a fully green project-wide test suite.

## Deferred work

- R14d — frontend global-path removal and cutover-gated behavior: planned, not active.
- R14c — coordinated production cutover and guarded real-writer selection: planned, not active.
- R14e — legacy global-table schema cleanup: planned, not active.
- Room epic Issue #17 remains open.

## Historical sources

- [Original approved R09i handoff at activation commit](https://github.com/hoanganh-ng/local-music-queue/blob/323bd1600ede60c40037ab7e2af2b58a79d0acec/documents/00-project-management/SPRINTS/027-room-activity-runtime-parity.md)
- [PR #24](https://github.com/hoanganh-ng/local-music-queue/pull/24)
- [Merge commit](https://github.com/hoanganh-ng/local-music-queue/commit/c112000d2a70d105419a57f4059bafd4051e423d)
- [`active.md`](./active.md)
- [`PROJECT_STATE.md`](../PROJECT_STATE.md)
