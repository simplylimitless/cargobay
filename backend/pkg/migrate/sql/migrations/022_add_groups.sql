CREATE TABLE IF NOT EXISTS groups (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS group_members (
    group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id  TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);

CREATE TABLE IF NOT EXISTS registry_group_access (
    registry_id TEXT NOT NULL REFERENCES registries(id) ON DELETE CASCADE,
    group_id    TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    can_read    BOOLEAN NOT NULL DEFAULT TRUE,
    can_publish BOOLEAN NOT NULL DEFAULT FALSE,
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (registry_id, group_id)
);
