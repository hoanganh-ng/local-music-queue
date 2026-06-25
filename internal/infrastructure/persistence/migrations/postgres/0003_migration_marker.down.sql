-- 0003_migration_marker.down.sql
-- Reverses 0003_migration_marker. The marker table is recreated on the next
-- up-migration; its presence is required by the data migration CLI as of R03.

DROP TABLE IF EXISTS migration_marker;