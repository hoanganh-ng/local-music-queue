# Local Music Queue V0.1 — Requirements

> Captured: 2026-04-17

## Overview

A local-network music queue application powered by **YouTube** (`youtube.com`).  
Multiple users on the same network can access it, browse the current playlist, and submit songs to a shared queue — all gated behind a PIN code.

---

## Functional Requirements

### Access Control
- A single static **PIN code** (configured by the host) is required to enter the app.
- Anyone connected to the local network and knowing the PIN can join.
- No user accounts; participants are identified by an ephemeral **display name** they choose at login.
- The host has their own **PIN code** to enter the app.
- The host has **full control** over the queue (add, remove, reorder, skip, etc.).

### Playback
- Playback is running on a host machine.
- The **host machine** controls actual audio/video output.
- All clients see real-time playback state (current track, elapsed time, paused/playing).

### Song Queue
- Users can **search** for songs on YouTube and submit them to the queue.
- Users can paste a **YouTube or YouTube Music URL** to submit a song (e.g. `music.youtube.com/watch?v=…`, `youtube.com/watch?v=…`, `youtu.be/…`).
- The server validates the URL, fetches track metadata (title, artist, thumbnail, duration) via `yt-dlp`, and adds it to the queue.
- The queue is **shared and live** — all clients see additions immediately.
- The current track auto-advances when a song ends.

### Activity Feed
- A live **activity log** is visible to all clients showing events:
  - `UserA added "Song Title" by Artist`
  - `UserB paused playback`
  - `UserC joined the room`
  - `UserD skipped the current song`

### UI Layout
```
┌─────────────────────────────────────────────────────────┐
│  [Activity Feed]    [   Playback Center   ]  [Queue]    │
│                     │       Thumbnail    |              │
|                     |       Title        │              │
│  • UserA added …    │  ───────────────── │  1. Song A   │
│  • UserB paused     │  ▶  0:42 / 3:55    │  2. Song B   │
│  • UserC joined     │  [Prev][Play][Next] │  3. Song C  │
│                     │                     │  …          │
└─────────────────────────────────────────────────────────┘
```

---

## Non-Functional Requirements

| Concern | Requirement |
|---|---|
| Network scope | LAN only (no public internet exposure) |
| Latency | Queue/activity updates < 50 ms |
| Concurrent users | ≤ 20 simultaneous clients |
| Platform | Server runs on the host machine; clients use any modern browser |