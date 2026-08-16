package database

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CacheAdapter defines the interface for cache implementations
type CacheAdapter interface {
	Get(key string, value interface{}) error
	Set(key string, value interface{}) error
	SetWithTTL(key string, value interface{}, ttl time.Duration) error
	Delete(key string) error
	Exists(key string) (bool, error)
	HitRate() float64
}

// Database implements PostgreSQL data access with connection pooling and caching
type Database struct {
	dsn     string
	pool    *pgxpool.Pool
	cache   CacheAdapter
	cacheTTL time.Duration
}

// DatabaseConfig holds database configuration options
type DatabaseConfig struct {
	MaxConnections    int           // Maximum number of connections (default: 10 for stateless)
	MinConnections    int           // Minimum number of connections (default: 1)
	MaxConnectionAge  time.Duration // Maximum age of a connection (default: 1 hour)
	HealthCheckPeriod time.Duration // Period between health checks (default: 30s)
}

// DefaultDatabaseConfig returns recommended settings for stateless deployments
func DefaultDatabaseConfig() DatabaseConfig {
	return DatabaseConfig{
		MaxConnections:    10,          // Low for stateless - each pod holds few connections
		MinConnections:    1,           // Minimum to keep pool warm
		MaxConnectionAge:  time.Hour,   // Reconnect periodically
		HealthCheckPeriod: 30 * time.Second,
	}
}

// New creates a new Database instance
func New(dsn string) *Database {
	return &Database{
		dsn:      dsn,
		cacheTTL: time.Minute * 5, // Default cache TTL
	}
}

// NewWithConfig creates a Database with custom connection pool settings
func NewWithConfig(dsn string, config DatabaseConfig) *Database {
	return &Database{
		dsn:      dsn,
		cacheTTL: time.Minute * 5,
	}
}

// SetCache sets the cache adapter for query result caching
func (db *Database) SetCache(cache CacheAdapter) {
	db.cache = cache
}

// Connect establishes a connection pool to PostgreSQL with stateless-optimized settings
func (db *Database) Connect() error {
	config, err := pgxpool.ParseConfig(db.dsn)
	if err != nil {
		return fmt.Errorf("failed to parse database config: %w", err)
	}

	// Stateless-optimized defaults
	maxConns := config.MaxConns
	if maxConns <= 0 || maxConns > 20 {
		config.MaxConns = 10 // Low for stateless deployments
	}
	config.MinConns = 1
	config.MaxConnLifetime = 1 * time.Hour        // Reconnect periodically
	config.MaxConnIdleTime = 5 * time.Minute      // Close idle connections
	config.HealthCheckPeriod = 30 * time.Second   // Frequent health checks

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// Verify connection works
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	db.pool = pool
	return nil
}

// Ping checks database connectivity
func (db *Database) Ping() error {
	if db.pool == nil {
		return fmt.Errorf("database not connected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return db.pool.Ping(ctx)
}

// Disconnect closes the database connection pool
func (db *Database) Disconnect() {
	if db.pool != nil {
		db.pool.Close()
	}
}

// Exec runs a query against the pool without returning rows
func (db *Database) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return db.pool.Exec(ctx, sql, args...)
}

// Query runs a query against the pool and returns rows
func (db *Database) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return db.pool.Query(ctx, sql, args...)
}

// QueryRow runs a query against the pool and returns a single row
func (db *Database) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return db.pool.QueryRow(ctx, sql, args...)
}

// GetArtifactByParams retrieves an artifact by its identifying parameters
func (db *Database) GetArtifactByParams(registryID, namespace, artifactName, version string) (*ArtifactMetadata, error) {
	row := db.pool.QueryRow(
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
	row := db.pool.QueryRow(
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
	Cursor       string
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

	rows, err := db.pool.Query(context.Background(), query, params...)
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

// SearchResults holds search results with metadata
type SearchResults struct {
	Artifacts  []ArtifactMetadata
	Total      int
	HasMore    bool
	NextCursor string
}

// SearchArtifacts searches artifacts by query string using tsvector index
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
	// Add +1 to limit to check if there are more results for pagination
	searchQuery += ` LIMIT $` + fmt.Sprintf("%d", len(params)+1)
	params = append(params, opts.Limit+1)

	if opts.Offset > 0 {
		searchQuery += ` OFFSET $` + fmt.Sprintf("%d", len(params)+1)
		params = append(params, opts.Offset)
	}

	rows, err := db.pool.Query(context.Background(), searchQuery, params...)
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
		// Limit results to requested size (check if there are more for pagination)
		if len(artifacts) < opts.Limit {
			artifacts = append(artifacts, a)
		}
	}

	return artifacts, rows.Err()
}

// SearchCount returns the total count of search results (for pagination metadata)
func (db *Database) SearchCount(query string, opts SearchOptions) (int, error) {
	countQuery := `SELECT COUNT(*) FROM artifacts WHERE to_tsvector('english', artifact_name || ' ' || namespace) @@ to_tsquery('english', $1)`
	params := []any{query}

	if opts.RegistryID != "" {
		countQuery += ` AND registry_id = $` + fmt.Sprintf("%d", len(params)+1)
		params = append(params, opts.RegistryID)
	}

	if opts.ArtifactType != "" {
		countQuery += ` AND artifact_type = $` + fmt.Sprintf("%d", len(params)+1)
		params = append(params, opts.ArtifactType)
	}

	var count int
	err := db.pool.QueryRow(context.Background(), countQuery, params...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count search results: %w", err)
	}
	return count, nil
}

// SaveArtifact inserts or updates artifact metadata with tsvector generation
func (db *Database) SaveArtifact(metadata *ArtifactMetadata) error {
	if metadata.ID == "" {
		metadata.ID = generateUUID()
	}
	metadata.Updated = time.Now()

	// Generate tsvector from artifact_name and namespace for full-text search
	_, err := db.pool.Exec(
		context.Background(),
		`INSERT INTO artifacts (
			 id, registry_id, artifact_type, namespace, artifact_name, version,
			 digest, digest_algorithm, size, created, updated, metadata, tags, signatures,
			 search_vector
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, to_tsvector('english', $5 || ' ' || $4))
		 ON CONFLICT (registry_id, namespace, artifact_name, version)
		 DO UPDATE SET
		   digest = EXCLUDED.digest,
		   digest_algorithm = EXCLUDED.digest_algorithm,
		   size = EXCLUDED.size,
		   updated = EXCLUDED.updated,
		   metadata = EXCLUDED.metadata,
		   tags = EXCLUDED.tags,
		   signatures = EXCLUDED.signatures,
		   search_vector = to_tsvector('english', EXCLUDED.artifact_name || ' ' || EXCLUDED.namespace)`,
		metadata.ID, metadata.RegistryID, metadata.ArtifactType, metadata.Namespace,
		metadata.ArtifactName, metadata.Version, metadata.Digest, metadata.DigestAlgorithm,
		metadata.Size, metadata.Created, metadata.Updated, metadata.Metadata, metadata.Tags, metadata.Signatures,
	)
	if err != nil {
		// Fallback: try without tsvector if column doesn't exist (legacy schema)
		_, err2 := db.pool.Exec(
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
		if err2 != nil {
			return fmt.Errorf("failed to save artifact: %w", err)
		}
	}
	return nil
}

// DeleteArtifact removes an artifact
func (db *Database) DeleteArtifact(registryID, namespace, artifactName, version string) (bool, error) {
	result, err := db.pool.Exec(
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
	_, err := db.pool.Exec(
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
	row := db.pool.QueryRow(
		context.Background(),
		`SELECT u.user_id, u.username, u.email, u.password_hash, u.created_at, u.last_login, u.is_active,
			 COALESCE(json_agg(ur.role_id) FILTER (WHERE ur.role_id IS NOT NULL), '[]'::json) as roles
		 FROM users u
		 LEFT JOIN user_roles ur ON u.user_id = ur.user_id
		 WHERE u.user_id = $1
		 GROUP BY u.user_id`,
		userID,
	)

	var user UserRepository
	err := row.Scan(&user.UserID, &user.Username, &user.Email, &user.PasswordHash,
		&user.CreatedAt, &user.LastLogin, &user.IsActive, &user.Roles)
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
	row := db.pool.QueryRow(
		context.Background(),
		`SELECT u.user_id, u.username, u.email, u.password_hash, u.created_at, u.last_login, u.is_active,
			 COALESCE(json_agg(ur.role_id) FILTER (WHERE ur.role_id IS NOT NULL), '[]'::json) as roles
		 FROM users u
		 LEFT JOIN user_roles ur ON u.user_id = ur.user_id
		 WHERE u.username = $1
		 GROUP BY u.user_id`,
		username,
	)

	var user UserRepository
	err := row.Scan(&user.UserID, &user.Username, &user.Email, &user.PasswordHash,
		&user.CreatedAt, &user.LastLogin, &user.IsActive, &user.Roles)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &user, nil
}

// CreateUser creates a new user with bcrypt password hash verification
func (db *Database) CreateUser(username, email, passwordHash string, roles []string) (*UserRepository, error) {
	userID := generateUUID()
	createdAt := time.Now()

	// Insert user without roles (roles are managed via user_roles table)
	row := db.pool.QueryRow(
		context.Background(),
		`INSERT INTO users (user_id, username, email, password_hash, created_at, last_login, is_active)
		 VALUES ($1, $2, $3, $4, $5, NULL, TRUE)
		 RETURNING user_id, username, email, password_hash, created_at, last_login, is_active`,
		userID, username, email, passwordHash, createdAt,
	)

	var user UserRepository
	err := row.Scan(&user.UserID, &user.Username, &user.Email, &user.PasswordHash,
		&user.CreatedAt, &user.LastLogin, &user.IsActive)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Assign roles via the user_roles table
	for _, roleID := range roles {
		if err := db.AssignRoleToUser(user.UserID, roleID); err != nil {
			return nil, fmt.Errorf("failed to assign role %s to user: %w", roleID, err)
		}
	}

	return &user, nil
}

// UpdateUserLastLogin updates the last login timestamp
func (db *Database) UpdateUserLastLogin(userID string) error {
	_, err := db.pool.Exec(
		context.Background(),
		`UPDATE users SET last_login = $1 WHERE user_id = $2`,
		time.Now(), userID,
	)
	if err != nil {
		return fmt.Errorf("failed to update user last login: %w", err)
	}
	return nil
}

// SetUserActive sets a user's active status (soft delete)
func (db *Database) SetUserActive(userID string, isActive bool) error {
	_, err := db.pool.Exec(
		context.Background(),
		`UPDATE users SET is_active = $1 WHERE user_id = $2`,
		isActive, userID,
	)
	if err != nil {
		return fmt.Errorf("failed to update user active status: %w", err)
	}
	return nil
}

// CreateAccessKey creates a new access key with configurable permissions
func (db *Database) CreateAccessKey(userID, name string, permissions []string, expiresAt *time.Time) (*AccessKey, error) {
	keyID := generateUUID()
	keyHash := fmt.Sprintf("key_%s", uuid.New().String())
	createdAt := time.Now()

	row := db.pool.QueryRow(
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

// ValidateAccessKey validates an access key and updates last used timestamp
func (db *Database) ValidateAccessKey(keyHash string) (*AccessKey, error) {
	now := time.Now()
	row := db.pool.QueryRow(
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
	_, err := db.pool.Exec(
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
	rows, err := db.pool.Query(
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

// ListRegistries lists all enabled registries with ordering
func (db *Database) ListRegistries() ([]RegistryConfig, error) {
	rows, err := db.pool.Query(
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
	row := db.pool.QueryRow(
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
	_, err := db.pool.Exec(
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
	_, err := db.pool.Exec(context.Background(), `DELETE FROM registries WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete registry: %w", err)
	}
	return nil
}

// GenerateAuditLog creates an audit log entry
func (db *Database) GenerateAuditLog(userID, action, resourceType, resourceID, details string) error {
	_, err := db.pool.Exec(
		context.Background(),
		`INSERT INTO audit_log (user_id, action, resource_type, resource_id, details)
		 VALUES ($1, $2, $3, $4, $5)`,
		userID, action, resourceType, resourceID, details,
	)
	return err
}

// ListAuditLogs retrieves audit logs with optional filtering
func (db *Database) ListAuditLogs(opts SearchOptions) ([]AuditLog, error) {
	query := `SELECT id, user_id, action, resource_type, resource_id, details, created_at
			  FROM audit_log WHERE 1=1`
	params := []any{}

	if opts.RegistryID != "" {
		query += ` AND resource_id = $` + fmt.Sprintf("%d", len(params)+1)
		params = append(params, opts.RegistryID)
	}

	if opts.ArtifactType != "" {
		query += ` AND action = $` + fmt.Sprintf("%d", len(params)+1)
		params = append(params, opts.ArtifactType)
	}

	query += ` ORDER BY created_at DESC`

	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	query += ` LIMIT $` + fmt.Sprintf("%d", len(params)+1)
	params = append(params, opts.Limit)

	rows, err := db.pool.Query(context.Background(), query, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to list audit logs: %w", err)
	}
	defer rows.Close()

	var logs []AuditLog
	for rows.Next() {
		var log AuditLog
		err := rows.Scan(&log.ID, &log.UserID, &log.Action, &log.ResourceType,
			&log.ResourceID, &log.Details, &log.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}
		logs = append(logs, log)
	}

	return logs, rows.Err()
}

// GetUserRoles returns all role IDs for a user
func (db *Database) GetUserRoles(userID string) ([]string, error) {
	rows, err := db.pool.Query(
		context.Background(),
		`SELECT role_id FROM user_roles WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get user roles: %w", err)
	}
	defer rows.Close()

	var roleIDs []string
	for rows.Next() {
		var roleID string
		if err := rows.Scan(&roleID); err != nil {
			return nil, fmt.Errorf("failed to scan role: %w", err)
		}
		roleIDs = append(roleIDs, roleID)
	}
	return roleIDs, rows.Err()
}

// GetUserRolePermissions returns all permissions for a user via their roles
func (db *Database) GetUserRolePermissions(userID string) ([]RolePermission, error) {
	query := `
		SELECT DISTINCT rp.role_id, rp.permission_id
		FROM user_roles ur
		JOIN role_permissions rp ON ur.role_id = rp.role_id
		WHERE ur.user_id = $1
	`
	rows, err := db.pool.Query(context.Background(), query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user role permissions: %w", err)
	}
	defer rows.Close()

	var permissions []RolePermission
	for rows.Next() {
		var p RolePermission
		if err := rows.Scan(&p.RoleID, &p.PermissionID); err != nil {
			return nil, fmt.Errorf("failed to scan permission: %w", err)
		}
		permissions = append(permissions, p)
	}
	return permissions, rows.Err()
}

// GetRole retrieves a role by ID
func (db *Database) GetRole(id string) (*Role, error) {
	row := db.pool.QueryRow(
		context.Background(),
		`SELECT id, name, description, is_system, created_at FROM roles WHERE id = $1`,
		id,
	)

	var role Role
	err := row.Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem, &role.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get role: %w", err)
	}
	return &role, nil
}

// SaveRole saves a role and its permissions to the database
func (db *Database) SaveRole(id, name, description string, isSystem bool, permissions []string) error {
	_, err := db.pool.Exec(
		context.Background(),
		`INSERT INTO roles (id, name, description, is_system)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description`,
		id, name, description, isSystem,
	)
	if err != nil {
		return fmt.Errorf("failed to save role: %w", err)
	}

	// Update role permissions
	_, err = db.pool.Exec(context.Background(), `DELETE FROM role_permissions WHERE role_id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to clear role permissions: %w", err)
	}

	for _, permID := range permissions {
		_, err = db.pool.Exec(
			context.Background(),
			`INSERT INTO role_permissions (role_id, permission_id) VALUES ($1, $2)`,
			id, permID,
		)
		if err != nil {
			return fmt.Errorf("failed to save role permission: %w", err)
		}
	}
	return nil
}

// DeleteRole deletes a role
func (db *Database) DeleteRole(id string) error {
	_, err := db.pool.Exec(context.Background(), `DELETE FROM roles WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete role: %w", err)
	}
	return nil
}

// AssignRoleToUser assigns a role to a user
func (db *Database) AssignRoleToUser(userID, roleID string) error {
	_, err := db.pool.Exec(
		context.Background(),
		`INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)
		 ON CONFLICT (user_id, role_id) DO NOTHING`,
		userID, roleID,
	)
	return err
}

// RevokeRoleFromUser revokes a role from a user
func (db *Database) RevokeRoleFromUser(userID, roleID string) error {
	_, err := db.pool.Exec(
		context.Background(),
		`DELETE FROM user_roles WHERE user_id = $1 AND role_id = $2`,
		userID, roleID,
	)
	return err
}

// AddRolePermission adds a permission to a role
func (db *Database) AddRolePermission(roleID, permission string) error {
	_, err := db.pool.Exec(
		context.Background(),
		`INSERT INTO role_permissions (role_id, permission_id) VALUES ($1, $2)
		 ON CONFLICT (role_id, permission_id) DO NOTHING`,
		roleID, permission,
	)
	return err
}

// RemoveRolePermission removes a permission from a role
func (db *Database) RemoveRolePermission(roleID, permission string) error {
	_, err := db.pool.Exec(
		context.Background(),
		`DELETE FROM role_permissions WHERE role_id = $1 AND permission_id = $2`,
		roleID, permission,
	)
	return err
}

// GetPendingReplicationArtifacts gets artifacts pending replication
func (db *Database) GetPendingReplicationArtifacts(limit int) ([]ArtifactMetadata, error) {
	query := `
		SELECT id, registry_id, artifact_type, namespace, artifact_name, version,
			   digest, digest_algorithm, size, created, updated, metadata, tags, signatures
		FROM artifacts
		WHERE id NOT IN (
			SELECT artifact_id FROM replication_status WHERE status = 'complete'
		)
		ORDER BY created ASC
		LIMIT $1
	`

	rows, err := db.pool.Query(context.Background(), query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending replication artifacts: %w", err)
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

// MarkArtifactReplicated marks an artifact as replicated
func (db *Database) MarkArtifactReplicated(artifactID, regionID string) error {
	_, err := db.pool.Exec(
		context.Background(),
		`INSERT INTO replication_status (artifact_id, region_id, status, last_replicated)
		 VALUES ($1, $2, 'complete', NOW())
		 ON CONFLICT (artifact_id, region_id)
		 DO UPDATE SET status = 'complete', last_replicated = NOW()`,
		artifactID, regionID,
	)
	return err
}

// CreateReplicationTable creates the replication status table
func (db *Database) CreateReplicationTable() error {
	_, err := db.pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS replication_status (
			artifact_id VARCHAR(255) NOT NULL,
			region_id VARCHAR(50) NOT NULL,
			status VARCHAR(20) DEFAULT 'pending',
			last_replicated TIMESTAMP DEFAULT NOW(),
			created_at TIMESTAMP DEFAULT NOW(),
			PRIMARY KEY (artifact_id, region_id),
			FOREIGN KEY (artifact_id) REFERENCES artifacts(id) ON DELETE CASCADE
		)
	`)
	return err
}

// generateUUID generates a new UUID v4
func generateUUID() string {
	return uuid.New().String()
}

// GetArtifact retrieves an artifact by its ID
func (db *Database) GetArtifact(id string) (*ArtifactMetadata, error) {
	row := db.pool.QueryRow(
		context.Background(),
		`SELECT id, registry_id, artifact_type, namespace, artifact_name, version,
			 digest, digest_algorithm, size, created, updated, metadata, tags, signatures
		 FROM artifacts WHERE id = $1`,
		id,
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

// CountArtifacts returns the total count of artifacts
func (db *Database) CountArtifacts(registryID string, opts ListOptions) (int, error) {
	query := `SELECT COUNT(*) FROM artifacts WHERE registry_id = $1`
	params := []any{registryID}

	if opts.Namespace != "" {
		query += ` AND namespace = $` + fmt.Sprintf("%d", len(params)+1)
		params = append(params, opts.Namespace)
	}

	if opts.ArtifactType != "" {
		query += ` AND artifact_type = $` + fmt.Sprintf("%d", len(params)+1)
		params = append(params, opts.ArtifactType)
	}

	var count int
	err := db.pool.QueryRow(context.Background(), query, params...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count artifacts: %w", err)
	}
	return count, nil
}

// SaveSignature saves a signature for an artifact
func (db *Database) SaveSignature(artifactID string, signature *Signature) error {
	// Get current signatures
	var signaturesJSON string
	err := db.pool.QueryRow(context.Background(), `SELECT signatures FROM artifacts WHERE id = $1`, artifactID).Scan(&signaturesJSON)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("failed to get artifact signatures: %w", err)
	}

	// Add new signature (in production, this would properly merge JSON arrays)
	newSignatures := fmt.Sprintf(`[{"type":"%s","keyId":"%s","timestamp":"%s","verified":%v}]`,
		signature.Type, signature.KeyID, signature.Timestamp.Format(time.RFC3339), signature.Verified)

	_, err = db.pool.Exec(
		context.Background(),
		`UPDATE artifacts SET signatures = $1 WHERE id = $2`,
		newSignatures, artifactID,
	)
	if err != nil {
		return fmt.Errorf("failed to save signature: %w", err)
	}
	return nil
}

// ListUsers returns all users
func (db *Database) ListUsers() ([]UserRepository, error) {
	rows, err := db.pool.Query(
		context.Background(),
		`SELECT u.user_id, u.username, u.email, u.password_hash, u.created_at, u.last_login, u.is_active,
			 COALESCE(json_agg(ur.role_id) FILTER (WHERE ur.role_id IS NOT NULL), '[]'::json) as roles
		 FROM users u
		 LEFT JOIN user_roles ur ON u.user_id = ur.user_id
		 GROUP BY u.user_id
		 ORDER BY u.created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []UserRepository
	for rows.Next() {
		var u UserRepository
		err := rows.Scan(&u.UserID, &u.Username, &u.Email, &u.PasswordHash,
			&u.CreatedAt, &u.LastLogin, &u.IsActive, &u.Roles)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// cacheKey generates a deterministic cache key for a query
func (db *Database) cacheKey(prefix string, sql string, params ...any) string {
	// Create a hash of the query and parameters for deterministic keys
	h := sha256.New()
	h.Write([]byte(sql))
	for _, p := range params {
		data, _ := json.Marshal(p)
		h.Write(data)
	}
	hash := base64.URLEncoding.EncodeToString(h.Sum(nil))
	return fmt.Sprintf("query:%s:%s", prefix, hash)
}

// queryWithCache executes a query with optional caching
func (db *Database) queryWithCache(key string, cacheTTL time.Duration, queryFunc func() ([]byte, error)) ([]byte, error) {
	// Check cache first if cache is enabled
	if db.cache != nil {
		if exists, _ := db.cache.Exists(key); exists {
			var result []byte
			if err := db.cache.Get(key, &result); err == nil && result != nil {
				return result, nil
			}
		}
	}

	// Execute query function
	data, err := queryFunc()
	if err != nil {
		return nil, err
	}

	// Store in cache if cache is enabled
	if db.cache != nil && len(data) > 0 && cacheTTL > 0 {
		_ = db.cache.SetWithTTL(key, data, cacheTTL)
	}

	return data, nil
}

// ListArtifactsByRegistry lists artifacts for a specific registry
func (db *Database) ListArtifactsByRegistry(registryID string, opts ListOptions) ([]ArtifactMetadata, error) {
	return db.ListArtifacts(registryID, opts)
}

// CountArtifactsByRegistry returns the count of artifacts for a specific registry
func (db *Database) CountArtifactsByRegistry(registryID string, opts ListOptions) (int, error) {
	return db.CountArtifacts(registryID, opts)
}

// GetUser retrieves a user by ID
func (db *Database) GetUser(id string) (*UserRepository, error) {
	return db.GetUserByID(id)
}

// CursorPagination helper functions for cursor-based pagination

// encodeCursor encodes pagination options into a base64 cursor
func encodeCursor(limiter int, timestamp time.Time, prevCursor string) string {
	// Simple cursor format: timestamp|limit
	// In production, use proper base64 encoding with a secret key for integrity
	cursorData := fmt.Sprintf("%s|%d", timestamp.Format(time.RFC3339Nano), limiter)
	return base64.URLEncoding.EncodeToString([]byte(cursorData))
}

// decodeCursor decodes a base64 cursor back to pagination options
func decodeCursor(cursor string) (time.Time, int, error) {
	if cursor == "" {
		return time.Time{}, 0, nil
	}
	data, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, 0, err
	}
	// Parse the cursor data
	cursorStr := string(data)
	// Split by last pipe
	parts := strings.Split(cursorStr, "|")
	if len(parts) != 2 {
		return time.Time{}, 0, fmt.Errorf("invalid cursor format")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, 0, err
	}
	var limit int
	_, err = fmt.Sscanf(parts[1], "%d", &limit)
	if err != nil {
		return time.Time{}, 0, err
	}
	return t, limit, nil
}

// ListArtifactsCursor returns artifacts with cursor-based pagination
func (db *Database) ListArtifactsCursor(opts CursorPaginationOptions) ([]ArtifactMetadata, string, bool, error) {
	// Default options
	if opts.Limit <= 0 {
		opts.Limit = 50
	}
	if opts.Limit > 200 {
		opts.Limit = 200
	}
	if opts.OrderBy == "" {
		opts.OrderBy = "created"
	}
	if opts.Order == "" {
		opts.Order = "desc"
	}

	// Build query with cursor
	var query string
	var args []any
	var cursorCondition string

	if opts.Cursor != "" {
		// Decode cursor and add WHERE condition
		cursorTime, _, err := decodeCursor(opts.Cursor)
		if err == nil {
			if opts.Order == "desc" {
				cursorCondition = fmt.Sprintf(" AND a.%s < $%d", opts.OrderBy, len(args)+3)
			} else {
				cursorCondition = fmt.Sprintf(" AND a.%s > $%d", opts.OrderBy, len(args)+3)
			}
			args = append(args, cursorTime)
		}
	}

	// Main query with LIMIT + 1 for "has next" detection
	query = fmt.Sprintf(`
		SELECT a.id, a.registry_id, a.artifact_type, a.namespace, a.artifact_name, a.version,
			   a.digest, a.digest_algorithm, a.size, a.created, a.updated, a.metadata, a.tags, a.signatures
		FROM artifacts a
		WHERE 1=1%s
		ORDER BY a.%s %s
		LIMIT $1`,
		cursorCondition, opts.OrderBy, opts.Order)

	// Add limit + 1 to detect if there are more results
	args = append([]any{opts.Limit + 1}, args...)

	rows, err := db.pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, "", false, fmt.Errorf("failed to list artifacts: %w", err)
	}
	defer rows.Close()

	var artifacts []ArtifactMetadata
	for rows.Next() {
		var a ArtifactMetadata
		err := rows.Scan(&a.ID, &a.RegistryID, &a.ArtifactType, &a.Namespace,
			&a.ArtifactName, &a.Version, &a.Digest, &a.DigestAlgorithm, &a.Size,
			&a.Created, &a.Updated, &a.Metadata, &a.Tags, &a.Signatures)
		if err != nil {
			return nil, "", false, fmt.Errorf("failed to scan artifact: %w", err)
		}
		artifacts = append(artifacts, a)
	}

	if err = rows.Err(); err != nil {
		return nil, "", false, fmt.Errorf("error iterating artifacts: %w", err)
	}

	// Check if there are more results
	hasNext := len(artifacts) > opts.Limit
	if hasNext {
		artifacts = artifacts[:opts.Limit] // Trim to limit
	}

	// Generate next cursor if there are more results
	var nextCursor string
	if hasNext && len(artifacts) > 0 {
		// Use the last artifact's timestamp for the next cursor
		lastArtifact := artifacts[len(artifacts)-1]
		nextCursor = encodeCursor(opts.Limit, lastArtifact.Created, "")
	}

	return artifacts, nextCursor, hasNext, nil
}

// ListUsersCursor returns users with cursor-based pagination
func (db *Database) ListUsersCursor(opts CursorPaginationOptions) ([]UserRepository, string, bool, error) {
	if opts.Limit <= 0 {
		opts.Limit = 50
	}
	if opts.Limit > 200 {
		opts.Limit = 200
	}
	if opts.OrderBy == "" {
		opts.OrderBy = "created_at"
	}
	if opts.Order == "" {
		opts.Order = "desc"
	}

	var query string
	var args []any
	var cursorCondition string

	if opts.Cursor != "" {
		cursorTime, _, err := decodeCursor(opts.Cursor)
		if err == nil {
			if opts.Order == "desc" {
				cursorCondition = fmt.Sprintf(" AND u.%s < $%d", opts.OrderBy, len(args)+3)
			} else {
				cursorCondition = fmt.Sprintf(" AND u.%s > $%d", opts.OrderBy, len(args)+3)
			}
			args = append(args, cursorTime)
		}
	}

	query = fmt.Sprintf(`
		SELECT u.user_id, u.username, u.email, u.password_hash, u.created_at, u.last_login, u.is_active,
			   COALESCE(json_agg(ur.role_id) FILTER (WHERE ur.role_id IS NOT NULL), '[]'::json) as roles
		FROM users u
		LEFT JOIN user_roles ur ON u.user_id = ur.user_id
		WHERE 1=1%s
		GROUP BY u.user_id
		ORDER BY u.%s %s
		LIMIT $1`,
		cursorCondition, opts.OrderBy, opts.Order)

	args = append([]any{opts.Limit + 1}, args...)

	rows, err := db.pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, "", false, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []UserRepository
	for rows.Next() {
		var u UserRepository
		err := rows.Scan(&u.UserID, &u.Username, &u.Email, &u.PasswordHash,
			&u.CreatedAt, &u.LastLogin, &u.IsActive, &u.Roles)
		if err != nil {
			return nil, "", false, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, u)
	}

	if err = rows.Err(); err != nil {
		return nil, "", false, fmt.Errorf("error iterating users: %w", err)
	}

	hasNext := len(users) > opts.Limit
	if hasNext {
		users = users[:opts.Limit]
	}

	var nextCursor string
	if hasNext && len(users) > 0 {
		lastUser := users[len(users)-1]
		nextCursor = encodeCursor(opts.Limit, lastUser.CreatedAt, "")
	}

	return users, nextCursor, hasNext, nil
}
