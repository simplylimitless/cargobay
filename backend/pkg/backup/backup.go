// Package backup implements full logical database backups: dumping every
// application table via database.Database.DumpStatements, storing the
// result through the existing storage.StorageAdapter (so backups can land
// on S3/GCS/Azure/local disk exactly like artifact blobs, without a
// separate storage abstraction), and restoring from a previously stored
// backup. There is no pg_dump/pg_restore involved -- the runtime image is
// alpine-based and only bundles the trivy CLI (see Dockerfile), not the
// Postgres client tools -- so the dump/restore logic is pure Go/database-sql
// (see database.Database.DumpStatements/RestoreStatements).
package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
)

// backupBucket is the storage.StorageAdapter bucket/prefix backups are
// written under, distinct from the buckets artifact blobs use.
const backupBucket = "backups"

// dumpMagic tags the archive format so Restore can refuse anything that
// isn't one of our own backups (e.g. a stray file uploaded into the same
// storage prefix) with a clear error instead of a confusing parse failure.
const dumpMagic = "CBBK1"

// backupStore is the subset of *database.Database's methods Backup depends
// on. Narrowed to an interface so tests can substitute a fake and exercise
// Run/Restore/StartScheduler without a live Postgres connection.
type backupStore interface {
	GetBackupSettings() (*database.BackupSettings, error)
	RecordBackupResult(checkedAt time.Time, succeeded bool, path string, errMsg string) error
	DumpStatements(ctx context.Context) ([]string, error)
	RestoreStatements(ctx context.Context, statements []string) error
}

// BackupInfo describes one stored backup archive.
type BackupInfo struct {
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"createdAt"`
}

// Backup performs full logical database backups/restores and writes/reads
// them through a pluggable storage.StorageAdapter.
type Backup struct {
	db             backupStore
	defaultStorage storage.StorageAdapter
}

// New creates a Backup. defaultStorage is used unless the current
// backup_settings row specifies a dedicated storage type (configured from
// Settings), in which case resolveStorage builds an independent adapter
// for that instead.
func New(db *database.Database, defaultStorage storage.StorageAdapter) *Backup {
	return &Backup{db: db, defaultStorage: defaultStorage}
}

// resolveStorage picks the adapter a backup operation should use: the
// dedicated one from settings.StorageType/StorageConfig if configured,
// otherwise defaultStorage. Re-evaluated on every call (rather than cached
// on the struct) so a storage change made via Settings takes effect
// immediately, without a restart.
func (b *Backup) resolveStorage(settings *database.BackupSettings) (storage.StorageAdapter, error) {
	if settings.StorageType == "" {
		return b.defaultStorage, nil
	}
	return storage.New(settings.StorageType, settings.StorageConfig)
}

// Run performs a single synchronous backup: dump every table, compress the
// result, write it to storage under a timestamped path, and record the
// outcome (last checked/backed-up/error/path) in backup_settings.
func (b *Backup) Run(ctx context.Context) error {
	checkedAt := time.Now()

	settings, err := b.db.GetBackupSettings()
	if err != nil {
		b.recordFailure(checkedAt, err)
		return err
	}

	dest, err := b.resolveStorage(settings)
	if err != nil {
		wrapped := fmt.Errorf("failed to resolve backup storage: %w", err)
		b.recordFailure(checkedAt, wrapped)
		return wrapped
	}

	statements, err := b.db.DumpStatements(ctx)
	if err != nil {
		b.recordFailure(checkedAt, err)
		return err
	}

	archive, err := encodeArchive(statements)
	if err != nil {
		b.recordFailure(checkedAt, err)
		return err
	}

	key := fmt.Sprintf("cargobay-%s.sql.gz", checkedAt.UTC().Format("20060102-150405"))
	if err := dest.Upload(backupBucket, key, archive, "application/gzip"); err != nil {
		wrapped := fmt.Errorf("failed to store backup: %w", err)
		b.recordFailure(checkedAt, wrapped)
		return wrapped
	}

	if recErr := b.db.RecordBackupResult(checkedAt, true, key, ""); recErr != nil {
		log.Printf("failed to record backup success: %v", recErr)
	}
	return nil
}

func (b *Backup) recordFailure(checkedAt time.Time, err error) {
	if recErr := b.db.RecordBackupResult(checkedAt, false, "", err.Error()); recErr != nil {
		log.Printf("failed to record backup failure: %v", recErr)
	}
}

// Restore loads a previously stored backup by its path (as returned by
// List) and replaces the entire contents of every backed-up table with it,
// inside a single transaction -- a failure partway through leaves the
// database exactly as it was rather than half-restored.
func (b *Backup) Restore(ctx context.Context, path string) error {
	if path == "" {
		return fmt.Errorf("backup path is required")
	}

	settings, err := b.db.GetBackupSettings()
	if err != nil {
		return fmt.Errorf("failed to load backup settings: %w", err)
	}
	src, err := b.resolveStorage(settings)
	if err != nil {
		return fmt.Errorf("failed to resolve backup storage: %w", err)
	}

	archive, err := src.Download(backupBucket, path)
	if err != nil {
		return fmt.Errorf("failed to load backup %q: %w", path, err)
	}

	statements, err := decodeArchive(archive)
	if err != nil {
		return fmt.Errorf("failed to parse backup %q: %w", path, err)
	}

	if err := b.db.RestoreStatements(ctx, statements); err != nil {
		return fmt.Errorf("failed to restore backup %q: %w", path, err)
	}
	return nil
}

// List returns every stored backup, newest first.
func (b *Backup) List(ctx context.Context) ([]BackupInfo, error) {
	settings, err := b.db.GetBackupSettings()
	if err != nil {
		return nil, fmt.Errorf("failed to load backup settings: %w", err)
	}
	src, err := b.resolveStorage(settings)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve backup storage: %w", err)
	}

	keys, err := src.ListFiles(backupBucket, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}

	infos := make([]BackupInfo, 0, len(keys))
	for _, key := range keys {
		name := filepath.Base(key)
		infos = append(infos, BackupInfo{Path: name, CreatedAt: parseBackupTimestamp(name)})
	}

	sort.Slice(infos, func(i, j int) bool { return infos[i].CreatedAt.After(infos[j].CreatedAt) })
	return infos, nil
}

// StartScheduler runs a background loop that polls the current settings (so
// changes made via the API take effect without a restart) and triggers Run
// whenever auto-backup is enabled and the configured interval has elapsed
// since the last check. Blocks until ctx is cancelled; call it in a
// goroutine.
func (b *Backup) StartScheduler(ctx context.Context) {
	const pollInterval = time.Minute

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			settings, err := b.db.GetBackupSettings()
			if err != nil {
				log.Printf("backup scheduler: failed to load settings: %v", err)
				continue
			}
			if !settings.AutoBackupEnabled {
				continue
			}

			if !backupDue(settings.LastCheckedAt, settings.BackupIntervalHours, time.Now()) {
				continue
			}

			if err := b.Run(ctx); err != nil {
				log.Printf("backup scheduler: backup failed: %v", err)
			} else {
				log.Printf("backup scheduler: backup succeeded")
			}
		}
	}
}

// backupDue decides whether a backup is due: true if there is no record of
// a prior check, or the configured interval has elapsed since the last one.
// Extracted as a pure function (no clock/DB access of its own) so the
// scheduling decision can be unit-tested directly.
func backupDue(lastCheckedAt *time.Time, intervalHours int, now time.Time) bool {
	if lastCheckedAt == nil {
		return true
	}
	return now.Sub(*lastCheckedAt) >= time.Duration(intervalHours)*time.Hour
}

// parseBackupTimestamp recovers the backup's creation time from its
// "cargobay-<timestamp>.sql.gz" filename. Falls back to the zero time for
// anything that doesn't match, so a stray file in the backups bucket
// doesn't fail the whole listing -- it just sorts last.
func parseBackupTimestamp(name string) time.Time {
	trimmed := strings.TrimSuffix(name, ".sql.gz")
	trimmed = strings.TrimPrefix(trimmed, "cargobay-")
	t, err := time.Parse("20060102-150405", trimmed)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// encodeArchive serializes statements into cargobay's own length-framed
// backup format, then gzip-compresses it. Length-framing (rather than a
// newline/semicolon delimiter) is deliberate: a dumped column value can
// itself legally contain a newline or semicolon, so any textual delimiter
// would risk mis-splitting a statement on restore.
func encodeArchive(statements []string) ([]byte, error) {
	var raw bytes.Buffer
	raw.WriteString(dumpMagic)
	for _, stmt := range statements {
		var lenBuf [8]byte
		binary.BigEndian.PutUint64(lenBuf[:], uint64(len(stmt)))
		raw.Write(lenBuf[:])
		raw.WriteString(stmt)
	}

	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	if _, err := w.Write(raw.Bytes()); err != nil {
		return nil, fmt.Errorf("failed to compress backup: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to compress backup: %w", err)
	}
	return gz.Bytes(), nil
}

// decodeArchive is the inverse of encodeArchive.
func decodeArchive(archive []byte) ([]string, error) {
	gr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("failed to decompress backup: %w", err)
	}
	defer gr.Close()

	raw, err := io.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress backup: %w", err)
	}

	if len(raw) < len(dumpMagic) || string(raw[:len(dumpMagic)]) != dumpMagic {
		return nil, fmt.Errorf("not a cargobay backup archive")
	}
	r := bytes.NewReader(raw[len(dumpMagic):])

	var statements []string
	for {
		var lenBuf [8]byte
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("corrupt backup archive: %w", err)
		}
		n := binary.BigEndian.Uint64(lenBuf[:])
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, fmt.Errorf("corrupt backup archive: %w", err)
		}
		statements = append(statements, string(buf))
	}
	return statements, nil
}
