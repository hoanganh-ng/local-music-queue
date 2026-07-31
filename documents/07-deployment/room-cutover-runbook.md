# R14c Production Runbook — Coordinated Authoritative Room Cutover

> **NON-EXECUTABLE — direction amendment (ADR 004, Sprint 034, 2026-07-30).** The Product Owner replaced the migrate-and-retire cutover contract with the **discard-and-retire** contract (Issue #17 comment #5128855220; `documents/00-project-management/ADRS/004-discard-legacy-global-state-at-room-cutover.md`). This runbook's migration steps assume a `cmd/room-cutover up` production copy into a migrated room, which **will no longer happen**: no migrated room will be created and no legacy global queue state, activities, auto-queue configuration, or play history will be copied. **No step in this runbook may be executed**, including "read-only" steps, until the implementation-correction sprint (ADR 004 Section 6) is accepted and integrated and the Product Owner authorizes a revised runbook. This document is preserved unchanged below as the historical Gate 1 record. Mandatory backups, the false/false rollback pair, closed-traffic execution, paired server/SPA deployment, fail-closed behavior, and the Sprint 032 governance sequence remain mandatory for any future revised cutover.

**Sprint:** 029 (R14c) — `documents/00-project-management/SPRINTS/029-coordinated-authoritative-room-cutover.md`
**Status:** Written and reviewed under Gate 1. **NOT executed. NON-EXECUTABLE under ADR 004 / Sprint 034 (see banner above).** Gate 2 (production execution) requires a separate Product Owner go/no-go. Amended under Sprint 030 (PR #27 Architect reviews) with focused documentation-only security corrections: all connection data is supplied through protected non-argv channels with the snapshot DSNs kept as un-exported private shell variables (see "Connection supply" below), and retained Compose evidence is limited to allowlisted pairing lines instead of full secret-bearing renders (step 9); command order and behavior are unchanged.

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

Supporting placeholders used below: `<REVIEWED_COMMIT_SHA>`, `<FALSE_PAIR_IMAGE_TAGS>`, `<TRUE_PAIR_IMAGE_TAGS>`, `<PROD_HOST>`, `<DUMP_FILE>`, `<EVIDENCE_DIR>`, `<SNAPSHOT_DSN>` (read-only production snapshot), `<ISOLATED_SNAPSHOT_DSN>` (a throwaway copy of the snapshot used only for the `up --dry-run` rehearsal), `<PGSERVICE_FILE>`, `<PGPASS_FILE>`, `<CONNECTION_ENV_FILE>` (the protected connection bundle defined below).

## Connection supply — no DSN or credential in argv

No PostgreSQL DSN, password, or other connection credential may ever appear as a command-line argument (positional or flag value) of `psql`, `pg_dump`, `pg_restore`, or any `room-cutover` invocation: argv is visible through `ps`/`/proc/*/cmdline` and can land in shell history and retained logs. All connection data lives in one operator-local **protected connection bundle**, each file mode `0600`, outside the repository working tree and any web-served path, and reaches every tool through the environment only:

- `<PGSERVICE_FILE>` — a libpq connection service file defining services `r14c-snapshot` (the read-only production snapshot), `r14c-snapshot-isolated` (the throwaway dry-run copy), and `r14c-live` (the live database; used only for the in-window backup). Host `psql`/`pg_dump` commands select a service with `PGSERVICEFILE`/`PGSERVICE`; no DSN in argv.
- `<PGPASS_FILE>` — the matching libpq password file, referenced via `PGPASSFILE`; passwords never appear in the service file, argv, or any other file.
- `<CONNECTION_ENV_FILE>` — a shell variable file containing plain (non-`export`) assignments of `SNAPSHOT_DSN` and `ISOLATED_SNAPSHOT_DSN` for the containerized `room-cutover` rehearsals. The operator sources it into a private shell with `. <CONNECTION_ENV_FILE>`; because the assignments are not exported, both DSNs stay **private shell variables** invisible to child processes. Only the selected value reaches only the selected command, through a per-command `DATABASE_URL="$…"` environment prefix plus name-only `docker compose run -e DATABASE_URL` passthrough — the value is never typed after `-e` and never appears in argv. `room-cutover` reads `DATABASE_URL` (or `MIGRATE_DATABASE_URL`) whenever `--postgres` is omitted; **`--postgres` is never used in this runbook.**

`<SNAPSHOT_DSN>` and `<ISOLATED_SNAPSHOT_DSN>` therefore exist only inside the connection bundle; they are never written into any command line, report, or committed file.

`<EVIDENCE_DIR>` is an operator-controlled host directory (outside the repository working tree and outside any web-served path) that holds every cutover report and the allowlisted pairing evidence (never a full rendered Compose configuration — see step 9). Reports are written *through a bind mount* so they survive `docker compose run --rm`. Create it before the window with mode `0700` and set every report file to `0600` (see the pre-window checklist). Never commit real reports or identities from `<EVIDENCE_DIR>` to the repository.

## The single pairing input

`ROOM_CUTOVER_AUTHORITATIVE` in the deployment `.env` is the only cutover switch:

- backend receives `--room-cutover-authoritative=${ROOM_CUTOVER_AUTHORITATIVE:-false}` via the Compose `command`;
- frontend is **rebuilt** with build arg `VITE_ROOM_CUTOVER_AUTHORITATIVE=${ROOM_CUTOVER_AUTHORITATIVE:-false}`;
- both services are **mode-qualified images** — `local-music-queue-backend:${ROOM_CUTOVER_AUTHORITATIVE:-false}` and `local-music-queue-frontend:${ROOM_CUTOVER_AUTHORITATIVE:-false}` — so the false and true artifacts are distinct and independently selectable. Building the true pair never overwrites the false pair, and Compose always selects a backend and frontend that carry the **same** mode; no command can produce a true server with a false SPA.

Never set the two halves independently. Flipping the value requires `docker compose build backend frontend` (the frontend value is baked at build time) followed by redeploying **both** containers as one pair.

## Pre-window checklist (all items before `<MAINTENANCE_WINDOW>` opens)

1. Confirm the reviewed commit: `git rev-parse HEAD` on the deployment host equals `<REVIEWED_COMMIT_SHA>` (the accepted R14c review commit).
2. Record the exact **false/false rollback pair** as immutable identifiers, and do this **before any rebuild or image prune** so no later `docker compose build` or `docker image prune` can move a tag or garbage-collect the layers you are relying on. Resolve the image straight from the **currently running** backend and frontend *containers* rather than from a tag (a tag such as `:false` is mutable and may not even point at what is live), then capture that image's ID *and* repo digest:

   ```bash
   # Resolve the image each LIVE container is actually running, by container id.
   backend_img=$(docker inspect --format '{{.Image}}' "$(docker compose ps -q backend)")
   frontend_img=$(docker inspect --format '{{.Image}}' "$(docker compose ps -q frontend)")

   # Record the immutable ID + repo digest of each running image.
   docker image inspect "$backend_img"  --format '{{.Id}} {{join .RepoDigests ","}}'
   docker image inspect "$frontend_img" --format '{{.Id}} {{join .RepoDigests ","}}'
   ```

   Record both output lines as `<FALSE_PAIR_IMAGE_TAGS>` — these captured IDs/digests are the only approved rollback identities, and they must be written down before step 10 builds anything or any prune runs. Because the images are mode-qualified (`:false` vs `:true`), a later `true` build cannot overwrite this pair; the recorded immutable IDs let Rollback re-select the exact live pair even if the `:false` tag is later reassigned.

   Then **preserve** the captured images, still before any rebuild, replacement, or prune. A captured ID alone does not keep the image alive: once the `:false` tag is reassigned the old image becomes untagged/dangling and an image prune can garbage-collect it. Pin each captured image under a **protected rollback tag** and, as a durable second copy, archive both images to `<BACKUP_LOCATION>`:

   ```bash
   # Pin the exact captured IDs under dedicated rollback tags (never reused by any build).
   docker tag "$backend_img"  local-music-queue-backend:rollback-r14c
   docker tag "$frontend_img" local-music-queue-frontend:rollback-r14c

   # Durable archives that survive any local image deletion or prune.
   docker save -o <BACKUP_LOCATION>/rollback-backend-r14c.tar  local-music-queue-backend:rollback-r14c
   docker save -o <BACKUP_LOCATION>/rollback-frontend-r14c.tar local-music-queue-frontend:rollback-r14c
   chmod 0600 <BACKUP_LOCATION>/rollback-backend-r14c.tar <BACKUP_LOCATION>/rollback-frontend-r14c.tar
   ```

   **Verify** the preserved artifacts resolve to the exact captured IDs before proceeding:

   ```bash
   # Each must print the same {{.Id}} recorded in <FALSE_PAIR_IMAGE_TAGS>.
   docker image inspect local-music-queue-backend:rollback-r14c  --format '{{.Id}}'
   docker image inspect local-music-queue-frontend:rollback-r14c --format '{{.Id}}'
   ```

   If either archive is later needed, `docker load -i <archive>.tar` restores the image and `docker image inspect --format '{{.Id}}'` must again equal the captured ID. From this point **until the Product Owner closes the rollback window**, the rollback tags and archives must not be deleted: no `docker rmi` of the `rollback-r14c` tags or the captured IDs, no `docker image prune -a` (or any prune that removes tagged/unused images), and no deletion of the two `.tar` archives from `<BACKUP_LOCATION>`. A plain dangling-only `docker image prune` cannot remove the pinned tags, and even a mistaken forced removal remains recoverable from the archives.
3. Create the durable evidence directory on the host with restrictive permissions (it must live outside the repository working tree and outside any web-served path):

   ```bash
   mkdir -p <EVIDENCE_DIR> && chmod 0700 <EVIDENCE_DIR>
   ```

4. Confirm `<TARGET_ROOM_SLUG>` is unused: authenticated `GET /api/rooms` and a direct slug lookup must show no existing room with that slug; the slug must not be reserved.
5. Validate `<HOST_USER_ID>` as a boolean/existence check (do not print the row into any retained log): the query must return exactly one row for a positive integer id. Connection data comes from the protected bundle; nothing sensitive appears in argv.

   ```bash
   # Prints 't' iff a users row with this positive id exists. No PII in output, no DSN in argv.
   PGSERVICEFILE=<PGSERVICE_FILE> PGPASSFILE=<PGPASS_FILE> PGSERVICE=r14c-snapshot \
     psql -tAc "SELECT EXISTS (SELECT 1 FROM users WHERE id = <HOST_USER_ID> AND id > 0);"
   ```

6. Confirm the account represented by `<LIVE_ALLOWED_GOOGLE_ACCOUNT>` resolves to that **same** `<HOST_USER_ID>`, as a secure operator-only comparison. Bind the real address through an environment variable so it never lands in a committed file, retained log, or shell history, and compare by boolean equality rather than printing the stored identity. Keep the address out of **process arguments** as well: hand it to `psql` through the environment and bind it inside the psql process with `\getenv` into a **quoted psql variable** (the `:'name'` interpolation), never on the command line and never by shell-interpolating it into the SQL string, so it is invisible to `ps`/`/proc/*/cmdline` and a value containing quotes or other SQL metacharacters cannot alter the query:

   ```bash
   # Operator pastes the real allow-listed address into their private shell; not echoed,
   # never written to a file, never placed on any command line. Connection data comes
   # from the protected bundle; neither the address nor a DSN appears in argv.
   read -rs LIVE_EMAIL
   LIVE_EMAIL="$LIVE_EMAIL" PGSERVICEFILE=<PGSERVICE_FILE> PGPASSFILE=<PGPASS_FILE> PGSERVICE=r14c-snapshot psql -tA <<'SQL'
   \getenv email LIVE_EMAIL
   SELECT (SELECT id FROM users WHERE lower(email) = lower(:'email')) = <HOST_USER_ID>;
   SQL
   unset LIVE_EMAIL
   ```

   The per-command `LIVE_EMAIL="$LIVE_EMAIL"` prefix scopes the variable to the single `psql` process without exporting it to the wider session; `\getenv` reads it inside psql (requires psql ≥ 15 — the deployment runs PostgreSQL 16, use a matching client), and `:'email'` makes psql safely single-quote and escape the value on the server side, so it is treated strictly as data. The quoted `<<'SQL'` heredoc prevents any shell expansion of the SQL text. The result must be `t`. Use `display_name` only if a human-readable disambiguation is genuinely required, and only in the operator's private session (again via `\getenv` into a quoted variable) — never in a retained artifact.
7. Confirm the migrated room's sole host resolves to `<HOST_USER_ID>` after `up` (checked again in the smoke matrix): the target room's single host membership must be exactly this user id.
8. Confirm login for `<LIVE_ALLOWED_GOOGLE_ACCOUNT>` on the current (false) deployment. **Three backend conditions are separate — do not conflate them:**
   - **Login eligibility** decides whether a Google account may authenticate at all. It is enforced in the auth interactor (Google ID-token verification plus the hard-coded allowed email-domain check) and is **not** driven by `HOST_EMAILS` or `ADMIN_EMAILS`. An account that fails this gate cannot sign in regardless of any role configuration.
   - **Account-level `users.role`** is a global role column on the `users` row (`host` / `admin` / `guest`), assigned at login time from the `HOST_EMAILS` / `ADMIN_EMAILS` allow-lists; an eligible account in neither list defaults to `guest`. This governs global capabilities and is **not** per-room and **not** created by the cutover.
   - **Room-host membership** is a per-room `room_members` row with role `host`. It is created only by `room-cutover up --host-user-id <HOST_USER_ID>`, which inserts exactly this one host membership for the migrated room (enforced sole-host by a one-host-per-room partial unique index). It does **not** read or write `users.role` and does **not** consult `HOST_EMAILS`.
   - Therefore `<LIVE_ALLOWED_GOOGLE_ACCOUNT>` must (a) satisfy login eligibility to sign in, and (b) map to `<HOST_USER_ID>`, which the cutover makes the sole room host via `room_members`. Verify the account can actually log in on the false deployment rather than inferring eligibility from `HOST_EMAILS` membership or from its account-level `users.role`.
9. Extract and review **allowlisted pairing evidence** from both Compose renders, confirming the backend flag, the frontend build arg, and the mode-qualified image references all agree. The full `docker compose config` output interpolates sensitive deployment values (database credentials, `DATABASE_URL`, Google and DuckDNS configuration) and is therefore **never written to disk**; it exists only transiently in the pipe, and the retained files contain only the image references and authoritative flags:

   ```bash
   ROOM_CUTOVER_AUTHORITATIVE=false docker compose config \
     | grep -E 'image: local-music-queue-|room-cutover-authoritative=|VITE_ROOM_CUTOVER_AUTHORITATIVE:' \
     > <EVIDENCE_DIR>/pairing-false.txt
   ROOM_CUTOVER_AUTHORITATIVE=true  docker compose config \
     | grep -E 'image: local-music-queue-|room-cutover-authoritative=|VITE_ROOM_CUTOVER_AUTHORITATIVE:' \
     > <EVIDENCE_DIR>/pairing-true.txt
   chmod 0600 <EVIDENCE_DIR>/pairing-false.txt <EVIDENCE_DIR>/pairing-true.txt
   ```

   The false extract must reference `local-music-queue-backend:false` + `local-music-queue-frontend:false` with `--room-cutover-authoritative=false` and `VITE_ROOM_CUTOVER_AUTHORITATIVE: "false"`; the true extract must show the `:true` pair with both flags `true`; backend and frontend must carry the same mode in each extract. Confirm each extract contains **no** environment block, credential, or other value beyond these allowlisted lines before retaining it.
10. Build both artifact pairs from `<REVIEWED_COMMIT_SHA>`; because the image references are mode-qualified, the two builds produce distinct, independently selectable pairs and the `true` build never overwrites the `false` rollback pair:

    ```bash
    ROOM_CUTOVER_AUTHORITATIVE=false docker compose build backend frontend   # -> :false pair
    ROOM_CUTOVER_AUTHORITATIVE=true  docker compose build backend frontend   # -> :true pair
    ```

    Confirm the backend image contains both `/app/server` and `/app/room-cutover`, and record the `:true` pair IDs/digests as `<TRUE_PAIR_IMAGE_TAGS>` the same way as step 2.
11. Run a read-only plan against a **production snapshot** (never the live database at this stage), **through the packaged `:true` backend image** so the rehearsal exercises the same binary that will run the cutover. The snapshot DSN reaches the container through the environment only: load the protected connection env file into the private shell, prefix the single command with `DATABASE_URL="$SNAPSHOT_DSN"`, and forward it by **name only** with `-e DATABASE_URL` (`room-cutover` falls back to `DATABASE_URL` because `--postgres` is omitted; never pass `--postgres` and never type a value after `-e`). Bind-mount `<EVIDENCE_DIR>` into the one-off container so the report survives `--rm`, then confirm it landed on the host and lock it down:

    ```bash
    . <CONNECTION_ENV_FILE>   # plain assignments (mode 0600): SNAPSHOT_DSN / ISOLATED_SNAPSHOT_DSN stay un-exported private shell variables
    DATABASE_URL="$SNAPSHOT_DSN" ROOM_CUTOVER_AUTHORITATIVE=true docker compose run --rm --no-deps \
      -e DATABASE_URL -v <EVIDENCE_DIR>:/evidence backend /app/room-cutover plan \
      --room-slug <TARGET_ROOM_SLUG> --room-name "<TARGET_ROOM_NAME>" --host-user-id <HOST_USER_ID> \
      --report-file /evidence/plan-report.json
    test -f <EVIDENCE_DIR>/plan-report.json && chmod 0600 <EVIDENCE_DIR>/plan-report.json
    ```

    Review the report; it must be PII-free and its counts must look plausible against known production volume. (The inline `ROOM_CUTOVER_AUTHORITATIVE=true` prefix makes Compose unambiguously select the prebuilt `local-music-queue-backend:true` image from step 10, regardless of what the deployment `.env` currently holds; the inline `DATABASE_URL` prefix scopes the snapshot DSN to this one command without exporting it to the wider session.)
12. Run `room-cutover up --dry-run` against an **isolated copy** of the snapshot (never the live database or the shared snapshot), again **through the packaged backend image** with `<EVIDENCE_DIR>` bind-mounted, writing a **separate** durable report and locking it down. The isolated DSN is supplied the same non-argv way — `DATABASE_URL="$ISOLATED_SNAPSHOT_DSN"` prefix plus name-only `-e DATABASE_URL`; `--postgres` is never used. Confirm it reports the same evidence as the plan without writing:

    ```bash
    DATABASE_URL="$ISOLATED_SNAPSHOT_DSN" ROOM_CUTOVER_AUTHORITATIVE=true docker compose run --rm --no-deps \
      -e DATABASE_URL -v <EVIDENCE_DIR>:/evidence backend /app/room-cutover up --dry-run \
      --room-slug <TARGET_ROOM_SLUG> --room-name "<TARGET_ROOM_NAME>" --host-user-id <HOST_USER_ID> \
      --report-file /evidence/up-dry-run.json
    test -f <EVIDENCE_DIR>/up-dry-run.json && chmod 0600 <EVIDENCE_DIR>/up-dry-run.json
    unset SNAPSHOT_DSN ISOLATED_SNAPSHOT_DSN
    ```

    Confirm `<EVIDENCE_DIR>/up-dry-run.json` exists on the host and that its counts/hashes match `plan-report.json`; a missing report means the bind mount was omitted and the step must be re-run. (As in step 11, the inline `ROOM_CUTOVER_AUTHORITATIVE=true` prefix pins the rehearsal to the prebuilt `:true` backend image. The trailing `unset` clears both snapshot DSNs from the private shell once the rehearsals are done.)
13. Confirm the isolated end-to-end rehearsal (Gate 1 evidence) is accepted.
14. Confirm `<BACKUP_LOCATION>` is writable, verified, and retains dumps for at least 30 days.

## Maintenance window procedure

Execute strictly in order inside `<MAINTENANCE_WINDOW>`. Any failure at any step → go directly to **Rollback**.

Connection supply in this window follows the no-argv rule: every `room-cutover` invocation (steps 4–6) omits `--postgres` and consumes the `DATABASE_URL` already present in the Compose backend service environment (the deployment `.env`); the backup connects through the protected service/passfile bundle. No DSN or credential appears in any command line.

1. **Close public traffic** (stop ingress/port exposure at `<PROD_HOST>`; method per deployment topology). Announce closure.
2. **Backup:** take and verify a timestamped dump, connecting via the `r14c-live` service from the protected bundle (no DSN in argv):

   ```bash
   PGSERVICEFILE=<PGSERVICE_FILE> PGPASSFILE=<PGPASS_FILE> PGSERVICE=r14c-live pg_dump -Fc -f <DUMP_FILE>
   pg_restore --list <DUMP_FILE> | head    # verify readability
   ```

   Copy `<DUMP_FILE>` to `<BACKUP_LOCATION>` and verify the copy.
3. **Stop frontend and backend** containers; keep PostgreSQL running:

   ```bash
   docker compose stop frontend backend
   ```

4. **Plan against the live database** (read-only). Bind-mount `<EVIDENCE_DIR>` into the one-off container so the report survives `--rm`, then confirm it landed on the host and lock it down:

   ```bash
   docker compose run --rm --no-deps -v <EVIDENCE_DIR>:/evidence backend /app/room-cutover plan \
     --room-slug <TARGET_ROOM_SLUG> --room-name "<TARGET_ROOM_NAME>" --host-user-id <HOST_USER_ID> \
     --report-file /evidence/live-plan.json
   test -f <EVIDENCE_DIR>/live-plan.json && chmod 0600 <EVIDENCE_DIR>/live-plan.json
   ```

   Review the report. Abort on any readiness failure.
5. **Execute the cutover** with the approved identity values, again writing through the bind mount and verifying the report on the host:

   ```bash
   docker compose run --rm --no-deps -v <EVIDENCE_DIR>:/evidence backend /app/room-cutover up \
     --room-slug <TARGET_ROOM_SLUG> --room-name "<TARGET_ROOM_NAME>" --host-user-id <HOST_USER_ID> \
     --report-file /evidence/live-up.json
   test -f <EVIDENCE_DIR>/live-up.json && chmod 0600 <EVIDENCE_DIR>/live-up.json
   ```

6. **Verify before any room write:**

   ```bash
   docker compose run --rm --no-deps -v <EVIDENCE_DIR>:/evidence backend /app/room-cutover verify \
     --report-file /evidence/live-verify.json
   test -f <EVIDENCE_DIR>/live-verify.json && chmod 0600 <EVIDENCE_DIR>/live-verify.json
   ```

   Must exit 0. No room mutation may happen before this succeeds. Confirm all three reports (`live-plan.json`, `live-up.json`, `live-verify.json`) now exist on the host under `<EVIDENCE_DIR>`; a missing report means the bind mount was omitted and the step must be re-run before proceeding.
7. **Deploy the true pair as one unit:** set `ROOM_CUTOVER_AUTHORITATIVE=true` in the deployment `.env`, then start the pre-built `:true` pair recorded as `<TRUE_PAIR_IMAGE_TAGS>` (Compose selects `local-music-queue-backend:true` + `local-music-queue-frontend:true`):

   ```bash
   docker compose up -d backend frontend
   ```

   The backend must start (its fail-closed guard passes only because steps 5–6 completed).
8. **Run the mandatory smoke matrix** (below) while traffic is still closed.
9. **Reopen traffic** only after every mandatory check passes. Announce reopening.

## Rollback (before reopening traffic)

Any failed mandatory check requires an immediate rollback:

1. Stop the true pair: `docker compose stop frontend backend`.
2. Set `ROOM_CUTOVER_AUTHORITATIVE=false` and explicitly restore the **exact** recorded false pair. Select the images by the immutable IDs/digests captured in pre-window step 2 (`<FALSE_PAIR_IMAGE_TAGS>`) — the protected `local-music-queue-backend:rollback-r14c` / `local-music-queue-frontend:rollback-r14c` tags pin those IDs; re-tag them back to `local-music-queue-backend:false` / `local-music-queue-frontend:false` if the `:false` tags were reassigned (and `docker load` the archives from `<BACKUP_LOCATION>` first if the local images were ever removed) — then start them as one pair. Verify each restored image's `{{.Id}}` equals the captured ID before starting. The mode-qualified tags guarantee this false pair was never overwritten by the `true` build.
3. **Do not** delete or edit `room_cutover_marker`.
4. **Do not** use force/reset/hash-edit tooling of any kind.
5. Retain `<DUMP_FILE>` and every report under `<EVIDENCE_DIR>`.
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

All evidence lives on the host under `<EVIDENCE_DIR>` (mode `0700`), never inside an image or a web-served path:

- Timestamped `<DUMP_FILE>` in `<BACKUP_LOCATION>` (≥ 30 days).
- `<EVIDENCE_DIR>/live-plan.json`, `<EVIDENCE_DIR>/live-up.json`, `<EVIDENCE_DIR>/live-verify.json` (redacted reports, mode `0600`), each confirmed present on the host after its `docker compose run --rm` exited.
- `<EVIDENCE_DIR>/plan-report.json` and `<EVIDENCE_DIR>/up-dry-run.json` from the pre-window snapshot plan and isolated `up --dry-run` rehearsal (each mode `0600`, each written through the bind mount and confirmed present on the host).
- Allowlisted pairing evidence `<EVIDENCE_DIR>/pairing-false.txt` and `<EVIDENCE_DIR>/pairing-true.txt` (image references and authoritative flags only — never the full rendered Compose configuration, which interpolates deployment secrets).
- Recorded false-pair and true-pair image IDs/digests (`<FALSE_PAIR_IMAGE_TAGS>`, `<TRUE_PAIR_IMAGE_TAGS>`), the protected `rollback-r14c` tags pinning the false pair, and the two rollback image archives in `<BACKUP_LOCATION>` (mode `0600`, retained until the Product Owner closes the rollback window).
- Completed smoke-matrix checklist with operator initials and timestamps.

None of these files may be committed to the repository; they can contain real identities and must stay in the operator-controlled `<EVIDENCE_DIR>` only.
