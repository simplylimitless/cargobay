-- Tracks how many times each artifact version has been pulled/downloaded
-- through the proxy, incremented atomically on each serve.
ALTER TABLE artifacts ADD COLUMN IF NOT EXISTS downloads BIGINT NOT NULL DEFAULT 0;
