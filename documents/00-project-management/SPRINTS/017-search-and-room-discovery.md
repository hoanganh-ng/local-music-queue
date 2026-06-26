# R12 – Search and room discovery

**Status:** planned (stub – work not yet started)

**Sprint name:** Search and room discovery

## Goal

Enable users to search for tracks and discover public rooms.  Searching for tracks should integrate with external music providers (e.g. YouTube, Spotify) to retrieve metadata and playable URIs.  Room discovery should allow users to browse or search for rooms by name, tags or popularity.  The sprint should provide APIs, persistence and caching required for efficient search operations.

## Current behaviour (pre‑sprint baseline)

There is no built‑in search functionality.  Adding tracks requires clients to manually supply URIs or embed codes, and there is no support for browsing public rooms.  As a result, track selection is cumbersome and discovering other rooms is impossible without external coordination.

## Desired behaviour (post‑sprint)

* **Track search**: Implement an API endpoint (e.g. `GET /search/tracks?q=<query>&provider=<provider>`) that queries the configured music providers.  Return a list of candidate tracks with metadata (title, artist, duration, thumbnail) and a provider‑specific URI.  Respect the provider’s rate limits and authentication requirements.
* **Room discovery**: Provide endpoints such as `GET /public/rooms` to list public rooms, optionally filtered by search query, tags or number of participants.  Only rooms marked as public should be returned.  Include basic metadata: room name, owner nickname, current track, number of participants and whether the queue is open to new additions.
* **Public/private designation**: Add a flag on rooms to indicate if they are public or private.  Only owners may toggle this setting.  Public rooms appear in discovery; private rooms remain accessible only via invite or room code.
* **Caching**: Implement caching for external provider search results to reduce repeated API calls.  Use an in‑memory cache with expiration (e.g. 10 minutes).  Document any limitations or quotas.
* **Provider abstraction**: Create abstractions to support multiple music providers.  Each provider should implement a common interface (e.g. `SearchTracks`) and handle authentication, query formatting and result parsing internally.

## Required context

* Understand the domain model and room schema from previous sprints.
* Determine which music providers are supported (YouTube, Spotify, etc.) and obtain API credentials if required.
* Familiarise with existing HTTP client infrastructure or third‑party SDKs for making requests to external services.
* Review any legal implications of fetching and displaying third‑party content (terms of service, caching restrictions, rate limits).

## Requirements

1. **Provider abstraction layer.**  Design a provider interface for track search and implement at least one provider (e.g. YouTube Data API).  Make it easy to add further providers later.
2. **Track search endpoint.**  Expose an HTTP endpoint to accept search queries.  Validate input, call the provider and return a normalised response format.  Cache results based on query and provider.
3. **Room discovery.**  Extend the room schema with `is_public` and optional `tags`.  Implement a search endpoint that returns public rooms matching the criteria.  Provide simple filtering and sorting (e.g. by current occupancy).
4. **Configuration.**  Allow enabling or disabling providers via environment variables.  Read API keys or OAuth credentials from configuration.
5. **Tests.**  Write unit tests for the provider abstraction and caching logic.  Add integration tests that mock provider responses.  Test room discovery filters and privacy constraints.
6. **Documentation.**  Document the search and discovery APIs, including provider limitations and expected result structure.

## Out of scope

* Implementing front‑end UI for search and discovery – this is focused on server‑side functionality.
* Deep integration with provider‑specific features (e.g. recommendations, user playlists).  Only simple search is addressed.
* Paid subscription management or premium features associated with external providers.  Those can be considered later.

## Implementation guidance

* Use the provider’s official SDK or REST API rather than scraping HTML.  This ensures compliance with terms of service and reduces fragility.
* Avoid storing provider authentication credentials in source control.  Use environment variables or secret management.
* Normalise provider responses into a common internal structure so that the rest of the system does not depend on provider‑specific fields.  Include a `provider` field in the response so clients know how to construct playable URIs.
* For room discovery, consider building simple search indexes (e.g. trigram indexes) in the database to support partial matching on room names and tags.

## Execution note

This stub outlines the **search and room discovery** sprint.  During shaping, confirm which providers are in scope and the acceptable caching behaviour.  Refine the privacy model for public rooms.  After implementation, summarise the final outcome and mark the sprint as **closed** in this document and in `ROOM_EPIC_SPRINT_SEQUENCE.md`.