# Frontend Implementation Guide — Vue.js + Vite

This document outlines the step-by-step implementation of the Local Music Queue frontend. The goal is a high-performance, responsive UI with real-time updates.

## Phase 1: Project Setup & Architecture
*Goal: Establish the base layout and design system.*

1.  **Initialize**: `npm create vite@latest frontend -- --template vue`.
2.  **CSS Foundation**:
    - Create `assets/main.css`: Define CSS Variables (Colors, Glassmorphism tokens, Typography).
    - Implement a reset and base layout (Sidebar for Activity, Main for Player, Sidebar for Queue).
3.  **State Management**:
    - Setup `src/store/`: Use `reactive` or `Pinia` to handle the global `QueueState` and `CurrentUser`.

---

## Phase 2: Component Development
*Goal: Build reusable, styled UI components.*

1.  **The "Glass" Components**:
    - `BaseButton`, `BaseInput`: Styled components using Vanilla CSS.
2.  **Dashboard Modules**:
    - `ActivityLog`: Renders the stream of event strings.
    - `NowPlaying`: Main visualizer, song title, and playback controls.
    - `QueueList`: Renders the upcoming tracks.
3.  **Song Entry**:
    - `SubmitForm`: Input field for YouTube URLs + submit logic.

---

## Phase 3: Services & Real-time Integration
*Goal: Connect the frontend to the Go backend.*

1.  **WebSocket Client**:
    - Create `src/services/websocket.js`: Handles connection, reconnection, and message parsing.
    - Sync incoming `STATE_UPDATE` messages to the Global Store.
2.  **Auth Service**:
    - Create `src/services/api.js`: Wrapper for the `/auth` endpoint to verify PINs and set session cookies/tokens.

---

## Phase 4: View Routing & Logic
*Goal: Finalize the user flows.*

1.  **Auth View**:
    - PIN entry screen with a "Waiting for Host" or "Enter Name" transition.
2.  **Dashboard View**:
    - Responsive layout:
        - Desktop: 3-column layout.
        - Mobile: Single column with tabbed navigation (Activity/Player/Queue).
3.  **Host Specific UI**:
    - Only show Play/Pause/Skip buttons if the `CurrentUser.Role === 'Host'`.
    - Integrated YouTube Iframe for the Host user to perform the actual audio output.
