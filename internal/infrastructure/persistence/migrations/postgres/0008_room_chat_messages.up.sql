-- 0008_room_chat_messages.up.sql
-- R11a: per-room plain-text chat persistence (no retention purge in
-- R11a; lifecycle hardening is deferred to R10f+). Mirrors the
-- documented R11a functional scope: any active member of an active
-- room may send and read chat messages; the table is keyed by
-- room_id with a (room_id, created_at) index for the per-call
-- history fetch.

CREATE TABLE IF NOT EXISTS room_chat_messages (
    id         BIGSERIAL PRIMARY KEY,
    room_id    BIGINT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    sender_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content    TEXT   NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Used by ListRecentMessages' bounded newest-first lookup.
CREATE INDEX IF NOT EXISTS idx_room_chat_messages_room_created
    ON room_chat_messages (room_id, created_at DESC);