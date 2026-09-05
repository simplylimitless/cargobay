package storage

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStreamAdapter is a minimal StorageAdapter stand-in whose stream methods
// return exactly what the test configures, so TrackingAdapter's wrapping
// logic can be exercised in isolation from a real backend.
type fakeStreamAdapter struct {
	StorageAdapter // nil embed; only the stream methods below are used
	streamRC       io.ReadCloser
	streamErr      error
}

func (f *fakeStreamAdapter) GetArtifactStream(registryID, namespace, artifactName, version string) (io.ReadCloser, error) {
	return f.streamRC, f.streamErr
}

// countingCloser lets a test assert Close was actually called.
type countingCloser struct {
	io.Reader
	closed bool
}

func (c *countingCloser) Close() error {
	c.closed = true
	return nil
}

func TestTrackingAdapterGetArtifactStreamFiresOnCloseWithByteCount(t *testing.T) {
	payload := []byte("hello streamed cache hit")
	rc := &countingCloser{Reader: bytes.NewReader(payload)}
	inner := &fakeStreamAdapter{streamRC: rc}

	var gotRegistry, gotNamespace, gotName, gotVersion string
	var gotBytes int64
	calls := 0
	tracking := NewTrackingAdapter(inner, func(registryID, namespace, artifactName, version string, bytesServed int64) {
		calls++
		gotRegistry, gotNamespace, gotName, gotVersion, gotBytes = registryID, namespace, artifactName, version, bytesServed
	})

	stream, err := tracking.GetArtifactStream("reg1", "ns1", "art1", "1.0.0")
	require.NoError(t, err)
	require.NotNil(t, stream)

	// onCacheHit must not fire before the stream is fully consumed/closed.
	data, err := io.ReadAll(stream)
	require.NoError(t, err)
	assert.Equal(t, payload, data)
	assert.Equal(t, 0, calls, "onCacheHit should not fire until Close")

	require.NoError(t, stream.Close())
	assert.True(t, rc.closed, "underlying ReadCloser must be closed")
	assert.Equal(t, 1, calls)
	assert.Equal(t, "reg1", gotRegistry)
	assert.Equal(t, "ns1", gotNamespace)
	assert.Equal(t, "art1", gotName)
	assert.Equal(t, "1.0.0", gotVersion)
	assert.Equal(t, int64(len(payload)), gotBytes)
}

func TestTrackingAdapterGetArtifactStreamZeroBytesDoesNotFire(t *testing.T) {
	rc := &countingCloser{Reader: bytes.NewReader(nil)}
	inner := &fakeStreamAdapter{streamRC: rc}

	calls := 0
	tracking := NewTrackingAdapter(inner, func(string, string, string, string, int64) {
		calls++
	})

	stream, err := tracking.GetArtifactStream("reg", "ns", "name", "v")
	require.NoError(t, err)

	_, err = io.ReadAll(stream)
	require.NoError(t, err)
	require.NoError(t, stream.Close())

	assert.Equal(t, 0, calls, "zero bytes read should not report a cache hit")
}

func TestTrackingAdapterGetArtifactStreamNilOnCacheHitDoesNotPanic(t *testing.T) {
	rc := &countingCloser{Reader: bytes.NewReader([]byte("data"))}
	inner := &fakeStreamAdapter{streamRC: rc}

	tracking := NewTrackingAdapter(inner, nil)

	stream, err := tracking.GetArtifactStream("reg", "ns", "name", "v")
	require.NoError(t, err)
	require.NotNil(t, stream)

	assert.NotPanics(t, func() {
		_, _ = io.ReadAll(stream)
		_ = stream.Close()
	})
}

func TestTrackingAdapterGetArtifactStreamPassesThroughNilResult(t *testing.T) {
	inner := &fakeStreamAdapter{streamRC: nil, streamErr: nil}

	calls := 0
	tracking := NewTrackingAdapter(inner, func(string, string, string, string, int64) {
		calls++
	})

	stream, err := tracking.GetArtifactStream("reg", "ns", "name", "v")
	require.NoError(t, err)
	assert.Nil(t, stream)
	assert.Equal(t, 0, calls)
}

func TestTrackingAdapterGetArtifactStreamPassesThroughError(t *testing.T) {
	wantErr := errors.New("boom")
	inner := &fakeStreamAdapter{streamRC: nil, streamErr: wantErr}

	calls := 0
	tracking := NewTrackingAdapter(inner, func(string, string, string, string, int64) {
		calls++
	})

	stream, err := tracking.GetArtifactStream("reg", "ns", "name", "v")
	assert.Equal(t, wantErr, err)
	assert.Nil(t, stream)
	assert.Equal(t, 0, calls)
}

func TestTrackingAdapterGetArtifactStreamPartialReadStillCountsOnClose(t *testing.T) {
	// A caller that reads only part of the stream (e.g. client disconnect)
	// should still get an accurate count of what it actually consumed.
	payload := bytes.Repeat([]byte("a"), 1000)
	rc := &countingCloser{Reader: bytes.NewReader(payload)}
	inner := &fakeStreamAdapter{streamRC: rc}

	var gotBytes int64
	tracking := NewTrackingAdapter(inner, func(_, _, _, _ string, bytesServed int64) {
		gotBytes = bytesServed
	})

	stream, err := tracking.GetArtifactStream("reg", "ns", "name", "v")
	require.NoError(t, err)

	buf := make([]byte, 100)
	n, err := stream.Read(buf)
	require.NoError(t, err)
	require.NoError(t, stream.Close())

	assert.Equal(t, int64(n), gotBytes)
	assert.Less(t, gotBytes, int64(len(payload)))
}
