-- 0009_room_cutover_support.down.sql
-- Reverses 0009_room_cutover_support. Drops ONLY the two tables this
-- migration introduced; historical tables (0001-0008) are never touched.
-- Down-migrations are local-dev only per ADR 002.

DROP TABLE IF EXISTS room_cutover_marker;
DROP TABLE IF EXISTS room_activities;
