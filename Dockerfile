# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application with CGO enabled for SQLite
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o server ./cmd/server

# Final stage
FROM alpine:latest

WORKDIR /app

# Install runtime dependencies including certbot for Let's Encrypt certificates
RUN apk add --no-cache \
    python3 \
    py3-pip \
    yt-dlp \
    ffmpeg \
    ca-certificates \
    tzdata \
    certbot \
    cronie && \
    pip3 install --break-system-packages certbot-dns-duckdns

# Create directories
RUN mkdir -p /app/data /app/certs

# Copy binary from builder
COPY --from=builder /app/server .

# Copy certificate setup script
COPY docker/setup-certs.sh /app/setup-certs.sh
RUN chmod +x /app/setup-certs.sh

# Set environment variables (will be overridden by docker-compose)
ENV PORT=443
ENV DB_PATH=/app/data/music_queue.db
ENV YTDLP_PATH=/usr/bin/yt-dlp
ENV CERT_FILE=/app/certs/server.crt
ENV KEY_FILE=/app/certs/server.key

# Expose HTTPS port
EXPOSE 443

# Start cron, setup certificates, and start server
CMD ["/bin/sh", "-c", "crond && /app/setup-certs.sh && /app/server"]
