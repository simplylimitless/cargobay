package storage

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLocalAdapterSaveStreamGetStreamRoundTrip covers the new streaming
// methods end to end.
func TestLocalAdapterSaveStreamGetStreamRoundTrip(t *testing.T) {
	adapter, err := NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)

	data := []byte("streamed artifact content")
	key, err := adapter.SaveArtifactStream("reg", "ns", "art", "1.0.0", bytes.NewReader(data))
	require.NoError(t, err)
	assert.NotEmpty(t, key)

	rc, err := adapter.GetArtifactStream("reg", "ns", "art", "1.0.0")
	require.NoError(t, err)
	require.NotNil(t, rc)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestLocalAdapterGetArtifactStreamNotFoundReturnsNilNil(t *testing.T) {
	adapter, err := NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)

	rc, err := adapter.GetArtifactStream("reg", "ns", "missing", "1.0.0")
	require.NoError(t, err)
	assert.Nil(t, rc)
}

func TestLocalAdapterSaveArtifactStreamOverwritesExisting(t *testing.T) {
	adapter, err := NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)

	_, err = adapter.SaveArtifactStream("reg", "ns", "art", "1.0.0", bytes.NewReader([]byte("old content, much longer than the new one")))
	require.NoError(t, err)

	_, err = adapter.SaveArtifactStream("reg", "ns", "art", "1.0.0", bytes.NewReader([]byte("new")))
	require.NoError(t, err)

	rc, err := adapter.GetArtifactStream("reg", "ns", "art", "1.0.0")
	require.NoError(t, err)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), got, "overwrite must truncate, not append")
}

// TestLocalAdapterStreamAndBufferedShareStoragePath is the critical
// compatibility guarantee for a live deployment: this refactor must not
// change where artifacts live on disk. An artifact already saved by the old
// buffered SaveArtifact has to remain readable via the new
// GetArtifactStream after a deploy, and vice versa, or every existing
// cached artifact becomes an unexpected cache miss (or worse, unreadable)
// the moment the new binary starts.
func TestLocalAdapterStreamAndBufferedShareStoragePath(t *testing.T) {
	adapter, err := NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)

	t.Run("written with old buffered SaveArtifact, read with new GetArtifactStream", func(t *testing.T) {
		data := []byte("pre-deploy artifact, written the old way")
		_, err := adapter.SaveArtifact("reg", "ns", "legacy-artifact", "1.0.0", data)
		require.NoError(t, err)

		rc, err := adapter.GetArtifactStream("reg", "ns", "legacy-artifact", "1.0.0")
		require.NoError(t, err)
		require.NotNil(t, rc, "artifact saved before the streaming refactor must still be found after it")
		defer rc.Close()

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("written with new SaveArtifactStream, read with old buffered GetArtifact", func(t *testing.T) {
		data := []byte("post-deploy artifact, written the streaming way")
		_, err := adapter.SaveArtifactStream("reg", "ns", "new-artifact", "1.0.0", bytes.NewReader(data))
		require.NoError(t, err)

		got, err := adapter.GetArtifact("reg", "ns", "new-artifact", "1.0.0")
		require.NoError(t, err)
		assert.Equal(t, data, got, "a rollback to the old buffered code path must still be able to read artifacts saved by the new streaming code")
	})

	t.Run("SaveArtifact and SaveArtifactStream resolve to the identical file", func(t *testing.T) {
		bufData := []byte("buffered write")
		bufPath, err := adapter.SaveArtifact("reg", "ns", "same-artifact", "1.0.0", bufData)
		require.NoError(t, err)

		streamData := []byte("stream write, overwriting the same file")
		streamPath, err := adapter.SaveArtifactStream("reg", "ns", "same-artifact", "1.0.0", bytes.NewReader(streamData))
		require.NoError(t, err)

		assert.Equal(t, bufPath, streamPath, "both methods must key artifacts identically so one can overwrite/read the other's output")

		got, err := adapter.GetArtifact("reg", "ns", "same-artifact", "1.0.0")
		require.NoError(t, err)
		assert.Equal(t, streamData, got)
	})

	t.Run("ArtifactExists and DeleteArtifact work on artifacts written via the streaming path", func(t *testing.T) {
		_, err := adapter.SaveArtifactStream("reg", "ns", "stream-only-artifact", "1.0.0", bytes.NewReader([]byte("data")))
		require.NoError(t, err)

		exists, err := adapter.ArtifactExists("reg", "ns", "stream-only-artifact", "1.0.0")
		require.NoError(t, err)
		assert.True(t, exists)

		require.NoError(t, adapter.DeleteArtifact("reg", "ns", "stream-only-artifact", "1.0.0"))

		exists, err = adapter.ArtifactExists("reg", "ns", "stream-only-artifact", "1.0.0")
		require.NoError(t, err)
		assert.False(t, exists)
	})
}

// TestLocalAdapterGetArtifactStreamPropagatesRealErrors ensures a genuine
// filesystem error (as opposed to not-found) is surfaced rather than
// silently swallowed as a cache miss.
func TestLocalAdapterGetArtifactStreamPropagatesRealErrors(t *testing.T) {
	adapter, err := NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)

	// Save an artifact, then replace its directory's permissions so opening
	// the file underneath fails with something other than "not exist".
	_, err = adapter.SaveArtifactStream("reg", "ns", "art", "1.0.0", bytes.NewReader([]byte("data")))
	require.NoError(t, err)

	path := adapter.getArtifactPath("reg", "ns", "art", "1.0.0")
	require.NoError(t, os.Chmod(path, 0000))
	t.Cleanup(func() { os.Chmod(path, 0755) })

	if os.Getuid() == 0 {
		t.Skip("running as root: permission bits don't block access")
	}

	_, err = adapter.GetArtifactStream("reg", "ns", "art", "1.0.0")
	assert.Error(t, err)
}
