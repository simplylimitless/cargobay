// Package migrate applies the baseline schema and versioned migration
// files (embedded from ./sql) against a Postgres database, tracking what
// has already been applied in a schema_migrations table so re-running is
// a no-op. It knows nothing about backups -- callers that want a safety
// net (e.g. cmd/server) should take one before calling Apply.
package migrate

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed sql/schema.sql
var schemaSQL string

//go:embed sql/migrations/*.sql
var migrationsFS embed.FS

const migrationsDir = "sql/migrations"

// trackingDDL creates the table Apply uses to record which migration
// files have already run. Applying it is itself idempotent, so it's safe
// to run on every startup ahead of computing what's pending.
const trackingDDL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version text PRIMARY KEY,
	applied_at timestamptz NOT NULL DEFAULT now()
)`

// db is the subset of *database.Database that Runner needs. Narrowed to
// an interface, matching pkg/backup's backupStore, so tests can run
// against a real Postgres connection without pulling in the rest of the
// database package's API surface.
type db interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Runner applies the baseline schema and pending migration files.
type Runner struct {
	db db
}

// New creates a Runner backed by db (typically *database.Database).
func New(d db) *Runner {
	return &Runner{db: d}
}

// migrationFiles returns the embedded migration filenames in apply order
// (lexical sort, which matches their zero-padded numeric prefixes).
func migrationFiles() ([]string, error) {
	entries, err := migrationsFS.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded migrations: %w", err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	return files, nil
}

// HasExistingSchema reports whether the target database already has
// application tables (checked via the "artifacts" table, present in the
// very first schema). Callers use this to decide whether a database is
// truly empty -- and therefore safe to migrate without a preceding
// backup -- or already holds a deployment's data.
func (r *Runner) HasExistingSchema(ctx context.Context) (bool, error) {
	var name *string
	err := r.db.QueryRow(ctx, `SELECT to_regclass('public.artifacts')::text`).Scan(&name)
	if err != nil {
		return false, fmt.Errorf("failed to check for existing schema: %w", err)
	}
	return name != nil, nil
}

// appliedVersions ensures the tracking table exists and returns the set
// of migration filenames already recorded in it.
func (r *Runner) appliedVersions(ctx context.Context) (map[string]bool, error) {
	if _, err := r.db.Exec(ctx, trackingDDL); err != nil {
		return nil, fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	rows, err := r.db.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("failed to scan schema_migrations row: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read schema_migrations: %w", err)
	}
	return applied, nil
}

// Pending returns the migration filenames that have not yet been applied,
// in the order they will be applied.
func (r *Runner) Pending(ctx context.Context) ([]string, error) {
	applied, err := r.appliedVersions(ctx)
	if err != nil {
		return nil, err
	}
	files, err := migrationFiles()
	if err != nil {
		return nil, err
	}

	pending := make([]string, 0, len(files))
	for _, f := range files {
		if !applied[f] {
			pending = append(pending, f)
		}
	}
	return pending, nil
}

// Apply runs the baseline schema (idempotent, safe to re-run every
// startup) and then every pending migration file, each inside its own
// transaction that also records the file's name in schema_migrations. It
// stops at the first failure, leaving schema_migrations accurately
// reflecting what actually committed. Returns the filenames it applied.
func (r *Runner) Apply(ctx context.Context) ([]string, error) {
	if err := r.applyStatements(ctx, "schema.sql", schemaSQL); err != nil {
		return nil, err
	}

	pending, err := r.Pending(ctx)
	if err != nil {
		return nil, err
	}

	applied := make([]string, 0, len(pending))
	for _, name := range pending {
		content, err := migrationsFS.ReadFile(migrationsDir + "/" + name)
		if err != nil {
			return applied, fmt.Errorf("failed to read migration %s: %w", name, err)
		}
		if err := r.applyMigration(ctx, name, string(content)); err != nil {
			return applied, err
		}
		applied = append(applied, name)
	}
	return applied, nil
}

// applyStatements runs sql inside its own transaction. Used for the
// baseline schema, which is not itself tracked in schema_migrations.
func (r *Runner) applyStatements(ctx context.Context, label, sql string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for %s: %w", label, err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to apply %s: %w", label, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit %s: %w", label, err)
	}
	return nil
}

// applyMigration runs one migration file's SQL and records it in
// schema_migrations inside a single transaction, so a crash mid-migration
// can never leave a file half-applied yet marked done (or vice versa).
func (r *Runner) applyMigration(ctx context.Context, name, sql string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for migration %s: %w", name, err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to apply migration %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
		return fmt.Errorf("failed to record migration %s: %w", name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit migration %s: %w", name, err)
	}
	return nil
}
