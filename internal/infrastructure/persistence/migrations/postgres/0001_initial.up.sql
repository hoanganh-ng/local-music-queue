-- 0001_initial.up.sql
-- Initial schema mirroring the existing SQLite seven-table model.
-- No room_id columns; room scoping is R06.
-- The play_history 50-row cap is enforced server-side from the Go repository
-- (the SQLite trg_play_history_cap trigger is intentionally not ported).

CREATE TABLE IF NOT EXISTS queue_state (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    data        TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS activities (
    id          BIGSERIAL PRIMARY KEY,
    "timestamp" TIMESTAMPTZ NOT NULL,
    type        TEXT NOT NULL,
    "user"      TEXT NOT NULL,
    description TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id               BIGSERIAL PRIMARY KEY,
    email            TEXT NOT NULL UNIQUE,
    display_name     TEXT NOT NULL,
    profile_picture  TEXT,
    role             TEXT NOT NULL,
    priority_balance INTEGER NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_sessions (
    id             BIGSERIAL PRIMARY KEY,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_date   DATE NOT NULL,
    first_seen_at  TIMESTAMPTZ NOT NULL,
    last_seen_at   TIMESTAMPTZ NOT NULL,
    UNIQUE (user_id, session_date)
);

CREATE TABLE IF NOT EXISTS priority_transactions (
    id               BIGSERIAL PRIMARY KEY,
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    song_id          TEXT NOT NULL,
    song_title       TEXT NOT NULL,
    transaction_type TEXT NOT NULL,
    amount           INTEGER NOT NULL,
    balance_after    INTEGER NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS auto_queue_config (
    id       INTEGER PRIMARY KEY CHECK (id = 1),
    enabled  BOOLEAN NOT NULL DEFAULT FALSE,
    strategy TEXT    NOT NULL DEFAULT 'related'
);

INSERT INTO auto_queue_config (id, enabled, strategy)
VALUES (1, FALSE, 'related')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS play_history (
    id        BIGSERIAL PRIMARY KEY,
    video_id  TEXT NOT NULL,
    title     TEXT NOT NULL,
    played_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_play_history_played_at
    ON play_history (played_at DESC);