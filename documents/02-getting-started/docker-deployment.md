# Docker Deployment Guide

This guide covers deploying Local Music Queue using Docker and Docker Compose.

## Prerequisites

- **Docker 20.10+** - [Installation guide](https://docs.docker.com/get-docker/)
- **Docker Compose 2.0+** - [Installation guide](https://docs.docker.com/compose/install/)
- **yt-dlp** - Included in backend Docker image (no manual installation needed)

---

## Quick Start

### 1. Clone and Configure

```bash
# Clone repository
git clone <repository-url>
cd local-music-queue

# Copy environment template
cp .env.example .env

# Edit configuration
nano .env
```

### 2. Configure Environment Variables

Edit `.env` with your settings:

```bash
# Frontend Ports
FRONTEND_HTTP_PORT=8011
FRONTEND_HTTPS_PORT=8012

# Backend Port
BACKEND_PORT=1111

# Google OAuth Configuration
GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com

# Role Configuration (comma-separated emails)
HOST_EMAILS=host@urekamedia.vn
ADMIN_EMAILS=admin@urekamedia.vn

# DuckDNS Configuration for HTTPS (optional)
DUCKDNS_DOMAIN=yourname.duckdns.org
DUCKDNS_TOKEN=your-duckdns-token-here
LETSENCRYPT_EMAIL=your-email@example.com
```

### 3. Start the Application

```bash
# Build and start all services
docker-compose up --build

# Or run in detached mode
docker-compose up -d --build
```

### 4. Access the Application

- **HTTP**: http://localhost:8011
- **HTTPS**: https://yourname.duckdns.org (if configured)
- **Backend API**: http://localhost:1111

---

## Docker Architecture

### Services

The `docker-compose.yml` defines three services:

```yaml
services:
  backend:      # Go API server
  frontend:     # Vue.js app served by Nginx
  # Optional: certbot for Let's Encrypt
```

### Container Details

**Backend Container:**
- **Base Image**: Alpine Linux (final stage)
- **Size**: ~20MB
- **Exposed Port**: 1111
- **Volume**: `.localdb/` (SQLite database persistence)
- **Includes**: yt-dlp binary

**Frontend Container:**
- **Base Image**: Nginx Alpine
- **Size**: ~25MB
- **Exposed Ports**: 8011 (HTTP), 8012 (HTTPS)
- **Serves**: Static Vue.js build + reverse proxy to backend

---

## Docker Compose Configuration

### Basic Configuration (HTTP Only)

```yaml
version: '3.8'

services:
  backend:
    build:
      context: .
      dockerfile: Dockerfile.backend
    ports:
      - "${BACKEND_PORT}:1111"
    volumes:
      - ./.localdb:/app/data
    environment:
      - PORT=1111
      - DB_PATH=/app/data/music_queue.db
      - GOOGLE_CLIENT_ID=${GOOGLE_CLIENT_ID}
      - HOST_EMAILS=${HOST_EMAILS}
      - ADMIN_EMAILS=${ADMIN_EMAILS}
    restart: unless-stopped

  frontend:
    build:
      context: ./frontend
      dockerfile: Dockerfile
      args:
        - VITE_API_BASE_URL=http://localhost:${BACKEND_PORT}
        - VITE_GOOGLE_CLIENT_ID=${GOOGLE_CLIENT_ID}
    ports:
      - "${FRONTEND_HTTP_PORT}:80"
    depends_on:
      - backend
    restart: unless-stopped
```

### With HTTPS (DuckDNS + Let's Encrypt)

See [HTTPS Setup Guide](../07-deployment/https-setup.md) for full configuration.

---

## Building Images

### Build Backend Image

```bash
# From project root
docker build -t local-music-queue-backend -f Dockerfile.backend .

# Run standalone
docker run -p 1111:1111 \
  -v $(pwd)/.localdb:/app/data \
  -e GOOGLE_CLIENT_ID=your-client-id \
  local-music-queue-backend
```

**Dockerfile.backend** (multi-stage build):
```dockerfile
# Stage 1: Build
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o server cmd/server/main.go

# Stage 2: Runtime
FROM alpine:latest
RUN apk add --no-cache yt-dlp
WORKDIR /app
COPY --from=builder /app/server .
EXPOSE 1111
CMD ["./server"]
```

### Build Frontend Image

```bash
# From frontend directory
cd frontend

docker build -t local-music-queue-frontend \
  --build-arg VITE_API_BASE_URL=http://localhost:1111 \
  --build-arg VITE_GOOGLE_CLIENT_ID=your-client-id \
  .

# Run standalone
docker run -p 80:80 local-music-queue-frontend
```

**Dockerfile** (multi-stage build):
```dockerfile
# Stage 1: Build
FROM node:18-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
ARG VITE_API_BASE_URL
ARG VITE_GOOGLE_CLIENT_ID
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL
ENV VITE_GOOGLE_CLIENT_ID=$VITE_GOOGLE_CLIENT_ID
RUN npm run build

# Stage 2: Runtime
FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
```

---

## Managing the Application

### Start Services

```bash
# Start all services
docker-compose up -d

# Start specific service
docker-compose up -d backend
```

### Stop Services

```bash
# Stop all services
docker-compose down

# Stop and remove volumes (deletes database!)
docker-compose down -v
```

### View Logs

```bash
# All services
docker-compose logs -f

# Specific service
docker-compose logs -f backend
docker-compose logs -f frontend

# Last 100 lines
docker-compose logs --tail=100 backend
```

### Restart Services

```bash
# Restart all
docker-compose restart

# Restart specific service
docker-compose restart backend
```

### Rebuild After Code Changes

```bash
# Rebuild and restart
docker-compose up -d --build

# Rebuild specific service
docker-compose up -d --build backend
```

---

## Data Persistence

### SQLite Database

The database is persisted in `.localdb/` directory:

```bash
# Backup database
cp .localdb/music_queue.db .localdb/music_queue.db.backup

# Restore database
cp .localdb/music_queue.db.backup .localdb/music_queue.db

# View database
sqlite3 .localdb/music_queue.db
```

### Volume Management

```bash
# List volumes
docker volume ls

# Inspect volume
docker volume inspect local-music-queue_localdb

# Remove volume (deletes data!)
docker volume rm local-music-queue_localdb
```

---

## Troubleshooting

### Container Won't Start

```bash
# Check container status
docker-compose ps

# View container logs
docker-compose logs backend

# Inspect container
docker inspect local-music-queue-backend-1
```

### Port Already in Use

```bash
# Find process using port
lsof -i :1111

# Change port in .env
BACKEND_PORT=8080

# Restart services
docker-compose down
docker-compose up -d
```

### Database Locked

```bash
# Stop all containers
docker-compose down

# Remove lock files
rm .localdb/music_queue.db-shm
rm .localdb/music_queue.db-wal

# Restart
docker-compose up -d
```

### yt-dlp Not Working

```bash
# Enter backend container
docker-compose exec backend sh

# Test yt-dlp
yt-dlp --version
yt-dlp --dump-json "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

# Update yt-dlp
apk update && apk upgrade yt-dlp
```

### Frontend Can't Connect to Backend

1. Check backend is running: `docker-compose ps`
2. Check backend logs: `docker-compose logs backend`
3. Verify `VITE_API_BASE_URL` in frontend build args
4. Check Nginx configuration: `docker-compose exec frontend cat /etc/nginx/conf.d/default.conf`

---

## Production Deployment

### Recommended Configuration

```bash
# .env for production
FRONTEND_HTTP_PORT=80
FRONTEND_HTTPS_PORT=443
BACKEND_PORT=1111

# Use production domain
DUCKDNS_DOMAIN=yourapp.duckdns.org
DUCKDNS_TOKEN=your-token
LETSENCRYPT_EMAIL=admin@yourdomain.com

# Restrict to production emails
HOST_EMAILS=host@company.com
ADMIN_EMAILS=admin@company.com
```

### Security Checklist

- [ ] Change default PINs (if still using PIN auth)
- [ ] Configure HTTPS with valid certificates
- [ ] Restrict HOST_EMAILS and ADMIN_EMAILS to trusted users
- [ ] Set up firewall rules (allow only 80, 443, 1111)
- [ ] Enable Docker logging driver
- [ ] Set up automated backups for `.localdb/`
- [ ] Configure restart policies (`restart: unless-stopped`)
- [ ] Review Nginx security headers

### Monitoring

```bash
# Container resource usage
docker stats

# Disk usage
docker system df

# Container health
docker-compose ps
```

---

## Updating the Application

### Pull Latest Changes

```bash
# Pull from git
git pull origin main

# Rebuild and restart
docker-compose down
docker-compose up -d --build
```

### Database Migrations

Currently, the application auto-creates tables on first run. For future migrations:

```bash
# Backup before updating
cp .localdb/music_queue.db .localdb/music_queue.db.$(date +%Y%m%d)

# Update and restart
docker-compose up -d --build
```

---

## Next Steps

- [HTTPS Setup](../07-deployment/https-setup.md) - Configure Let's Encrypt and DuckDNS
- [Nginx Configuration](../07-deployment/nginx-configuration.md) - Customize reverse proxy
- [Production Checklist](../07-deployment/production-checklist.md) - Pre-launch verification
