package proxy

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountingReaderAccumulatesAcrossReads(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 10000)
	c := &CountingReader{R: bytes.NewReader(data)}

	buf := make([]byte, 137) // deliberately not a clean divisor, forces many partial reads
	total := 0
	for {
		n, err := c.Read(buf)
		total += n
		if err != nil {
			require.Equal(t, io.EOF, err)
			break
		}
	}

	assert.Equal(t, len(data), total)
	assert.Equal(t, int64(len(data)), c.N)
}

func TestCountingReaderIoCopy(t *testing.T) {
	data := []byte("the quick brown fox jumps over the lazy dog")
	c := &CountingReader{R: bytes.NewReader(data)}

	var dst bytes.Buffer
	n, err := io.Copy(&dst, c)
	require.NoError(t, err)

	assert.Equal(t, int64(len(data)), n)
	assert.Equal(t, int64(len(data)), c.N)
	assert.Equal(t, data, dst.Bytes())
}

func TestCountingReaderEmptyReader(t *testing.T) {
	c := &CountingReader{R: bytes.NewReader(nil)}

	buf := make([]byte, 16)
	n, err := c.Read(buf)

	assert.Equal(t, 0, n)
	assert.Equal(t, io.EOF, err)
	assert.Equal(t, int64(0), c.N)
}

// errAfterNReader returns some bytes and then a non-EOF error, simulating an
// upstream connection that drops mid-stream.
type errAfterNReader struct {
	data []byte
	pos  int
	err  error
}

func (e *errAfterNReader) Read(p []byte) (int, error) {
	if e.pos >= len(e.data) {
		return 0, e.err
	}
	n := copy(p, e.data[e.pos:])
	e.pos += n
	if e.pos >= len(e.data) {
		return n, e.err
	}
	return n, nil
}

func TestCountingReaderCountsBytesReadBeforeError(t *testing.T) {
	wantErr := errors.New("connection reset")
	payload := []byte("partial payload before the connection drops")
	c := &CountingReader{R: &errAfterNReader{data: payload, err: wantErr}}

	buf := make([]byte, len(payload))
	n, err := c.Read(buf)

	// The underlying reader returns the full payload and the error on the
	// same call here, so N reflects those bytes even though err != nil.
	assert.Equal(t, len(payload), n)
	assert.Equal(t, wantErr, err)
	assert.Equal(t, int64(len(payload)), c.N)
}
