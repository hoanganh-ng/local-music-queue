-- 0002_legacy_id.down.sql
-- Reverses 0002_legacy_id.up.sql. Local-dev only per ADR 002 §14.

DROP INDEX IF EXISTS idx_users_legacy_id;

ALTER TABLE users DROP COLUMN IF EXISTS legacy_id;