-- Migration: Private registries + per-user registry access grants.
-- - `private` marks a registry as restricted to explicitly-assigned users
--   (anonymous pulls and unassigned users are blocked).
-- - `proxy` was already read/written by Go code (database.RegistryConfig)
--   but never actually persisted; ListRegistries/GetRegistry/SaveRegistry
--   only touched id/name/url/type/enabled/priority. Adding the column here
--   makes per-registry upstream proxying actually work for private
--   registries that still want pull-through caching.
-- - `registry_access` is the per-user read/publish grant table.

ALTER TABLE registries ADD COLUMN IF NOT EXISTS private BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE registries ADD COLUMN IF NOT EXISTS proxy   BOOLEAN NOT NULL DEFAULT FALSE;

-- `host` binds a registry to a hostname (e.g. "private1.cargobay.example.com").
-- Downstream docker/npm/maven/etc clients point at that hostname directly —
-- no path prefix, no repo/package renaming — and cargobay picks the target
-- registry by reading the incoming Host header. Registries with no bound
-- host (NULL) are unaffected: they keep serving as the default public proxy
-- under the existing /docker, /npm, ... path prefixes. NULL (not '') so
-- multiple registries can leave it unset without violating the uniqueness
-- constraint (Postgres treats each NULL as distinct).
ALTER TABLE registries ADD COLUMN IF NOT EXISTS host TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_registries_host ON registries (host) WHERE host IS NOT NULL;

CREATE TABLE IF NOT EXISTS registry_access (
    registry_id TEXT NOT NULL REFERENCES registries(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    can_read    BOOLEAN NOT NULL DEFAULT TRUE,
    can_publish BOOLEAN NOT NULL DEFAULT FALSE,
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (registry_id, user_id)
);
