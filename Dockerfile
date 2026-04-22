# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build dependencies if any (none expected for CGO-free sqlite)
RUN apk add --no-cache git

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
# Use CGO_ENABLED=0 for a static binary
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server/*

# Final stage
FROM alpine:latest

WORKDIR /app

# Install runtime dependencies
# yt-dlp requires python3
# ffmpeg is recommended for yt-dlp metadata/extraction tasks
# ca-certificates is required for HTTPS
RUN apk add --no-cache \
    python3 \
    yt-dlp \
    ffmpeg \
    ca-certificates \
    tzdata

# Create directory for the database
RUN mkdir -p /app/data

# Copy binary from builder
COPY --from=builder /app/server .

# Set environment variables
ENV PORT=1111
ENV CLIENT_PIN=5555
ENV HOST_PIN=9512
ENV ADMIN_PIN=1598
ENV DB_PATH=/app/data/music_queue.db
ENV YTDLP_PATH=/usr/bin/yt-dlp

# Expose the application port
EXPOSE 1111

# Run the server
CMD ["./server"]
