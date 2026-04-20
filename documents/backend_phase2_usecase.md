# Backend Phase 2: Usecase Layer

The orchestration layer. This is where the application "features" are implemented using the entities and interfaces from Phase 1.

## Objective
Implement features like "Add Song to Queue" or "Skip Current Track" using pure logic.

## Tasks
1.  **Queue Interactor** (`internal/usecase/queue/`):
    - Implement the logic to fetch metadata (via `YouTubeService` interface) and add it to the list (via `QueueRepository` interface).
    - Implement "Host only" logic for skipping or pausing.
2.  **Auth Interactor** (`internal/usecase/auth/`):
    - Implement PIN verification logic.
    - Implement display name assignment.
3.  **Activity Interactor**:
    - Logic for creating log entries when actions happen.

## Validation (Against Phase 1)
- **Interface Adherence**: This layer *validates* Phase 1 by proving whether the defined interfaces are sufficient for the features.
- **Mock Testing**: Use mocks of Phase 1 interfaces to test Usecases in isolation. If a Usecase can't be tested easily, Phase 1 interfaces might need refactoring.

## Parallel Readiness
- Can be developed independently of the database or external APIs (Infrastructure).
- Once finished, **Phase 4 (Delivery)** can start to expose these features via HTTP/WebSockets.
