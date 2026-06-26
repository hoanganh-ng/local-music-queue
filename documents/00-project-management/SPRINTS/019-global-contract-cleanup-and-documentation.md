# R14 – Global contract cleanup and documentation

**Status:** planned (stub – work not yet started)

**Sprint name:** Global contract cleanup and documentation

## Goal

Perform a comprehensive review of all contracts, APIs and documentation produced throughout the room epic.  Remove deprecated endpoints and fields, harmonise naming and error conventions, and ensure that public and internal contracts are unambiguous.  Produce polished documentation (OpenAPI specs, ADRs, developer guides) and verify that test coverage accurately reflects the final state of the system.

## Current behaviour (pre‑sprint baseline)

After the preceding sprints, the project will have accumulated numerous endpoints, event types and domain concepts.  Some may have been superseded or amended without removing the old versions.  Documentation may be fragmented across sprint stubs, ADRs and code comments.  Test suites may still reference outdated behaviour.  Without a cleanup, the system risks confusion and technical debt.

## Desired behaviour (post‑sprint)

* **Contract audit**: Review all REST endpoints and WebSocket event types.  Identify deprecated or redundant endpoints and remove or alias them with proper deprecation notices.  Ensure consistent naming, parameter naming and HTTP status codes.  Align error formats and response schemas.
* **Documentation consolidation**: Produce a comprehensive OpenAPI specification covering all REST endpoints.  Document WebSocket message schemas, including event names and payload structures.  Update developer guides and README files to reflect the final feature set.
* **ADR updates**: Write or update Architectural Decision Records to capture significant design decisions made during the epic.  Ensure that ADR numbers and references match the implemented state.  Mark obsolete ADRs as superseded.
* **Test coverage**: Evaluate test coverage across the backend and front‑end (if applicable).  Remove or update tests that refer to old behaviour.  Add tests for any uncovered critical paths.
* **Deprecation policy**: Establish a policy for deprecating APIs in future releases.  Document the process for marking features as deprecated, providing migration paths and eventually removing them.

## Required context

* A complete list of endpoints, event types and domain models from previous sprints (R00–R13).
* Existing ADRs and their status (open, closed, superseded).
* The current OpenAPI or API documentation, if any, and how it is generated.
* Feedback from consumers of the API (e.g. front‑end team or external integrators) regarding pain points or confusing aspects of the contract.

## Requirements

1. **Contract inventory.**  Catalogue every REST endpoint and WebSocket event.  Note the purpose, parameters, return types and any known issues.
2. **Consistency checks.**  Audit naming conventions (camelCase vs. snake_case), HTTP verb usage, status codes and error messages.  Align them to a coherent standard.  Provide a style guide if one does not exist.
3. **Deprecation and removal.**  Remove unused or obsolete endpoints.  For endpoints that will be removed in a future version, mark them as deprecated in documentation and log a warning when called.
4. **Documentation generation.**  Use tooling (e.g. OpenAPI generators) to produce an up‑to‑date API spec.  Host the spec in the repository and link to it from the README.  Document WebSocket event schemas manually if necessary.
5. **ADR housekeeping.**  Write new ADRs for design decisions not yet captured.  Mark any ADRs superseded by later decisions.  Include cross‑references between ADRs and sprint docs where helpful.
6. **Testing.**  Ensure test coverage for all critical paths.  Remove tests that reference deprecated behaviour.  Add regression tests for contract changes.
7. **Communication.**  Prepare release notes summarising major changes and deprecations.  Communicate the final contract to stakeholders.

## Out of scope

* Introducing new features or breaking changes unrelated to cleanup.  The focus is consolidation and documentation.
* Overhauling the entire architecture – major refactors should be planned separately if needed.
* Front‑end documentation beyond what is needed to understand server contracts.

## Implementation guidance

* Adopt a contract‑first philosophy: start from the OpenAPI spec and ensure code matches the spec.  Generate server stubs if helpful.
* Use consistent error response structures (e.g. an `error` object with `code`, `message` and optional `details`) across all endpoints.
* Consider versioning the API (e.g. `/v1/rooms`) if breaking changes are introduced during cleanup.  If versioning is not used, clearly document any breakage and provide migration guidance.
* For WebSocket events, document both the event type and the data payload.  Provide examples for each event.

## Execution note

This sprint acts as the epilogue of the room epic.  When executed, take the time to thoroughly audit the system, involve stakeholders in documentation review and ensure that no significant decisions remain undocumented.  After completing the sprint, mark its status as **closed** here and in `ROOM_EPIC_SPRINT_SEQUENCE.md`, and celebrate the conclusion of the epic.