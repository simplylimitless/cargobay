package database

import (
	"time"
)

// ArtifactMetadata represents a versioned artifact in the repository
type ArtifactMetadata struct {
	ID              string         `db:"id"`
	RegistryID      string         `db:"registry_id"`
	ArtifactType    string         `db:"artifact_type"`
	Namespace       string         `db:"namespace"`
	ArtifactName    string         `db:"artifact_name"`
	Version         string         `db:"version"`
	Digest          string         `db:"digest"`
	DigestAlgorithm string         `db:"digest_algorithm"`
	Size            int64          `db:"size"`
	Created         time.Time      `db:"created"`
	Updated         time.Time      `db:"updated"`
	Metadata        map[string]any `db:"metadata"`
	Tags            []string       `db:"tags"`
	Signatures      []Signature    `db:"signatures"`
}

// Signature represents a digital signature on an artifact
type Signature struct {
	Type      string    `db:"type"`
	KeyID     string    `db:"key_id"`
	Timestamp time.Time `db:"timestamp"`
	Verified  bool      `db:"verified"`
}

// UserRepository represents a user in the system
type UserRepository struct {
	UserID       string     `db:"user_id" json:"userId"`
	Username     string     `db:"username" json:"username"`
	Email        string     `db:"email" json:"email"`
	PasswordHash string     `db:"password_hash" json:"-"`
	Roles        []string   `db:"roles" json:"roles"`
	CreatedAt    time.Time  `db:"created_at" json:"createdAt"`
	LastLogin    *time.Time `db:"last_login" json:"lastLogin"`
	IsActive     bool       `db:"is_active" json:"isActive"`
}

// AccessKey represents an API access key
type AccessKey struct {
	ID         string    `db:"id"`
	UserID     string    `db:"user_id"`
	Name       string    `db:"name"`
	KeyHash    string    `db:"key_hash"`
	Permissions []string `db:"permissions"`
	CreatedAt  time.Time `db:"created_at"`
	LastUsed   *time.Time `db:"last_used"`
	ExpiresAt  *time.Time `db:"expires_at"`
	IsActive   bool      `db:"is_active"`
}

// RegistryConfig represents an upstream registry
type RegistryConfig struct {
	ID       string `db:"id"`
	Name     string `db:"name"`
	URL      string `db:"url"`
	Type     string `db:"type"`
	Proxy    bool   `db:"proxy"`
	Enabled  bool   `db:"enabled"`
	Priority int    `db:"priority"`
}

// AuditLog represents an audit log entry
type AuditLog struct {
	ID          string    `db:"id"`
	UserID      string    `db:"user_id"`
	Action      string    `db:"action"`
	ResourceType string   `db:"resource_type"`
	ResourceID  string    `db:"resource_id"`
	Details     string    `db:"details"`
	CreatedAt   time.Time `db:"created_at"`
}

// Permission defines a granular permission
type Permission struct {
	ID          string `db:"id"`
	Name        string `db:"name"`
	Description string `db:"description"`
	Resource    string `db:"resource"`
	Action      string `db:"action"`
	IsSystem    bool   `db:"is_system"`
}

// Role represents a role in the RBAC system
type Role struct {
	ID          string    `db:"id"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	IsSystem    bool      `db:"is_system"`
	CreatedAt   time.Time `db:"created_at"`
}

// RolePermission links roles to permissions
type RolePermission struct {
	RoleID      string `db:"role_id"`
	PermissionID string `db:"permission_id"`
}

// CursorPaginationOptions represents cursor-based pagination parameters
type CursorPaginationOptions struct {
	Namespace    string // Filter by namespace
	ArtifactType string // Filter by artifact type
	Limit        int    // Number of results per page (default: 50, max: 200)
	Cursor       string // Cursor from previous page (base64-encoded timestamp)
	OrderBy      string // Field to order by (default: created)
	Order        string // Order direction: "asc" or "desc" (default: desc)
}

// CursorPaginationResponse represents a paginated response with cursors
type CursorPaginationResponse[T any] struct {
	Items      []T      `json:"items"`
	NextCursor string   `json:"nextCursor,omitempty"`
	PrevCursor string   `json:"prevCursor,omitempty"`
	HasNext    bool     `json:"hasNext"`
	HasPrev    bool     `json:"hasPrev"`
	Limit      int      `json:"limit"`
}
