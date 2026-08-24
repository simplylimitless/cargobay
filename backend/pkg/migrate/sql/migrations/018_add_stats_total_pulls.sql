-- Total Pulls on the Stats page was being computed as SUM(downloads) over
-- the live artifacts table, so deleting/re-caching artifacts silently wiped
-- the figure — unlike bandwidth_saved_bytes, which is a genuine running
-- accumulator. Add a matching accumulator column for pull counts so it
-- survives artifact deletion the same way.
ALTER TABLE stats ADD COLUMN IF NOT EXISTS total_pulls BIGINT NOT NULL DEFAULT 0;

-- Backfill from current artifact rows so existing counts aren't lost.
UPDATE stats SET total_pulls = COALESCE((SELECT SUM(downloads) FROM artifacts), 0) WHERE id = 1;
