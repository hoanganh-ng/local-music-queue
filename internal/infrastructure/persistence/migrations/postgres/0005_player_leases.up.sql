-- 0005_player_leases.up.sql
-- Per-room player lease. ADR 001 §6. Exactly one active lease per room,
-- enforced by a partial unique index on (room_id) WHERE ended_at IS NULL.
-- On lease expiry beyond grace, the interactor ends the lease row and
-- archives the room; this migration does NOT enforce archive-on-expiry at
-- the SQL level (the archive path runs through the use case + hub ticker).

CREATE TABLE IF NOT EXISTS player_leases (
    id                  BIGSERIAL PRIMARY KEY,
    room_id             BIGINT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    claimed_by_user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    claimed_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_heartbeat_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at          TIMESTAMPTZ NOT NULL,
    ended_at            TIMESTAMPTZ NULL
);

-- One active lease per room. NULL ended_at means active.
CREATE UNIQUE INDEX IF NOT EXISTS idx_player_leases_one_active_per_room
    ON player_leases (room_id) WHERE ended_at IS NULL;

-- Sweeper index: list active leases ordered by expires_at.
CREATE INDEX IF NOT EXISTS idx_player_leases_active_expires_at
    ON player_leases (expires_at) WHERE ended_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_player_leases_room_id
    ON player_leases (room_id);