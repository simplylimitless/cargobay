package proxy

import "io"

// CountingReader wraps an io.Reader and tracks the total bytes read. Proxy
// handlers that stream an upstream response straight into storage (so a
// large artifact never sits fully buffered in memory) use it to learn the
// final size for artifact metadata after the stream completes.
type CountingReader struct {
	R io.Reader
	N int64
}

func (c *CountingReader) Read(p []byte) (int, error) {
	n, err := c.R.Read(p)
	c.N += int64(n)
	return n, err
}
