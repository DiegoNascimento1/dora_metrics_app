DROP INDEX IF EXISTS platform.project_active_last_synced_idx;
ALTER TABLE platform.project DROP COLUMN IF EXISTS last_synced_at;
