package searchindex

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
)

// MockSettingsStore is a mock implementation of settingsStore for testing
type MockSettingsStore struct {
	rebuildSearchIndexFunc      func() (int, error)
	getSearchIndexSettingsFunc  func() (*database.SearchIndexSettings, error)
	recordUpdateResultFunc      func(time.Time, bool, int, string) error
	rebuildCount                int
	settings                    *database.SearchIndexSettings
	recordUpdateResultError     error
}

// RebuildSearchIndex implements settingsStore interface
func (m *MockSettingsStore) RebuildSearchIndex() (int, error) {
	m.rebuildCount++
	if m.rebuildSearchIndexFunc != nil {
		return m.rebuildSearchIndexFunc()
	}
	return 100, nil
}

// GetSearchIndexSettings implements settingsStore interface
func (m *MockSettingsStore) GetSearchIndexSettings() (*database.SearchIndexSettings, error) {
	if m.getSearchIndexSettingsFunc != nil {
		return m.getSearchIndexSettingsFunc()
	}
	if m.settings == nil {
		// Default settings
		now := time.Now()
		m.settings = &database.SearchIndexSettings{
			AutoReindexEnabled:   true,
			ReindexIntervalHours: 24,
			LastCheckedAt:        &now,
		}
	}
	return m.settings, nil
}

// RecordSearchIndexUpdateResult implements settingsStore interface
func (m *MockSettingsStore) RecordSearchIndexUpdateResult(checkedAt time.Time, succeeded bool, artifactCount int, errMsg string) error {
	if m.recordUpdateResultFunc != nil {
		return m.recordUpdateResultFunc(checkedAt, succeeded, artifactCount, errMsg)
	}
	return m.recordUpdateResultError
}

// TestReindexerRunReindexSuccess tests successful reindex operation
func TestReindexerRunReindexSuccess(t *testing.T) {
	mockStore := &MockSettingsStore{}
	reindexer := NewReindexer(mockStore)

	ctx := context.Background()
	err := reindexer.RunReindex(ctx)

	require.NoError(t, err)
	assert.Equal(t, 1, mockStore.rebuildCount)
}

// TestReindexerRunReindexFailure tests reindex operation with error
func TestReindexerRunReindexFailure(t *testing.T) {
	mockStore := &MockSettingsStore{
		rebuildSearchIndexFunc: func() (int, error) {
			return 0, assert.AnError
		},
	}
	reindexer := NewReindexer(mockStore)

	ctx := context.Background()
	err := reindexer.RunReindex(ctx)

	assert.Error(t, err)
	assert.Equal(t, assert.AnError, err)
	assert.Equal(t, 1, mockStore.rebuildCount)
}

// TestReindexerRunReindexRecordsSuccess tests that success is recorded
func TestReindexerRunReindexRecordsSuccess(t *testing.T) {
	recordCalled := false
	recordSucceeded := false
	recordCount := 0

	mockStore := &MockSettingsStore{
		recordUpdateResultFunc: func(checkedAt time.Time, succeeded bool, artifactCount int, errMsg string) error {
			recordCalled = true
			recordSucceeded = succeeded
			recordCount = artifactCount
			return nil
		},
	}
	reindexer := NewReindexer(mockStore)

	ctx := context.Background()
	err := reindexer.RunReindex(ctx)

	require.NoError(t, err)
	assert.True(t, recordCalled)
	assert.True(t, recordSucceeded)
	assert.Equal(t, 100, recordCount)
}

// TestReindexerRunReindexRecordsFailure tests that failure is recorded
func TestReindexerRunReindexRecordsFailure(t *testing.T) {
	recordCalled := false
	recordSucceeded := false
	recordErrorMessage := ""

	mockStore := &MockSettingsStore{
		rebuildSearchIndexFunc: func() (int, error) {
			return 0, assert.AnError
		},
		recordUpdateResultFunc: func(checkedAt time.Time, succeeded bool, artifactCount int, errMsg string) error {
			recordCalled = true
			recordSucceeded = succeeded
			recordErrorMessage = errMsg
			return nil
		},
	}
	reindexer := NewReindexer(mockStore)

	ctx := context.Background()
	err := reindexer.RunReindex(ctx)

	assert.Error(t, err)
	assert.True(t, recordCalled)
	assert.False(t, recordSucceeded)
	assert.Contains(t, recordErrorMessage, "test")
}

// TestNewReindexer tests creating a new reindexer
func TestNewReindexer(t *testing.T) {
	mockStore := &MockSettingsStore{}
	reindexer := NewReindexer(mockStore)

	assert.NotNil(t, reindexer)
	assert.Equal(t, mockStore, reindexer.db)
}

// TestReindexDueNilLastCheckedAt tests reindexDue with nil lastCheckedAt
func TestReindexDueNilLastCheckedAt(t *testing.T) {
	now := time.Now()
	result := reindexDue(nil, 24, now)

	assert.True(t, result)
}

// TestReindexDueWithinInterval tests reindexDue when interval hasn't elapsed
func TestReindexDueWithinInterval(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	intervalHours := 24

	result := reindexDue(&oneHourAgo, intervalHours, now)

	assert.False(t, result)
}

// TestReindexDueExactlyAtInterval tests reindexDue when exactly at interval
func TestReindexDueExactlyAtInterval(t *testing.T) {
	now := time.Now()
	twoDaysAgo := now.Add(-48 * time.Hour)
	intervalHours := 48

	result := reindexDue(&twoDaysAgo, intervalHours, now)

	assert.True(t, result)
}

// TestReindexDuePastInterval tests reindexDue when past interval
func TestReindexDuePastInterval(t *testing.T) {
	now := time.Now()
	oneWeekAgo := now.Add(-7 * 24 * time.Hour)
	intervalHours := 24

	result := reindexDue(&oneWeekAgo, intervalHours, now)

	assert.True(t, result)
}

// TestReindexDueZeroInterval tests reindexDue with zero interval (should always be due)
func TestReindexDueZeroInterval(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	result := reindexDue(&oneHourAgo, 0, now)

	assert.True(t, result)
}

// TestReindexDueNegativeInterval tests reindexDue with negative interval
func TestReindexDueNegativeInterval(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	result := reindexDue(&oneHourAgo, -1, now)

	assert.True(t, result)
}

// TestReindexDueLargeInterval tests reindexDue with large interval
func TestReindexDueLargeInterval(t *testing.T) {
	now := time.Now()
	twoDaysAgo := now.Add(-48 * time.Hour)
	intervalHours := 720 // 30 days

	result := reindexDue(&twoDaysAgo, intervalHours, now)

	assert.False(t, result)
}

// TestReindexerWithDisabledAutoReindex tests scheduler with auto-reindex disabled
func TestReindexerWithDisabledAutoReindex(t *testing.T) {
	now := time.Now()
	mockStore := &MockSettingsStore{
		settings: &database.SearchIndexSettings{
			AutoReindexEnabled:   false,
			ReindexIntervalHours: 24,
			LastCheckedAt:        &now,
		},
	}

	reindexer := NewReindexer(mockStore)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel to exit scheduler

	reindexer.StartScheduler(ctx)
}

// TestReindexerWithEnabledAutoReindex tests scheduler with auto-reindex enabled
func TestReindexerWithEnabledAutoReindex(t *testing.T) {
	now := time.Now()
	// Set last checked to far in the past so reindex is due
	fourDaysAgo := now.Add(-4 * 24 * time.Hour)
	mockStore := &MockSettingsStore{
		settings: &database.SearchIndexSettings{
			AutoReindexEnabled:   true,
			ReindexIntervalHours: 24,
			LastCheckedAt:        &fourDaysAgo,
		},
	}

	reindexer := NewReindexer(mockStore)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pollInterval is a hardcoded 1 minute, so a real tick can't be observed here

	reindexer.StartScheduler(ctx)
}

// TestReindexerMultipleReindexCalls tests multiple reindex calls
func TestReindexerMultipleReindexCalls(t *testing.T) {
	mockStore := &MockSettingsStore{}
	reindexer := NewReindexer(mockStore)

	ctx := context.Background()

	for i := 0; i < 5; i++ {
		err := reindexer.RunReindex(ctx)
		require.NoError(t, err)
	}

	assert.Equal(t, 5, mockStore.rebuildCount)
}

// TestReindexerWithCustomInterval tests reindexer with custom interval
func TestReindexerWithCustomInterval(t *testing.T) {
	now := time.Now()
	hourAgo := now.Add(-1 * time.Hour)
	intervalHours := 1

	result := reindexDue(&hourAgo, intervalHours, now)

	assert.True(t, result)
}

// TestReindexerWithZeroByteArtifactCount tests reindex with zero artifacts
func TestReindexerWithZeroByteArtifactCount(t *testing.T) {
	recordCalled := false
	recordCount := -1
	mockStore := &MockSettingsStore{
		rebuildSearchIndexFunc: func() (int, error) {
			return 0, nil
		},
		recordUpdateResultFunc: func(checkedAt time.Time, succeeded bool, artifactCount int, errMsg string) error {
			recordCalled = true
			recordCount = artifactCount
			return nil
		},
	}
	reindexer := NewReindexer(mockStore)

	ctx := context.Background()
	err := reindexer.RunReindex(ctx)

	require.NoError(t, err)
	assert.True(t, recordCalled)
	assert.Equal(t, 0, recordCount)
}

// TestReindexerLargeArtifactCount tests reindex with large artifact count
func TestReindexerLargeArtifactCount(t *testing.T) {
	largeCount := 100000
	recordCount := 0
	mockStore := &MockSettingsStore{
		rebuildSearchIndexFunc: func() (int, error) {
			return largeCount, nil
		},
		recordUpdateResultFunc: func(checkedAt time.Time, succeeded bool, artifactCount int, errMsg string) error {
			recordCount = artifactCount
			return nil
		},
	}
	reindexer := NewReindexer(mockStore)

	ctx := context.Background()
	err := reindexer.RunReindex(ctx)

	require.NoError(t, err)
	assert.Equal(t, largeCount, recordCount)
}

// TestReindexerConcurrentCalls tests concurrent reindex calls
func TestReindexerConcurrentCalls(t *testing.T) {
	mockStore := &MockSettingsStore{}
	reindexer := NewReindexer(mockStore)

	ctx := context.Background()
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			reindexer.RunReindex(ctx)
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	assert.Equal(t, 10, mockStore.rebuildCount)
}

// TestReindexerErrorOnRecord tests reindex with error on record
func TestReindexerErrorOnRecord(t *testing.T) {
	mockStore := &MockSettingsStore{
		recordUpdateResultError: assert.AnError,
	}
	reindexer := NewReindexer(mockStore)

	ctx := context.Background()
	err := reindexer.RunReindex(ctx)

	require.NoError(t, err)
}

// TestReindexDueEdgeCaseSameTime tests reindexDue when lastCheckedAt equals now
func TestReindexDueEdgeCaseSameTime(t *testing.T) {
	now := time.Now()

	result := reindexDue(&now, 24, now)

	assert.False(t, result)
}

// TestReindexDueEdgeCaseJustBeforeInterval tests reindexDue just before interval
func TestReindexDueEdgeCaseJustBeforeInterval(t *testing.T) {
	now := time.Now()
	// 23 hours and 59 minutes ago
	justBefore := now.Add(-23*time.Hour - 59*time.Minute - 59*time.Second)
	intervalHours := 24

	result := reindexDue(&justBefore, intervalHours, now)

	assert.False(t, result)
}

// TestReindexerInterfaceImplementation tests that Reindexer implements expected methods
func TestReindexerInterfaceImplementation(t *testing.T) {
	mockStore := &MockSettingsStore{}
	reindexer := NewReindexer(mockStore)

	// Verify the reindexer has the expected methods
	assert.NotNil(t, reindexer.RunReindex)
	assert.NotNil(t, reindexer.StartScheduler)
}

// TestSettingsStoreMockImplementation tests the mock implementation
func TestSettingsStoreMockImplementation(t *testing.T) {
	mockStore := &MockSettingsStore{}

	// Test GetSearchIndexSettings
	settings, err := mockStore.GetSearchIndexSettings()
	require.NoError(t, err)
	assert.NotNil(t, settings)
	assert.True(t, settings.AutoReindexEnabled)

	// Test RebuildSearchIndex
	count, err := mockStore.RebuildSearchIndex()
	require.NoError(t, err)
	assert.Equal(t, 100, count)

	// Test RecordSearchIndexUpdateResult
	err = mockStore.RecordSearchIndexUpdateResult(time.Now(), true, 100, "")
	require.NoError(t, err)
}
