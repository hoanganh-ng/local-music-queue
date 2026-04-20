# 🎵 Local Music Queue

A local-network music queue application powered by **YouTube**. Perfect for parties or office environments where anyone on the same LAN can contribute to a shared playlist.

![Glassmorphism UI](https://img.shields.io/badge/UI-Glassmorphism-blue?style=flat-square)
![Go](https://img.shields.io/badge/Backend-Go-00ADD8?style=flat-square&logo=go)
![Vue.js](https://img.shields.io/badge/Frontend-Vue.js-4FC08D?style=flat-square&logo=vue.js)

## ✨ Features

- 🔐 **PIN Protected Access**: Separate PINs for guests and hosts.
- 🔍 **YouTube Integration**: Search for songs or paste YouTube/YouTube Music URLs.
- ⚡ **Real-time Updates**: Live queue and activity feed powered by WebSockets.
- 📺 **Host Playback**: Centralized playback on the host machine using YouTube IFrame API.
- 📝 **Activity Feed**: See who joined, who added songs, and who skipped tracks.
- 💎 **Premium Design**: Modern glassmorphism UI with a focus on aesthetics and responsiveness.

## 🏗️ Architecture

The project follows **Clean Architecture** principles to ensure maintainability and testability:

- **Domain**: Core business entities and logic.
- **Usecase**: Application-specific business rules.
- **Infrastructure**: External services (YouTube via `yt-dlp`, SQLite database).
- **Delivery**: HTTP API and WebSocket handlers.

## 🚀 Getting Started

### Prerequisites

- **Go 1.22+**
- **Node.js & npm** (latest LTS recommended)
- **yt-dlp**: Required on the host machine to fetch metadata.
  - Install via `pip install yt-dlp` or your package manager (e.g., `brew install yt-dlp`).

### Backend Setup

1. From the project root:
   ```bash
   go mod download
   ```
2. Create the local database directory:
   ```bash
   mkdir -p .localdb
   ```
3. Run the server:
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
3. Start the development server:
   ```bash
   npm run dev
   ```
   *Default port: 5173*

## ⚙️ Configuration

The backend can be configured via environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | Server port | `1111` |
| `CLIENT_PIN` | PIN for guests to join | `5555` |
| `HOST_PIN` | PIN for host access | `6666` |
| `DB_PATH` | Path to SQLite database | `./.localdb/music_queue.db` |
| `YTDLP_PATH` | Path to `yt-dlp` executable | `yt-dlp` |

## 🛠️ Development

### Testing

- **Backend**: `go test ./...`
- **Frontend Unit**: `npm run test:unit` (inside `frontend/`)
- **Frontend E2E**: `npm run test:e2e` (inside `frontend/`)

## 📄 License

This project is open-source and available under the [MIT License](LICENSE).
