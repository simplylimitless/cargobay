package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gzipT gzip-compresses raw bytes, for building archive-shaped test fixtures
// (decodeArchive always expects a gzip stream, even for malformed inputs).
func gzipT(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, err := w.Write(raw)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// gunzipT decompresses a gzip stream produced by encodeArchive, for
// inspecting the uncompressed archive bytes directly.
func gunzipT(t *testing.T, archive []byte) []byte {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(archive))
	require.NoError(t, err)
	defer r.Close()
	raw, err := io.ReadAll(r)
	require.NoError(t, err)
	return raw
}

// MockBackupStore is a mock implementation of backupStore for testing
type MockBackupStore struct {
	getBackupSettingsFunc  func() (*database.BackupSettings, error)
	recordBackupResultFunc func(time.Time, bool, string, string) error
	dumpStatementsFunc     func(context.Context) ([]string, error)
	restoreStatementsFunc  func(context.Context, []string) error
	settings               *database.BackupSettings
	dumpStatementsError    error
	restoreStatementsError error
}

// GetBackupSettings implements backupStore interface
func (m *MockBackupStore) GetBackupSettings() (*database.BackupSettings, error) {
	if m.getBackupSettingsFunc != nil {
		return m.getBackupSettingsFunc()
	}
	if m.settings == nil {
		now := time.Now()
		m.settings = &database.BackupSettings{
			AutoBackupEnabled:   true,
			BackupIntervalHours: 24,
			StorageType:         "",
			StorageConfig:       map[string]string{},
			LastCheckedAt:       &now,
		}
	}
	return m.settings, nil
}

// RecordBackupResult implements backupStore interface
func (m *MockBackupStore) RecordBackupResult(checkedAt time.Time, succeeded bool, path string, errMsg string) error {
	if m.recordBackupResultFunc != nil {
		return m.recordBackupResultFunc(checkedAt, succeeded, path, errMsg)
	}
	return nil
}

// DumpStatements implements backupStore interface
func (m *MockBackupStore) DumpStatements(ctx context.Context) ([]string, error) {
	if m.dumpStatementsFunc != nil {
		return m.dumpStatementsFunc(ctx)
	}
	return []string{"SELECT 1"}, m.dumpStatementsError
}

// RestoreStatements implements backupStore interface
func (m *MockBackupStore) RestoreStatements(ctx context.Context, statements []string) error {
	if m.restoreStatementsFunc != nil {
		return m.restoreStatementsFunc(ctx, statements)
	}
	return m.restoreStatementsError
}

// MockStorageAdapter is a mock implementation of storage.StorageAdapter for testing
type MockStorageAdapter struct {
	uploadFunc    func(string, string, []byte, string) error
	downloadFunc  func(string, string) ([]byte, error)
	listFilesFunc func(string, string) ([]string, error)
}

// Upload implements storage.StorageAdapter interface
func (m *MockStorageAdapter) Upload(bucket, key string, data []byte, contentType string) error {
	if m.uploadFunc != nil {
		return m.uploadFunc(bucket, key, data, contentType)
	}
	return nil
}

// Download implements storage.StorageAdapter interface
func (m *MockStorageAdapter) Download(bucket, key string) ([]byte, error) {
	if m.downloadFunc != nil {
		return m.downloadFunc(bucket, key)
	}
	return nil, nil
}

// DeleteFile implements storage.StorageAdapter interface
func (m *MockStorageAdapter) DeleteFile(bucket, key string) error {
	return nil
}

// ListFiles implements storage.StorageAdapter interface
func (m *MockStorageAdapter) ListFiles(bucket, prefix string) ([]string, error) {
	if m.listFilesFunc != nil {
		return m.listFilesFunc(bucket, prefix)
	}
	return []string{}, nil
}

// Connect implements storage.StorageAdapter interface
func (m *MockStorageAdapter) Connect() error {
	return nil
}

// Disconnect implements storage.StorageAdapter interface
func (m *MockStorageAdapter) Disconnect() error {
	return nil
}

// GetArtifact implements storage.StorageAdapter interface
func (m *MockStorageAdapter) GetArtifact(registryID, namespace, artifactName, version string) ([]byte, error) {
	return nil, nil
}

// SaveArtifact implements storage.StorageAdapter interface
func (m *MockStorageAdapter) SaveArtifact(registryID, namespace, artifactName, version string, data []byte) (string, error) {
	return "", nil
}

// GetArtifactStream implements storage.StorageAdapter interface
func (m *MockStorageAdapter) GetArtifactStream(registryID, namespace, artifactName, version string) (io.ReadCloser, error) {
	data, err := m.GetArtifact(registryID, namespace, artifactName, version)
	if err != nil || data == nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// SaveArtifactStream implements storage.StorageAdapter interface
func (m *MockStorageAdapter) SaveArtifactStream(registryID, namespace, artifactName, version string, r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return m.SaveArtifact(registryID, namespace, artifactName, version, data)
}

// DeleteArtifact implements storage.StorageAdapter interface
func (m *MockStorageAdapter) DeleteArtifact(registryID, namespace, artifactName, version string) error {
	return nil
}

// ArtifactExists implements storage.StorageAdapter interface
func (m *MockStorageAdapter) ArtifactExists(registryID, namespace, artifactName, version string) (bool, error) {
	return false, nil
}

// TestNewBackup tests creating a new Backup instance
func TestNewBackup(t *testing.T) {
	mockStore := &MockBackupStore{}
	mockStorage := &MockStorageAdapter{}

	backup := New(mockStore, mockStorage)

	assert.NotNil(t, backup)
	assert.Equal(t, mockStore, backup.db)
	assert.Equal(t, mockStorage, backup.defaultStorage)
}

// TestBackupInfoStruct tests BackupInfo struct
func TestBackupInfoStruct(t *testing.T) {
	now := time.Now()
	info := BackupInfo{
		Path:      "cargobay-20240101-120000.sql.gz",
		CreatedAt: now,
	}

	assert.Equal(t, "cargobay-20240101-120000.sql.gz", info.Path)
	assert.Equal(t, now, info.CreatedAt)
}

// TestEncodeArchive tests encoding statements into backup archive
func TestEncodeArchive(t *testing.T) {
	statements := []string{
		"CREATE TABLE users (id SERIAL PRIMARY KEY)",
		"INSERT INTO users VALUES (1, 'test')",
	}

	archive, err := encodeArchive(statements)
	require.NoError(t, err)
	assert.NotNil(t, archive)

	// Verify magic header (archive is gzip-compressed, so decompress first)
	raw := gunzipT(t, archive)
	assert.Equal(t, "CBBK1", string(raw[:5]))
}

// TestEncodeArchiveEmpty tests encoding empty statements
func TestEncodeArchiveEmpty(t *testing.T) {
	statements := []string{}

	archive, err := encodeArchive(statements)
	require.NoError(t, err)
	assert.NotNil(t, archive)
}

// TestEncodeArchiveMultipleStatements tests encoding multiple statements
func TestEncodeArchiveMultipleStatements(t *testing.T) {
	statements := []string{
		"CREATE TABLE users (id SERIAL)",
		"CREATE TABLE artifacts (id SERIAL)",
		"CREATE TABLE registries (id SERIAL)",
	}

	archive, err := encodeArchive(statements)
	require.NoError(t, err)

	// Decode and verify
	decoded, err := decodeArchive(archive)
	require.NoError(t, err)
	assert.Equal(t, len(statements), len(decoded))
	assert.Equal(t, statements, decoded)
}

// TestDecodeArchiveSuccess tests decoding a valid archive
func TestDecodeArchiveSuccess(t *testing.T) {
	statements := []string{"SELECT 1", "SELECT 2"}
	archive, err := encodeArchive(statements)
	require.NoError(t, err)

	decoded, err := decodeArchive(archive)
	require.NoError(t, err)
	assert.Equal(t, statements, decoded)
}

// TestDecodeArchiveInvalidMagic tests decoding archive with invalid magic
func TestDecodeArchiveInvalidMagic(t *testing.T) {
	invalid := gzipT(t, []byte("INVALID123"))
	decoded, err := decodeArchive(invalid)
	assert.Error(t, err)
	assert.Nil(t, decoded)
	assert.Contains(t, err.Error(), "not a cargobay backup archive")
}

// TestDecodeArchiveEmpty tests decoding empty archive
func TestDecodeArchiveEmpty(t *testing.T) {
	archive := []byte{}
	decoded, err := decodeArchive(archive)
	assert.Error(t, err)
	assert.Nil(t, decoded)
}

// TestDecodeArchiveCorruptLength tests decoding archive with corrupt length
func TestDecodeArchiveCorruptLength(t *testing.T) {
	// Create archive with valid magic but invalid length field
	var buf bytes.Buffer
	buf.WriteString("CBBK1")
	buf.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}) // invalid length

	decoded, err := decodeArchive(buf.Bytes())
	assert.Error(t, err)
	assert.Nil(t, decoded)
}

// TestBackupRunSuccess tests successful backup execution
func TestBackupRunSuccess(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{
			AutoBackupEnabled:    true,
			BackupIntervalHours:  24,
			StorageType:          "",
			StorageConfig:        map[string]string{},
		},
	}
	mockStorage := &MockStorageAdapter{}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	err := backup.Run(ctx)

	require.NoError(t, err)
}

// TestBackupRunWithSettingsError tests backup with settings error
func TestBackupRunWithSettingsError(t *testing.T) {
	mockStore := &MockBackupStore{
		getBackupSettingsFunc: func() (*database.BackupSettings, error) {
			return nil, assert.AnError
		},
	}
	mockStorage := &MockStorageAdapter{}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	err := backup.Run(ctx)

	assert.Error(t, err)
	assert.Equal(t, assert.AnError, err)
}

// TestBackupRunWithDumpError tests backup with dump error
func TestBackupRunWithDumpError(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{},
		dumpStatementsError: assert.AnError,
	}
	mockStorage := &MockStorageAdapter{}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	err := backup.Run(ctx)

	assert.Error(t, err)
	assert.Equal(t, assert.AnError, err)
}

// TestBackupRestoreSuccess tests successful restore execution
func TestBackupRestoreSuccess(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{
			AutoBackupEnabled:    true,
			StorageType:          "",
			StorageConfig:        map[string]string{},
		},
	}
	mockStorage := &MockStorageAdapter{
		downloadFunc: func(bucket, key string) ([]byte, error) {
			// Return a valid encoded archive
			return encodeArchive([]string{"SELECT 1"})
		},
	}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	err := backup.Restore(ctx, "cargobay-20240101-120000.sql.gz")

	require.NoError(t, err)
}

// TestBackupRestoreEmptyPath tests restore with empty path
func TestBackupRestoreEmptyPath(t *testing.T) {
	mockStore := &MockBackupStore{}
	mockStorage := &MockStorageAdapter{}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	err := backup.Restore(ctx, "")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backup path is required")
}

// TestBackupRestoreWithSettingsError tests restore with settings error
func TestBackupRestoreWithSettingsError(t *testing.T) {
	mockStore := &MockBackupStore{
		getBackupSettingsFunc: func() (*database.BackupSettings, error) {
			return nil, assert.AnError
		},
	}
	mockStorage := &MockStorageAdapter{}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	err := backup.Restore(ctx, "backup.sql.gz")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load backup settings")
}

// TestBackupRestoreWithDownloadError tests restore with download error
func TestBackupRestoreWithDownloadError(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{},
	}
	mockStorage := &MockStorageAdapter{
		downloadFunc: func(bucket, key string) ([]byte, error) {
			return nil, assert.AnError
		},
	}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	err := backup.Restore(ctx, "backup.sql.gz")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load backup")
}

// TestBackupListSuccess tests successful listing of backups
func TestBackupListSuccess(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{},
	}
	mockStorage := &MockStorageAdapter{
		listFilesFunc: func(bucket, prefix string) ([]string, error) {
			return []string{
				"cargobay-20240102-120000.sql.gz",
				"cargobay-20240101-120000.sql.gz",
			}, nil
		},
	}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	infos, err := backup.List(ctx)

	require.NoError(t, err)
	assert.Len(t, infos, 2)
	// Should be sorted by date, newest first
	assert.Contains(t, infos[0].Path, "20240102")
}

// TestBackupListWithStorageError tests listing with storage error
func TestBackupListWithStorageError(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{},
	}
	mockStorage := &MockStorageAdapter{
		listFilesFunc: func(bucket, prefix string) ([]string, error) {
			return nil, assert.AnError
		},
	}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	infos, err := backup.List(ctx)

	assert.Error(t, err)
	assert.Nil(t, infos)
}

// TestBackupDueNilLastCheckedAt tests backupDue with nil lastCheckedAt
func TestBackupDueNilLastCheckedAt(t *testing.T) {
	now := time.Now()
	result := backupDue(nil, 24, now)

	assert.True(t, result)
}

// TestBackupDueIntervalElapsed tests backupDue when interval has elapsed
func TestBackupDueIntervalElapsed(t *testing.T) {
	now := time.Now()
	oneWeekAgo := now.Add(-7 * 24 * time.Hour)
	intervalHours := 24

	result := backupDue(&oneWeekAgo, intervalHours, now)

	assert.True(t, result)
}

// TestBackupDueIntervalNotElapsed tests backupDue when interval hasn't elapsed
func TestBackupDueIntervalNotElapsed(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	intervalHours := 24

	result := backupDue(&oneHourAgo, intervalHours, now)

	assert.False(t, result)
}

// TestBackupDueExactlyAtBoundary tests backupDue at exact boundary
func TestBackupDueExactlyAtBoundary(t *testing.T) {
	now := time.Now()
	twoDaysAgo := now.Add(-48 * time.Hour)
	intervalHours := 48

	result := backupDue(&twoDaysAgo, intervalHours, now)

	assert.True(t, result)
}

// TestBackupDueWithZeroInterval tests backupDue with zero interval
func TestBackupDueWithZeroInterval(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	result := backupDue(&oneHourAgo, 0, now)

	assert.True(t, result)
}

// TestBackupDueWithNegativeInterval tests backupDue with negative interval
func TestBackupDueWithNegativeInterval(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	result := backupDue(&oneHourAgo, -1, now)

	assert.True(t, result)
}

// TestBackupDueWithLargeInterval tests backupDue with large interval
func TestBackupDueWithLargeInterval(t *testing.T) {
	now := time.Now()
	twoDaysAgo := now.Add(-48 * time.Hour)
	intervalHours := 720 // 30 days

	result := backupDue(&twoDaysAgo, intervalHours, now)

	assert.False(t, result)
}

// TestParseBackupTimestampValid tests parsing valid backup timestamp
func TestParseBackupTimestampValid(t *testing.T) {
	name := "cargobay-20240115-143045.sql.gz"
	timestamp := parseBackupTimestamp(name)

	expected := time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC)
	assert.Equal(t, expected, timestamp)
}

// TestParseBackupTimestampInvalid tests parsing invalid backup timestamp
func TestParseBackupTimestampInvalid(t *testing.T) {
	name := "invalid-backup-file.sql.gz"
	timestamp := parseBackupTimestamp(name)

	// Should return zero time for invalid names
	assert.Equal(t, time.Time{}, timestamp)
}

// TestParseBackupTimestampNoExtension tests parsing without .sql.gz extension
func TestParseBackupTimestampNoExtension(t *testing.T) {
	name := "cargobay-20240115-143045"
	timestamp := parseBackupTimestamp(name)

	expected := time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC)
	assert.Equal(t, expected, timestamp)
}

// TestParseBackupTimestampEmpty tests parsing empty string
func TestParseBackupTimestampEmpty(t *testing.T) {
	timestamp := parseBackupTimestamp("")
	assert.Equal(t, time.Time{}, timestamp)
}

// TestBackupWithCustomStorageType tests backup with custom storage type
func TestBackupWithCustomStorageType(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{
			StorageType:   "s3",
			StorageConfig: map[string]string{}, // missing required "bucket"
		},
	}
	mockStorage := &MockStorageAdapter{}
	backup := New(mockStore, mockStorage)

	// resolveStorage should return an error because s3 config is incomplete
	// but the test verifies the method exists and is called
	settings, err := backup.db.GetBackupSettings()
	require.NoError(t, err)
	_, err = backup.resolveStorage(settings)
	assert.Error(t, err)
}

// TestBackupMultipleRestoreStatements tests restore with multiple statements
func TestBackupMultipleRestoreStatements(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{},
	}
	mockStorage := &MockStorageAdapter{
		downloadFunc: func(bucket, key string) ([]byte, error) {
			statements := []string{
				"BEGIN",
				"CREATE TABLE test (id INT)",
				"INSERT INTO test VALUES (1)",
				"COMMIT",
			}
			return encodeArchive(statements)
		},
	}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	err := backup.Restore(ctx, "backup.sql.gz")

	require.NoError(t, err)
}

// TestBackupArchiveWithSpecialChars tests archive with special characters in statements
func TestBackupArchiveWithSpecialChars(t *testing.T) {
	statements := []string{
		"CREATE TABLE test (data TEXT)",
		"INSERT INTO test VALUES ('Special chars: ;''\"\\n')",
		"SELECT * FROM test WHERE data LIKE '%test%'",
	}

	archive, err := encodeArchive(statements)
	require.NoError(t, err)

	decoded, err := decodeArchive(archive)
	require.NoError(t, err)
	assert.Equal(t, statements, decoded)
}

// TestBackupStartSchedulerWithAutoDisabled tests scheduler with auto-backup disabled
func TestBackupStartSchedulerWithAutoDisabled(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{
			AutoBackupEnabled: false,
		},
	}
	mockStorage := &MockStorageAdapter{}
	backup := New(mockStore, mockStorage)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	backup.StartScheduler(ctx)
}

// TestBackupStartSchedulerStopsOnContextCancel tests scheduler stops on context cancel
func TestBackupStartSchedulerStopsOnContextCancel(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{
			AutoBackupEnabled: true,
		},
	}
	mockStorage := &MockStorageAdapter{}
	backup := New(mockStore, mockStorage)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		backup.StartScheduler(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Scheduler did not stop after context cancellation")
	}
}

// TestBackupRunWithDedicatedStorage tests backup with dedicated storage configuration
func TestBackupRunWithDedicatedStorage(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{
			AutoBackupEnabled: true,
			StorageType:       "local",
			StorageConfig: map[string]string{
				"path": "/tmp/backups",
			},
		},
	}
	mockStorage := &MockStorageAdapter{}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	err := backup.Run(ctx)

	require.NoError(t, err)
}

// TestBackupRecordFailure tests recording backup failure
func TestBackupRecordFailure(t *testing.T) {
	recordCalled := false
	recordErr := ""

	mockStore := &MockBackupStore{
		recordBackupResultFunc: func(checkedAt time.Time, succeeded bool, path string, errMsg string) error {
			recordCalled = true
			recordErr = errMsg
			return nil
		},
	}
	mockStorage := &MockStorageAdapter{}
	backup := New(mockStore, mockStorage)

	checkedAt := time.Now()
	backup.recordFailure(checkedAt, assert.AnError)

	assert.True(t, recordCalled)
	assert.Contains(t, recordErr, "test")
}

// TestBackupRestoreWithRestoreError tests restore with restore error
func TestBackupRestoreWithRestoreError(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{},
		restoreStatementsError: assert.AnError,
	}
	mockStorage := &MockStorageAdapter{
		downloadFunc: func(bucket, key string) ([]byte, error) {
			return encodeArchive([]string{"SELECT 1"})
		},
	}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	err := backup.Restore(ctx, "backup.sql.gz")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to restore backup")
}

// TestBackupInfoSort tests backup info sorting (newest first)
func TestBackupInfoSort(t *testing.T) {
	infos := []BackupInfo{
		{Path: "cargobay-20240103-120000.sql.gz", CreatedAt: time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC)},
		{Path: "cargobay-20240101-120000.sql.gz", CreatedAt: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
		{Path: "cargobay-20240102-120000.sql.gz", CreatedAt: time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)},
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].CreatedAt.After(infos[j].CreatedAt)
	})

	assert.Equal(t, "cargobay-20240103-120000.sql.gz", infos[0].Path)
	assert.Equal(t, "cargobay-20240102-120000.sql.gz", infos[1].Path)
	assert.Equal(t, "cargobay-20240101-120000.sql.gz", infos[2].Path)
}

// TestBackupArchiveCompression tests that archive is gzip compressed
func TestBackupArchiveCompression(t *testing.T) {
	// Generate many statements to ensure compression has effect
	statements := make([]string, 100)
	for i := 0; i < 100; i++ {
		statements[i] = fmt.Sprintf("INSERT INTO test VALUES (%d, 'data_%d')", i, i)
	}

	archive, err := encodeArchive(statements)
	require.NoError(t, err)

	// The archive should be gzip compressed
	// We can't easily verify the compression ratio without decompressing,
	// but we can verify it's not plain text
	assert.NotContains(t, string(archive), "INSERT")
}

// TestBackupDecodeArchiveTruncated tests decoding truncated archive
func TestBackupDecodeArchiveTruncated(t *testing.T) {
	// Valid magic header but truncated data, then gzip-compressed like a real archive
	var raw bytes.Buffer
	raw.WriteString("CBBK1")
	raw.Write([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10}) // says 16 bytes
	raw.Write([]byte{0x01, 0x02, 0x03})                               // but only 3 bytes provided

	decoded, err := decodeArchive(gzipT(t, raw.Bytes()))
	assert.Error(t, err)
	assert.Nil(t, decoded)
	assert.Contains(t, err.Error(), "corrupt backup archive")
}

// TestBackupMultipleArchiveEncodeDecode tests multiple encode/decode cycles
func TestBackupMultipleArchiveEncodeDecode(t *testing.T) {
	originalStatements := []string{
		"CREATE TABLE users (id SERIAL PRIMARY KEY)",
		"CREATE TABLE artifacts (id SERIAL PRIMARY KEY, name TEXT)",
		"CREATE TABLE registries (id SERIAL PRIMARY KEY, url TEXT)",
	}

	for i := 0; i < 5; i++ {
		archive, err := encodeArchive(originalStatements)
		require.NoError(t, err, "Encode iteration %d failed", i)

		decoded, err := decodeArchive(archive)
		require.NoError(t, err, "Decode iteration %d failed", i)
		assert.Equal(t, originalStatements, decoded, "Mismatch in iteration %d", i)
	}
}

// TestBackupArchiveLargeStatements tests archive with large statements
func TestBackupArchiveLargeStatements(t *testing.T) {
	// Create a large statement (1MB)
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte('A' + (i % 26))
	}

	statements := []string{
		"INSERT INTO large_table (data) VALUES ('" + string(largeData) + "')",
	}

	archive, err := encodeArchive(statements)
	require.NoError(t, err)

	decoded, err := decodeArchive(archive)
	require.NoError(t, err)
	assert.Equal(t, statements, decoded)
}

// TestBackupRestoreWithEmptyArchive tests restore with empty archive
func TestBackupRestoreWithEmptyArchive(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{},
	}
	mockStorage := &MockStorageAdapter{
		downloadFunc: func(bucket, key string) ([]byte, error) {
			return encodeArchive([]string{})
		},
	}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	err := backup.Restore(ctx, "backup.sql.gz")

	require.NoError(t, err)
}

// TestBackupRunWithEmptyStatements tests backup with empty statements
func TestBackupRunWithEmptyStatements(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{},
		dumpStatementsFunc: func(context.Context) ([]string, error) {
			return []string{}, nil
		},
	}
	mockStorage := &MockStorageAdapter{}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	err := backup.Run(ctx)

	require.NoError(t, err)
}

// TestBackupRestoreContextCancellation tests restore with context cancellation
func TestBackupRestoreContextCancellation(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{},
	}
	mockStorage := &MockStorageAdapter{
		downloadFunc: func(bucket, key string) ([]byte, error) {
			return encodeArchive([]string{"SELECT 1"})
		},
	}
	backup := New(mockStore, mockStorage)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Restore doesn't check ctx itself; that's left to RestoreStatements
	// against the real DB. The mock ignores ctx, so this just verifies
	// Restore doesn't panic/misbehave when handed an already-cancelled ctx.
	err := backup.Restore(ctx, "backup.sql.gz")

	assert.NoError(t, err)
}

// TestBackupListWithNoFiles tests listing with no backup files
func TestBackupListWithNoFiles(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{},
	}
	mockStorage := &MockStorageAdapter{
		listFilesFunc: func(bucket, prefix string) ([]string, error) {
			return []string{}, nil
		},
	}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	infos, err := backup.List(ctx)

	require.NoError(t, err)
	assert.Empty(t, infos)
}

// TestBackupListWithStrayFiles tests listing with stray files that don't match pattern
func TestBackupListWithStrayFiles(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{},
	}
	mockStorage := &MockStorageAdapter{
		listFilesFunc: func(bucket, prefix string) ([]string, error) {
			return []string{
				"cargobay-20240101-120000.sql.gz",
				"stray-file.txt",
				"cargobay-invalid.sql.gz",
			}, nil
		},
	}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	infos, err := backup.List(ctx)

	require.NoError(t, err)
	// Should include valid backup and ignore/parse invalid ones
	assert.GreaterOrEqual(t, len(infos), 1)
}

// TestBackupResolveStorageWithEmptyType tests resolveStorage with empty storage type
func TestBackupResolveStorageWithEmptyType(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{
			StorageType: "",
		},
	}
	mockStorage := &MockStorageAdapter{}
	backup := New(mockStore, mockStorage)

	settings, _ := backup.db.GetBackupSettings()
	resolved, err := backup.resolveStorage(settings)

	require.NoError(t, err)
	assert.Equal(t, mockStorage, resolved)
}

// TestBackupRestoreWithInvalidArchiveType tests restore with non-cargobay archive
func TestBackupRestoreWithInvalidArchiveType(t *testing.T) {
	mockStore := &MockBackupStore{
		settings: &database.BackupSettings{},
	}
	mockStorage := &MockStorageAdapter{
		downloadFunc: func(bucket, key string) ([]byte, error) {
			return gzipT(t, []byte("this is not a cargobay backup")), nil
		},
	}
	backup := New(mockStore, mockStorage)

	ctx := context.Background()
	err := backup.Restore(ctx, "backup.sql.gz")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a cargobay backup archive")
}
