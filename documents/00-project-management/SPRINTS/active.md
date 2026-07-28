# Active Sprint

**No sprint is currently active.**

R14d — Frontend global-path retirement behind the cutover build gate — was accepted by the Product Owner and closed on 2026-07-28. PR #25 was merged into `dev` at merge commit `67bd57a8abef92d42cdfefb90069563340147d10`; the final reviewed feature head was `491770ed90e3940307df3933d4236508fac838f3`.

R14d delivered two frontend artifacts from one source behind the single Vite build-time setting `VITE_ROOM_CUTOVER_AUTHORITATIVE`: the `false` artifact preserves the authoritative pre-cutover Dashboard/global runtime and remains the current rollback-compatible artifact; the `true` artifact lands on RoomEntry, retires legacy global client state, and disables the global `/ws` client. The true artifact has **not** been deployed.

The accepted verification record reports 443/443 frontend tests and a successful build in each baked mode, plus focused local compatibility checks. Real Google-login and server-authenticated room list/create/open/invite checks remained blocked in the isolated environment and must be completed before or during the separately approved R14c paired maintenance-window smoke test.

R14c is the next sprint in the approved sequence, but it has **not** been activated. R14c, R14e, and all unrelated planned work remain inactive. Production cutover has **not** been executed. Room epic Issue #17 remains open.

See [`028-frontend-global-path-retirement.md`](./028-frontend-global-path-retirement.md) and [PR #25](https://github.com/hoanganh-ng/local-music-queue/pull/25) for the complete contract, implementation, verification evidence, review findings, and closure record.
