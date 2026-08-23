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
	Downloads       int64          `db:"downloads"`
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
	Timezone     string     `db:"timezone" json:"timezone"`
}

// AccessKey represents an API access key
type AccessKey struct {
	ID          string     `db:"id"`
	UserID      string     `db:"user_id"`
	Name        string     `db:"name"`
	KeyHash     string     `db:"key_hash"`
	Permissions []string   `db:"permissions"`
	CreatedAt   time.Time  `db:"created_at"`
	LastUsed    *time.Time `db:"last_used"`
	ExpiresAt   *time.Time `db:"expires_at"`
	IsActive    bool       `db:"is_active"`
	// KeyType is "session" for short-lived login sessions (created by
	// auth.Login) or "personal" for user-managed PATs (created via the
	// API Tokens UI). Keeps the two apart without relying on Name.
	KeyType string `db:"key_type"`
	// Description is an optional free-text note set by the user when
	// creating a personal access token, to help tell PATs with similar
	// names apart. Always empty for session keys.
	Description string `db:"description"`
}

// RegistryConfig represents an upstream registry
type RegistryConfig struct {
	ID       string `db:"id" json:"id"`
	Name     string `db:"name" json:"name"`
	URL      string `db:"url" json:"url"`
	Type     string `db:"type" json:"type"`
	Proxy    bool   `db:"proxy" json:"proxy"`
	Enabled  bool   `db:"enabled" json:"enabled"`
	Priority int    `db:"priority" json:"priority"`
	Private  bool   `db:"private" json:"private"`
	// Host binds this registry to a hostname for virtual-host routing
	// (e.g. "private1.cargobay.example.com"). Empty means unbound: the
	// registry is only reachable as the default public proxy for its
	// type at the existing /docker, /npm, ... path prefixes.
	Host string `db:"host" json:"host"`
	// Credentials cargobay presents when it pulls through Proxy+URL from an
	// upstream registry that itself requires authentication -- e.g. another
	// private cargobay instance, or any private Docker/Maven/npm/PyPI/NuGet/
	// Helm registry. UpstreamAuthType is one of "none" (default), "basic"
	// (HTTP Basic, the convention for Maven/PyPI/Docker Hub-style private
	// registries), or "bearer" (a static bearer token, the convention for
	// npm/GitHub Packages-style registries). UpstreamSecret is the password
	// for basic auth or the token for bearer auth; it is never serialized
	// to JSON so it can't leak back out over the API.
	UpstreamAuthType string `db:"upstream_auth_type" json:"upstreamAuthType"`
	UpstreamUsername string `db:"upstream_username" json:"upstreamUsername"`
	UpstreamSecret   string `db:"upstream_secret" json:"-"`
	// HasUpstreamSecret is a derived, read-only flag set by the API layer so
	// the frontend can show "credential configured" without ever receiving
	// the secret itself.
	HasUpstreamSecret bool `db:"-" json:"hasUpstreamSecret"`
}

// RegistryAccess grants a specific user read and/or publish rights on a
// private registry. Public (non-private) registries never consult this
// table — they're open to everyone, including anonymous callers.
type RegistryAccess struct {
	RegistryID string    `db:"registry_id" json:"registryId"`
	UserID     string    `db:"user_id" json:"userId"`
	CanRead    bool      `db:"can_read" json:"canRead"`
	CanPublish bool      `db:"can_publish" json:"canPublish"`
	GrantedAt  time.Time `db:"granted_at" json:"grantedAt"`
}

// AuditLog represents an audit log entry
type AuditLog struct {
	ID           string    `db:"id"`
	UserID       string    `db:"user_id"`
	Action       string    `db:"action"`
	ResourceType string    `db:"resource_type"`
	ResourceID   string    `db:"resource_id"`
	Details      string    `db:"details"`
	CreatedAt    time.Time `db:"created_at"`
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
	RoleID       string `db:"role_id"`
	PermissionID string `db:"permission_id"`
}

// VulnDBSettings tunes how the backend refreshes Trivy's vulnerability DB.
// Singleton row (id always 1).
type VulnDBSettings struct {
	AutoUpdateEnabled   bool       `db:"auto_update_enabled" json:"autoUpdateEnabled"`
	UpdateIntervalHours int        `db:"update_interval_hours" json:"updateIntervalHours"`
	LastCheckedAt       *time.Time `db:"last_checked_at" json:"lastCheckedAt"`
	LastUpdatedAt       *time.Time `db:"last_updated_at" json:"lastUpdatedAt"`
	LastError           string     `db:"last_error" json:"lastError"`
}

// SearchIndexSettings tunes how the backend rebuilds the Postgres full-text
// search index (the search_vector column on artifacts). Singleton row (id
// always 1).
type SearchIndexSettings struct {
	AutoReindexEnabled   bool       `db:"auto_reindex_enabled" json:"autoReindexEnabled"`
	ReindexIntervalHours int        `db:"reindex_interval_hours" json:"reindexIntervalHours"`
	LastCheckedAt        *time.Time `db:"last_checked_at" json:"lastCheckedAt"`
	LastReindexedAt      *time.Time `db:"last_reindexed_at" json:"lastReindexedAt"`
	LastArtifactCount    int        `db:"last_artifact_count" json:"lastArtifactCount"`
	LastError            string     `db:"last_error" json:"lastError"`
}

// VulnScanSettings tunes how the backend rescans cached Docker/OCI artifacts
// for vulnerabilities via Trivy. Distinct from VulnDBSettings (which governs
// refreshing Trivy's own CVE database). Singleton row (id always 1).
type VulnScanSettings struct {
	AutoScanEnabled   bool       `db:"auto_scan_enabled" json:"autoScanEnabled"`
	ScanIntervalHours int        `db:"scan_interval_hours" json:"scanIntervalHours"`
	LastCheckedAt     *time.Time `db:"last_checked_at" json:"lastCheckedAt"`
	LastScanAt        *time.Time `db:"last_scan_at" json:"lastScanAt"`
	LastError         string     `db:"last_error" json:"lastError"`
}

// BackupSettings tunes how the backend performs scheduled full database
// backups (see backend/pkg/backup). Singleton row (id always 1).
// StorageType/StorageConfig mirror config.StorageConfig's shape (type +
// generic key/value map); an empty StorageType means backups reuse the main
// artifact storage adapter instead of a dedicated one.
type BackupSettings struct {
	AutoBackupEnabled   bool              `db:"auto_backup_enabled" json:"autoBackupEnabled"`
	BackupIntervalHours int               `db:"backup_interval_hours" json:"backupIntervalHours"`
	LastCheckedAt       *time.Time        `db:"last_checked_at" json:"lastCheckedAt"`
	LastBackupAt        *time.Time        `db:"last_backup_at" json:"lastBackupAt"`
	LastBackupPath      string            `db:"last_backup_path" json:"lastBackupPath"`
	LastError           string            `db:"last_error" json:"lastError"`
	StorageType         string            `db:"storage_type" json:"storageType"`
	StorageConfig       map[string]string `db:"storage_config" json:"storageConfig"`
}

// CursorPaginationOptions represents cursor-based pagination parameters
type CursorPaginationOptions struct {
	Namespace    string   // Filter by namespace
	ArtifactType string   // Filter by artifact type
	Limit        int      // Number of results per page (default: 50, max: 200)
	Cursor       string   // Cursor from previous page (base64-encoded timestamp)
	OrderBy      string   // Field to order by (default: created)
	Order        string   // Order direction: "asc" or "desc" (default: desc)
	RegistryIDs  []string // Restrict results to these registry IDs. Nil means
	// "no restriction" (used by callers like ListUsersCursor that don't scope
	// by registry at all); a non-nil empty slice means "no readable
	// registries" and yields zero rows, not an unfiltered listing.
}

// CursorPaginationResponse represents a paginated response with cursors
type CursorPaginationResponse[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
	PrevCursor string `json:"prevCursor,omitempty"`
	HasNext    bool   `json:"hasNext"`
	HasPrev    bool   `json:"hasPrev"`
	Limit      int    `json:"limit"`
}
