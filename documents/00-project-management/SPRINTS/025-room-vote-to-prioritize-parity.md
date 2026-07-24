# R09h – Room vote-to-prioritize parity

**Status:** R09h was **implemented on `dev`; Product Owner acceptance
pending**. R09h is the room-scoped democratic **vote-to-prioritize**
parity slice for the R09 player-control epic and the second-listed
blocking prerequisite for the entire R14c cutover (sequence
`R05b → R09h → R14b → R09i → R14d → R14c → R14e`). It is backend +
WebSocket only (no frontend UI; matches the R09b vote-to-skip scope).
R14b, R09i, and every later R14 slice remain planned and NOT active.

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

### Queue-owned prioritize

- `roomqueue.Interactor.PrioritizeVote` re-resolves the active room,
  acquires the existing queue mutation mutex, loads fresh state, and
  verifies the stored snapshot index still identifies the same
  non-current target song. It rejects removed, moved, ambiguous
  (duplicate song ID), current, or no-current-song targets as stale
  with the sentinel `ErrStalePrioritizeVote` and **zero** side effects
  (no partial mutation, no save). On success it calls the existing
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
  conflict, expiry sweep recovers the slug from a prioritize key, and
  skip/prioritize session isolation.
- **Queue** (`internal/usecase/roomqueue`, DB-gated): happy-path move +
  persist, removed / moved / became-current / no-current / duplicate
  song ID all return stale with no mutation, no broadcast, and no
  priority debit.
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

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `git diff --check`

See the *Implementation summary (R09h)* / *Closure* records appended
here once the Product Owner accepts.
