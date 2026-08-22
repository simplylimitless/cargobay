-- Migration: Add tsvector column and GIN index for scalable full-text search
-- This migration improves search performance for millions of artifacts by:
-- 1. Adding a pre-computed tsvector column
-- 2. Creating a GIN index on the search vector
-- 3. Updating SaveArtifact to maintain the search vector

-- Add tsvector column if it doesn't exist
ALTER TABLE artifacts ADD COLUMN IF NOT EXISTS search_vector tsvector;

-- Populate search_vector for existing records
UPDATE artifacts SET search_vector = to_tsvector('english', artifact_name || ' ' || namespace);

-- Create GIN index for efficient full-text search
CREATE INDEX IF NOT EXISTS idx_artifacts_search_vector ON artifacts USING GIN (search_vector);

-- Create additional indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_artifacts_registry_type ON artifacts (registry_id, artifact_type);
CREATE INDEX IF NOT EXISTS idx_artifacts_namespace ON artifacts (namespace);

-- Create a view for easy search with ranking
CREATE OR REPLACE VIEW artifact_search_ranked AS
SELECT
    id,
    registry_id,
    artifact_type,
    namespace,
    artifact_name,
    version,
    digest,
    digest_algorithm,
    size,
    created,
    updated,
    metadata,
    tags,
    signatures,
    search_vector,
    ts_rank(search_vector, query) AS relevance
FROM artifacts,
     to_tsquery('english', COALESCE(NULLIF(current_setting('app.search_query', true), ''), 'dummy')) AS query
WHERE search_vector @@ query OR query = to_tsquery('dummy');

-- Function to update search_vector on insert/update
CREATE OR REPLACE FUNCTION update_search_vector()
RETURNS TRIGGER AS $$
BEGIN
    NEW.search_vector = to_tsvector('english', NEW.artifact_name || ' ' || NEW.namespace);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Drop trigger if exists (for idempotent migration)
DROP TRIGGER IF EXISTS trigger_update_search_vector ON artifacts;

-- Create trigger to maintain search_vector automatically
CREATE TRIGGER trigger_update_search_vector
BEFORE INSERT OR UPDATE ON artifacts
FOR EACH ROW
EXECUTE FUNCTION update_search_vector();
