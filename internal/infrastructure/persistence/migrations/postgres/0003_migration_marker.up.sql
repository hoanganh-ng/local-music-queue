-- 0003_migration_marker.up.sql
-- Adds the durable migration_marker table used by the R03 data migration CLI
-- to prove a target database is the exact result of a prior successful run.
--
-- The single-row table stores per-table SHA256 hashes computed over the
-- source SQLite projection. The second-run probe compares these hashes to a
-- fresh recomputation; only an exact match is treated as an already-migrated
-- target. A target with rows but no marker is treated as dirty and rejected.
--
-- Bytea is used for hashes to make accidental leakage less harmful than text.
-- source_path is the absolute path the operator ran the CLI with; it is not a
-- secret and is shown in the integrity report header.

CREATE TABLE IF NOT EXISTS migration_marker (
    id                       INTEGER     PRIMARY KEY CHECK (id = 1),
    source_path              TEXT        NOT NULL,
    source_sha256            BYTEA       NOT NULL,
    queue_state_sha256       BYTEA       NOT NULL,
    users_sha256             BYTEA       NOT NULL,
    activities_sha256        BYTEA       NOT NULL,
    play_history_sha256      BYTEA       NOT NULL,
    user_sessions_sha256     BYTEA       NOT NULL,
    priority_tx_sha256       BYTEA       NOT NULL,
    auto_queue_config_sha256 BYTEA       NOT NULL,
    started_at               TIMESTAMPTZ NOT NULL,
    finished_at              TIMESTAMPTZ NOT NULL
);