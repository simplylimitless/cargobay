-- Adds a UI-configurable backup destination: storage_type/storage_config
-- mirror config.StorageConfig's shape (type + generic key/value map), so
-- backups can target a distinct storage backend from Settings without a
-- server restart. Empty storage_type means "reuse the main artifact
-- storage adapter" -- the pre-existing behavior -- preserved as the default.
ALTER TABLE backup_settings
    ADD COLUMN IF NOT EXISTS storage_type   TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS storage_config JSONB NOT NULL DEFAULT '{}'::jsonb;
