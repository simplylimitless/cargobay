-- Optional free-text note on a personal access token, so users with several
-- PATs can tell them apart beyond the short `name` field (e.g. "k3s prod
-- imagePullSecret" vs. what it's actually for). Not used for session keys.
ALTER TABLE access_keys ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
