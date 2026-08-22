package migrate

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
)

// baseDSN points at the same Postgres server the rest of the test suite
// uses (see docker-compose.yml). Override with TEST_DATABASE_URL to point
// elsewhere (e.g. CI). Tests that need it skip cleanly if the server isn't
// reachable, rather than failing the whole package.
func baseDSN() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://cargobay:password@localhost:5432/postgres"
}

// withTestDB creates a fresh, empty database on the same server as
// baseDSN, hands the caller a *database.Database connected to it, and
// drops it afterward. Skips the test if no Postgres server is reachable.
func withTestDB(t *testing.T) *database.Database {
	t.Helper()

	adminCfg, err := pgxpool.ParseConfig(baseDSN())
	require.NoError(t, err)
	adminCfg.MaxConns = 2

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	adminPool, err := pgxpool.NewWithConfig(ctx, adminCfg)
	if err != nil {
		t.Skipf("no postgres server reachable at %s: %v", baseDSN(), err)
	}
	defer adminPool.Close()
	if err := adminPool.Ping(ctx); err != nil {
		t.Skipf("no postgres server reachable at %s: %v", baseDSN(), err)
	}

	dbName := fmt.Sprintf("cargobay_migrate_test_%d", time.Now().UnixNano())
	_, err = adminPool.Exec(ctx, "CREATE DATABASE "+dbName)
	require.NoError(t, err, "failed to create test database")

	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Terminate any lingering connections before dropping, otherwise
		// DROP DATABASE fails with "database is being accessed by others".
		_, _ = adminPool.Exec(dropCtx,
			`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`,
			dbName)
		_, _ = adminPool.Exec(dropCtx, "DROP DATABASE IF EXISTS "+dbName)
	})

	testDSN := replaceDBName(baseDSN(), dbName)
	db := database.New(testDSN)
	require.NoError(t, db.Connect())
	t.Cleanup(db.Disconnect)

	return db
}

// replaceDBName swaps the path component of a postgres DSN. baseDSN always
// ends in "/postgres" (the connection used to issue CREATE/DROP DATABASE),
// so this is a plain suffix replace rather than a full URL parse.
func replaceDBName(dsn, newName string) string {
	const suffix = "/postgres"
	if len(dsn) >= len(suffix) && dsn[len(dsn)-len(suffix):] == suffix {
		return dsn[:len(dsn)-len(suffix)] + "/" + newName
	}
	return dsn + "/" + newName
}

func TestApplyOnFreshDatabase(t *testing.T) {
	db := withTestDB(t)
	ctx := context.Background()
	r := New(db)

	existing, err := r.HasExistingSchema(ctx)
	require.NoError(t, err)
	require.False(t, existing, "a freshly created database should report no existing schema")

	pending, err := r.Pending(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, pending, "a fresh database should have every migration pending")

	applied, err := r.Apply(ctx)
	require.NoError(t, err)
	require.Equal(t, pending, applied)

	// Core tables from the baseline schema plus a couple of specific
	// migrations should now exist.
	for _, table := range []string{"artifacts", "roles", "permissions", "vulnerability_scans", "backup_settings"} {
		var name *string
		err := db.QueryRow(ctx, fmt.Sprintf(`SELECT to_regclass('public.%s')::text`, table)).Scan(&name)
		require.NoError(t, err)
		require.NotNilf(t, name, "expected table %s to exist after Apply", table)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	db := withTestDB(t)
	ctx := context.Background()
	r := New(db)

	_, err := r.Apply(ctx)
	require.NoError(t, err)

	// A second Apply on an already-migrated database should apply nothing
	// and error on nothing (schema.sql is safe to re-run; no pending
	// migrations remain).
	applied, err := r.Apply(ctx)
	require.NoError(t, err)
	require.Empty(t, applied)

	pending, err := r.Pending(ctx)
	require.NoError(t, err)
	require.Empty(t, pending)

	existing, err := r.HasExistingSchema(ctx)
	require.NoError(t, err)
	require.True(t, existing, "a migrated database should report existing schema")
}

func TestMigrationFilesAreSorted(t *testing.T) {
	files, err := migrationFiles()
	require.NoError(t, err)
	require.NotEmpty(t, files)

	for i := 1; i < len(files); i++ {
		require.Less(t, files[i-1], files[i], "migration filenames must sort into apply order")
	}
}
