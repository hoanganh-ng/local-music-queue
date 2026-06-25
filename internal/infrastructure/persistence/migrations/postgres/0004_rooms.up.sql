-- 0004_rooms.up.sql
-- Room, membership, and invite persistence. R04 only.
-- No room_id columns are added to existing tables (queue_state, activities,
-- auto_queue_config, play_history); that is R06. The R04 schema strictly
-- adds three new tables and the indexes/constraints that enforce
-- exactly-one-host, unique slug, and token-hash lookup.

CREATE TABLE IF NOT EXISTS rooms (
    id          BIGSERIAL PRIMARY KEY,
    slug        TEXT NOT NULL,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT rooms_slug_unique UNIQUE (slug),
    CONSTRAINT rooms_status_valid CHECK (status IN ('active', 'archived'))
);

CREATE TABLE IF NOT EXISTS room_members (
    room_id    BIGINT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL,
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (room_id, user_id),
    CONSTRAINT room_members_role_valid CHECK (role IN ('host', 'admin', 'guest'))
);

-- Exactly one host per room. A partial unique index on (room_id) where role='host'
-- enforces the invariant at the database level; the use case layer additionally
-- asserts the invariant before writes.
CREATE UNIQUE INDEX IF NOT EXISTS idx_room_members_one_host_per_room
    ON room_members (room_id) WHERE role = 'host';

CREATE TABLE IF NOT EXISTS room_invites (
    id            BIGSERIAL PRIMARY KEY,
    room_id       BIGINT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    token_hash    TEXT NOT NULL UNIQUE,
    created_by    BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at    TIMESTAMPTZ NOT NULL,
    revoked_at    TIMESTAMPTZ NULL,
    max_uses      INTEGER NOT NULL DEFAULT 0,
    use_count     INTEGER NOT NULL DEFAULT 0,
    CONSTRAINT room_invites_max_uses_nonneg CHECK (max_uses >= 0),
    CONSTRAINT room_invites_use_count_nonneg CHECK (use_count >= 0)
);

CREATE INDEX IF NOT EXISTS idx_room_invites_room_id ON room_invites (room_id);