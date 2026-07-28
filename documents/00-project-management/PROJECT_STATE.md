# Project State Baseline

**Last refreshed:** 2026-07-28  
**Branch:** `dev`  
**Current active sprint:** R14d — Frontend global-path retirement behind the cutover build gate ([`SPRINTS/028-frontend-global-path-retirement.md`](./SPRINTS/028-frontend-global-path-retirement.md))

## Current state

R09i — Room activity runtime parity — is closed and accepted by the Product Owner. PR #24 was merged into `dev` at merge commit `c112000d2a70d105419a57f4059bafd4051e423d`; the final reviewed feature head was `3e2542ef0e3ffcb81ffdf3024cc6658e6107a7f4`.

R09i delivered backend room-activity production parity for qualifying room queue, playback, voting, and auto-queue mutations. Activity ownership, exact descriptions, authenticated actor attribution, vote ordering, best-effort failure behavior, and lock-release rules are covered by focused tests. Normal pre-R14c server composition still injects one explicit `NoopRoomActivityRepository` into roomqueue, roomvote, and roomautoqueue; normal runtime does not select `PostgresRoomActivityRepository`.

No REST route, WebSocket event, schema, frontend, Docker, Nginx, OAuth, session, or deployment contract changed in R09i. Production cutover has not been executed. Room epic Issue #17 remains open.

Verification for the accepted R09i head recorded passing focused, race-focused, persistence, vet, formatting, and diff checks. Full-suite runs retained documented pre-existing PostgreSQL connection-exhaustion and advisory-lock environmental failures; the repository is not represented as fully green project-wide.

## Accepted cutover sequence

The approved order remains:

```text
R05b → R09h → R14b → R09i → R14d → R14c → R14e
```

Completed and accepted prerequisites:

- R05b — room entry and player-lease UI slices
- R09h — room vote-to-prioritize parity
- R14b — schema and offline room-cutover mechanism
- R09i — room activity runtime parity

R14d was activated on 2026-07-28 on branch `sprint/r14d-frontend-global-path-retirement`, cut from `dev` at the approved base commit `f9760693e174344dba4bccd92fde22c281b3a4e5`. R14d is frontend-only. R14c and R14e remain inactive, and production cutover has not been executed.

R14c retains ownership of the coordinated production cutover, the runtime `--room-cutover-authoritative` guard, validation of schema version 9 plus the durable `room_cutover_marker`, and selecting the real PostgreSQL room-activity writer only after legacy activities have been copied and `room_activities_id_seq` has been resynchronized. The Go backend must not consume the SPA gate. R14d behavior remains gated by the Vite build-time `VITE_ROOM_CUTOVER_AUTHORITATIVE` switch and activates only through the coordinated R14c deployment. R14e retains ownership of schema version 10 and removal of legacy global tables.

## Accepted room capability baseline

Accepted room behavior on `dev` includes room lifecycle, invites, membership and role enforcement, player lease flows, room-scoped queue and playback controls, room-scoped voting, room-scoped auto-queue, activity production parity, per-room WebSocket synchronization and deltas, room archival/member removal, and the existing frontend room flows.

Deferred scope remains deferred: R10f+ lifecycle hardening, R11b+ chat expansion, R12 discovery/search work, R13 broad session/authentication hardening, and any later room slices not explicitly activated.

## Completion estimate

The existing room-feature estimate remains approximately **80–85% accepted**. This closure-only update does not recalculate the estimate; major remaining planned work includes R14d, R14c, R14e, and the explicitly deferred hardening slices.

## Authoritative references

- [`SPRINTS/027-room-activity-runtime-parity.md`](./SPRINTS/027-room-activity-runtime-parity.md)
- [`SPRINTS/active.md`](./SPRINTS/active.md)
- [PR #24](https://github.com/hoanganh-ng/local-music-queue/pull/24)
- Room epic Issue #17
- [`ROOM_EPIC_SPRINT_SEQUENCE.md`](./ROOM_EPIC_SPRINT_SEQUENCE.md)
- [`ADRS/003-legacy-global-state-migration-and-contract-retirement.md`](./ADRS/003-legacy-global-state-migration-and-contract-retirement.md)
