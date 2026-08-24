package database

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	dsn      string
	pool     *pgxpool.Pool
	cache    CacheAdapter
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
		MaxConnections:    10,        // Low for stateless - each pod holds few connections
		MinConnections:    1,         // Minimum to keep pool warm
		MaxConnectionAge:  time.Hour, // Reconnect periodically
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
	config.MaxConnLifetime = 1 * time.Hour      // Reconnect periodically
	config.MaxConnIdleTime = 5 * time.Minute    // Close idle connections
	config.HealthCheckPeriod = 30 * time.Second // Frequent health checks

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

// Begin starts a transaction on the pool. Exposed for callers (e.g.
// pkg/migrate) that need multiple statements to commit or roll back
// together, rather than the fire-and-forget Exec/Query/QueryRow above.
func (db *Database) Begin(ctx context.Context) (pgx.Tx, error) {
	return db.pool.Begin(ctx)
}

// GetArtifactByParams retrieves an artifact by its identifying parameters
func (db *Database) GetArtifactByParams(registryID, namespace, artifactName, version string) (*ArtifactMetadata, error) {
	row := db.pool.QueryRow(
		context.Background(),
		`SELECT id, registry_id, artifact_type, namespace, artifact_name, version,
			 digest, digest_algorithm, size, total_size, created, updated, metadata, tags, signatures, downloads
		 FROM artifacts
		 WHERE registry_id = $1 AND namespace = $2 AND artifact_name = $3 AND version = $4`,
		registryID, namespace, artifactName, version,
	)

	var artifact ArtifactMetadata
	err := row.Scan(
		&artifact.ID, &artifact.RegistryID, &artifact.ArtifactType,
		&artifact.Namespace, &artifact.ArtifactName, &artifact.Version,
		&artifact.Digest, &artifact.DigestAlgorithm, &artifact.Size, &artifact.TotalSize,
		&artifact.Created, &artifact.Updated, &artifact.Metadata, &artifact.Tags, &artifact.Signatures, &artifact.Downloads,
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
			 digest, digest_algorithm, size, total_size, created, updated, metadata, tags, signatures, downloads
		 FROM artifacts
		 WHERE registry_id = $1 AND digest = $2
		 ORDER BY created DESC`,
		registryID, digest,
	)

	var artifact ArtifactMetadata
	err := row.Scan(
		&artifact.ID, &artifact.RegistryID, &artifact.ArtifactType,
		&artifact.Namespace, &artifact.ArtifactName, &artifact.Version,
		&artifact.Digest, &artifact.DigestAlgorithm, &artifact.Size, &artifact.TotalSize,
		&artifact.Created, &artifact.Updated, &artifact.Metadata, &artifact.Tags, &artifact.Signatures, &artifact.Downloads,
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
				 digest, digest_algorithm, size, total_size, created, updated, metadata, tags, signatures, downloads
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
			&a.ArtifactName, &a.Version, &a.Digest, &a.DigestAlgorithm, &a.Size, &a.TotalSize,
			&a.Created, &a.Updated, &a.Metadata, &a.Tags, &a.Signatures, &a.Downloads)
		if err != nil {
			return nil, fmt.Errorf("failed to scan artifact: %w", err)
		}
		artifacts = append(artifacts, a)
	}

	return artifacts, rows.Err()
}

// ListArtifactsMulti lists artifacts across a set of registry IDs — used for
// the unscoped "browse everything the caller can read" listing, where the
// caller has already filtered registryIDs down to what RBAC allows. An empty
// slice means "no readable registries" and returns no rows, not "no filter".
func (db *Database) ListArtifactsMulti(registryIDs []string, opts ListOptions) ([]ArtifactMetadata, error) {
	if len(registryIDs) == 0 {
		return nil, nil
	}

	query := `SELECT id, registry_id, artifact_type, namespace, artifact_name, version,
				 digest, digest_algorithm, size, total_size, created, updated, metadata, tags, signatures, downloads
			  FROM artifacts WHERE registry_id = ANY($1)`
	params := []any{registryIDs}

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
			&a.ArtifactName, &a.Version, &a.Digest, &a.DigestAlgorithm, &a.Size, &a.TotalSize,
			&a.Created, &a.Updated, &a.Metadata, &a.Tags, &a.Signatures, &a.Downloads)
		if err != nil {
			return nil, fmt.Errorf("failed to scan artifact: %w", err)
		}
		artifacts = append(artifacts, a)
	}

	return artifacts, rows.Err()
}

// CountArtifactsMulti is the counterpart to ListArtifactsMulti.
func (db *Database) CountArtifactsMulti(registryIDs []string, opts ListOptions) (int, error) {
	if len(registryIDs) == 0 {
		return 0, nil
	}

	query := `SELECT COUNT(*) FROM artifacts WHERE registry_id = ANY($1)`
	params := []any{registryIDs}

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
					 digest, digest_algorithm, size, total_size, created, updated, metadata, tags, signatures, downloads
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
			&a.ArtifactName, &a.Version, &a.Digest, &a.DigestAlgorithm, &a.Size, &a.TotalSize,
			&a.Created, &a.Updated, &a.Metadata, &a.Tags, &a.Signatures, &a.Downloads)
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

// AutocompleteArtifacts returns distinct artifact names matching a prefix, for search-as-you-type suggestions
func (db *Database) AutocompleteArtifacts(prefix string, limit int) ([]string, error) {
	query := `
		SELECT DISTINCT artifact_name
		FROM artifacts
		WHERE artifact_name ILIKE $1
		ORDER BY artifact_name
		LIMIT $2
	`

	rows, err := db.pool.Query(context.Background(), query, prefix+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("failed to autocomplete artifacts: %w", err)
	}
	defer rows.Close()

	suggestions := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan suggestion: %w", err)
		}
		suggestions = append(suggestions, name)
	}

	return suggestions, rows.Err()
}

// SaveArtifact inserts or updates artifact metadata with tsvector generation
func (db *Database) SaveArtifact(metadata *ArtifactMetadata) error {
	if metadata.ID == "" {
		metadata.ID = generateUUID()
	}
	metadata.Updated = time.Now()
	if metadata.Signatures == nil {
		metadata.Signatures = []Signature{}
	}

	// Generate tsvector from artifact_name and namespace for full-text search
	_, err := db.pool.Exec(
		context.Background(),
		`INSERT INTO artifacts (
			 id, registry_id, artifact_type, namespace, artifact_name, version,
			 digest, digest_algorithm, size, total_size, created, updated, metadata, tags, signatures,
			 search_vector
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, to_tsvector('english', $5 || ' ' || $4))
		 ON CONFLICT (registry_id, namespace, artifact_name, version)
		 DO UPDATE SET
		   digest = EXCLUDED.digest,
		   digest_algorithm = EXCLUDED.digest_algorithm,
		   size = EXCLUDED.size,
		   total_size = EXCLUDED.total_size,
		   updated = EXCLUDED.updated,
		   metadata = EXCLUDED.metadata,
		   tags = EXCLUDED.tags,
		   signatures = EXCLUDED.signatures,
		   search_vector = to_tsvector('english', EXCLUDED.artifact_name || ' ' || EXCLUDED.namespace)`,
		metadata.ID, metadata.RegistryID, metadata.ArtifactType, metadata.Namespace,
		metadata.ArtifactName, metadata.Version, metadata.Digest, metadata.DigestAlgorithm,
		metadata.Size, metadata.TotalSize, metadata.Created, metadata.Updated, metadata.Metadata, metadata.Tags, metadata.Signatures,
	)
	if err != nil {
		// Fallback: try without tsvector if column doesn't exist (legacy schema)
		_, err2 := db.pool.Exec(
			context.Background(),
			`INSERT INTO artifacts (
				 id, registry_id, artifact_type, namespace, artifact_name, version,
				 digest, digest_algorithm, size, total_size, created, updated, metadata, tags, signatures
			 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			 ON CONFLICT (registry_id, namespace, artifact_name, version)
			 DO UPDATE SET
			   digest = EXCLUDED.digest,
			   digest_algorithm = EXCLUDED.digest_algorithm,
			   size = EXCLUDED.size,
			   total_size = EXCLUDED.total_size,
			   updated = EXCLUDED.updated,
			   metadata = EXCLUDED.metadata,
			   tags = EXCLUDED.tags,
			   signatures = EXCLUDED.signatures`,
			metadata.ID, metadata.RegistryID, metadata.ArtifactType, metadata.Namespace,
			metadata.ArtifactName, metadata.Version, metadata.Digest, metadata.DigestAlgorithm,
			metadata.Size, metadata.TotalSize, metadata.Created, metadata.Updated, metadata.Metadata, metadata.Tags, metadata.Signatures,
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

// IncrementArtifactDownloads atomically bumps the pull/download counter for
// one artifact version, plus the instance-wide total_pulls accumulator in
// the stats table. Called from proxy handlers at the point they serve
// artifact bytes to a client. The stats accumulator is kept separate from
// the per-artifact count because the artifact row (and its count) can later
// be deleted, e.g. by clearing the cache, and the Stats page's "Total
// Pulls" figure should survive that.
func (db *Database) IncrementArtifactDownloads(registryID, namespace, artifactName, version string) error {
	ctx := context.Background()
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(
		ctx,
		`UPDATE artifacts SET downloads = downloads + 1
		 WHERE registry_id = $1 AND namespace = $2 AND artifact_name = $3 AND version = $4`,
		registryID, namespace, artifactName, version,
	); err != nil {
		return fmt.Errorf("failed to increment artifact downloads: %w", err)
	}

	if _, err := tx.Exec(ctx, `UPDATE stats SET total_pulls = total_pulls + 1 WHERE id = 1`); err != nil {
		return fmt.Errorf("failed to increment total pulls: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit download increment: %w", err)
	}
	return nil
}

// TopArtifactsByDownloads returns the N most-pulled artifact versions across
// every registry, ordered by download count descending, for the Stats page
// leaderboard.
func (db *Database) TopArtifactsByDownloads(limit int) ([]ArtifactMetadata, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := db.pool.Query(
		context.Background(),
		`SELECT id, registry_id, artifact_type, namespace, artifact_name, version,
			 digest, digest_algorithm, size, total_size, created, updated, metadata, tags, signatures, downloads
		 FROM artifacts
		 WHERE downloads > 0
		 ORDER BY downloads DESC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list top artifacts: %w", err)
	}
	defer rows.Close()

	var artifacts []ArtifactMetadata
	for rows.Next() {
		var a ArtifactMetadata
		err := rows.Scan(&a.ID, &a.RegistryID, &a.ArtifactType, &a.Namespace,
			&a.ArtifactName, &a.Version, &a.Digest, &a.DigestAlgorithm, &a.Size, &a.TotalSize,
			&a.Created, &a.Updated, &a.Metadata, &a.Tags, &a.Signatures, &a.Downloads)
		if err != nil {
			return nil, fmt.Errorf("failed to scan artifact: %w", err)
		}
		artifacts = append(artifacts, a)
	}
	return artifacts, rows.Err()
}

// TotalDownloads returns the instance-wide running total of pulls, tracked
// independently of the artifacts table (see IncrementArtifactDownloads) so
// it isn't reset when artifacts are deleted or the cache is cleared.
func (db *Database) TotalDownloads() (int64, error) {
	var total int64
	err := db.pool.QueryRow(context.Background(), `SELECT total_pulls FROM stats WHERE id = 1`).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to get total pulls: %w", err)
	}
	return total, nil
}

// IncrementBandwidthSaved atomically adds to the running total of bytes
// served from local storage on a cache hit — i.e. bytes that did not need to
// be re-fetched from an upstream registry. Called by the storage tracking
// adapter (see pkg/storage/tracking.go) on every successful GetArtifact.
func (db *Database) IncrementBandwidthSaved(bytes int64) error {
	_, err := db.pool.Exec(
		context.Background(),
		`UPDATE stats SET bandwidth_saved_bytes = bandwidth_saved_bytes + $1 WHERE id = 1`,
		bytes,
	)
	if err != nil {
		return fmt.Errorf("failed to record bandwidth saved: %w", err)
	}
	return nil
}

// GetBandwidthSaved returns the cumulative bytes served from cache.
func (db *Database) GetBandwidthSaved() (int64, error) {
	var total int64
	err := db.pool.QueryRow(context.Background(), `SELECT bandwidth_saved_bytes FROM stats WHERE id = 1`).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to get bandwidth saved: %w", err)
	}
	return total, nil
}

// GetUserByID retrieves a user by ID
func (db *Database) GetUserByID(userID string) (*UserRepository, error) {
	row := db.pool.QueryRow(
		context.Background(),
		`SELECT u.user_id, u.username, u.email, u.password_hash, u.created_at, u.last_login, u.is_active, u.timezone,
			 COALESCE(json_agg(ur.role_id) FILTER (WHERE ur.role_id IS NOT NULL), '[]'::json) as roles
		 FROM users u
		 LEFT JOIN user_roles ur ON u.user_id = ur.user_id
		 WHERE u.user_id = $1
		 GROUP BY u.user_id`,
		userID,
	)

	var user UserRepository
	err := row.Scan(&user.UserID, &user.Username, &user.Email, &user.PasswordHash,
		&user.CreatedAt, &user.LastLogin, &user.IsActive, &user.Timezone, &user.Roles)
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
		`SELECT u.user_id, u.username, u.email, u.password_hash, u.created_at, u.last_login, u.is_active, u.timezone,
			 COALESCE(json_agg(ur.role_id) FILTER (WHERE ur.role_id IS NOT NULL), '[]'::json) as roles
		 FROM users u
		 LEFT JOIN user_roles ur ON u.user_id = ur.user_id
		 WHERE u.username = $1
		 GROUP BY u.user_id`,
		username,
	)

	var user UserRepository
	err := row.Scan(&user.UserID, &user.Username, &user.Email, &user.PasswordHash,
		&user.CreatedAt, &user.LastLogin, &user.IsActive, &user.Timezone, &user.Roles)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &user, nil
}

// GetUserByEmail retrieves a user by email
func (db *Database) GetUserByEmail(email string) (*UserRepository, error) {
	row := db.pool.QueryRow(
		context.Background(),
		`SELECT u.user_id, u.username, u.email, u.password_hash, u.created_at, u.last_login, u.is_active, u.timezone,
			 COALESCE(json_agg(ur.role_id) FILTER (WHERE ur.role_id IS NOT NULL), '[]'::json) as roles
		 FROM users u
		 LEFT JOIN user_roles ur ON u.user_id = ur.user_id
		 WHERE u.email = $1
		 GROUP BY u.user_id`,
		email,
	)

	var user UserRepository
	err := row.Scan(&user.UserID, &user.Username, &user.Email, &user.PasswordHash,
		&user.CreatedAt, &user.LastLogin, &user.IsActive, &user.Timezone, &user.Roles)
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

// UpdateUserProfile updates a user's username and email
func (db *Database) UpdateUserProfile(userID, username, email, timezone string) error {
	_, err := db.pool.Exec(
		context.Background(),
		`UPDATE users SET username = $1, email = $2, timezone = $3 WHERE user_id = $4`,
		username, email, timezone, userID,
	)
	if err != nil {
		return fmt.Errorf("failed to update user profile: %w", err)
	}
	return nil
}

// UpdateUserPassword updates a user's password hash
func (db *Database) UpdateUserPassword(userID, passwordHash string) error {
	_, err := db.pool.Exec(
		context.Background(),
		`UPDATE users SET password_hash = $1 WHERE user_id = $2`,
		passwordHash, userID,
	)
	if err != nil {
		return fmt.Errorf("failed to update user password: %w", err)
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
		`INSERT INTO access_keys (id, user_id, name, key_hash, permissions, created_at, expires_at, is_active, key_type)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, TRUE, 'session')
		 RETURNING id, user_id, name, key_hash, permissions, created_at, last_used, expires_at, is_active, key_type`,
		keyID, userID, name, keyHash, permissions, createdAt, expiresAt,
	)

	var key AccessKey
	err := row.Scan(&key.ID, &key.UserID, &key.Name, &key.KeyHash,
		&key.Permissions, &key.CreatedAt, &key.LastUsed, &key.ExpiresAt, &key.IsActive, &key.KeyType)
	if err != nil {
		return nil, fmt.Errorf("failed to create access key: %w", err)
	}
	return &key, nil
}

// CreatePersonalAccessToken creates a new user-managed access key whose
// secret is presented to the caller exactly once. Unlike CreateAccessKey
// (used for short-lived login sessions, which stores the raw token so it
// can be handed back to callers that already treat it as opaque), the
// caller here must pass tokenHash — the token's hash, computed with
// auth.HashToken before calling this method — so the raw secret is never
// persisted. scope is stored in the existing permissions column, e.g.
// []string{"read"} or []string{"read", "write"}.
func (db *Database) CreatePersonalAccessToken(userID, name, tokenHash string, scope []string, expiresAt *time.Time, description string) (*AccessKey, error) {
	keyID := generateUUID()
	createdAt := time.Now()

	row := db.pool.QueryRow(
		context.Background(),
		`INSERT INTO access_keys (id, user_id, name, key_hash, permissions, created_at, expires_at, is_active, key_type, description)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, TRUE, 'personal', $8)
		 RETURNING id, user_id, name, key_hash, permissions, created_at, last_used, expires_at, is_active, key_type, description`,
		keyID, userID, name, tokenHash, scope, createdAt, expiresAt, description,
	)

	var key AccessKey
	err := row.Scan(&key.ID, &key.UserID, &key.Name, &key.KeyHash,
		&key.Permissions, &key.CreatedAt, &key.LastUsed, &key.ExpiresAt, &key.IsActive, &key.KeyType, &key.Description)
	if err != nil {
		return nil, fmt.Errorf("failed to create personal access token: %w", err)
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
		 RETURNING id, user_id, name, key_hash, permissions, created_at, last_used, expires_at, is_active, key_type`,
		now, keyHash,
	)

	var key AccessKey
	err := row.Scan(&key.ID, &key.UserID, &key.Name, &key.KeyHash,
		&key.Permissions, &key.CreatedAt, &key.LastUsed, &key.ExpiresAt, &key.IsActive, &key.KeyType)
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

// ListUserAccessKeys lists all access keys for a user, of every key_type
// (session and personal alike). Intended for admin bulk-invalidation flows
// (e.g. deactivating a user must kill their login sessions too) — user-facing
// token management should use ListUserPersonalAccessTokens instead.
func (db *Database) ListUserAccessKeys(userID string) ([]AccessKey, error) {
	return db.listUserAccessKeys(userID, "")
}

// ListUserPersonalAccessTokens lists only the user-managed PATs for a user,
// excluding the login-session keys auth.Login creates on every sign-in.
func (db *Database) ListUserPersonalAccessTokens(userID string) ([]AccessKey, error) {
	return db.listUserAccessKeys(userID, "AND key_type = 'personal'")
}

func (db *Database) listUserAccessKeys(userID, extraWhere string) ([]AccessKey, error) {
	rows, err := db.pool.Query(
		context.Background(),
		`SELECT id, user_id, name, key_hash, permissions, created_at, last_used, expires_at, is_active, key_type, description
		 FROM access_keys WHERE user_id = $1 `+extraWhere+` ORDER BY created_at DESC`,
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
			&k.Permissions, &k.CreatedAt, &k.LastUsed, &k.ExpiresAt, &k.IsActive, &k.KeyType, &k.Description)
		if err != nil {
			return nil, fmt.Errorf("failed to scan access key: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// ListRegistries lists all enabled registries with ordering
func (db *Database) ListRegistries() ([]RegistryConfig, error) {
	return db.listRegistries("WHERE enabled = TRUE ")
}

// ListAllRegistries lists every registry regardless of enabled state, for
// admin management UIs that need to show (and re-enable) disabled
// registries rather than have them disappear entirely.
func (db *Database) ListAllRegistries() ([]RegistryConfig, error) {
	return db.listRegistries("")
}

func (db *Database) listRegistries(whereClause string) ([]RegistryConfig, error) {
	rows, err := db.pool.Query(
		context.Background(),
		`SELECT id, name, url, type, enabled, priority, private, proxy, COALESCE(host, ''),
		        upstream_auth_type, COALESCE(upstream_username, ''), (upstream_secret IS NOT NULL AND upstream_secret != '')
		 FROM registries `+whereClause+`ORDER BY priority ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list registries: %w", err)
	}
	defer rows.Close()

	var registries []RegistryConfig
	for rows.Next() {
		var r RegistryConfig
		err := rows.Scan(&r.ID, &r.Name, &r.URL, &r.Type, &r.Enabled, &r.Priority, &r.Private, &r.Proxy, &r.Host,
			&r.UpstreamAuthType, &r.UpstreamUsername, &r.HasUpstreamSecret)
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
		`SELECT id, name, url, type, enabled, priority, private, proxy, COALESCE(host, ''),
		        upstream_auth_type, COALESCE(upstream_username, ''), COALESCE(upstream_secret, '')
		 FROM registries WHERE id = $1`,
		id,
	)

	var r RegistryConfig
	err := row.Scan(&r.ID, &r.Name, &r.URL, &r.Type, &r.Enabled, &r.Priority, &r.Private, &r.Proxy, &r.Host,
		&r.UpstreamAuthType, &r.UpstreamUsername, &r.UpstreamSecret)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get registry: %w", err)
	}
	r.HasUpstreamSecret = r.UpstreamSecret != ""
	return &r, nil
}

// GetRegistryByHost retrieves the registry bound to the given hostname (no
// port), if any. Used for virtual-host-style routing so downstream clients
// can address a specific registry purely by the hostname they connect to,
// with no path prefix or repository/package renaming.
func (db *Database) GetRegistryByHost(host string) (*RegistryConfig, error) {
	if host == "" {
		return nil, nil
	}
	row := db.pool.QueryRow(
		context.Background(),
		`SELECT id, name, url, type, enabled, priority, private, proxy, COALESCE(host, ''),
		        upstream_auth_type, COALESCE(upstream_username, ''), COALESCE(upstream_secret, '')
		 FROM registries WHERE host = $1 AND enabled = TRUE`,
		host,
	)

	var r RegistryConfig
	err := row.Scan(&r.ID, &r.Name, &r.URL, &r.Type, &r.Enabled, &r.Priority, &r.Private, &r.Proxy, &r.Host,
		&r.UpstreamAuthType, &r.UpstreamUsername, &r.UpstreamSecret)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get registry by host: %w", err)
	}
	r.HasUpstreamSecret = r.UpstreamSecret != ""
	return &r, nil
}

// GetDefaultRegistry retrieves the highest-priority enabled, non-private,
// proxy-enabled registry of the given type — the fallback target for
// protocol-proxy requests that don't match any registry's bound Host.
func (db *Database) GetDefaultRegistry(artifactType string) (*RegistryConfig, error) {
	row := db.pool.QueryRow(
		context.Background(),
		`SELECT id, name, url, type, enabled, priority, private, proxy, COALESCE(host, ''),
		        upstream_auth_type, COALESCE(upstream_username, ''), COALESCE(upstream_secret, '')
		 FROM registries
		 WHERE type = $1 AND enabled = TRUE AND private = FALSE AND proxy = TRUE
		 ORDER BY priority ASC LIMIT 1`,
		artifactType,
	)

	var r RegistryConfig
	err := row.Scan(&r.ID, &r.Name, &r.URL, &r.Type, &r.Enabled, &r.Priority, &r.Private, &r.Proxy, &r.Host,
		&r.UpstreamAuthType, &r.UpstreamUsername, &r.UpstreamSecret)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get default registry: %w", err)
	}
	r.HasUpstreamSecret = r.UpstreamSecret != ""
	return &r, nil
}

// SaveRegistry saves registry configuration. UpstreamSecret is only written
// when non-empty, so editing a registry without retyping its credential
// (the standard "leave blank to keep existing" UX) doesn't clobber it.
func (db *Database) SaveRegistry(config *RegistryConfig) error {
	if config.UpstreamAuthType == "" {
		config.UpstreamAuthType = "none"
	}
	_, err := db.pool.Exec(
		context.Background(),
		`INSERT INTO registries (id, name, url, type, enabled, priority, private, proxy, host, upstream_auth_type, upstream_username, upstream_secret)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), $10, NULLIF($11, ''), NULLIF($12, ''))
		 ON CONFLICT (id)
		 DO UPDATE SET
		   name = EXCLUDED.name,
		   url = EXCLUDED.url,
		   type = EXCLUDED.type,
		   enabled = EXCLUDED.enabled,
		   priority = EXCLUDED.priority,
		   private = EXCLUDED.private,
		   proxy = EXCLUDED.proxy,
		   host = EXCLUDED.host,
		   upstream_auth_type = EXCLUDED.upstream_auth_type,
		   upstream_username = EXCLUDED.upstream_username,
		   upstream_secret = COALESCE(EXCLUDED.upstream_secret, registries.upstream_secret)`,
		config.ID, config.Name, config.URL, config.Type, config.Enabled, config.Priority, config.Private, config.Proxy, config.Host,
		config.UpstreamAuthType, config.UpstreamUsername, config.UpstreamSecret,
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

// GrantRegistryAccess creates or updates a user's read/publish grant on a
// private registry.
func (db *Database) GrantRegistryAccess(access *RegistryAccess) error {
	_, err := db.pool.Exec(
		context.Background(),
		`INSERT INTO registry_access (registry_id, user_id, can_read, can_publish)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (registry_id, user_id)
		 DO UPDATE SET
		   can_read = EXCLUDED.can_read,
		   can_publish = EXCLUDED.can_publish`,
		access.RegistryID, access.UserID, access.CanRead, access.CanPublish,
	)
	if err != nil {
		return fmt.Errorf("failed to grant registry access: %w", err)
	}
	return nil
}

// RevokeRegistryAccess removes a user's grant on a registry.
func (db *Database) RevokeRegistryAccess(registryID, userID string) error {
	_, err := db.pool.Exec(
		context.Background(),
		`DELETE FROM registry_access WHERE registry_id = $1 AND user_id = $2`,
		registryID, userID,
	)
	if err != nil {
		return fmt.Errorf("failed to revoke registry access: %w", err)
	}
	return nil
}

// GetRegistryAccess retrieves a single user's grant on a registry, if any.
func (db *Database) GetRegistryAccess(registryID, userID string) (*RegistryAccess, error) {
	row := db.pool.QueryRow(
		context.Background(),
		`SELECT registry_id, user_id, can_read, can_publish, granted_at FROM registry_access WHERE registry_id = $1 AND user_id = $2`,
		registryID, userID,
	)

	var a RegistryAccess
	err := row.Scan(&a.RegistryID, &a.UserID, &a.CanRead, &a.CanPublish, &a.GrantedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get registry access: %w", err)
	}
	return &a, nil
}

// ListRegistryAccessForRegistry lists all user grants on a registry.
func (db *Database) ListRegistryAccessForRegistry(registryID string) ([]RegistryAccess, error) {
	rows, err := db.pool.Query(
		context.Background(),
		`SELECT registry_id, user_id, can_read, can_publish, granted_at FROM registry_access WHERE registry_id = $1 ORDER BY granted_at ASC`,
		registryID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list registry access: %w", err)
	}
	defer rows.Close()

	var grants []RegistryAccess
	for rows.Next() {
		var a RegistryAccess
		if err := rows.Scan(&a.RegistryID, &a.UserID, &a.CanRead, &a.CanPublish, &a.GrantedAt); err != nil {
			return nil, fmt.Errorf("failed to scan registry access: %w", err)
		}
		grants = append(grants, a)
	}
	return grants, rows.Err()
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
			   digest, digest_algorithm, size, total_size, created, updated, metadata, tags, signatures, downloads
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
			&a.ArtifactName, &a.Version, &a.Digest, &a.DigestAlgorithm, &a.Size, &a.TotalSize,
			&a.Created, &a.Updated, &a.Metadata, &a.Tags, &a.Signatures, &a.Downloads)
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
			 digest, digest_algorithm, size, total_size, created, updated, metadata, tags, signatures, downloads
		 FROM artifacts WHERE id = $1`,
		id,
	)

	var artifact ArtifactMetadata
	err := row.Scan(
		&artifact.ID, &artifact.RegistryID, &artifact.ArtifactType,
		&artifact.Namespace, &artifact.ArtifactName, &artifact.Version,
		&artifact.Digest, &artifact.DigestAlgorithm, &artifact.Size, &artifact.TotalSize,
		&artifact.Created, &artifact.Updated, &artifact.Metadata, &artifact.Tags, &artifact.Signatures, &artifact.Downloads,
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

// CountUsers returns the total number of users in the system
func (db *Database) CountUsers() (int, error) {
	var count int
	err := db.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count users: %w", err)
	}
	return count, nil
}

// ListUsers returns all users
func (db *Database) ListUsers() ([]UserRepository, error) {
	rows, err := db.pool.Query(
		context.Background(),
		`SELECT u.user_id, u.username, u.email, u.password_hash, u.created_at, u.last_login, u.is_active, u.timezone,
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
			&u.CreatedAt, &u.LastLogin, &u.IsActive, &u.Timezone, &u.Roles)
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

	// registryIDs == non-nil-but-empty means the caller has no readable
	// registries at all (e.g. an anonymous caller with only private
	// registries configured) — short-circuit rather than running an
	// `= ANY($1)` with an empty array that Postgres would happily match zero
	// rows against anyway, just without the early return.
	if opts.RegistryIDs != nil && len(opts.RegistryIDs) == 0 {
		return nil, "", false, nil
	}

	// args[0] is always the LIMIT; every other placeholder is numbered
	// relative to its own append so this stays correct regardless of which
	// optional filters are present.
	args := []any{opts.Limit + 1}
	var conditions string

	if opts.RegistryIDs != nil {
		args = append(args, opts.RegistryIDs)
		conditions += fmt.Sprintf(" AND a.registry_id = ANY($%d)", len(args))
	}

	if opts.Namespace != "" {
		args = append(args, opts.Namespace)
		conditions += fmt.Sprintf(" AND a.namespace = $%d", len(args))
	}

	if opts.ArtifactType != "" {
		args = append(args, opts.ArtifactType)
		conditions += fmt.Sprintf(" AND a.artifact_type = $%d", len(args))
	}

	if opts.Cursor != "" {
		// Decode cursor and add WHERE condition
		cursorTime, _, err := decodeCursor(opts.Cursor)
		if err == nil {
			args = append(args, cursorTime)
			if opts.Order == "desc" {
				conditions += fmt.Sprintf(" AND a.%s < $%d", opts.OrderBy, len(args))
			} else {
				conditions += fmt.Sprintf(" AND a.%s > $%d", opts.OrderBy, len(args))
			}
		}
	}

	// Main query with LIMIT + 1 for "has next" detection
	query := fmt.Sprintf(`
		SELECT a.id, a.registry_id, a.artifact_type, a.namespace, a.artifact_name, a.version,
			   a.digest, a.digest_algorithm, a.size, a.total_size, a.created, a.updated, a.metadata, a.tags, a.signatures
		FROM artifacts a
		WHERE 1=1%s
		ORDER BY a.%s %s
		LIMIT $1`,
		conditions, opts.OrderBy, opts.Order)

	rows, err := db.pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, "", false, fmt.Errorf("failed to list artifacts: %w", err)
	}
	defer rows.Close()

	var artifacts []ArtifactMetadata
	for rows.Next() {
		var a ArtifactMetadata
		err := rows.Scan(&a.ID, &a.RegistryID, &a.ArtifactType, &a.Namespace,
			&a.ArtifactName, &a.Version, &a.Digest, &a.DigestAlgorithm, &a.Size, &a.TotalSize,
			&a.Created, &a.Updated, &a.Metadata, &a.Tags, &a.Signatures, &a.Downloads)
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
		SELECT u.user_id, u.username, u.email, u.password_hash, u.created_at, u.last_login, u.is_active, u.timezone,
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
			&u.CreatedAt, &u.LastLogin, &u.IsActive, &u.Timezone, &u.Roles)
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

// GetVulnDBSettings returns the singleton vulnerability-DB update settings row.
func (db *Database) GetVulnDBSettings() (*VulnDBSettings, error) {
	row := db.pool.QueryRow(
		context.Background(),
		`SELECT auto_update_enabled, update_interval_hours, last_checked_at, last_updated_at, last_error
		 FROM vulnerability_db_settings WHERE id = 1`,
	)

	var s VulnDBSettings
	err := row.Scan(&s.AutoUpdateEnabled, &s.UpdateIntervalHours, &s.LastCheckedAt, &s.LastUpdatedAt, &s.LastError)
	if err != nil {
		return nil, fmt.Errorf("failed to get vulnerability DB settings: %w", err)
	}
	return &s, nil
}

// UpdateVulnDBSettings persists the user-tunable fields (auto-update toggle
// and refresh interval). Status fields (last checked/updated/error) are only
// ever written by RecordVulnDBUpdateResult.
func (db *Database) UpdateVulnDBSettings(autoUpdateEnabled bool, intervalHours int) error {
	_, err := db.pool.Exec(
		context.Background(),
		`UPDATE vulnerability_db_settings SET auto_update_enabled = $1, update_interval_hours = $2 WHERE id = 1`,
		autoUpdateEnabled, intervalHours,
	)
	if err != nil {
		return fmt.Errorf("failed to update vulnerability DB settings: %w", err)
	}
	return nil
}

// RecordVulnDBUpdateResult stamps the outcome of a DB refresh attempt.
func (db *Database) RecordVulnDBUpdateResult(checkedAt time.Time, succeeded bool, errMsg string) error {
	var err error
	if succeeded {
		_, err = db.pool.Exec(
			context.Background(),
			`UPDATE vulnerability_db_settings SET last_checked_at = $1, last_updated_at = $1, last_error = '' WHERE id = 1`,
			checkedAt,
		)
	} else {
		_, err = db.pool.Exec(
			context.Background(),
			`UPDATE vulnerability_db_settings SET last_checked_at = $1, last_error = $2 WHERE id = 1`,
			checkedAt, errMsg,
		)
	}
	if err != nil {
		return fmt.Errorf("failed to record vulnerability DB update result: %w", err)
	}
	return nil
}

// GetVulnScanSettings returns the singleton vulnerability-scan settings row.
func (db *Database) GetVulnScanSettings() (*VulnScanSettings, error) {
	row := db.pool.QueryRow(
		context.Background(),
		`SELECT auto_scan_enabled, scan_interval_hours, last_checked_at, last_scan_at, last_error
		 FROM vulnerability_scan_settings WHERE id = 1`,
	)

	var s VulnScanSettings
	err := row.Scan(&s.AutoScanEnabled, &s.ScanIntervalHours, &s.LastCheckedAt, &s.LastScanAt, &s.LastError)
	if err != nil {
		return nil, fmt.Errorf("failed to get vulnerability scan settings: %w", err)
	}
	return &s, nil
}

// UpdateVulnScanSettings persists the user-tunable fields (auto-scan toggle
// and rescan interval). Status fields (last checked/scanned/error) are only
// ever written by RecordVulnScanResult.
func (db *Database) UpdateVulnScanSettings(autoScanEnabled bool, intervalHours int) error {
	_, err := db.pool.Exec(
		context.Background(),
		`UPDATE vulnerability_scan_settings SET auto_scan_enabled = $1, scan_interval_hours = $2 WHERE id = 1`,
		autoScanEnabled, intervalHours,
	)
	if err != nil {
		return fmt.Errorf("failed to update vulnerability scan settings: %w", err)
	}
	return nil
}

// RecordVulnScanResult stamps the outcome of a rescan sweep.
func (db *Database) RecordVulnScanResult(checkedAt time.Time, succeeded bool, errMsg string) error {
	var err error
	if succeeded {
		_, err = db.pool.Exec(
			context.Background(),
			`UPDATE vulnerability_scan_settings SET last_checked_at = $1, last_scan_at = $1, last_error = '' WHERE id = 1`,
			checkedAt,
		)
	} else {
		_, err = db.pool.Exec(
			context.Background(),
			`UPDATE vulnerability_scan_settings SET last_checked_at = $1, last_error = $2 WHERE id = 1`,
			checkedAt, errMsg,
		)
	}
	if err != nil {
		return fmt.Errorf("failed to record vulnerability scan result: %w", err)
	}
	return nil
}

// GetSearchIndexSettings returns the singleton search-index reindex settings row.
func (db *Database) GetSearchIndexSettings() (*SearchIndexSettings, error) {
	row := db.pool.QueryRow(
		context.Background(),
		`SELECT auto_reindex_enabled, reindex_interval_hours, last_checked_at, last_reindexed_at, last_artifact_count, last_error
		 FROM search_index_settings WHERE id = 1`,
	)

	var s SearchIndexSettings
	err := row.Scan(&s.AutoReindexEnabled, &s.ReindexIntervalHours, &s.LastCheckedAt, &s.LastReindexedAt, &s.LastArtifactCount, &s.LastError)
	if err != nil {
		return nil, fmt.Errorf("failed to get search index settings: %w", err)
	}
	return &s, nil
}

// UpdateSearchIndexSettings persists the user-tunable fields (auto-reindex
// toggle and refresh interval). Status fields are only ever written by
// RecordSearchIndexUpdateResult.
func (db *Database) UpdateSearchIndexSettings(autoReindexEnabled bool, intervalHours int) error {
	_, err := db.pool.Exec(
		context.Background(),
		`UPDATE search_index_settings SET auto_reindex_enabled = $1, reindex_interval_hours = $2 WHERE id = 1`,
		autoReindexEnabled, intervalHours,
	)
	if err != nil {
		return fmt.Errorf("failed to update search index settings: %w", err)
	}
	return nil
}

// RecordSearchIndexUpdateResult stamps the outcome of a reindex attempt.
func (db *Database) RecordSearchIndexUpdateResult(checkedAt time.Time, succeeded bool, artifactCount int, errMsg string) error {
	var err error
	if succeeded {
		_, err = db.pool.Exec(
			context.Background(),
			`UPDATE search_index_settings SET last_checked_at = $1, last_reindexed_at = $1, last_artifact_count = $2, last_error = '' WHERE id = 1`,
			checkedAt, artifactCount,
		)
	} else {
		_, err = db.pool.Exec(
			context.Background(),
			`UPDATE search_index_settings SET last_checked_at = $1, last_error = $2 WHERE id = 1`,
			checkedAt, errMsg,
		)
	}
	if err != nil {
		return fmt.Errorf("failed to record search index update result: %w", err)
	}
	return nil
}

// RebuildSearchIndex recomputes the search_vector tsvector for every
// artifact row from its current artifact_name/namespace, then returns how
// many rows were touched. Needed because rows written before search_vector
// existed (or via the legacy no-tsvector fallback in SaveArtifact) never get
// one populated otherwise.
func (db *Database) RebuildSearchIndex() (int, error) {
	tag, err := db.pool.Exec(
		context.Background(),
		`UPDATE artifacts SET search_vector = to_tsvector('english', artifact_name || ' ' || namespace)`,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to rebuild search index: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// GetBackupSettings returns the singleton scheduled-backup settings row.
func (db *Database) GetBackupSettings() (*BackupSettings, error) {
	row := db.pool.QueryRow(
		context.Background(),
		`SELECT auto_backup_enabled, backup_interval_hours, last_checked_at, last_backup_at, last_backup_path, last_error, storage_type, storage_config
		 FROM backup_settings WHERE id = 1`,
	)

	var s BackupSettings
	var storageConfigRaw []byte
	err := row.Scan(&s.AutoBackupEnabled, &s.BackupIntervalHours, &s.LastCheckedAt, &s.LastBackupAt, &s.LastBackupPath, &s.LastError, &s.StorageType, &storageConfigRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to get backup settings: %w", err)
	}
	s.StorageConfig = map[string]string{}
	if len(storageConfigRaw) > 0 {
		if err := json.Unmarshal(storageConfigRaw, &s.StorageConfig); err != nil {
			return nil, fmt.Errorf("failed to parse backup storage config: %w", err)
		}
	}
	return &s, nil
}

// UpdateBackupSettings persists the user-tunable fields (auto-backup toggle
// and interval). Status fields are only ever written by RecordBackupResult.
func (db *Database) UpdateBackupSettings(autoBackupEnabled bool, intervalHours int) error {
	_, err := db.pool.Exec(
		context.Background(),
		`UPDATE backup_settings SET auto_backup_enabled = $1, backup_interval_hours = $2 WHERE id = 1`,
		autoBackupEnabled, intervalHours,
	)
	if err != nil {
		return fmt.Errorf("failed to update backup settings: %w", err)
	}
	return nil
}

// UpdateBackupStorageSettings persists the backup destination: storageType
// ("" reuses the main artifact storage adapter, or "local"/"s3"/"gcs"/"azure")
// and its config map (storage.New's key/value config for that type).
func (db *Database) UpdateBackupStorageSettings(storageType string, storageConfig map[string]string) error {
	if storageConfig == nil {
		storageConfig = map[string]string{}
	}
	raw, err := json.Marshal(storageConfig)
	if err != nil {
		return fmt.Errorf("failed to encode backup storage config: %w", err)
	}
	_, err = db.pool.Exec(
		context.Background(),
		`UPDATE backup_settings SET storage_type = $1, storage_config = $2 WHERE id = 1`,
		storageType, raw,
	)
	if err != nil {
		return fmt.Errorf("failed to update backup storage settings: %w", err)
	}
	return nil
}

// RecordBackupResult stamps the outcome of a backup attempt.
func (db *Database) RecordBackupResult(checkedAt time.Time, succeeded bool, path string, errMsg string) error {
	var err error
	if succeeded {
		_, err = db.pool.Exec(
			context.Background(),
			`UPDATE backup_settings SET last_checked_at = $1, last_backup_at = $1, last_backup_path = $2, last_error = '' WHERE id = 1`,
			checkedAt, path,
		)
	} else {
		_, err = db.pool.Exec(
			context.Background(),
			`UPDATE backup_settings SET last_checked_at = $1, last_error = $2 WHERE id = 1`,
			checkedAt, errMsg,
		)
	}
	if err != nil {
		return fmt.Errorf("failed to record backup result: %w", err)
	}
	return nil
}

// backupTables lists every application table included in a full logical
// backup, in FK-safe insert order (parents before children). There is no
// schema introspection here -- a new table needs a line added -- which
// keeps dump/restore behavior predictable and reviewable rather than
// silently picking up whatever information_schema returns.
var backupTables = []string{
	"roles", "permissions", "role_permissions",
	"users", "user_roles", "access_keys",
	"registries", "registry_access",
	"artifacts", "vulnerability_scans", "audit_log",
	"vulnerability_db_settings", "search_index_settings",
	"vulnerability_scan_settings", "backup_settings",
}

// DumpStatements returns cargobay's own logical database dump: a TRUNCATE
// for every backupTables entry (child-to-parent order, so each succeeds
// without needing CASCADE) followed by one INSERT per row, in backupTables
// (parent-to-child) order. This is not pg_dump-compatible -- the runtime
// image doesn't bundle the pg_dump/pg_restore client tools (see Dockerfile,
// which only adds the trivy CLI) -- so backend/pkg/backup uses this instead
// of shelling out, and writes the result through the existing
// storage.StorageAdapter.
func (db *Database) DumpStatements(ctx context.Context) ([]string, error) {
	statements := make([]string, 0, len(backupTables)*2)

	for i := len(backupTables) - 1; i >= 0; i-- {
		statements = append(statements, fmt.Sprintf("TRUNCATE TABLE %s", backupTables[i]))
	}

	for _, table := range backupTables {
		rows, err := db.pool.Query(ctx, fmt.Sprintf("SELECT * FROM %s", table))
		if err != nil {
			return nil, fmt.Errorf("failed to read table %s: %w", table, err)
		}

		fields := rows.FieldDescriptions()
		columns := make([]string, len(fields))
		for i, f := range fields {
			columns[i] = string(f.Name)
		}

		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				rows.Close()
				return nil, fmt.Errorf("failed to read row from %s: %w", table, err)
			}
			literals := make([]string, len(values))
			for i, v := range values {
				literals[i] = sqlLiteral(v)
			}
			statements = append(statements, fmt.Sprintf(
				"INSERT INTO %s (%s) VALUES (%s)",
				table, strings.Join(columns, ", "), strings.Join(literals, ", "),
			))
		}
		rowErr := rows.Err()
		rows.Close()
		if rowErr != nil {
			return nil, fmt.Errorf("failed to read table %s: %w", table, rowErr)
		}
	}

	return statements, nil
}

// RestoreStatements runs every statement produced by DumpStatements (as
// loaded from a stored backup) inside a single transaction, so a failure
// partway through leaves the database exactly as it was rather than
// half-restored.
func (db *Database) RestoreStatements(ctx context.Context, statements []string) error {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin restore transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, stmt := range statements {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("restore statement failed: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// sqlLiteral formats a single pgx-decoded column value as a SQL literal for
// use in a generated INSERT statement. Untyped string/array literals are
// implicitly cast to the target column's real type (jsonb, text[], etc.) by
// Postgres, so this doesn't need to know each column's declared type.
func sqlLiteral(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return "NULL"
	case bool:
		if val {
			return "TRUE"
		}
		return "FALSE"
	case string:
		return quoteSQLString(val)
	case []byte:
		return quoteSQLString(string(val))
	case int16:
		return strconv.FormatInt(int64(val), 10)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case int:
		return strconv.Itoa(val)
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case time.Time:
		return quoteSQLString(val.UTC().Format(time.RFC3339Nano))
	case []string:
		return quoteSQLString(pgTextArrayLiteral(val))
	default:
		encoded, err := json.Marshal(val)
		if err != nil {
			return "NULL"
		}
		return quoteSQLString(string(encoded))
	}
}

func quoteSQLString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// pgTextArrayLiteral renders a Postgres array literal body (without the
// surrounding SQL string quotes) for a text[] column value.
func pgTextArrayLiteral(items []string) string {
	parts := make([]string, len(items))
	for i, item := range items {
		esc := strings.ReplaceAll(item, `\`, `\\`)
		esc = strings.ReplaceAll(esc, `"`, `\"`)
		parts[i] = `"` + esc + `"`
	}
	return "{" + strings.Join(parts, ",") + "}"
}
