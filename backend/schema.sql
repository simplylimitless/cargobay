-- Minimal schema for cargobay's PostgreSQL metadata store.
-- Loaded automatically by the official postgres image on first boot via
-- docker-entrypoint-initdb.d (see docker-compose.yml). Column shapes match
-- the queries in backend/pkg/database/database.go and the structs in
-- backend/pkg/database/models.go.
--
-- NOTE: this is a one-shot bootstrap script, not a migration system. There
-- is no versioning or upgrade path yet — see PROJECT.md / follow-up notes
-- for the planned auto-detect-and-upgrade-on-start migration tooling.

CREATE TABLE IF NOT EXISTS artifacts (
    id               TEXT PRIMARY KEY,
    registry_id      TEXT NOT NULL,
    artifact_type    TEXT NOT NULL,
    namespace        TEXT NOT NULL,
    artifact_name    TEXT NOT NULL,
    version          TEXT NOT NULL,
    digest           TEXT NOT NULL DEFAULT '',
    digest_algorithm TEXT NOT NULL DEFAULT '',
    size             BIGINT NOT NULL DEFAULT 0,
    created          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated          TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata         JSONB NOT NULL DEFAULT '{}',
    tags             TEXT[] NOT NULL DEFAULT '{}',
    signatures       JSONB NOT NULL DEFAULT '[]',
    UNIQUE (registry_id, namespace, artifact_name, version)
);

CREATE INDEX IF NOT EXISTS idx_artifacts_search
    ON artifacts USING GIN (to_tsvector('english', artifact_name || ' ' || namespace));

CREATE TABLE IF NOT EXISTS users (
    user_id       TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    roles         TEXT[] NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login    TIMESTAMPTZ,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS access_keys (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    key_hash    TEXT NOT NULL UNIQUE,
    permissions TEXT[] NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used   TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS registries (
    id       TEXT PRIMARY KEY,
    name     TEXT NOT NULL,
    url      TEXT NOT NULL,
    type     TEXT NOT NULL,
    enabled  BOOLEAN NOT NULL DEFAULT TRUE,
    priority INTEGER NOT NULL DEFAULT 100,
    private  BOOLEAN NOT NULL DEFAULT FALSE,
    proxy    BOOLEAN NOT NULL DEFAULT FALSE,
    host     TEXT,
    -- Credentials cargobay presents when pulling through an upstream
    -- registry that itself requires authentication (e.g. another private
    -- cargobay instance, or any private Docker/Maven/npm/PyPI/NuGet/Helm
    -- registry). upstream_auth_type is one of 'none' | 'basic' | 'bearer'.
    upstream_auth_type TEXT NOT NULL DEFAULT 'none',
    upstream_username  TEXT,
    upstream_secret    TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_registries_host ON registries (host) WHERE host IS NOT NULL;

-- Per-user read/publish grants for private registries. Public registries
-- (private = FALSE) are readable by everyone incl. anonymous and never
-- consult this table; private registries are only usable by an explicitly
-- granted user (or an admin, via the registry:write permission bypass).
CREATE TABLE IF NOT EXISTS registry_access (
    registry_id TEXT NOT NULL REFERENCES registries(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    can_read    BOOLEAN NOT NULL DEFAULT TRUE,
    can_publish BOOLEAN NOT NULL DEFAULT FALSE,
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (registry_id, user_id)
);

-- RBAC tables. The 5 built-in roles (admin/developer/viewer/publisher/auditor)
-- and their permissions live in-memory in pkg/rbac (see staticRoles /
-- staticPermissions) and are not seeded here — `roles`/`permissions` only
-- need rows for custom roles created via the API. `user_roles` is the source
-- of truth for permission checks (RBAC.HasPermission).
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS roles (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    is_system   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS permissions (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    resource    TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL DEFAULT '',
    is_system   BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id       TEXT NOT NULL,
    permission_id TEXT NOT NULL,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    role_id TEXT NOT NULL,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS audit_log (
    id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id       TEXT NOT NULL,
    action        TEXT NOT NULL,
    resource_type TEXT NOT NULL DEFAULT '',
    resource_id   TEXT NOT NULL DEFAULT '',
    details       TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log (created_at DESC);

-- Singleton row holding tunable settings for how the backend refreshes
-- Trivy's vulnerability DB (see migrations/006_add_vulnerability_db_settings.sql).
CREATE TABLE IF NOT EXISTS vulnerability_db_settings (
    id                    INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    auto_update_enabled   BOOLEAN NOT NULL DEFAULT TRUE,
    update_interval_hours INTEGER NOT NULL DEFAULT 24,
    last_checked_at       TIMESTAMPTZ,
    last_updated_at       TIMESTAMPTZ,
    last_error            TEXT NOT NULL DEFAULT ''
);
INSERT INTO vulnerability_db_settings (id) VALUES (1) ON CONFLICT DO NOTHING;
