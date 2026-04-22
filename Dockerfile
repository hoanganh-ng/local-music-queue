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

# Install runtime dependencies including openssl for certificate generation
RUN apk add --no-cache \
    python3 \
    yt-dlp \
    ffmpeg \
    ca-certificates \
    tzdata \
    openssl

# Create directories
RUN mkdir -p /app/data /app/certs

# Copy binary from builder
COPY --from=builder /app/server .

# Copy certificate generation script
COPY docker/generate-backend-cert.sh /app/generate-cert.sh
RUN chmod +x /app/generate-cert.sh

# Set environment variables (will be overridden by docker-compose)
ENV PORT=443
ENV DB_PATH=/app/data/music_queue.db
ENV YTDLP_PATH=/usr/bin/yt-dlp
ENV CERT_FILE=/app/certs/server.crt
ENV KEY_FILE=/app/certs/server.key

# Expose HTTPS port
EXPOSE 443

# Generate certificate and start server
CMD ["/bin/sh", "-c", "/app/generate-cert.sh && /app/server"]
