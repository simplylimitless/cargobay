-- Singleton row holding tunable settings for the backend's scheduled
-- full database backup job. Editable from Settings -> Backup & Restore in
-- the UI. See backend/pkg/backup for the dump/restore/scheduler logic.
CREATE TABLE IF NOT EXISTS backup_settings (
    id                    INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    auto_backup_enabled   BOOLEAN NOT NULL DEFAULT FALSE,
    backup_interval_hours INTEGER NOT NULL DEFAULT 24,
    last_checked_at       TIMESTAMPTZ,
    last_backup_at        TIMESTAMPTZ,
    last_backup_path      TEXT NOT NULL DEFAULT '',
    last_error            TEXT NOT NULL DEFAULT ''
);
INSERT INTO backup_settings (id) VALUES (1) ON CONFLICT DO NOTHING;
