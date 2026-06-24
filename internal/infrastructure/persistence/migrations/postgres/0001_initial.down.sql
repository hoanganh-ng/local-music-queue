-- 0001_initial.down.sql
-- Reverses the initial schema. Down-migrations are local-dev only per ADR 002.

DROP TABLE IF EXISTS play_history;
DROP TABLE IF EXISTS auto_queue_config;
DROP TABLE IF EXISTS priority_transactions;
DROP TABLE IF EXISTS user_sessions;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS activities;
DROP TABLE IF EXISTS queue_state;