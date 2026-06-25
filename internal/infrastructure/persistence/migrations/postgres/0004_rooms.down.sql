-- 0004_rooms.down.sql
-- Reverses 0004_rooms. Down-migrations are local-dev only per ADR 002.

DROP TABLE IF EXISTS room_invites;
DROP TABLE IF EXISTS room_members;
DROP TABLE IF EXISTS rooms;