package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Database implements PostgreSQL data access
type Database struct {
	dsn  string
	conn *pgx.Conn
}

// New creates a new Database instance
func New(dsn string) *Database {
	return &Database{dsn: dsn}
}

// Connect establishes a connection to PostgreSQL
func (db *Database) Connect() error {
	conn, err := pgx.Connect(context.Background(), db.dsn)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	db.conn = conn
	return nil
}

// Disconnect closes the database connection
func (db *Database) Disconnect() {
	if db.conn != nil {
		db.conn.Close(context.Background())
	}
}

// GetArtifact retrieves an artifact by its identifying parameters
func (db *Database) GetArtifact(registryID, namespace, artifactName, version string) (*ArtifactMetadata, error) {
	row := db.conn.QueryRow(
		context.Background(),
		`SELECT id, registry_id, artifact_type, namespace, artifact_name, version,
			 digest, digest_algorithm, size, created, updated, metadata, tags, signatures
		 FROM artifacts
		 WHERE registry_id = $1 AND namespace = $2 AND artifact_name = $3 AND version = $4`,
		registryID, namespace, artifactName, version,
	)

	var artifact ArtifactMetadata
	err := row.Scan(
		&artifact.ID, &artifact.RegistryID, &artifact.ArtifactType,
		&artifact.Namespace, &artifact.ArtifactName, &artifact.Version,
		&artifact.Digest, &artifact.DigestAlgorithm, &artifact.Size,
		&artifact.Created, &artifact.Updated, &artifact.Metadata, &artifact.Tags, &artifact.Signatures,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get artifact: %w", err)
	}
	return &artifact, nil
}

// GetArtifactByDigest retrieves an artifact by its content digest
func (db *Database) GetArtifactByDigest(registryID, digest string) (*ArtifactMetadata, error) {
	row := db.conn.QueryRow(
		context.Background(),
		`SELECT id, registry_id, artifact_type, namespace, artifact_name, version,
			 digest, digest_algorithm, size, created, updated, metadata, tags, signatures
		 FROM artifacts
		 WHERE registry_id = $1 AND digest = $2
		 ORDER BY created DESC`,
		registryID, digest,
	)

	var artifact ArtifactMetadata
	err := row.Scan(
		&artifact.ID, &artifact.RegistryID, &artifact.ArtifactType,
		&artifact.Namespace, &artifact.ArtifactName, &artifact.Version,
		&artifact.Digest, &artifact.DigestAlgorithm, &artifact.Size,
		&artifact.Created, &artifact.Updated, &artifact.Metadata, &artifact.Tags, &artifact.Signatures,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get artifact by digest: %w", err)
	}
	return &artifact, nil
}

// ListOptions for artifact listing
type ListOptions struct {
	Namespace    string
	ArtifactType string
	Limit        int
	Offset       int
	OrderBy      string
	Order        string
}

// ListArtifacts lists artifacts with optional filtering and pagination
func (db *Database) ListArtifacts(registryID string, opts ListOptions) ([]ArtifactMetadata, error) {
	query := `SELECT id, registry_id, artifact_type, namespace, artifact_name, version,
				 digest, digest_algorithm, size, created, updated, metadata, tags, signatures
			  FROM artifacts WHERE registry_id = $1`
	params := []any{registryID}

	if opts.Namespace != "" {
		query += ` AND namespace = $` + fmt.Sprintf("%d", len(params)+1)
		params = append(params, opts.Namespace)
	}

	if opts.ArtifactType != "" {
		query += ` AND artifact_type = $` + fmt.Sprintf("%d", len(params)+1)
		params = append(params, opts.ArtifactType)
	}

	order := opts.OrderBy
	if order == "" {
		order = "created"
	}
	dir := opts.Order
	if dir == "" {
		dir = "desc"
	}
	query += fmt.Sprintf(` ORDER BY %s %s`, order, dir)

	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	query += ` LIMIT $` + fmt.Sprintf("%d", len(params)+1)
	params = append(params, opts.Limit)

	if opts.Offset > 0 {
		query += ` OFFSET $` + fmt.Sprintf("%d", len(params)+1)
		params = append(params, opts.Offset)
	}

	rows, err := db.conn.Query(context.Background(), query, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to list artifacts: %w", err)
	}
	defer rows.Close()

	var artifacts []ArtifactMetadata
	for rows.Next() {
		var a ArtifactMetadata
		err := rows.Scan(&a.ID, &a.RegistryID, &a.ArtifactType, &a.Namespace,
			&a.ArtifactName, &a.Version, &a.Digest, &a.DigestAlgorithm, &a.Size,
			&a.Created, &a.Updated, &a.Metadata, &a.Tags, &a.Signatures)
		if err != nil {
			return nil, fmt.Errorf("failed to scan artifact: %w", err)
		}
		artifacts = append(artifacts, a)
	}

	return artifacts, rows.Err()
}

// SearchOptions for artifact search
type SearchOptions struct {
	RegistryID   string
	ArtifactType string
	Limit        int
	Offset       int
}

// SearchArtifacts searches artifacts by query string
func (db *Database) SearchArtifacts(query string, opts SearchOptions) ([]ArtifactMetadata, error) {
	searchQuery := `SELECT id, registry_id, artifact_type, namespace, artifact_name, version,
					 digest, digest_algorithm, size, created, updated, metadata, tags, signatures
				  FROM artifacts
				  WHERE to_tsvector('english', artifact_name || ' ' || namespace) @@ to_tsquery('english', $1)`

	params := []any{query}

	if opts.RegistryID != "" {
		searchQuery += ` AND registry_id = $` + fmt.Sprintf("%d", len(params)+1)
		params = append(params, opts.RegistryID)
	}

	if opts.ArtifactType != "" {
		searchQuery += ` AND artifact_type = $` + fmt.Sprintf("%d", len(params)+1)
		params = append(params, opts.ArtifactType)
	}

	searchQuery += ` ORDER BY ts_rank(to_tsvector('english', artifact_name || ' ' || namespace), to_tsquery('english', $1)) DESC`

	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	searchQuery += ` LIMIT $` + fmt.Sprintf("%d", len(params)+1)
	params = append(params, opts.Limit)

	if opts.Offset > 0 {
		searchQuery += ` OFFSET $` + fmt.Sprintf("%d", len(params)+1)
		params = append(params, opts.Offset)
	}

	rows, err := db.conn.Query(context.Background(), searchQuery, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to search artifacts: %w", err)
	}
	defer rows.Close()

	var artifacts []ArtifactMetadata
	for rows.Next() {
		var a ArtifactMetadata
		err := rows.Scan(&a.ID, &a.RegistryID, &a.ArtifactType, &a.Namespace,
			&a.ArtifactName, &a.Version, &a.Digest, &a.DigestAlgorithm, &a.Size,
			&a.Created, &a.Updated, &a.Metadata, &a.Tags, &a.Signatures)
		if err != nil {
			return nil, fmt.Errorf("failed to scan artifact: %w", err)
		}
		artifacts = append(artifacts, a)
	}

	return artifacts, rows.Err()
}

// SaveArtifact inserts or updates artifact metadata
func (db *Database) SaveArtifact(metadata *ArtifactMetadata) error {
	if metadata.ID == "" {
		metadata.ID = generateUUID()
	}
	metadata.Updated = time.Now()

	_, err := db.conn.Exec(
		context.Background(),
		`INSERT INTO artifacts (
			 id, registry_id, artifact_type, namespace, artifact_name, version,
			 digest, digest_algorithm, size, created, updated, metadata, tags, signatures
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 ON CONFLICT (registry_id, namespace, artifact_name, version)
		 DO UPDATE SET
		   digest = EXCLUDED.digest,
		   digest_algorithm = EXCLUDED.digest_algorithm,
		   size = EXCLUDED.size,
		   updated = EXCLUDED.updated,
		   metadata = EXCLUDED.metadata,
		   tags = EXCLUDED.tags,
		   signatures = EXCLUDED.signatures`,
		metadata.ID, metadata.RegistryID, metadata.ArtifactType, metadata.Namespace,
		metadata.ArtifactName, metadata.Version, metadata.Digest, metadata.DigestAlgorithm,
		metadata.Size, metadata.Created, metadata.Updated, metadata.Metadata, metadata.Tags, metadata.Signatures,
	)
	if err != nil {
		return fmt.Errorf("failed to save artifact: %w", err)
	}
	return nil
}

// DeleteArtifact removes an artifact
func (db *Database) DeleteArtifact(registryID, namespace, artifactName, version string) (bool, error) {
	result, err := db.conn.Exec(
		context.Background(),
		`DELETE FROM artifacts WHERE registry_id = $1 AND namespace = $2 AND artifact_name = $3 AND version = $4`,
		registryID, namespace, artifactName, version,
	)
	if err != nil {
		return false, fmt.Errorf("failed to delete artifact: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

// UpdateArtifactTags updates the tags for an artifact
func (db *Database) UpdateArtifactTags(registryID, namespace, artifactName, version string, tags []string) error {
	_, err := db.conn.Exec(
		context.Background(),
		`UPDATE artifacts SET tags = $1, updated = $2
		 WHERE registry_id = $3 AND namespace = $4 AND artifact_name = $5 AND version = $6`,
		tags, time.Now(), registryID, namespace, artifactName, version,
	)
	if err != nil {
		return fmt.Errorf("failed to update artifact tags: %w", err)
	}
	return nil
}

// GetUserByID retrieves a user by ID
func (db *Database) GetUserByID(userID string) (*UserRepository, error) {
	row := db.conn.QueryRow(
		context.Background(),
		`SELECT user_id, username, email, password_hash, roles, created_at, last_login, is_active
		 FROM users WHERE user_id = $1`,
		userID,
	)

	var user UserRepository
	err := row.Scan(&user.UserID, &user.Username, &user.Email, &user.PasswordHash,
		&user.Roles, &user.CreatedAt, &user.LastLogin, &user.IsActive)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &user, nil
}

// GetUserByUsername retrieves a user by username
func (db *Database) GetUserByUsername(username string) (*UserRepository, error) {
	row := db.conn.QueryRow(
		context.Background(),
		`SELECT user_id, username, email, password_hash, roles, created_at, last_login, is_active
		 FROM users WHERE username = $1`,
		username,
	)

	var user UserRepository
	err := row.Scan(&user.UserID, &user.Username, &user.Email, &user.PasswordHash,
		&user.Roles, &user.CreatedAt, &user.LastLogin, &user.IsActive)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &user, nil
}

// CreateUser creates a new user
func (db *Database) CreateUser(username, email, passwordHash string, roles []string) (*UserRepository, error) {
	userID := generateUUID()
	createdAt := time.Now()

	row := db.conn.QueryRow(
		context.Background(),
		`INSERT INTO users (user_id, username, email, password_hash, roles, created_at, last_login, is_active)
		 VALUES ($1, $2, $3, $4, $5, $6, NULL, TRUE)
		 RETURNING user_id, username, email, password_hash, roles, created_at, last_login, is_active`,
		userID, username, email, passwordHash, roles, createdAt,
	)

	var user UserRepository
	err := row.Scan(&user.UserID, &user.Username, &user.Email, &user.PasswordHash,
		&user.Roles, &user.CreatedAt, &user.LastLogin, &user.IsActive)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	return &user, nil
}

// UpdateUserLastLogin updates the last login timestamp
func (db *Database) UpdateUserLastLogin(userID string) error {
	_, err := db.conn.Exec(
		context.Background(),
		`UPDATE users SET last_login = $1 WHERE user_id = $2`,
		time.Now(), userID,
	)
	if err != nil {
		return fmt.Errorf("failed to update user last login: %w", err)
	}
	return nil
}

// CreateAccessKey creates a new access key
func (db *Database) CreateAccessKey(userID, name string, permissions []string, expiresAt *time.Time) (*AccessKey, error) {
	keyID := generateUUID()
	keyHash := fmt.Sprintf("key_%s", generateUUID())
	createdAt := time.Now()

	row := db.conn.QueryRow(
		context.Background(),
		`INSERT INTO access_keys (id, user_id, name, key_hash, permissions, created_at, expires_at, is_active)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, TRUE)
		 RETURNING id, user_id, name, key_hash, permissions, created_at, last_used, expires_at, is_active`,
		keyID, userID, name, keyHash, permissions, createdAt, expiresAt,
	)

	var key AccessKey
	err := row.Scan(&key.ID, &key.UserID, &key.Name, &key.KeyHash,
		&key.Permissions, &key.CreatedAt, &key.LastUsed, &key.ExpiresAt, &key.IsActive)
	if err != nil {
		return nil, fmt.Errorf("failed to create access key: %w", err)
	}
	return &key, nil
}

// ValidateAccessKey validates an access key
func (db *Database) ValidateAccessKey(keyHash string) (*AccessKey, error) {
	now := time.Now()
	row := db.conn.QueryRow(
		context.Background(),
		`UPDATE access_keys SET last_used = $1
		 WHERE key_hash = $2 AND is_active = TRUE AND (expires_at IS NULL OR expires_at > $1)
		 RETURNING id, user_id, name, key_hash, permissions, created_at, last_used, expires_at, is_active`,
		now, keyHash,
	)

	var key AccessKey
	err := row.Scan(&key.ID, &key.UserID, &key.Name, &key.KeyHash,
		&key.Permissions, &key.CreatedAt, &key.LastUsed, &key.ExpiresAt, &key.IsActive)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to validate access key: %w", err)
	}
	return &key, nil
}

// InvalidateAccessKey revokes an access key
func (db *Database) InvalidateAccessKey(id string) error {
	_, err := db.conn.Exec(
		context.Background(),
		`UPDATE access_keys SET is_active = FALSE WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to invalidate access key: %w", err)
	}
	return nil
}

// ListUserAccessKeys lists all access keys for a user
func (db *Database) ListUserAccessKeys(userID string) ([]AccessKey, error) {
	rows, err := db.conn.Query(
		context.Background(),
		`SELECT id, user_id, name, key_hash, permissions, created_at, last_used, expires_at, is_active
		 FROM access_keys WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list access keys: %w", err)
	}
	defer rows.Close()

	var keys []AccessKey
	for rows.Next() {
		var k AccessKey
		err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyHash,
			&k.Permissions, &k.CreatedAt, &k.LastUsed, &k.ExpiresAt, &k.IsActive)
		if err != nil {
			return nil, fmt.Errorf("failed to scan access key: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// ListRegistries lists all enabled registries
func (db *Database) ListRegistries() ([]RegistryConfig, error) {
	rows, err := db.conn.Query(
		context.Background(),
		`SELECT id, name, url, type, enabled, priority FROM registries WHERE enabled = TRUE ORDER BY priority ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list registries: %w", err)
	}
	defer rows.Close()

	var registries []RegistryConfig
	for rows.Next() {
		var r RegistryConfig
		err := rows.Scan(&r.ID, &r.Name, &r.URL, &r.Type, &r.Enabled, &r.Priority)
		if err != nil {
			return nil, fmt.Errorf("failed to scan registry: %w", err)
		}
		registries = append(registries, r)
	}
	return registries, rows.Err()
}

// GetRegistry retrieves a registry by ID
func (db *Database) GetRegistry(id string) (*RegistryConfig, error) {
	row := db.conn.QueryRow(
		context.Background(),
		`SELECT id, name, url, type, enabled, priority FROM registries WHERE id = $1`,
		id,
	)

	var r RegistryConfig
	err := row.Scan(&r.ID, &r.Name, &r.URL, &r.Type, &r.Enabled, &r.Priority)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get registry: %w", err)
	}
	return &r, nil
}

// SaveRegistry saves registry configuration
func (db *Database) SaveRegistry(config *RegistryConfig) error {
	_, err := db.conn.Exec(
		context.Background(),
		`INSERT INTO registries (id, name, url, type, enabled, priority)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (id)
		 DO UPDATE SET
		   name = EXCLUDED.name,
		   url = EXCLUDED.url,
		   type = EXCLUDED.type,
		   enabled = EXCLUDED.enabled,
		   priority = EXCLUDED.priority`,
		config.ID, config.Name, config.URL, config.Type, config.Enabled, config.Priority,
	)
	if err != nil {
		return fmt.Errorf("failed to save registry: %w", err)
	}
	return nil
}

// DeleteRegistry removes a registry
func (db *Database) DeleteRegistry(id string) error {
	_, err := db.conn.Exec(context.Background(), `DELETE FROM registries WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete registry: %w", err)
	}
	return nil
}

// generateUUID generates a new UUID v4
func generateUUID() string {
	// In production, use github.com/google/uuid
	return fmt.Sprintf("uuid-%d", time.Now().UnixNano())
}
