-- Adds a per-user display timezone preference. Empty string means "no
-- preference set -- use the browser's local timezone" so existing users see
-- unchanged behavior. Storage of timestamps themselves is unaffected: all
-- timestamp columns are already TIMESTAMPTZ (UTC internally); this column
-- only controls client-side rendering.
ALTER TABLE users ADD COLUMN IF NOT EXISTS timezone TEXT NOT NULL DEFAULT '';
