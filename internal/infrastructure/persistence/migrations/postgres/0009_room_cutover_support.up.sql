-- 0009_room_cutover_support.up.sql
-- R14b: schema support for the offline room-cutover mechanism (ADR 003,
-- Sprint 022 / R14a contract). Schema version 8 -> 9.
--
-- This migration adds exactly two tables and does NOT modify any historical
-- table (0001-0008). It is strictly additive:
--
--   * room_activities       — the per-room activity feed. Mirrors the global
--                             activities table (0001) but is keyed by room_id
--                             so each room owns an independent feed. The
--                             room-cutover copies the single global activities
--                             feed into the target room, preserving the legacy
--                             activity id, timestamp, type, user, and
--                             description verbatim (only room_id is added).
--
--   * room_cutover_marker   — the durable single-row idempotency marker written
--                             inside the cutover transaction. A re-run compares
--                             the recorded identity + source/target integrity
--                             hashes against a fresh recomputation and only
--                             short-circuits to a no-op on an exact match; any
--                             drift fails explicitly. target_room_id and
--                             host_user_id are plain audit snapshots WITHOUT
--                             foreign keys so the marker survives independent
--                             lifecycle changes to the referenced rows.
--
-- Runtime wiring (composing RoomActivityRepository into cmd/server, the
-- --room-cutover-authoritative startup guard, and the legacy-route 410 Gone
-- tombstones) is intentionally NOT part of this migration; it is owned by the
-- later R09i / R14c sprints.

CREATE TABLE IF NOT EXISTS room_activities (
    id          BIGSERIAL PRIMARY KEY,
    room_id     BIGINT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    "timestamp" TIMESTAMPTZ NOT NULL,
    type        TEXT NOT NULL,
    "user"      TEXT NOT NULL,
    description TEXT NOT NULL
);

-- Used by GetActivities' bounded newest-first lookup. The (timestamp DESC,
-- id DESC) tail makes the ordering total and stable even when several
-- activities share a timestamp.
CREATE INDEX IF NOT EXISTS idx_room_activities_room_timestamp
    ON room_activities (room_id, "timestamp" DESC, id DESC);

CREATE TABLE IF NOT EXISTS room_cutover_marker (
    id                    INTEGER     PRIMARY KEY CHECK (id = 1),
    room_cutover_id       UUID        NOT NULL,
    target_room_slug      TEXT        NOT NULL,
    target_room_id        BIGINT      NOT NULL,
    host_user_id          BIGINT      NOT NULL,
    source_hashes         JSONB       NOT NULL,
    target_hashes         JSONB       NOT NULL,
    legacy_id_offset      BIGINT      NOT NULL DEFAULT 0,
    cutover_pre_commit_at TIMESTAMPTZ NOT NULL,
    binary_build_sha      TEXT        NOT NULL
);
