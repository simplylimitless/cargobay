-- Singleton row accumulating instance-wide stats. Currently tracks bytes
-- served from local storage on a cache hit (i.e. bytes that did not need to
-- be re-fetched from an upstream registry) — backs the Stats page's
-- "bandwidth saved" figure.
CREATE TABLE IF NOT EXISTS stats (
    id                     INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    bandwidth_saved_bytes  BIGINT NOT NULL DEFAULT 0
);
INSERT INTO stats (id) VALUES (1) ON CONFLICT DO NOTHING;
