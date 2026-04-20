# Backend Phase 4: Delivery Layer

The communication layer. This exposes the Usecases from Phase 2 to the network using the Infrastructure from Phase 3.

## Objective
Provide the HTTP and WebSocket endpoints for the frontend to consume.

## Tasks
1.  **WebSocket Hub** (`internal/delivery/ws/`):
    - Manage active connections.
    - Implement a "Broadcaster" that pushes `QueueState` updates whenever a Usecase changes the data.
2.  **HTTP Handlers** (`internal/delivery/http/`):
    - Login/Auth endpoint (POST `/api/auth`).
    - Queue status endpoint (GET `/api/queue`).
3.  **Dependency Injection** (`cmd/server/main.go`):
    - Initialize Infrastructure (SQLite, YouTube).
    - Initialize Usecases with the Infrastructure.
    - Start the HTTP/WS server.

## Validation (Against Phase 2 & 3)
- **E2E Validation**: This layer *validates* the entire stack.
- **Handler Testing**: Use `httptest` to send real JSON requests and ensure they trigger the correct Usecase logic and return the expected HTTP codes.
- **Manual Smoke Test**: Use `wscat` or a simple script to verify WebSocket messages arrive when a song is added.
