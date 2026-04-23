# Architecture Overview

Local Music Queue follows **Clean Architecture** principles, ensuring separation of concerns, testability, and maintainability.

## Clean Architecture Layers

The backend is organized into four distinct layers, each with specific responsibilities:

```
┌─────────────────────────────────────────────────────────┐
│                   Delivery Layer                         │
│         internal/delivery/http/ & internal/delivery/ws/  │
│                                                           │
│  • HTTP REST API handlers                                │
│  • WebSocket hub and connection management               │
│  • Request/response transformation                       │
│  • Authentication middleware                             │
└─────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────┐
│                   Usecase Layer                          │
│                  internal/usecase/                       │
│                                                           │
│  • Queue operations (add, skip, remove, clear)           │
│  • Authentication logic (Google OAuth, role assignment)  │
│  • Priority system (token awards, prioritization)        │
│  • Activity tracking and logging                         │
└─────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────┐
│                Infrastructure Layer                      │
│               internal/infrastructure/                   │
│                                                           │
│  • SQLite repositories (queue, user, activity)           │
│  • YouTube service (yt-dlp integration)                  │
│  • Configuration loading                                 │
└─────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────┐
│                    Domain Layer                          │
│                  internal/domain/                        │
│                                                           │
│  • Core entities (Song, Queue, User, Activity)           │
│  • Repository interfaces                                 │
│  • Service interfaces                                    │
│  • Business rules and validation                         │
└─────────────────────────────────────────────────────────┘
```

## Layer Responsibilities

### 1. Domain Layer (`internal/domain/`)

**Purpose:** Core business logic and entities. No external dependencies.

**Contents:**
- **Entities** (`entity/`): Song, Queue, User, Activity, SearchResult
- **Repository Interfaces** (`repository/`): QueueRepository, UserRepository
- **Service Interfaces** (`service/`): YouTubeService

**Key Principle:** This layer defines WHAT the system does, not HOW it does it.

### 2. Usecase Layer (`internal/usecase/`)

**Purpose:** Application-specific business rules and orchestration.

**Contents:**
- **Queue Interactor** (`queue/`): AddSong, SkipSong, RemoveSong, ClearQueue, SetStatus, SyncPlayback
- **Auth Interactor** (`auth/`): VerifyGoogleToken, LoginWithGoogle, role assignment
- **Priority Interactor** (`priority/`): PrioritizeSong, CheckAndAwardDailyPriority, GetUserPriorityBalance
- **Activity Interactor** (`activity/`): GetRecentActivities, LogActivity

**Key Principle:** Usecases orchestrate the flow of data between entities and external services.

### 3. Infrastructure Layer (`internal/infrastructure/`)

**Purpose:** Implementation of external interfaces (databases, APIs, file systems).

**Contents:**
- **Persistence** (`persistence/`): SQLite implementations of repository interfaces
- **YouTube Service** (`youtube/`): yt-dlp wrapper for metadata fetching and search
- **Configuration** (`config/`): Environment variable loading and validation

**Key Principle:** This layer is replaceable. You could swap SQLite for PostgreSQL without changing business logic.

### 4. Delivery Layer (`internal/delivery/`)

**Purpose:** Interface with the outside world (HTTP, WebSocket).

**Contents:**
- **HTTP Handlers** (`http/`): REST API endpoints for queue, auth, search, priority
- **WebSocket Hub** (`ws/`): Connection management, delta broadcasting, event types

**Key Principle:** This layer translates external requests into usecase calls and formats responses.

## Dependency Rule

**Dependencies point inward only:**

```
Delivery → Usecase → Infrastructure → Domain
                                        ↑
                                   (no outward dependencies)
```

- Domain layer has ZERO dependencies on other layers
- Usecase layer depends only on Domain interfaces
- Infrastructure implements Domain interfaces
- Delivery depends on Usecase and Domain

This ensures:
- **Testability**: Mock interfaces at each boundary
- **Flexibility**: Swap implementations without changing business logic
- **Maintainability**: Changes in one layer don't cascade to others

## Data Flow Example: Adding a Song

```
1. User submits YouTube URL via frontend
                ↓
2. HTTP Handler (Delivery) receives POST /api/queue/add
                ↓
3. Handler calls QueueInteractor.AddSong (Usecase)
                ↓
4. Interactor calls YouTubeService.FetchMetadata (Infrastructure)
                ↓
5. Interactor creates Song entity (Domain)
                ↓
6. Interactor calls QueueRepository.Save (Infrastructure)
                ↓
7. Interactor logs activity via ActivityRepository (Infrastructure)
                ↓
8. Handler broadcasts WebSocket event via Hub (Delivery)
                ↓
9. All connected clients receive real-time update
```

## Frontend Architecture

The Vue.js frontend follows a component-based architecture:

```
┌─────────────────────────────────────────────────────────┐
│                      Views                               │
│  • AuthView (Google OAuth login)                         │
│  • DashboardView (main application)                      │
└─────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────┐
│                    Components                            │
│  • NowPlaying (player controls)                          │
│  • QueueList (upcoming songs)                            │
│  • ActivityLog (event feed)                              │
│  • SubmitForm (search and add)                           │
└─────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────┐
│                     Services                             │
│  • api.js (REST client)                                  │
│  • websocket.js (real-time updates)                      │
└─────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────┐
│                  State Management                        │
│  • store/index.js (reactive global state)                │
└─────────────────────────────────────────────────────────┘
```

## Communication Patterns

### REST API (HTTP)
- Used for: Authentication, queue mutations, search, priority operations
- Pattern: Request → Handler → Usecase → Repository → Response
- Format: JSON

### WebSocket (Real-time)
- Used for: Live queue updates, activity feed, playback sync
- Pattern: Event → Hub → Broadcast to all clients
- Format: JSON with event types and sequence numbers
- Events: 11 types (full_sync, song_added, song_skipped, etc.)

### Delta Broadcasting
- Only changed data is sent over WebSocket (not full state)
- Reduces bandwidth by ~90%
- Sequence numbers allow gap detection

## Database Schema

SQLite database with 5 tables:

```
queue_state
├── id (always 1, single-row table)
└── state_json (full queue serialized as JSON)

activities
├── id
├── type (SongAdded, SongSkipped, etc.)
├── user_name
├── song_title
└── timestamp

users
├── id
├── email
├── name
├── role (Host/Admin/Guest)
├── priority_balance
└── profile_picture_url

user_sessions
├── user_id
├── session_date (UNIQUE constraint for daily awards)
└── created_at

priority_transactions
├── id
├── user_id
├── amount (+1 for award, -1 for spend)
├── transaction_type
└── timestamp
```

## Testing Strategy

**Unit Tests:**
- Domain entities: Business rule validation
- Usecases: Logic with mocked repositories

**Integration Tests:**
- Infrastructure: Real SQLite database, real yt-dlp calls
- HTTP handlers: httptest for request/response verification

**Frontend Tests:**
- Components: Vue Test Utils
- E2E: Playwright

## Key Design Decisions

1. **Single-row queue state**: Simplifies concurrency, entire queue is atomic
2. **Delta broadcasting**: Reduces WebSocket bandwidth significantly
3. **Daily priority via UNIQUE constraint**: Prevents race conditions in distributed system
4. **Email-based roles**: Simple, secure role assignment without complex RBAC
5. **yt-dlp over YouTube API**: No API key required, more reliable metadata
6. **SQLite over PostgreSQL**: Simpler deployment, sufficient for ≤20 concurrent users

## Scalability Considerations

**Current limits:**
- ~20 concurrent WebSocket connections
- Single SQLite database (no replication)
- Single server instance (no horizontal scaling)

**To scale beyond:**
- Replace SQLite with PostgreSQL
- Add Redis for WebSocket pub/sub
- Load balance multiple server instances
- Separate YouTube metadata service
