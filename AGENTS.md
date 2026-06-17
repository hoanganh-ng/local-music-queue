# AGENTS.md

## Project Overview

Local Music Queue is a local-network music queue application powered by YouTube. It uses a Clean Architecture pattern with Go backend and Vue.js frontend, enabling real-time collaborative playlist management via WebSockets.

**Key Features:**

- Google OAuth authentication with role-based access (Host/Admin/Guest)
- Priority queue system with daily token awards
- Real-time WebSocket updates with delta broadcasting
- YouTube search and metadata fetching via yt-dlp
- HTTPS support with Let's Encrypt and DuckDNS

## Documentation

Comprehensive documentation is available in the `documents/` folder:

- **[Overview](documents/01-overview/)** - Architecture, technology stack
- **[Getting Started](documents/02-getting-started/)** - Installation, Docker deployment, environment variables
- **[Features](documents/03-features/)** - Authentication, priority system, queue management
- **[API Reference](documents/04-api-reference/)** - REST endpoints, WebSocket events
- **[Frontend](documents/05-frontend/)** - Components, state management
- **[Backend](documents/06-backend/)** - Domain, usecase, infrastructure layers
- **[Deployment](documents/07-deployment/)** - Docker, HTTPS, production setup
- **[Development](documents/08-development/)** - Local development, testing
- **[Roadmap](documents/09-roadmap/)** - Implemented and future features

## Quick Commands

### Backend

- **Run server**: `go run cmd/server/main.go` (default port 1111)
- **Run tests**: `go test ./...`
- **Run single test**: `go test -run TestName ./path/to/package`
- **Download dependencies**: `go mod download`

### Frontend

- **Install dependencies**: `cd frontend && npm install`
- **Dev server**: `cd frontend && npm run dev` (default port 5173)
- **Build**: `cd frontend && npm run build`
- **Unit tests**: `cd frontend && npm run test:unit`

### Docker

- **Full stack**: `docker-compose up --build`
- **Backend only**: `docker build -t local-music-queue-backend -f Dockerfile.backend . && docker run -p 1111:1111 -v $(pwd)/.localdb:/app/data local-music-queue-backend`
- **Frontend only**: `cd frontend && docker build -t local-music-queue-frontend . && docker run -p 80:80 local-music-queue-frontend`

## Architecture

### Backend (Go 1.22+)

Follows Clean Architecture with four layers:

- **Domain** (`internal/domain/`): Core entities (Song, Queue, User, Activity) and repository/service interfaces
  - `entity/`: Song, Queue, User, Activity, SearchResult
  - `repository/`: QueueRepository, UserRepository interfaces
  - `service/`: YouTubeService interface

- **Usecase** (`internal/usecase/`): Application business logic
  - `queue/`: Queue operations (add, skip, remove, clear songs)
  - `auth/`: Authentication (PIN-based and Google OAuth)
  - `activity/`: Activity feed tracking (joins, additions, skips)
  - `priority/`: Priority queue system for song prioritization
  - `vote/`: Community voting for skip and prioritize actions

- **Infrastructure** (`internal/infrastructure/`): External service implementations
  - `persistence/`: SQLite repository implementations (queue and user data)
  - `youtube/`: yt-dlp service for YouTube metadata fetching
  - `config/`: Configuration loading from environment variables

- **Delivery** (`internal/delivery/`): HTTP API and WebSocket handlers
  - `http/`: REST API endpoints for queue, auth, search, priority operations
  - `ws/`: WebSocket hub for real-time state broadcasting

### Frontend (Vue 3 + Vite)

- **Views** (`src/views/`): AuthView (login), DashboardView (main queue interface)
- **Components** (`src/components/`): Reusable UI components
- **Services** (`src/services/`):
  - `api.js`: HTTP client for REST endpoints
  - `websocket.js`: WebSocket connection and message handling
- **Store** (`src/store/`): State management
- **Router** (`src/router/`): Vue Router configuration

## Key Configuration

Backend environment variables (see `.env.example` and [Environment Variables Guide](documents/02-getting-started/environment-variables.md)):

**Required:**

- `GOOGLE_CLIENT_ID`: Google OAuth client ID (get from Google Cloud Console)
- `HOST_EMAILS`: Comma-separated emails with host access (full control + player hosting)
- `ADMIN_EMAILS`: Comma-separated emails with admin access (remote playback control)

**Optional:**

- `PORT`: Server port (default 1111)
- `DB_PATH`: SQLite database path (default `./.localdb/music_queue.db`)
- `YTDLP_PATH`: yt-dlp executable path (default `yt-dlp`)
- `CERT_FILE` / `KEY_FILE`: HTTPS certificate paths (for HTTPS mode)
- `CLIENT_PIN` / `HOST_PIN`: Legacy PIN authentication (deprecated)

**Docker Compose:**

- `FRONTEND_HTTP_PORT`: Frontend HTTP port (default 8011)
- `FRONTEND_HTTPS_PORT`: Frontend HTTPS port (default 8012)
- `BACKEND_PORT`: Backend API port (default 1111)
- `DUCKDNS_DOMAIN`: DuckDNS subdomain for HTTPS (e.g., myapp.duckdns.org)
- `DUCKDNS_TOKEN`: DuckDNS authentication token
- `LETSENCRYPT_EMAIL`: Email for Let's Encrypt certificate notifications

## Database

SQLite database stores:

- Queue state (songs, playback status, volume)
- User data (authentication, priority balance)
- Activity log (joins, additions, skips)
- User sessions (daily priority token tracking)
- Priority transactions (token award/spend audit trail)

Database is initialized automatically on first run. Located at `.localdb/music_queue.db` by default.

See [Database Schema](documents/06-backend/database-schema.md) for detailed table structure.

## WebSocket Protocol

Real-time updates via `/ws` endpoint. Hub broadcasts queue state changes to all connected clients using delta broadcasting (only changed data sent).

**13 Event Types:**

- `full_sync` - Initial state on connect
- `user_joined` - New user joined
- `song_added` - Song added to queue
- `song_skipped` - Song skipped
- `song_previous` - Previous song
- `song_removed` - Song removed
- `queue_cleared` - Queue cleared
- `status_changed` - Play/Pause
- `elapsed_sync` - Playback time sync
- `volume_changed` - Volume change
- `song_prioritized` - Song moved to front
- `priority_balance_updated` - User balance updated
- `vote_updated` - Vote session updated with new vote count
- `vote_resolved` - Vote session passed or expired

See [WebSocket Events](documents/04-api-reference/websocket-events.md) for detailed event payloads.

## Testing

- Backend: `go test ./...` runs all tests with coverage
- Frontend: `npm run test:unit` runs Vitest suite
- Tests use standard Go testing and Vue Test Utils

## Development Notes

- Backend uses gorilla/websocket for real-time communication
- Frontend uses Vue Router for navigation between auth and dashboard views
- YouTube integration via yt-dlp (must be installed on host machine)
- CORS is enabled for local development
- Request logging middleware logs all HTTP requests
- Google OAuth is the primary authentication method (PIN auth deprecated)
- Priority system awards 1 token per day on first login
- HTTPS support via Let's Encrypt and DuckDNS for production deployment

## Important Implementation Details

### Authentication

- **Primary**: Google OAuth 2.0 with email-based role assignment
- **Roles**: Host (full control + player), Admin (remote control), Guest (add songs)
- **Email Domain**: Restricted to `@urekamedia.vn` by default (configurable in `internal/usecase/auth/interactor.go`)
- **Legacy PIN**: Still present but deprecated

### Priority System

- Users earn 1 token per day on first login (UNIQUE constraint prevents double-claiming)
- Tokens can be spent to move own songs to front of queue
- All transactions logged in `priority_transactions` table
- Balance displayed in UI with ⚡ icon

### Community Voting System

- **Democratic Control**: Guests and Admins can vote to skip or prioritize songs
- **Strict Majority**: Requires `(connectedUsers / 2) + 1` votes to pass (minimum 2)
- **Time-Limited**: 30-second voting window with live countdown
- **Real-time Updates**: Live vote counts broadcast via `vote_updated` WebSocket events
- **In-Memory Sessions**: Vote sessions stored in memory, cleared on server restart
- **Role Restriction**: Host excluded from voting (has direct controls)
- **Auto-Expiry**: Hub runs a 5-second ticker to expire old vote sessions

### WebSocket Delta Broadcasting

- Only changed data sent (not full state) - reduces bandwidth by ~90%
- Sequence numbers on all messages for gap detection
- Auto-reconnect on disconnect (3-second retry)

### Database

- Single-row queue state (entire queue as JSON blob for atomic updates)
- 5 tables: queue_state, activities, users, user_sessions, priority_transactions
- Pure Go SQLite driver (no CGo) - `modernc.org/sqlite`

## Common Tasks

### Adding a New API Endpoint

1. Define handler in `internal/delivery/http/handlers.go`
2. Add route in `cmd/server/main.go`
3. Implement usecase logic in appropriate `internal/usecase/` package
4. Update frontend `src/services/api.js`
5. Add tests

### Adding a New WebSocket Event

1. Define event type in `internal/delivery/ws/events.go`
2. Add broadcast call in usecase layer
3. Handle event in `frontend/src/services/websocket.js`
4. Update UI components to react to event

### Modifying User Roles

1. Edit `HOST_EMAILS` or `ADMIN_EMAILS` in `.env`
2. Restart backend: `docker-compose restart backend`
3. User roles updated on next login

### Changing Email Domain Restriction

Edit `internal/usecase/auth/interactor.go` line ~50:

```go
allowedDomain := "yourcompany.com"  // Change this
```

## Troubleshooting

### yt-dlp not found

```bash
# Install yt-dlp
pip install yt-dlp
# Or specify path in .env
YTDLP_PATH=/usr/local/bin/yt-dlp
```

### Google OAuth not working

1. Verify `GOOGLE_CLIENT_ID` matches in both backend and frontend `.env`
2. Check authorized origins in Google Cloud Console include your domain
3. Ensure email domain matches restriction in code

### Database locked

```bash
# Stop all instances
docker-compose down
# Remove lock files
rm .localdb/music_queue.db-shm .localdb/music_queue.db-wal
# Restart
docker-compose up -d
```

### WebSocket connection fails

1. Check backend is running: `docker-compose ps`
2. Verify `VITE_API_BASE_URL` in frontend `.env`
3. Check Nginx WebSocket proxy configuration

## Additional Resources

- **Full Documentation**: See `documents/` folder for comprehensive guides
- **API Reference**: `documents/04-api-reference/`
- **Deployment Guide**: `documents/07-deployment/`
- **Feature Roadmap**: `documents/09-roadmap/`
