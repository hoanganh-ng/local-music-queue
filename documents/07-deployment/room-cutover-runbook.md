# R14c Production Runbook — Coordinated Authoritative Room Cutover

**Sprint:** 029 (R14c) — `documents/00-project-management/SPRINTS/029-coordinated-authoritative-room-cutover.md`
**Status:** Written and reviewed under Gate 1. **NOT executed.** Gate 2 (production execution) requires a separate Product Owner go/no-go.

This runbook uses **placeholders only**. No production hostname, credential, DSN, email address, token, or account identity appears here. Every `<PLACEHOLDER>` must be filled in by the operator from the approved Gate 2 inputs at execution time and must never be committed to the repository.

## Operator inputs (approved at Gate 2 go/no-go)

| Placeholder | Meaning | Constraint |
|---|---|---|
| `<TARGET_ROOM_SLUG>` | Slug of the room the global state is migrated into | Valid, non-reserved, unused slug |
| `<TARGET_ROOM_NAME>` | Display name of the target room | Non-empty |
| `<HOST_USER_ID>` | Canonical PostgreSQL `users.id` made sole room host | Positive integer, must exist in `users` |
| `<MAINTENANCE_WINDOW>` | Agreed start/end of the closed-traffic window | Product Owner approved |
| `<BACKUP_LOCATION>` | Verified destination for the pre-cutover `pg_dump` | ≥ 30-day retention |
| `<LIVE_ALLOWED_GOOGLE_ACCOUNT>` | Real allow-listed Google account for the smoke matrix | Mandatory (R14d's isolated review could not exercise real login) |

Supporting placeholders used below: `<REVIEWED_COMMIT_SHA>`, `<FALSE_PAIR_IMAGE_TAGS>`, `<TRUE_PAIR_IMAGE_TAGS>`, `<PROD_HOST>`, `<DUMP_FILE>`.

## The single pairing input

`ROOM_CUTOVER_AUTHORITATIVE` in the deployment `.env` is the only cutover switch:

- backend receives `--room-cutover-authoritative=${ROOM_CUTOVER_AUTHORITATIVE:-false}` via the Compose `command`;
- frontend is **rebuilt** with build arg `VITE_ROOM_CUTOVER_AUTHORITATIVE=${ROOM_CUTOVER_AUTHORITATIVE:-false}`.

Never set the two halves independently. Flipping the value requires `docker compose build backend frontend` (the frontend value is baked at build time) followed by redeploying **both** containers as one pair.

## Pre-window checklist (all items before `<MAINTENANCE_WINDOW>` opens)

1. Confirm the reviewed commit: `git rev-parse HEAD` on the deployment host equals `<REVIEWED_COMMIT_SHA>` (the accepted R14c review commit).
2. Record the exact **false/false rollback pair**: image IDs/tags of the currently-live backend and frontend (`<FALSE_PAIR_IMAGE_TAGS>`). These are the only approved rollback artifacts.
3. Confirm `<TARGET_ROOM_SLUG>` is unused: authenticated `GET /api/rooms` and a direct slug lookup must show no existing room with that slug; the slug must not be reserved.
4. Validate `<HOST_USER_ID>`: positive integer that exists in the canonical PostgreSQL `users` table (`SELECT id, name FROM users WHERE id = <HOST_USER_ID>;`).
5. Confirm `<LIVE_ALLOWED_GOOGLE_ACCOUNT>` is present in the deployed `HOST_EMAILS`/allow-list configuration and can log in on the current (false) deployment.
6. Render and review **both** Compose configurations, confirming the backend flag and frontend build arg always agree:

   ```bash
   ROOM_CUTOVER_AUTHORITATIVE=false docker compose config > /tmp/compose-false.yml
   ROOM_CUTOVER_AUTHORITATIVE=true  docker compose config > /tmp/compose-true.yml
   ```

7. Build and tag both artifact pairs from `<REVIEWED_COMMIT_SHA>`:

   ```bash
   ROOM_CUTOVER_AUTHORITATIVE=false docker compose build backend frontend   # tag as <FALSE_PAIR_IMAGE_TAGS>
   ROOM_CUTOVER_AUTHORITATIVE=true  docker compose build backend frontend   # tag as <TRUE_PAIR_IMAGE_TAGS>
   ```

   Confirm the backend image contains both `/app/server` and `/app/room-cutover`.
8. Run a read-only plan against a **production snapshot** (never the live database at this stage):

   ```bash
   /app/room-cutover plan --room-slug <TARGET_ROOM_SLUG> --room-name "<TARGET_ROOM_NAME>" \
     --host-user-id <HOST_USER_ID> --postgres "<SNAPSHOT_DSN>" --report-file /tmp/plan-report.json
   ```

   Review the report; it must be PII-free and its counts must look plausible against known production volume.
9. Run `room-cutover up --dry-run` against an **isolated copy** of the snapshot and confirm it reports the same evidence without writing.
10. Confirm the isolated end-to-end rehearsal (Gate 1 evidence) is accepted.
11. Confirm `<BACKUP_LOCATION>` is writable, verified, and retains dumps for at least 30 days.

## Maintenance window procedure

Execute strictly in order inside `<MAINTENANCE_WINDOW>`. Any failure at any step → go directly to **Rollback**.

1. **Close public traffic** (stop ingress/port exposure at `<PROD_HOST>`; method per deployment topology). Announce closure.
2. **Backup:** take and verify a timestamped dump:

   ```bash
   pg_dump "$DATABASE_URL" -Fc -f <DUMP_FILE>
   pg_restore --list <DUMP_FILE> | head    # verify readability
   ```

   Copy `<DUMP_FILE>` to `<BACKUP_LOCATION>` and verify the copy.
3. **Stop frontend and backend** containers; keep PostgreSQL running:

   ```bash
   docker compose stop frontend backend
   ```

4. **Plan against the live database** (read-only):

   ```bash
   docker compose run --rm --no-deps backend /app/room-cutover plan \
     --room-slug <TARGET_ROOM_SLUG> --room-name "<TARGET_ROOM_NAME>" --host-user-id <HOST_USER_ID> \
     --report-file /tmp/live-plan.json
   ```

   Review the report. Abort on any readiness failure.
5. **Execute the cutover** with the approved identity values:

   ```bash
   docker compose run --rm --no-deps backend /app/room-cutover up \
     --room-slug <TARGET_ROOM_SLUG> --room-name "<TARGET_ROOM_NAME>" --host-user-id <HOST_USER_ID> \
     --report-file /tmp/live-up.json
   ```

6. **Verify before any room write:**

   ```bash
   docker compose run --rm --no-deps backend /app/room-cutover verify --report-file /tmp/live-verify.json
   ```

   Must exit 0. No room mutation may happen before this succeeds.
7. **Deploy the true pair as one unit:** set `ROOM_CUTOVER_AUTHORITATIVE=true` in the deployment `.env`, then start the pre-built `<TRUE_PAIR_IMAGE_TAGS>`:

   ```bash
   docker compose up -d backend frontend
   ```

   The backend must start (its fail-closed guard passes only because steps 5–6 completed).
8. **Run the mandatory smoke matrix** (below) while traffic is still closed.
9. **Reopen traffic** only after every mandatory check passes. Announce reopening.

## Rollback (before reopening traffic)

Any failed mandatory check requires an immediate rollback:

1. Stop the true pair: `docker compose stop frontend backend`.
2. Set `ROOM_CUTOVER_AUTHORITATIVE=false` and restore the **exact** recorded `<FALSE_PAIR_IMAGE_TAGS>`; start them as one pair.
3. **Do not** delete or edit `room_cutover_marker`.
4. **Do not** use force/reset/hash-edit tooling of any kind.
5. Retain `<DUMP_FILE>` and all redacted `--report-file` outputs.
6. Record any true-mode room writes made during smoke testing (they exist only in room tables).
7. Defer the choice between forward recovery and database restoration to a separate Product Owner decision.

Rollback is **not** deleting the migrated room and is **not** rerunning the cutover.

## Mandatory production smoke matrix (traffic closed)

### Server and migration

- [ ] True server refuses to start before marker creation (verified in rehearsal; guard log names the missing marker).
- [ ] `schema_migrations` is clean and version ≥ 9.
- [ ] `room_cutover_marker` row `id = 1` exists.
- [ ] `room-cutover verify` succeeded before any room mutation.
- [ ] All reports are PII-free (no emails, tokens, DSNs).
- [ ] Target room `<TARGET_ROOM_SLUG>` exists with `<HOST_USER_ID>` as sole host member.
- [ ] Queue, activity, auto-queue config, and play-history counts match the `up`/`verify` report.

### Retired contracts

- [ ] Each of the 15 legacy REST methods returns exactly `410`, body `{"error":"gone","code":"global_contract_retired","documentation":"documents/00-project-management/SPRINTS/022-legacy-global-state-migration-and-contract-retirement-plan.md"}`, `Content-Type: application/json`, and `Link: </api/rooms>; rel="successor-version"`:
  `GET /api/queue`, `POST /api/queue/add`, `POST /api/queue/skip`, `POST /api/queue/status`, `POST /api/queue/sync`, `POST /api/queue/ended`, `POST /api/queue/prev`, `POST /api/queue/remove`, `POST /api/queue/clear`, `POST /api/queue/volume`, `POST /api/queue/prioritize`, `POST /api/vote/skip`, `POST /api/vote/prioritize`, `POST /api/autoqueue/toggle`, `GET /api/autoqueue/status`.
- [ ] Global `/ws` returns `410` during the HTTP phase and never upgrades or registers a client.

### Preserved contracts (using `<LIVE_ALLOWED_GOOGLE_ACCOUNT>`)

- [ ] Real Google login succeeds.
- [ ] Authenticated room list succeeds.
- [ ] Migrated room `<TARGET_ROOM_SLUG>` opens with queue state matching the report.
- [ ] Room WebSocket `/ws/rooms/<TARGET_ROOM_SLUG>` full sync succeeds.
- [ ] Direct room URL works.
- [ ] Room create / list / open / invite redemption work.
- [ ] Player lease claim / heartbeat / release work.
- [ ] Room queue and playback mutations work.
- [ ] Skip and prioritize votes work.
- [ ] Room auto-queue status / toggle work.
- [ ] `GET /api/user/priority-balance` and `GET /api/youtube/search` remain reachable.

### Activity continuity (after `room-cutover verify`)

- [ ] Perform exactly one approved qualifying room mutation.
- [ ] Exactly the expected room activity is appended to `room_activities`.
- [ ] Its ID is greater than the copied legacy maximum.
- [ ] No legacy `activities` row is appended by the room mutation.

### Frontend

- [ ] Login and `/` land on RoomEntry.
- [ ] No Dashboard route is reachable.
- [ ] Sign-out works.
- [ ] No global `/ws` attempt or reconnect occurs (verify in browser network tab).
- [ ] Room flows use room-scoped APIs only.

## Evidence to retain

- Timestamped `<DUMP_FILE>` in `<BACKUP_LOCATION>` (≥ 30 days).
- `/tmp/live-plan.json`, `/tmp/live-up.json`, `/tmp/live-verify.json` (redacted reports, mode 0600).
- Rendered `/tmp/compose-false.yml` and `/tmp/compose-true.yml`.
- Completed smoke-matrix checklist with operator initials and timestamps.
