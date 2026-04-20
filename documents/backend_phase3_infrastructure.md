# Backend Phase 3: Infrastructure Layer

The "cables and pipes" layer. This implements the abstract contracts defined in Phase 1 with specific technologies.

## Objective
Implement concrete logic for fetching YouTube data and storing the queue.

## Tasks
1.  **YouTube Adapter** (`internal/infrastructure/youtube/`):
    - Implement the `YouTubeService` interface using the `yt-dlp` binary wrapper.
2.  **Persistence Adapter** (`internal/infrastructure/persistence/`):
    - Implement the `QueueRepository` interface using SQLite.
    - Write SQL migrations or schema management logic.
3.  **Config Management**:
    - Handle environment variables (PINs, port, etc.).

## Validation (Against Phase 1)
- **Implementation Validation**: This layer *validates* that the Phase 1 interfaces are compatible with real-world libraries and constraints (e.g., error handling from CLI tools or database latency).
- **Integration Testing**: Run tests against a real SQLite file and check if `yt-dlp` returns correct metadata for a valid URL.

## Parallel Readiness
- Can be developed in parallel with Phase 2.
- Provides the "Real" objects that will replace Mocks in Phase 4.
