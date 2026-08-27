-- Ordered list of member registry IDs for aggregating "virtual" registry
-- types (e.g. "maven-virtual"), which resolve artifacts by trying each
-- member in order rather than a single upstream URL.
ALTER TABLE registries ADD COLUMN IF NOT EXISTS members TEXT[];
