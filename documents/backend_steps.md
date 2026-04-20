# Backend Implementation Guide — Clean Architecture

This document outlines the step-by-step implementation of the Local Music Queue backend using Go. Each layer is developed from the center (Domain) outward, ensuring high test coverage and modularity.

## Phase 1: Domain Layer (The Core)
*Responsibility: Business entities and rules. No external dependencies.*

1.  **Define Entities**: 
    - Create `internal/domain/entity/song.go`: Define `Song` struct.
    - Create `internal/domain/entity/queue.go`: Define `QueueState` and `QueueAction`.
    - Create `internal/domain/entity/activity.go`: Define `ActivityLog`.
2.  **Define Interfaces (Ports)**:
    - Create `internal/domain/repository/`: Define interfaces for `QueueRepository` and `ActivityRepository`.
    - Create `internal/domain/service/`: Define interface for `YouTubeMetaDataDownloader`.
3.  **Testing**:
    - Write unit tests in `internal/domain/entity/` to verify business rules (e.g., a Queue cannot skip if empty, URL validation logic).

---

## Phase 2: Usecase Layer (Application Logic)
*Responsibility: Orchestrating the flow of data to and from entities.*

1.  **Queue Interactor**:
    - Implement `internal/usecase/queue/interactor.go`. Methods: `AddSong`, `SkipSong`, `GetState`.
2.  **Auth Interactor**:
    - Implement `internal/usecase/auth/interactor.go`. Methods: `VerifyPIN`, `ResolveRole` (Host vs Guest).
3.  **Testing**:
    - Use `golang/mock` or manual mocks for Repository interfaces.
    - Test `AddSong` workflow: Ensure it calls the YouTube service and then saves to the repository.

---

## Phase 3: Infrastructure Layer (Adapters)
*Responsibility: Talking to the outside world (DB, External APIs, CLI).*

1.  **YouTube Adapter**:
    - Implement `internal/infrastructure/youtube/ytdlp.go` using `go-ytdlp`.
2.  **Persistence Adapter**:
    - Implement `internal/infrastructure/persistence/sqlite.go` using `sqlc` or raw SQL.
3.  **Testing**:
    - **Integration Tests**: Run `yt-dlp` against a real (but short) sample video.
    - **DB Tests**: Verify SQLite CRUD operations.

---

## Phase 4: Delivery Layer (The Interface)
*Responsibility: Translating HTTP/WebSocket requests into Usecase calls.*

1.  **REST API**:
    - Implement `internal/delivery/http/handlers.go`: Auth check, Health check.
2.  **WebSocket Hub**:
    - Implement `internal/delivery/ws/hub.go`: Manage connections, broadcast state changes to all clients.
3.  **Testing**:
    - Use `net/http/httptest` to verify response codes and payloads.
    - Test WebSocket connection lifecycle.
