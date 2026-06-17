# Documentation Structure Summary

This document provides an overview of the new documentation structure created on 2026-04-23.

## Source of Truth
**Note:** `00-project-management/PROJECT_STATE.md` is the authoritative source for current implementation facts.

## Structure

```
documents/
├── 00-project-management/          # Project management and baseline state
├── 01-overview/                    # Project introduction
│   ├── README.md                   # Project overview and quick start
│   ├── architecture.md             # Clean Architecture explanation
│   └── technology-stack.md         # Technologies used
│
├── 02-getting-started/             # Setup and installation
│   ├── installation.md             # Local development setup
│   ├── docker-deployment.md        # Docker deployment guide
│   ├── environment-variables.md    # Complete env var reference
│   └── first-run.md                # [Planned] Initial configuration
│
├── 03-features/                    # Feature documentation
│   ├── authentication.md           # Google OAuth, roles, email restrictions
│   ├── priority-system.md          # Daily tokens, prioritization mechanics
│   ├── queue-management.md         # [Planned] Queue operations
│   ├── youtube-integration.md      # [Planned] Search, metadata, yt-dlp
│   ├── real-time-updates.md        # [Planned] WebSocket, delta broadcasting
│   ├── activity-tracking.md        # [Planned] Activity log
│   ├── playback-control.md         # [Planned] YouTube player, sync
│   ├── permissions.md              # [Planned] Host/Admin/Guest capabilities
│   ├── auto-queue-feature.md       # Auto-queue configuration
│   └── voting-system.md            # Community voting mechanics
│
├── 04-api-reference/               # API documentation
│   ├── rest-endpoints.md           # [Planned] All HTTP endpoints
│   ├── websocket-events.md         # [Planned] All WebSocket events
│   └── error-codes.md              # [Planned] Error responses
│
├── 05-frontend/                    # Frontend guide
│   ├── components.md               # [Planned] Component hierarchy
│   ├── state-management.md         # [Planned] Store structure
│   ├── routing.md                  # [Planned] Vue Router config
│   └── styling.md                  # [Planned] Design system
│
├── 06-backend/                     # Backend guide
│   ├── domain-layer.md             # [Planned] Entities and interfaces
│   ├── usecase-layer.md            # [Planned] Business logic
│   ├── infrastructure-layer.md     # [Planned] Repositories, services
│   ├── delivery-layer.md           # [Planned] HTTP, WebSocket
│   └── database-schema.md          # [Planned] SQLite tables
│
├── 07-deployment/                  # Deployment guides
│   ├── docker.md                   # [Planned] Dockerfile details
│   ├── docker-compose.md           # [Planned] Service orchestration
│   ├── https-setup.md              # Let's Encrypt, DuckDNS, certificates
│   ├── nginx-configuration.md      # [Planned] Reverse proxy config
│   └── production-checklist.md     # [Planned] Pre-launch verification
│
├── 08-development/                 # Development guides
│   ├── local-development.md        # [Planned] Running without Docker
│   ├── testing.md                  # [Planned] Running tests
│   ├── contributing.md             # [Planned] Code style, PR process
│   └── debugging.md                # [Planned] Common issues
│
└── 09-roadmap/                     # Project roadmap
    ├── implemented-features.md     # Complete list of implemented features
    └── future-features.md          # Planned enhancements
```
