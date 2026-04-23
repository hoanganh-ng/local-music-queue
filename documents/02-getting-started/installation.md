# Installation Guide

This guide covers local installation and setup of Local Music Queue for development.

## Prerequisites

### Required Software

- **Go 1.22+** - [Download](https://go.dev/dl/)
- **Node.js 18+** - [Download](https://nodejs.org/)
- **npm 9+** - Comes with Node.js
- **yt-dlp** - [Installation guide](https://github.com/yt-dlp/yt-dlp#installation)
- **Git** - [Download](https://git-scm.com/)

### Optional (for Docker deployment)

- **Docker 20.10+** - [Download](https://docs.docker.com/get-docker/)
- **Docker Compose 2.0+** - [Download](https://docs.docker.com/compose/install/)

---

## Local Development Setup

### 1. Clone the Repository

```bash
git clone <repository-url>
cd local-music-queue
```

### 2. Install yt-dlp

**Linux/macOS:**
```bash
# Using pip
pip install yt-dlp

# Or using package manager
# Ubuntu/Debian
sudo apt install yt-dlp

# macOS
brew install yt-dlp
```

**Windows:**
```bash
# Using pip
pip install yt-dlp

# Or download binary from https://github.com/yt-dlp/yt-dlp/releases
```

Verify installation:
```bash
yt-dlp --version
```

### 3. Backend Setup

```bash
# Navigate to project root
cd /path/to/local-music-queue

# Download Go dependencies
go mod download

# Verify dependencies
go mod verify

# Create database directory
mkdir -p .localdb

# Copy environment template
cp .env.example .env

# Edit .env with your configuration
nano .env  # or use your preferred editor
```

**Minimum .env configuration:**
```bash
PORT=1111
CLIENT_PIN=5555
HOST_PIN=6666
DB_PATH=./.localdb/music_queue.db
YTDLP_PATH=yt-dlp

# Google OAuth (required for authentication)
GOOGLE_CLIENT_ID=your-google-client-id.apps.googleusercontent.com
HOST_EMAILS=host@urekamedia.vn
ADMIN_EMAILS=admin@urekamedia.vn
```

**Run the backend:**
```bash
go run cmd/server/main.go
```

You should see:
```
Server starting on :1111
WebSocket hub started
```

### 4. Frontend Setup

```bash
# Navigate to frontend directory
cd frontend

# Install dependencies
npm install

# Copy environment template
cp .env.example .env

# Edit .env with your configuration
nano .env
```

**Frontend .env configuration:**
```bash
VITE_API_BASE_URL=http://localhost:1111
VITE_GOOGLE_CLIENT_ID=your-google-client-id.apps.googleusercontent.com
```

**Run the frontend:**
```bash
npm run dev
```

You should see:
```
VITE v5.x.x  ready in xxx ms

➜  Local:   http://localhost:5173/
➜  Network: use --host to expose
```

### 5. Access the Application

Open your browser and navigate to:
- **Frontend**: http://localhost:5173
- **Backend API**: http://localhost:1111/api/queue

---

## Google OAuth Setup

To enable authentication, you need to create a Google OAuth 2.0 client.

### 1. Create Google Cloud Project

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select existing one
3. Enable **Google+ API** (for user profile access)

### 2. Create OAuth 2.0 Credentials

1. Navigate to **APIs & Services** → **Credentials**
2. Click **Create Credentials** → **OAuth client ID**
3. Select **Web application**
4. Configure:
   - **Name**: Local Music Queue
   - **Authorized JavaScript origins**:
     - `http://localhost:5173` (development)
     - `http://localhost` (production)
     - Add your production domain if applicable
   - **Authorized redirect URIs**:
     - `http://localhost:5173` (development)
     - `http://localhost` (production)
5. Click **Create**
6. Copy the **Client ID** (format: `xxx.apps.googleusercontent.com`)

### 3. Configure Environment Variables

Add the Client ID to both `.env` files:

**Backend `.env`:**
```bash
GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
HOST_EMAILS=your-email@urekamedia.vn
ADMIN_EMAILS=admin@urekamedia.vn
```

**Frontend `.env`:**
```bash
VITE_GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
```

### 4. Email Domain Restriction

By default, only `@urekamedia.vn` emails can log in. To change this:

Edit `internal/usecase/auth/interactor.go`:
```go
// Line ~50
allowedDomain := "urekamedia.vn"  // Change to your domain
```

Or remove the restriction entirely by commenting out the domain check.

---

## Running Tests

### Backend Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run specific package
go test ./internal/usecase/queue/

# Run specific test
go test -run TestAddSong ./internal/usecase/queue/
```

### Frontend Tests

```bash
cd frontend

# Run unit tests
npm run test:unit

# Run E2E tests (requires running application)
npm run test:e2e
```

---

## Troubleshooting

### Backend Issues

**Issue: `yt-dlp: command not found`**
```bash
# Verify yt-dlp is in PATH
which yt-dlp

# If not found, install it or specify full path in .env
YTDLP_PATH=/usr/local/bin/yt-dlp
```

**Issue: `database is locked`**
```bash
# Stop all running instances
pkill -f "go run cmd/server/main.go"

# Remove lock file
rm .localdb/music_queue.db-shm
rm .localdb/music_queue.db-wal
```

**Issue: `port 1111 already in use`**
```bash
# Find process using port
lsof -i :1111

# Kill the process
kill -9 <PID>

# Or change port in .env
PORT=8080
```

### Frontend Issues

**Issue: `CORS error when calling API`**

Make sure backend is running and CORS is enabled. Check `cmd/server/main.go` for CORS middleware.

**Issue: `Google Sign-In button not showing`**

1. Verify `VITE_GOOGLE_CLIENT_ID` is set in frontend `.env`
2. Check browser console for errors
3. Ensure authorized origins are configured in Google Cloud Console

**Issue: `WebSocket connection failed`**

1. Verify backend is running on correct port
2. Check `VITE_API_BASE_URL` in frontend `.env`
3. Ensure no firewall blocking WebSocket connections

---

## Development Workflow

### Recommended Setup

1. **Terminal 1**: Run backend
   ```bash
   go run cmd/server/main.go
   ```

2. **Terminal 2**: Run frontend
   ```bash
   cd frontend && npm run dev
   ```

3. **Terminal 3**: Run tests on file changes
   ```bash
   # Backend
   go test ./... -watch  # (requires external tool like gowatch)
   
   # Frontend
   cd frontend && npm run test:unit -- --watch
   ```

### Hot Reload

- **Backend**: Use [air](https://github.com/cosmtrek/air) for hot reload
  ```bash
  go install github.com/cosmtrek/air@latest
  air
  ```

- **Frontend**: Vite provides hot reload by default

---

## Next Steps

- [Docker Deployment](docker-deployment.md) - Deploy with Docker Compose
- [Environment Variables](environment-variables.md) - Complete configuration reference
- [Development Guide](../08-development/local-development.md) - Development best practices
