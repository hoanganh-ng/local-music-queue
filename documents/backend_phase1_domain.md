# Backend Phase 1: Domain Layer

The foundation of the application. This layer defines "what" the system is made of and the rules that govern it, independent of any technology.

## Objective
Establish core entities and the interfaces (contracts) that other layers must follow.

## Tasks
1.  **Define Entities** (`internal/domain/entity/`):
    - `song.go`: Struct for YouTube metadata.
    - `queue.go`: Struct for the shared queue state and playback position.
    - `user.go`: Struct for ephemeral display names and roles (Host vs. Guest).
    - `activity.go`: Struct for log entries.
2.  **Define Interfaces** (`internal/domain/repository/` and `internal/domain/service/`):
    - `QueueRepository`: Methods for Saving/Loading queue state.
    - `YouTubeService`: Method for fetching metadata from a URL.
3.  **Core Logic**:
    - Add methods to entities for internal state changes (e.g., `Queue.Next()`, `Song.IsValid()`).

## Validation & Testing
- **Unit Tests**: Test entity methods directly.
- **Rules Documentation**: Ensure all business invariants (e.g., "Queue cannot skip if empty") are covered by a test.

## Parallel Readiness
Once this phase is done (specifically the interfaces), **Phase 2 (Usecase)** and **Phase 3 (Infrastructure)** can start in parallel.
