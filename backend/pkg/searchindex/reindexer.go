// Package searchindex rebuilds cargobay's Postgres full-text search index
// (the search_vector column on artifacts) on a schedule or on demand.
package searchindex

import (
	"context"
	"log"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
)

// settingsStore is the subset of *database.Database's methods Reindexer
// depends on. Narrowed to an interface so tests can substitute a fake and
// exercise RunReindex/StartScheduler without a live Postgres connection.
type settingsStore interface {
	GetSearchIndexSettings() (*database.SearchIndexSettings, error)
	RecordSearchIndexUpdateResult(checkedAt time.Time, succeeded bool, artifactCount int, errMsg string) error
	RebuildSearchIndex() (int, error)
}

// Reindexer rebuilds the search_vector tsvector for every artifact row.
// This is needed because rows written before search_vector existed (or via
// SaveArtifact's legacy no-tsvector fallback) never get one populated
// otherwise, which is what makes full-text search "go stale" over time.
type Reindexer struct {
	db settingsStore
}

// NewReindexer creates a Reindexer.
func NewReindexer(db *database.Database) *Reindexer {
	return &Reindexer{db: db}
}

// RunReindex performs a single synchronous rebuild and records the outcome
// (last checked/reindexed/error) in search_index_settings.
func (u *Reindexer) RunReindex(ctx context.Context) error {
	checkedAt := time.Now()

	count, err := u.db.RebuildSearchIndex()
	if err != nil {
		if recErr := u.db.RecordSearchIndexUpdateResult(checkedAt, false, 0, err.Error()); recErr != nil {
			log.Printf("failed to record search index reindex failure: %v", recErr)
		}
		return err
	}

	if recErr := u.db.RecordSearchIndexUpdateResult(checkedAt, true, count, ""); recErr != nil {
		log.Printf("failed to record search index reindex success: %v", recErr)
	}
	return nil
}

// StartScheduler runs a background loop that polls the current settings (so
// changes made via the API take effect without a restart) and triggers
// RunReindex whenever auto-reindex is enabled and the configured interval
// has elapsed since the last check. Blocks until ctx is cancelled; call it
// in a goroutine.
func (u *Reindexer) StartScheduler(ctx context.Context) {
	const pollInterval = time.Minute

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			settings, err := u.db.GetSearchIndexSettings()
			if err != nil {
				log.Printf("search index scheduler: failed to load settings: %v", err)
				continue
			}
			if !settings.AutoReindexEnabled {
				continue
			}

			if !reindexDue(settings.LastCheckedAt, settings.ReindexIntervalHours, time.Now()) {
				continue
			}

			if err := u.RunReindex(ctx); err != nil {
				log.Printf("search index scheduler: reindex failed: %v", err)
			} else {
				log.Printf("search index scheduler: reindex succeeded")
			}
		}
	}
}

// reindexDue decides whether a rebuild is due: true if there is no record of
// a prior check, or the configured interval has elapsed since the last one.
// Extracted as a pure function (no clock/DB access of its own) so the
// scheduling decision can be unit-tested directly.
func reindexDue(lastCheckedAt *time.Time, intervalHours int, now time.Time) bool {
	if lastCheckedAt == nil {
		return true
	}
	return now.Sub(*lastCheckedAt) >= time.Duration(intervalHours)*time.Hour
}
