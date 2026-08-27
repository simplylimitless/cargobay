-- Tracks bytes served from local cache for each artifact version (i.e.
-- bandwidth saved by not re-fetching from the upstream registry).
ALTER TABLE artifacts ADD COLUMN IF NOT EXISTS bandwidth_saved BIGINT NOT NULL DEFAULT 0;
