package replication

import (
	"fmt"
	"sync"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
)

// ReplicationService manages cross-region and cross-cloud replication
type ReplicationService struct {
	db           *database.Database
	storage      storage.StorageAdapter
	cache        *cache.Cache
	masterURL    string
	regions      []RegionConfig
	mu           sync.RWMutex
	isLeader     bool
	stopCh       chan struct{}
	wg           sync.WaitGroup
	stats        ReplicationStats
	statsLock    sync.Mutex
}

// RegionConfig represents a replication region
type RegionConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Type        string `json:"type"` // primary, secondary, edge
	Enabled     bool   `json:"enabled"`
	Priority    int    `json:"priority"`
	ReplicateFrom string `json:"replicate_from"` // source region ID
}

// ReplicationStats tracks replication performance
type ReplicationStats struct {
	SyncedArtifacts int64
	FailedSyncs     int64
	PendingTasks    int64
	LastSyncTime    time.Time
	ReplicationLag  time.Duration
}

// New creates a new ReplicationService
func New(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, masterURL string, regions []RegionConfig) *ReplicationService {
	return &ReplicationService{
		db:        db,
		storage:   storage,
		cache:     cache,
		masterURL: masterURL,
		regions:   regions,
		isLeader:  masterURL != "",
		stopCh:    make(chan struct{}),
	}
}

// Start begins the replication process
func (r *ReplicationService) Start() {
	r.mu.RLock()
	enabledRegions := make([]RegionConfig, 0)
	for _, region := range r.regions {
		if region.Enabled && region.Type == "secondary" {
			enabledRegions = append(enabledRegions, region)
		}
	}
	r.mu.RUnlock()

	if len(enabledRegions) == 0 {
		return
	}

	// Start replication workers
	for i := 0; i < 4; i++ {
		r.wg.Add(1)
		go r.replicationWorker(i, enabledRegions)
	}

	// Start sync scheduler
	r.wg.Add(1)
	go r.syncScheduler()
}

// Stop stops the replication service
func (r *ReplicationService) Stop() {
	close(r.stopCh)
	r.wg.Wait()
}

// replicationWorker processes replication tasks
func (r *ReplicationService) replicationWorker(workerID int, regions []RegionConfig) {
	defer r.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.replicateToRegions(regions)
		}
	}
}

// replicateToRegions replicates artifacts to all enabled regions
func (r *ReplicationService) replicateToRegions(regions []RegionConfig) {
	// Find artifacts that need replication
	artifacts, err := r.db.GetPendingReplicationArtifacts(100)
	if err != nil {
		r.statsLock.Lock()
		r.stats.FailedSyncs++
		r.statsLock.Unlock()
		return
	}

	if len(artifacts) == 0 {
		return
	}

	for _, artifact := range artifacts {
		for _, region := range regions {
			if err := r.replicateArtifactToRegion(&artifact, region); err != nil {
				r.statsLock.Lock()
				r.stats.FailedSyncs++
				r.statsLock.Unlock()
				continue
			}
			r.statsLock.Lock()
			r.stats.SyncedArtifacts++
			r.stats.LastSyncTime = time.Now()
			r.statsLock.Unlock()

			// Mark as replicated
			r.db.MarkArtifactReplicated(artifact.ID, region.ID)
		}
	}
}

// replicateArtifactToRegion replicates a single artifact to a region
func (r *ReplicationService) replicateArtifactToRegion(artifact *database.ArtifactMetadata, region RegionConfig) error {
	// Get artifact data from local storage
	data, err := r.storage.GetArtifact(
		artifact.RegistryID,
		artifact.Namespace,
		artifact.ArtifactName,
		artifact.Version,
	)
	if err != nil {
		return fmt.Errorf("failed to get artifact: %w", err)
	}

	if data == nil {
		return fmt.Errorf("artifact not found in storage")
	}

	// Replicate to region
	client, err := NewRegionClient(region.URL)
	if err != nil {
		return fmt.Errorf("failed to create region client: %w", err)
	}

	defer client.Close()

	return client.PushArtifact(artifact, data)
}

// syncScheduler periodically syncs all artifacts
func (r *ReplicationService) syncScheduler() {
	defer r.wg.Done()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.fullSync()
		}
	}
}

// fullSync performs a complete sync of all artifacts
func (r *ReplicationService) fullSync() {
	r.mu.RLock()
	enabledRegions := make([]RegionConfig, 0)
	for _, region := range r.regions {
		if region.Enabled && region.Type == "secondary" {
			enabledRegions = append(enabledRegions, region)
		}
	}
	r.mu.RUnlock()

	if len(enabledRegions) == 0 {
		return
	}

	opts := database.SearchOptions{Limit: 1000}

	for {
		artifacts, err := r.db.SearchArtifacts("", opts)
		if err != nil {
			return
		}

		if len(artifacts) == 0 {
			break
		}

		for _, artifact := range artifacts {
			for _, region := range enabledRegions {
				r.replicateArtifactToRegion(&artifact, region)
			}
		}

		opts.Offset += len(artifacts)
	}
}

// PushArtifact pushes an artifact to all regions
func (r *ReplicationService) PushArtifact(artifact *database.ArtifactMetadata, data []byte) error {
	r.mu.RLock()
	enabledRegions := make([]RegionConfig, 0)
	for _, region := range r.regions {
		if region.Enabled && region.Type == "secondary" {
			enabledRegions = append(enabledRegions, region)
		}
	}
	r.mu.RUnlock()

	for _, region := range enabledRegions {
		if err := r.replicateArtifactToRegion(artifact, region); err != nil {
			return err
		}
	}

	return nil
}

// GetStats returns replication statistics
func (r *ReplicationService) GetStats() ReplicationStats {
	r.statsLock.Lock()
	defer r.statsLock.Unlock()
	return r.stats
}

// RegionClient is an interface for region replication
type RegionClient interface {
	PushArtifact(artifact *database.ArtifactMetadata, data []byte) error
	PullArtifact(registryID, namespace, artifactName, version string) ([]byte, error)
	Close() error
}

// NewRegionClient creates a new region client
func NewRegionClient(url string) (RegionClient, error) {
	// For now, we'll use HTTP-based replication
	// In production, this could use gRPC or other protocols
	return NewHTTPRegionClient(url)
}

// HTTPRegionClient implements region replication over HTTP
type HTTPRegionClient struct {
	baseURL string
}

// NewHTTPRegionClient creates a new HTTP-based region client
func NewHTTPRegionClient(url string) (*HTTPRegionClient, error) {
	return &HTTPRegionClient{baseURL: url}, nil
}

// PushArtifact pushes an artifact to a remote region
func (c *HTTPRegionClient) PushArtifact(artifact *database.ArtifactMetadata, data []byte) error {
	// In production, this would make an HTTP request to the remote region
	// For now, this is a placeholder
	return nil
}

// PullArtifact pulls an artifact from a remote region
func (c *HTTPRegionClient) PullArtifact(registryID, namespace, artifactName, version string) ([]byte, error) {
	// In production, this would make an HTTP request to the remote region
	// For now, this is a placeholder
	return nil, nil
}

// Close closes the region client
func (c *HTTPRegionClient) Close() error {
	return nil
}
