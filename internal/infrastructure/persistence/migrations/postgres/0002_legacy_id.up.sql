-- 0002_legacy_id.up.sql
-- Adds the legacy_id column to users so that the R03 data migration CLI can
-- record the original SQLite users.id mapping. ADR 002 §11 requires this column
-- to be preserved through R03; R06 drops it after the room_id refactor absorbs
-- the user-identity information.
--
-- The column is nullable so existing R02 rows remain untouched. A plain
-- (non-partial) unique index is used because PostgreSQL's ON CONFLICT
-- inference requires a non-partial unique index arbiter; multiple NULLs
-- coexist because PostgreSQL btree unique indexes treat NULLs as distinct.

ALTER TABLE users ADD COLUMN IF NOT EXISTS legacy_id BIGINT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_legacy_id ON users(legacy_id);