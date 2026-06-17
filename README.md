# 🎵 Local Music Queue

A local-network music queue application powered by **YouTube**. Perfect for parties or office environments where anyone on the same network can contribute to a shared playlist with role-based access control and priority queue management.

![Go](https://img.shields.io/badge/Backend-Go-00ADD8?style=flat-square&logo=go)
![Vue.js](https://img.shields.io/badge/Frontend-Vue.js-4FC08D?style=flat-square&logo=vue.js)
![WebSocket](https://img.shields.io/badge/Real--time-WebSocket-green?style=flat-square)
![OAuth](https://img.shields.io/badge/Auth-Google%20OAuth-4285F4?style=flat-square&logo=google)

## 📌 Project State
For the authoritative current state of the implementation, including security caveats and known risks, please refer directly to **[PROJECT_STATE.md](documents/00-project-management/PROJECT_STATE.md)**.

## ✨ Features

- 🔐 **Google OAuth Authentication**: Email-based role assignment (Host, Admin, Guest)
- 🔍 **YouTube Integration**: Search and paste YouTube/YouTube Music URLs
- ⚡ **Real-time Updates**: Live queue and activity feed via WebSockets with delta broadcasting
- 📺 **Host Playback**: Centralized playback control on the host machine
- 📝 **Activity Feed**: Track joins, song additions, skips, and priority changes
- 💎 **Priority Queue System**: Daily token awards for song prioritization
- 🗳️ **Community Voting**: Democratic skip and priority votes.
- 🔒 **Role-Based Access**: Host, Admin, Guest
- 🌐 **HTTPS Support**: Let's Encrypt integration with DuckDNS for production

## 🏗️ Architecture

The project follows **Clean Architecture** principles with four distinct layers:

### Backend (Go 1.22+)

- **Domain** (`internal/domain/`): Core entities (Song, Queue, User, Activity) and interfaces
- **Usecase** (`internal/usecase/`): Business logic for queue, auth, activity, and priority operations
- **Infrastructure** (`internal/infrastructure/`): SQLite persistence, YouTube metadata via yt-dlp, config management
- **Delivery** (`internal/delivery/`): HTTP REST API and WebSocket handlers

### Frontend (Vue 3 + Vite)

- **Views**: Authentication and dashboard interfaces
- **Components**: Reusable UI elements
- **Services**: API client and WebSocket connection management
- **Store**: State management for queue and user data

## 🚀 Getting Started

### Prerequisites

- **Go 1.22+**
- **Node.js & npm** (latest LTS recommended)
- **yt-dlp**: Required on the host machine to fetch metadata
  - Install via `pip install yt-dlp` or your package manager (e.g., `brew install yt-dlp`)
- **Google OAuth**: Set up Google Cloud Console credentials for authentication

### Backend Setup

1. From the project root:
   ```bash
   go mod download
   ```
2. Create the local database directory:
   ```bash
   mkdir -p .localdb
   ```
3. Configure environment variables (see [Configuration](#configuration))
4. Run the server:
   ```bash
   go run cmd/server/main.go
   ```
   *Default port: 1111*

### Frontend Setup

1. Navigate to the frontend directory:
   ```bash
   cd frontend
   ```
2. Install dependencies:
   ```bash
   npm install
   ```
3. Configure environment variables for API endpoint
4. Start the development server:
   ```bash
   npm run dev
   ```
   *Default port: 5173*

### Docker Setup

#### Full Stack (Recommended)

```bash
docker-compose up --build
```



## ⚙️ Configuration

### Backend Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | Server port | `1111` |
| `GOOGLE_CLIENT_ID` | Google OAuth client ID | Required |
| `HOST_EMAILS` | Comma-separated host emails | Required |
| `ADMIN_EMAILS` | Comma-separated admin emails | Optional |
| `DB_PATH` | SQLite database path | `./.localdb/music_queue.db` |
| `YTDLP_PATH` | yt-dlp executable path | `yt-dlp` |
| `CERT_FILE` | HTTPS certificate path | Optional |
| `KEY_FILE` | HTTPS key path | Optional |

### Docker Compose Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `FRONTEND_HTTP_PORT` | Frontend HTTP port | `8011` |
| `FRONTEND_HTTPS_PORT` | Frontend HTTPS port | `8012` |
| `BACKEND_PORT` | Backend API port | `1111` |
| `DUCKDNS_DOMAIN` | DuckDNS subdomain | Optional |
| `DUCKDNS_TOKEN` | DuckDNS token | Optional |
| `LETSENCRYPT_EMAIL` | Let's Encrypt email | Optional |

## 🛠️ Development

### Testing

- **Backend**: `go test ./...`
  - *Note*: `TestLoadDefaults` fails because it expects the historical HostPIN default 6666, while current configuration defaults it to 9512.
- **Backend (single test)**: `go test -run TestName ./path/to/package`
- **Frontend Unit**: `cd frontend && npm run test:unit -- --run`

### Quick Commands

- **Backend server**: `go run cmd/server/main.go`
- **Frontend dev**: `cd frontend && npm run dev`
- **Frontend build**: `cd frontend && npm run build`
- **Download Go deps**: `go mod download`

## 📚 Documentation

Comprehensive documentation is available in the `documents/` folder:

- **[Project Management](documents/00-project-management/)** - Sprints and baseline state

- **[Overview](documents/01-overview/)** - Architecture and technology stack
- **[Getting Started](documents/02-getting-started/)** - Installation and deployment
- **[Features](documents/03-features/)** - Authentication, priority system, queue management
- **[API Reference](documents/04-api-reference/)** - REST endpoints and WebSocket events
- **[Frontend](documents/05-frontend/)** - Components and state management
- **[Backend](documents/06-backend/)** - Domain, usecase, and infrastructure layers
- **[Deployment](documents/07-deployment/)** - Docker, HTTPS, and production setup
- **[Development](documents/08-development/)** - Local development and testing
- **[Roadmap](documents/09-roadmap/)** - Implemented and future features

## 🔑 Key Features Explained

### Authentication

- **Primary**: Google OAuth 2.0 with email-based role assignment
- **Roles**: Host (full control + player), Admin (remote control), Guest (add songs)
- **Email Domain**: Restricted to `@urekamedia.vn` by default (configurable)

### Priority System

- Users earn 1 token per day on first login
- Tokens can be spent to move own songs to front of queue
- All transactions logged for audit trail
- Balance displayed in UI with ⚡ icon

### Community Voting

- **Democratic Control**: Guests and Admins can vote to skip or prioritize songs
- **Time-Limited**: 30-second voting window with live countdown
- **Real-time Updates**: Live vote counts broadcast to all connected clients
- **In-Memory Sessions**: Vote sessions cleared on server restart
- **Role Restriction**: Host excluded from voting (has direct controls)

### Real-time Updates

- WebSocket delta broadcasting with 16 event types wrapped in a standard sequence envelope
- Auto-reconnect on disconnect (3-second retry)

### Database

- SQLite with 7 tables.
- Pure Go SQLite driver (no CGo dependency)
- Atomic queue state updates via single-row JSON blob storage

## 📄 License

This project is open-source and available under the [MIT License](LICENSE).