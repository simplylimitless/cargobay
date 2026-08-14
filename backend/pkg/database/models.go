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
	Type       string `db:"type"`
	KeyID      string `db:"key_id"`
	Signature  string `db:"signature"`
	Timestamp  time.Time `db:"timestamp"`
	Verified   bool   `db:"verified"`
}

// UserRepository represents a user in the system
type UserRepository struct {
	UserID     string    `db:"user_id"`
	Username   string    `db:"username"`
	Email      string    `db:"email"`
	PasswordHash string  `db:"password_hash"`
	Roles      []string  `db:"roles"`
	CreatedAt  time.Time `db:"created_at"`
	LastLogin  *time.Time `db:"last_login"`
	IsActive   bool      `db:"is_active"`
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

