-- 0007_room_auto_queue.up.sql
-- R09f: per-room auto-queue persistence (config + history). Mirrors the
-- global auto_queue_config / play_history tables (R01) but is keyed by
-- room_id so rooms are independent — toggling the global auto-queue
-- MUST NOT mutate a per-room setting (and vice versa).
--
-- The room_auto_queue_config table is one row per room; missing rows
-- resolve to (enabled=false, strategy='related') at the repository
-- layer (mirrors how the global table seeds the default in 0001).
--
-- The room_play_history table is capped at 50 rows per room,
-- enforced server-side from the Go repository (same as the global
-- play_history cap from R01).

CREATE TABLE IF NOT EXISTS room_auto_queue_config (
    room_id    BIGINT PRIMARY KEY REFERENCES rooms(id) ON DELETE CASCADE,
    enabled    BOOLEAN NOT NULL DEFAULT FALSE,
    strategy   TEXT    NOT NULL DEFAULT 'related',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS room_play_history (
    id        BIGSERIAL PRIMARY KEY,
    room_id   BIGINT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    video_id  TEXT NOT NULL,
    title     TEXT NOT NULL,
    played_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Used by AppendHistory's per-room 50-row cap and GetRecentHistory's
-- newest-first lookup.
CREATE INDEX IF NOT EXISTS idx_room_play_history_room_played_at
    ON room_play_history (room_id, played_at DESC);
