# Local Music Queue

> A local-network collaborative music queue application powered by YouTube

## Overview

Local Music Queue is a real-time, multi-user music queue system that allows people on the same network to collaboratively manage a shared YouTube playlist. Built with Go and Vue.js, it features Google OAuth authentication, priority queue mechanics, and WebSocket-based real-time synchronization.

## Key Features

- **Google OAuth Authentication** - Secure login with role-based access control (Host/Admin/Guest)
- **Real-Time Collaboration** - WebSocket-based live updates across all connected clients
- **YouTube Integration** - Search YouTube, fetch metadata, and play videos seamlessly
- **Priority Queue System** - Users earn daily tokens to prioritize their songs
- **Activity Tracking** - Live feed showing all user actions (additions, skips, joins)
- **Host-Controlled Playback** - YouTube IFrame player with play/pause/skip/volume controls
- **Clean Architecture** - Modular, testable codebase following SOLID principles
- **Docker Deployment** - Production-ready containerized deployment with HTTPS support

## Quick Start

### Using Docker (Recommended)

```bash
# Clone the repository
git clone <repository-url>
cd local-music-queue

# Configure environment variables
cp .env.example .env
# Edit .env with your settings (Google OAuth, PINs, etc.)

# Start the application
docker-compose up --build

# Access the application
# Frontend: http://localhost
# Backend API: http://localhost:1111
```

### Local Development

**Backend:**
```bash
go run cmd/server/main.go
```

**Frontend:**
```bash
cd frontend
npm install
npm run dev
```

See [Installation Guide](../02-getting-started/installation.md) for detailed setup instructions.

## Architecture

Local Music Queue follows **Clean Architecture** principles with clear separation of concerns:

```
┌─────────────────────────────────────────────────────────┐
│                     Delivery Layer                       │
│              (HTTP Handlers, WebSocket Hub)              │
├─────────────────────────────────────────────────────────┤
│                    Usecase Layer                         │
│         (Queue, Auth, Priority, Activity Logic)          │
├─────────────────────────────────────────────────────────┤
│                 Infrastructure Layer                     │
│        (SQLite, YouTube/yt-dlp, Configuration)           │
├─────────────────────────────────────────────────────────┤
│                     Domain Layer                         │
│          (Entities, Repository/Service Interfaces)       │
└─────────────────────────────────────────────────────────┘
```

See [Architecture Guide](architecture.md) for detailed explanation.

## Technology Stack

**Backend:**
- Go 1.22+
- SQLite (modernc.org/sqlite)
- gorilla/websocket
- yt-dlp for YouTube metadata

**Frontend:**
- Vue 3 (Composition API)
- Vite
- Vue Router
- YouTube IFrame API

**Infrastructure:**
- Docker & Docker Compose
- Nginx (reverse proxy)
- Let's Encrypt (HTTPS)
- DuckDNS (dynamic DNS)

See [Technology Stack](technology-stack.md) for more details.

## User Roles

- **Host** - Full control over playback and queue, hosts the YouTube player
- **Admin** - Can control playback remotely (play/pause/skip/volume) but cannot host player
- **Guest** - Can add songs, use priority tokens, view queue and activity

Roles are assigned via email whitelists in environment variables.

## Documentation

- **[Getting Started](../02-getting-started/)** - Installation, deployment, configuration
- **[Features](../03-features/)** - Detailed feature documentation
- **[API Reference](../04-api-reference/)** - REST endpoints and WebSocket events
- **[Frontend Guide](../05-frontend/)** - Component structure and state management
- **[Backend Guide](../06-backend/)** - Architecture layers and database schema
- **[Deployment](../07-deployment/)** - Docker, HTTPS, production setup
- **[Development](../08-development/)** - Local development, testing, contributing
- **[Roadmap](../09-roadmap/)** - Implemented features and future plans

## License

[Add your license here]

## Contributing

See [Contributing Guide](../08-development/contributing.md) for development guidelines.
