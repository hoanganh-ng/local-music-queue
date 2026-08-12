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

RUN apk add --no-cache \
    yt-dlp \
    ffmpeg \
    ca-certificates \
    tzdata

RUN mkdir -p /app/data

COPY --from=builder /app/server .

ENV APP_ENV=production
ENV PORT=1111
ENV DB_PATH=/app/data/music_queue.db
ENV YTDLP_PATH=/usr/bin/yt-dlp

EXPOSE 1111

CMD ["/app/server"]
