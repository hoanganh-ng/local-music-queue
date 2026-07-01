-- 0007_room_auto_queue.down.sql
-- Reverses 0007_room_auto_queue. Local-dev only.

DROP TABLE IF EXISTS room_play_history;
DROP TABLE IF EXISTS room_auto_queue_config;
