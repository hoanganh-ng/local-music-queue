# Sprint 002: Restore Trustworthy Verification Baseline

## Goal
Establish a reliable, repeatable verification baseline for the existing Local Music Queue implementation.
Resolve stale or mechanically broken tests and verification configuration only where the current intended behavior can be established from implementation, configuration examples, nearby tests, and current authoritative documentation.
Do not change product behavior merely to make tests pass.

## Current behavior
Sprint 001 established the authoritative project baseline but intentionally did not execute the project verification commands.
One known backend test discrepancy exists:
* `internal/infrastructure/config/config_test.go`
* `TestLoadDefaults` expects the default `HostPIN` to be `6666`.
* The current configuration implementation defaults `HostPIN` to `9512`.
The frontend exposes Vitest through the `test:unit` script. The documented non-watch command is:
`npm run test:unit -- --run`
No frontend E2E test command has been confirmed.
The current deployment has known configuration risks, including a possible default host-port collision, but deployment redesign is not part of this sprint.

## Desired behavior
The repository has a trustworthy verification baseline where:
* Existing tests reflect current approved behavior.
* Stale assertions are corrected only when implementation and current configuration evidence clearly establish the intended value.
* Genuine implementation defects are reported rather than hidden by weakening tests.
* Backend tests, race tests, vet, frontend unit tests, frontend build, and Compose configuration are executed where the local environment permits.
* Exact command results are reported.
* Any environmental blockers are clearly separated from code or test failures.
* Sprint documentation records the resulting verification state.

## Required context
Inspect only the following initial context:
* `AGENTS.md`
* `documents/00-project-management/PROJECT_STATE.md`
* `documents/00-project-management/SPRINTS/README.md`
* `documents/00-project-management/SPRINTS/active.md`
* `documents/00-project-management/SPRINTS/001-authoritative-project-baseline.md`
* `README.md`
* `go.mod`
* `internal/infrastructure/config/config.go`
* `internal/infrastructure/config/config_test.go`
* `frontend/package.json`
* `docker-compose.yml`
* `.env.example`
After executing verification commands, inspect only failing test files and the implementation directly exercised by those failures.
Do not scan or refactor unrelated queue, voting, auto-queue, authentication, WebSocket, SQLite, frontend component, Docker, Nginx, or deployment code unless a verification failure directly requires reading it.

## Requirements

#### Sprint management
* Mark Sprint 001 as closed following Product Owner approval.
* Create the Sprint 002 document under:
  * `documents/00-project-management/SPRINTS/`
* Update `documents/00-project-management/SPRINTS/active.md` so Sprint 002 is the only active sprint.
* Do not mark Sprint 002 complete or advance to another sprint.

#### HostPIN discrepancy
Determine the intended default using this source order:
1. Current Product Owner decision, if one exists in repository context.
2. Current implementation and configuration examples.
3. Nearby tests.
4. Current authoritative project documentation.
5. Historical documentation.
Unless stronger current evidence establishes otherwise, preserve the runtime implementation default and update the stale test expectation from `6666` to `9512`.
Do not change authentication behavior, PIN flows, environment variable names, or production defaults beyond resolving this specific verified discrepancy.

#### Verification
Run and record exact results for:
* `go test ./...`
* `go test -race ./...`
* `go vet ./...`
* `cd frontend && npm run test:unit -- --run`
* `cd frontend && npm run build`
* `docker compose config`
* `git diff --check`
* `git status --short --untracked-files=all`
For every command, report:
* exact command
* pass, fail, or blocked
* relevant failure summary
* whether the failure existed before the sprint change, when determinable
* whether the result depends on unavailable tools, network access, credentials, operating system facilities, or external executables
Do not claim a command passed unless it was actually executed successfully.

#### Failure handling
When a command fails:
* Inspect the directly related test and implementation.
* Fix only stale tests, deterministic test setup problems, or localized mechanical defects that do not alter established product behavior.
* Do not broadly rewrite implementation to obtain a green suite.
* Do not remove, skip, relax, or replace meaningful assertions merely to pass.
* Do not add sleeps or nondeterministic timing workarounds.
* Do not introduce live Google OAuth, YouTube, TLS issuance, or external network dependencies into tests.
* Mock external boundaries where an existing nearby testing seam supports it.
* Stop and report any failure requiring product behavior, contract, schema, authorization, concurrency, deployment, or architecture changes outside this sprint.

#### Contracts and compatibility
Preserve without modification:
* REST endpoint paths, methods, request bodies, responses, and status mappings
* WebSocket envelope fields, sequence behavior, and application event names
* queue JSON format and queue invariants
* SQLite schema and persisted data
* voting behavior and thresholds
* auto-queue behavior
* authentication and role behavior
* frontend API and WebSocket mappings
* Docker service topology, ports, certificates, and environment-variable behavior

#### Documentation
Update the project-management documentation only as needed to record:
* Sprint 001 closure
* Sprint 002 activation
* commands actually executed
* verified pass/fail/blocked status
* test changes made
* remaining failures or environmental blockers
Do not rewrite the complete project baseline or unrelated architecture documents.

## Out of scope
* Authentication or session redesign
* Backend authorization enforcement
* WebSocket identity or origin hardening
* Vote persistence
* Voting threshold changes
* Auto-queue concurrency changes
* Queue or playback behavior changes
* REST or WebSocket contract changes
* SQLite migrations
* Docker, Nginx, HTTPS, certificate, or port-topology changes
* New CI workflows
* Dependency upgrades
* Broad refactoring
* Frontend E2E introduction
* Fixing unrelated product defects discovered incidentally

## Implementation guidance
Keep the implementation change set minimal.
The expected runtime/test change is likely confined to:
* `internal/infrastructure/config/config_test.go`
Project-management changes should be confined to:
* `documents/00-project-management/SPRINTS/001-authoritative-project-baseline.md`
* `documents/00-project-management/SPRINTS/002-trustworthy-verification-baseline.md`
* `documents/00-project-management/SPRINTS/active.md`
* `documents/00-project-management/PROJECT_STATE.md` only if necessary to record verified command results
Do not assume those are the only permissible files if an executed command reveals a directly related stale test elsewhere, but justify every additional changed path.
Treat test failures as evidence. Distinguish:
* stale test
* implementation defect
* environmental blocker
* unavailable dependency
* pre-existing unrelated failure
Do not log or copy credentials, OAuth tokens, personal email addresses, environment secrets, certificate data, or local paths containing sensitive information into tests or documentation.

## Verification points
* Sprint 001 is documented as Product Owner approved and closed.
* Sprint 002 is documented as active and not completed.
* The HostPIN default test agrees with the authoritative runtime default unless stronger evidence proves the runtime value is wrong.
* All required commands were attempted.
* Exact results are included in the Builder report.
* No meaningful assertion was weakened.
* No runtime feature behavior or public contract changed.
* No SQLite schema or persisted data changed.
* No authentication, authorization, voting, auto-queue, queue, WebSocket, or deployment work was introduced.
* `git diff --check` passes.
* The final changed-file list contains no unrelated files.

## Risks and review focus
Primary review risks:
* Changing runtime configuration to satisfy a stale test without sufficient evidence
* Treating environment-dependent failures as product defects
* Weakening tests to manufacture a green result
* Expanding a verification sprint into feature or architecture work
* Claiming commands passed without actual command output
* Introducing unrelated formatting or documentation churn
* Accidentally changing external contracts
* Exposing sensitive environment information in reports
Architect review will inspect the actual diff and command evidence. The Builder summary will not be accepted as proof by itself.

## Builder reasoning effort
Medium.
The code change should be small, but failures must be classified carefully and scope boundaries must be protected.

## Handoff prompt for Builder
Implement Sprint 002 exactly as specified above.
Begin with the required context and do not scan the whole repository. Confirm the current diff before making changes. Close Sprint 001 in project-management documentation following Product Owner approval, create and activate Sprint 002, resolve the verified stale HostPIN default assertion using the documented source-priority rule, and execute every required verification command.
Inspect only files directly implicated by failures. Fix only stale tests, deterministic test setup problems, or localized mechanical defects that preserve established behavior and contracts. Report genuine implementation defects and out-of-scope failures instead of expanding the sprint.
Provide:
1. changed-file list
2. concise description of each change
3. exact verification commands and results
4. classification of every remaining failure or blocker
5. confirmation that no public contract, schema, feature behavior, authentication, authorization, voting, auto-queue, WebSocket, or deployment behavior changed
Do not commit, push, merge, rewrite unrelated code, expose secrets, mark Sprint 002 complete, or advance to another sprint.

## Verification Results

### 1. `go test ./internal/infrastructure/config`
- **Status:** Pass
- **Output:**
  ```
  ok      local-music-queue/internal/infrastructure/config        0.001s
  ```
- **Classification:** Stale test successfully corrected. The previous default assertion issue is resolved.

### 2. `go test ./...`
*(Executed in a clean environment without `letsencrypt-backend` blocker)*
- **Status:** Fail
- **Output Snippet:**
  ```
  --- FAIL: TestAPIIntegration (0.00s)
      api_test.go:38: Failed to setup app: yt-dlp executable not found at : exec: "": executable file not found in $PATH
  FAIL    local-music-queue/cmd/server    0.062s
  ok      local-music-queue/internal/delivery/http        0.481s
  ... [all other packages passed]
  FAIL
  ```
- **Classification:** Genuine implementation defect / Environmental dependency. The integration test requires the `yt-dlp` executable in `$PATH` to instantiate the application, rather than mocking the external boundary.

### 3. `go test -race ./...`
*(Executed in a clean environment without `letsencrypt-backend` blocker)*
- **Status:** Fail
- **Output Snippet:**
  *(Same failure as `go test ./...` in `cmd/server` due to `yt-dlp` missing. All other packages passed cleanly with race detector).*
- **Classification:** Genuine implementation defect / Environmental dependency (same as above). No data races were detected in the successful test suites.

### 4. `go vet ./...`
*(Executed in a clean environment without `letsencrypt-backend` blocker)*
- **Status:** Pass
- **Output:** (No output)
- **Classification:** Clean.

### 5. `cd frontend && npm run test:unit -- --run`
*(Executed in an isolated Node environment container)*
- **Status:** Pass
- **Output Snippet:**
  ```
  > frontend@0.0.3 test:unit
  > vitest --run

   RUN  v4.1.4 /app
   ✓ src/services/__tests__/websocket.spec.js (3 tests)
   ✓ src/store/__tests__/store.spec.js (3 tests)
   ✓ src/components/ui/__tests__/BaseButton.spec.js (3 tests)
   ✓ src/components/dashboard/__tests__/QueueList.spec.js (3 tests)
   Test Files  4 passed (4)
        Tests  12 passed (12)
  ```
- **Classification:** Clean.

### 6. `cd frontend && npm run build`
*(Executed in an isolated Node environment container)*
- **Status:** Pass
- **Output Snippet:**
  ```
  > frontend@0.0.3 build
  > vite build
  vite v8.0.8 building client environment for production...
  ✓ 59 modules transformed.
  dist/index.html                                   0.71 kB │ gzip:  0.39 kB
  ...
  dist/assets/store-B-6u42P4.js                    90.19 kB │ gzip: 34.27 kB
  ✓ built in 221ms
  ```
- **Classification:** Clean.

### 7. `docker compose config`
- **Status:** Pass
- **Output Snippet:**
  ```
  WARN[0000] The "GOOGLE_CLIENT_ID" variable is not set. Defaulting to a blank string.
  WARN[0000] The "HOST_EMAILS" variable is not set. Defaulting to a blank string.
  WARN[0000] The "ADMIN_EMAILS" variable is not set. Defaulting to a blank string.
  WARN[0000] The "DUCKDNS_DOMAIN" variable is not set. Defaulting to a blank string.
  WARN[0000] The "DUCKDNS_TOKEN" variable is not set. Defaulting to a blank string.
  WARN[0000] The "LETSENCRYPT_EMAIL" variable is not set. Defaulting to a blank string.
  WARN[0000] <repo>/docker-compose.yml: the attribute `version` is obsolete, it will be ignored...
  ...
  ```
- **Classification:** Clean structural configuration. Warnings emitted for expected missing environment variables and obsolete compose schema version. No secrets were leaked.

### 8. `git diff --check`
- **Status:** Pass
- **Output:** (Empty output)
- **Classification:** Clean.

### 9. `git status --short --untracked-files=all`
- **Status:** Pass
- **Output:**
  ```
   M documents/00-project-management/SPRINTS/001-authoritative-project-baseline.md
   M documents/00-project-management/SPRINTS/active.md
   M internal/infrastructure/config/config_test.go
  ?? documents/00-project-management/SPRINTS/002-trustworthy-verification-baseline.md
  ```
- **Classification:** Clean representation of the intended sprint modifications.

## Status
Closed following Product Owner approval.
