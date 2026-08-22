-- Migration: Add RBAC tables (roles, permissions, role_permissions, user_roles,
-- audit_log). backend/pkg/database/database.go already reads/writes these
-- tables (GetUserRoles, SaveRole, AssignRoleToUser, GenerateAuditLog, ...)
-- but they were never created. The 5 built-in roles live in-memory in
-- pkg/rbac (staticRoles/staticPermissions) and are not seeded here.

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
