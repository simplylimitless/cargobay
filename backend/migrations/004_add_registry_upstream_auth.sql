-- Credentials cargobay uses when it, as a client, pulls through an upstream
-- registry that itself requires authentication (e.g. another private
-- cargobay instance, a private Docker/Maven/npm/PyPI/NuGet/Helm registry).
-- This is separate from registry_access, which controls who may read/publish
-- *through* cargobay -- these columns control what cargobay presents to the
-- upstream server it is proxying.
ALTER TABLE registries ADD COLUMN IF NOT EXISTS upstream_auth_type TEXT NOT NULL DEFAULT 'none';
ALTER TABLE registries ADD COLUMN IF NOT EXISTS upstream_username  TEXT;
ALTER TABLE registries ADD COLUMN IF NOT EXISTS upstream_secret    TEXT;
