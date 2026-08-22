-- Singleton row holding tunable settings for how the backend rebuilds the
-- Postgres full-text search index (the search_vector column on artifacts).
-- Editable from Settings -> Search Index in the UI.
CREATE TABLE IF NOT EXISTS search_index_settings (
    id                      INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    auto_reindex_enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    reindex_interval_hours  INTEGER NOT NULL DEFAULT 24,
    last_checked_at         TIMESTAMPTZ,
    last_reindexed_at       TIMESTAMPTZ,
    last_artifact_count     INTEGER NOT NULL DEFAULT 0,
    last_error              TEXT NOT NULL DEFAULT ''
);
INSERT INTO search_index_settings (id) VALUES (1) ON CONFLICT DO NOTHING;
