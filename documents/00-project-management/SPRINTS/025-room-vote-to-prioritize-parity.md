# R09h – Room vote-to-prioritize parity

**Status:** R09h was **implemented on `dev` and accepted by the
Product Owner on 2026-07-24** at commit
`95ea6a36dfab80c476db4d8b7dad1d2799977d4d` (acceptance recorded on the
room epic Issue #17, which remains open for the broader room epic).
R09h is the room-scoped democratic **vote-to-prioritize** parity slice
for the R09 player-control epic and the second-listed blocking
prerequisite for the entire R14c cutover (sequence
`R05b → R09h → R14b → R09i → R14d → R14c → R14e`); that R09h
prerequisite for R14c is now **satisfied**. It is backend + WebSocket
only (no frontend UI; matches the R09b vote-to-skip scope). With R09h
closed, **no sprint is active on `dev`**; R14b, R09i, R14d, R14c, and
R14e remain planned and NOT active.

**Sprint name:** Room vote-to-prioritize parity

## Goal

Give a room the same democratic vote-to-prioritize capability the
global surface already exposes, so that when the R14c cutover retires
the legacy global `/api/vote/prioritize` route there is a room-scoped
equivalent to replace it. Any active room member may open or join a
vote to move a specific upcoming song immediately after the current
song; when a strict majority of the connected members agree, the queue
performs exactly one authoritative prioritize.

## Current behaviour (pre-sprint baseline)

- The global `POST /api/vote/prioritize` runs an in-memory global vote
  session over the global queue (R05 contract).
- R07d already exposes a **direct** room prioritize —
  `POST /api/rooms/{slug}/queue/prioritize` — which is a single-actor
  queue mutation, not a democratic vote.
- R09b added room vote-to-**skip** (`POST /api/rooms/{slug}/vote/skip`)
  with the in-memory `roomvote.Interactor`, the per-room
  `room_vote_updated` / `room_vote_resolved` events, and the
  server-owned `expiryAdapter` ticker.
- There was no room-scoped vote-to-**prioritize**. That gap blocks the
  entire R14c cutover because a partial cutover would leave the legacy
  global prioritize vote as the only democratic prioritize path.

## Desired behaviour (post-sprint)

R09h adds one HTTP mutation and reuses existing per-room WebSocket
events without changing global queue behaviour, global vote thresholds,
priority balances, auto-queue, R09b vote-to-skip, or the R07d direct
room-prioritize contract.

### HTTP

- `POST /api/rooms/{slug}/vote/prioritize` — behind the existing
  `roomAuth` (`makeRoomActor`) wrapper (bearer token). Request body is
  exactly `{"song_index": <integer>}`. Strict JSON decoding rejects a
  missing, `null`, negative, malformed, trailing-data, or unknown-field
  body with `400`. Actor identity comes ONLY from the resolved bearer
  session (`actorFromCtx`); any body-supplied identity is ignored.
- Any active room member (host / admin / guest) may cast one ballot. No
  room role, global role, or player lease is required.

### Vote session state machine

- Prioritize sessions live in the **same** in-memory
  `roomvote.Interactor` map as skip sessions — no second voting
  package, session map, mutex, ticker, or background worker is
  introduced. Keys are `prioritize:{slug}:{songID}`; skip keys remain
  `skip:{slug}:{songID}`, so the two types coexist without collision.
- The first ballot for a target song creates a 30-second session;
  subsequent ballots within the window add the voter to `VotedBy`
  (`map[int]bool`). Sessions are single-instance only and never
  persisted.
- A prioritize ballot that arrives after the prior session for the
  **exact same** `(slug, songID)` target has expired evicts and
  snapshots that old session, creates a fresh session, casts on it, and
  returns `Resolution="expired"`. The handler fans out one
  `room_vote_resolved{"expired"}` for the evicted session BEFORE the
  fresh session's `room_vote_updated`, then replies HTTP
  `200 {"resolution":"expired"}`. Eviction-on-entry is scoped to THIS
  key only, so a coexisting session for a different target song is left
  to the shared sweep.
- A live (non-expired) session keyed on the same song ID whose
  snapshotted index no longer matches the requested index (two distinct
  queue entries share one video ID) is rejected immediately with
  `ErrStalePrioritizeSession` **before** its ballot map is touched, so
  ballots for one entry can never accrue toward a different entry's
  session.
- Threshold is captured at session creation via
  `(*RoomWSHub).UniqueConnectedUserIDs(slug)` using the existing room
  strict-majority helper `max(2, n/2 + 1)`. `entity.MajorityThreshold`
  is neither used nor modified.
- Session-key parsing is generalized (`splitSessionKey` handles both
  `skip:` and `prioritize:` prefixes) so the single existing
  `ExpireSessions` sweep reaps both types. No second expiry loop is
  started. The skip cast's eviction-on-entry predicate (`hasSlugPrefix`)
  still matches only `skip:{slug}:` keys, so a skip cast never disturbs
  a coexisting prioritize session.
- The ticker-driven `ExpireSessions` sweep reports each expired
  prioritize session's `session_id` as the session's own
  `session.ID` (`prioritize:{songID}`) — the SAME identifier the client
  saw on `room_vote_updated` and the passed / expired-on-entry
  resolutions — so a client can correlate the ticker expiry with the
  session it was tracking. Skip sessions retain the internal map key
  (`skip:{slug}:{songID}`) as their expiry `session_id` to preserve the
  accepted R09b identifier contract.

### Queue-owned prioritize

- `roomqueue.Interactor.PrioritizeVote` re-resolves the active room,
  acquires the existing queue mutation mutex, loads fresh state, and
  verifies the stored snapshot index still identifies the same
  non-current target song. It rejects removed, moved, ambiguous
  (duplicate song ID), and no-current-song targets as stale with the
  sentinel `ErrStalePrioritizeVote` (mapped to HTTP `409`) and **zero**
  side effects (no partial mutation, no save). A target that became the
  currently-playing song is a distinct case: it returns
  `entity.ErrVoteOnCurrentSong` (mapped to HTTP `400`, **not** `409`),
  also with zero side effects. On success it calls the existing
  `entity.Queue.Prioritize`, saves exactly once, and returns the
  authoritative event tuple `(state, fromIndex, toIndex, song)`. It
  never broadcasts and never touches priority balances.

### Per-room WebSocket events (reused; no new constant)

- `room_vote_updated` — broadcast on every successful ballot.
- `room_vote_resolved` — broadcast when the session is deleted
  (`outcome="passed"` on a passing vote, `outcome="expired"` on
  expiry).
- On a passing vote, the existing `room_queue_song_prioritized` event
  carries the authoritative prioritize result. No new WebSocket event
  constant is added.
- `VotedBy` remains absent from the wire; the session is serialised
  through the existing `RoomVoteSessionDTO`, which omits `voted_by`.
- The global `/ws` event inventory is unchanged; these events ride
  `GET /ws/rooms/{slug}` only.

### Error mapping

- `400`: `room.ErrInvalidSlug`, `entity.ErrNoCurrentSong`,
  `roomqueue.ErrInvalidIndex`, `entity.ErrVoteOnCurrentSong`, and any
  strict-body-decode failure.
- `403`: `room.ErrForbidden` (non-member).
- `409`: `roomvote.ErrStalePrioritizeSession` (mapped before the skip
  `ErrStaleSession`; distinct sentinel).
- `410`: `entity.ErrVoteSessionExpired`.

### Out of scope (explicit non-goals)

- No change to global `/api/vote/prioritize`, global
  `/api/queue/prioritize`, `/ws`, R09b vote-to-skip, or the R07d direct
  room-prioritize contract.
- No new WebSocket event constant; `VotedBy` never crosses the wire.
- No priority-balance debit, no auto-queue trigger, no persistence /
  migration / schema change.
- No frontend UI; no Docker / Nginx / CORS / auth / session changes.
- R14b, R09i, and every later R14 slice remain inactive.

## Tests

Focused tests were added at each layer:

- **Use case** (`internal/usecase/roomvote`): membership, invalid /
  current-song index rejection, threshold at creation, duplicate
  ballots, pass → queue-owned prioritize, stale target surfaces
  conflict, a same-song-ID/different-index live session rejected with
  `ErrStalePrioritizeSession` without touching its ballot map, an
  expired matching session restarting a fresh session with
  `Resolution="expired"` (fresh-session identity, reset ballots,
  unchanged conflicting ballots, zero prioritize calls), the expiry
  sweep recovering the slug from a prioritize key AND reporting the
  prioritize expiry `session_id` as `session.ID`
  (`prioritize:{songID}`) while a coexisting skip session keeps its
  map-key identifier, and skip/prioritize session isolation.
- **Queue** (`internal/usecase/roomqueue`, DB-gated): happy-path move +
  persist; removed / moved / no-current / duplicate song ID all return
  `ErrStalePrioritizeVote` (409) with no mutation, no broadcast, and no
  priority debit; a became-current target returns
  `entity.ErrVoteOnCurrentSong` (400) with the same zero side effects.
- **Handler** (`internal/delivery/http`): strict body validation table,
  401 unauthenticated, 403 non-member, 400 current / out-of-range
  index, 204 plain cast, 200 + broadcasts on pass, 409 duplicate
  ballot, removed-target-between-casts, and `writeRoomVoteError`
  sentinel mapping.
- **WebSocket** (`internal/delivery/ws`): a prioritize session rides the
  `room_vote_updated` envelope with `voted_by` / `VotedBy` absent from
  the marshaled payload.
- **Expiry adapter** (`cmd/server`): a mixed skip + prioritize expiry
  batch fans out both `room_vote_resolved outcome="expired"`
  broadcasts.
- **Route** (`cmd/server`): the prioritize route is registered behind
  `roomAuth` (401 without token; 405 on GET).
- **Skip regression**: `splitSessionKey` / `hasSlugPrefix` unit tables
  and a behavioral test pinning that a skip cast's eviction-on-entry
  never reaps a coexisting prioritize session.

## Verification

- `go vet ./cmd/... ./internal/...`
- `go test ./cmd/... ./internal/...`
- `go test -race ./cmd/... ./internal/...`
- PostgreSQL-backed scoped tests (the DB-gated `roomqueue` prioritize
  cases run against a live PostgreSQL instance, not skipped)
- PostgreSQL-backed focused prioritize tests
- `git diff --check`

## Corrective history

- The runtime freshness corrections (expired-restart on next ballot,
  same-ID/different-index guard, became-current →
  `entity.ErrVoteOnCurrentSong` → `400`) landed at actual `dev` head
  `357f4d2bd90efe2749dc5d5560fb652267ec1277`. An earlier review cited
  the predecessor `6c5dd479…`; the actual corrective head is
  `357f4d2…` and its commit spans six runtime/test files despite a
  documentation-flavoured commit message.
- A follow-up pass (baseline `357f4d2…`) corrected the ticker-driven
  `ExpireSessions` prioritize `session_id` to use `session.ID`
  (`prioritize:{songID}`) so it correlates with `room_vote_updated`,
  added a focused expiry-identifier regression test, and reconciled
  this document, `SPRINTS/active.md`, and the stale prioritize handler
  comments with the accepted contract. The accepted skip identifier
  contract was left unchanged.

## Closure

R09h is **closed and accepted by the Product Owner on 2026-07-24** at
commit `95ea6a36dfab80c476db4d8b7dad1d2799977d4d`, recorded on the room
epic Issue #17 (which remains open because it tracks the broader room
epic). The R09h blocking prerequisite for the R14c cutover is
**satisfied**. No sprint is active on `dev` after this closure; R14b,
R09i, R14d, R14c, and R14e remain planned and NOT active. This closure
pass is documentation-only — it changes no runtime code, tests,
routes, WebSocket events, migrations, or contracts — and leaves the
accepted R09h implementation and the corrective history above
unchanged.
