# Technology Stack

## Backend Technologies

### Core Language & Runtime
- **Go 1.22+** - Primary backend language
  - Fast compilation and execution
  - Built-in concurrency (goroutines)
  - Strong standard library
  - Static typing with excellent tooling

### Web Framework & Libraries
- **net/http** - Standard library HTTP server
- **gorilla/websocket** - WebSocket implementation for real-time updates
- **gorilla/mux** - HTTP router (if used)

### Database
- **SQLite** - Embedded relational database
  - **Driver**: `modernc.org/sqlite` - Pure Go SQLite driver (no CGo)
  - Single-file database (`.localdb/music_queue.db`)
  - ACID transactions
  - Sufficient for ≤20 concurrent users
  - Zero configuration required

### External Services
- **yt-dlp** - YouTube metadata extraction
  - Command-line tool for fetching video information
  - No API key required
  - Supports multiple YouTube URL formats
  - 5-minute in-memory cache for search results

### Authentication
- **Google OAuth 2.0** - User authentication
  - ID token verification via Google's tokeninfo endpoint
  - Email-based role assignment
  - Profile picture integration

---

## Frontend Technologies

### Core Framework
- **Vue 3** (v3.5.32) - Progressive JavaScript framework
  - Composition API for better code organization
  - Reactive state management
  - Component-based architecture
  - Excellent performance

### Build Tool
- **Vite** - Next-generation frontend tooling
  - Lightning-fast HMR (Hot Module Replacement)
  - Optimized production builds
  - Native ES modules support

### Routing
- **Vue Router** - Official Vue.js router
  - Client-side routing
  - Route guards for authentication
  - Navigation between Auth and Dashboard views

### State Management
- **Reactive API** - Vue 3's built-in reactivity
  - Global store using `reactive()`
  - localStorage persistence for sessions
  - No external state library needed

### External APIs
- **YouTube IFrame API** - Video playback
  - Embedded YouTube player (host only)
  - Playback control (play/pause/seek)
  - Event listeners for state changes

### Testing
- **Vitest** - Unit testing framework
  - Fast, Vite-native test runner
  - Jest-compatible API
- **Vue Test Utils** - Component testing utilities
- **Playwright** - End-to-end testing

---

## Infrastructure & DevOps

### Containerization
- **Docker** - Application containerization
  - Multi-stage builds for optimized images
  - Backend: Go builder → Alpine runtime (~20MB)
  - Frontend: Node builder → Nginx runtime (~25MB)

### Orchestration
- **Docker Compose** - Multi-container orchestration
  - Single-command deployment
  - Service dependencies
  - Volume management for SQLite persistence

### Web Server
- **Nginx** - Reverse proxy and static file server
  - SPA routing with `try_files` fallback
  - API proxy (`/api` → backend:1111)
  - WebSocket proxy with Upgrade headers
  - HTTPS termination

### HTTPS & DNS
- **Let's Encrypt** - Free SSL/TLS certificates
  - Automatic certificate issuance
  - 90-day renewal cycle
- **DuckDNS** - Dynamic DNS service
  - Free subdomain (*.duckdns.org)
  - Automatic IP updates

---

## Development Tools

### Version Control
- **Git** - Source control
- **GitHub** - Repository hosting (assumed)

### Code Quality
- **go fmt** - Go code formatting
- **go vet** - Go static analysis
- **ESLint** - JavaScript linting (if configured)
- **Prettier** - Code formatting (if configured)

### Testing
- **go test** - Go testing framework
- **Vitest** - Frontend unit tests
- **Playwright** - E2E tests

---

## Architecture Patterns

### Backend Patterns
- **Clean Architecture** - Layered architecture with dependency inversion
- **Repository Pattern** - Data access abstraction
- **Dependency Injection** - Manual DI in `cmd/server/main.go`
- **Interface Segregation** - Small, focused interfaces

### Frontend Patterns
- **Component-Based Architecture** - Reusable UI components
- **Service Layer** - API and WebSocket abstraction
- **Reactive State Management** - Centralized global store
- **Single Page Application (SPA)** - Client-side routing

### Communication Patterns
- **REST API** - HTTP JSON endpoints for mutations
- **WebSocket** - Real-time bidirectional communication
- **Delta Broadcasting** - Send only changed data (not full state)
- **Event-Driven** - WebSocket events trigger UI updates

---

## Key Dependencies

### Backend (Go)
```go
require (
    github.com/gorilla/websocket v1.5.0
    modernc.org/sqlite v1.x.x
    // Standard library: net/http, encoding/json, etc.
)
```

### Frontend (npm)
```json
{
  "dependencies": {
    "vue": "^3.5.32",
    "vue-router": "^4.x.x"
  },
  "devDependencies": {
    "vite": "^5.x.x",
    "vitest": "^1.x.x",
    "@vue/test-utils": "^2.x.x",
    "@playwright/test": "^1.x.x"
  }
}
```

---

## Why These Technologies?

### Go for Backend
- **Performance**: Compiled language with low memory footprint
- **Concurrency**: Goroutines make WebSocket hub trivial to implement
- **Simplicity**: Small language spec, easy to learn and maintain
- **Deployment**: Single binary, no runtime dependencies

### Vue 3 for Frontend
- **Developer Experience**: Intuitive API, excellent documentation
- **Performance**: Virtual DOM with optimized reactivity
- **Ecosystem**: Rich component libraries and tooling
- **Size**: Smaller bundle than React or Angular

### SQLite for Database
- **Zero Config**: No separate database server required
- **Portability**: Single file, easy backups
- **Performance**: Fast for read-heavy workloads
- **Sufficient**: Handles ≤20 concurrent users easily

### yt-dlp for YouTube
- **No API Key**: Avoids YouTube API quota limits
- **Reliability**: Actively maintained, handles format changes
- **Flexibility**: Supports many video platforms (not just YouTube)

### Docker for Deployment
- **Consistency**: Same environment in dev and production
- **Isolation**: Dependencies contained within images
- **Portability**: Run anywhere Docker runs
- **Simplicity**: Single `docker-compose up` command

---

## System Requirements

### Development
- **Go**: 1.22 or higher
- **Node.js**: 18.x or higher
- **npm**: 9.x or higher
- **yt-dlp**: Latest version (installed globally or in PATH)

### Production (Docker)
- **Docker**: 20.10 or higher
- **Docker Compose**: 2.0 or higher
- **yt-dlp**: Included in backend Docker image

### Runtime
- **Memory**: ~100MB for backend, ~50MB for frontend (Nginx)
- **Storage**: ~50MB for application, variable for SQLite database
- **Network**: LAN or internet (with HTTPS)

---

## Browser Compatibility

### Supported Browsers
- **Chrome/Edge**: 90+
- **Firefox**: 88+
- **Safari**: 14+

### Required Features
- ES6+ JavaScript
- WebSocket API
- localStorage API
- CSS Grid and Flexbox
- YouTube IFrame API support
