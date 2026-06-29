-- 0006_room_queue_state.up.sql
-- R07a: per-room playback queue state. One JSONB row per room,
-- mirroring the existing global queue_state shape. The interactor
-- layer reuses entity.Queue invariants (Songs / CurrentIndex / Status /
-- Elapsed / History) and serializes the entire *entity.Queue to JSONB.
--
-- This migration intentionally does NOT introduce per-song rows; that
-- remains a deferred design question. The FK from room_id to rooms(id)
-- is the only constraint.

CREATE TABLE IF NOT EXISTS room_queue_state (
    room_id     BIGINT PRIMARY KEY REFERENCES rooms(id) ON DELETE CASCADE,
    data        JSONB NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);