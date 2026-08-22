package storage

// TrackingAdapter wraps a StorageAdapter and reports the byte size of every
// successful GetArtifact call — a cache hit means those bytes were served
// from local storage instead of being re-fetched from the upstream registry,
// i.e. bandwidth saved. All other operations pass through unchanged.
type TrackingAdapter struct {
	StorageAdapter
	onCacheHit func(bytesServed int64)
}

// NewTrackingAdapter wraps inner so onCacheHit is called with the byte size
// of every artifact successfully served from local storage.
func NewTrackingAdapter(inner StorageAdapter, onCacheHit func(bytesServed int64)) *TrackingAdapter {
	return &TrackingAdapter{StorageAdapter: inner, onCacheHit: onCacheHit}
}

// GetArtifact retrieves an artifact from the wrapped adapter and reports its
// size to onCacheHit on success.
func (t *TrackingAdapter) GetArtifact(registryID, namespace, artifactName, version string) ([]byte, error) {
	data, err := t.StorageAdapter.GetArtifact(registryID, namespace, artifactName, version)
	if err == nil && len(data) > 0 && t.onCacheHit != nil {
		t.onCacheHit(int64(len(data)))
	}
	return data, err
}
