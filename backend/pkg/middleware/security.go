package middleware

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SecurityHeadersMiddleware adds defensive HTTP headers to every response.
// When cargobay runs behind a reverse proxy (nginx, Caddy, cloud LB) that
// terminates TLS, HSTS and other headers may be set there instead; the
// middleware leaves them alone if they are already present.
func SecurityHeadersMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			if h.Get("X-Content-Type-Options") == "" {
				h.Set("X-Content-Type-Options", "nosniff")
			}
			if h.Get("X-Frame-Options") == "" {
				h.Set("X-Frame-Options", "DENY")
			}
			if h.Get("X-XSS-Protection") == "" {
				h.Set("X-XSS-Protection", "1; mode=block")
			}
			// HSTS — only set when the request itself is HTTPS.  When TLS
			// terminates at a reverse proxy the original scheme is lost, so
			// this is best-effort and may be empty in production.
			if r.TLS != nil && h.Get("Strict-Transport-Security") == "" {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequestSizeLimitMiddleware rejects requests whose body exceeds maxBytes.
// It reads the body into a temporary buffer so that the handler never
// sees a request larger than the configured ceiling.
func RequestSizeLimitMiddleware(maxBytes int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Allow unbounded body for large blob uploads (Docker push).
			if r.URL.Path != "" && strings.HasSuffix(r.URL.Path, "/blobs/uploads/") {
				next.ServeHTTP(w, r)
				return
			}
			if r.Body != nil && r.ContentLength > int64(maxBytes) {
				http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			if r.Body == nil {
				next.ServeHTTP(w, r)
				return
			}
			var buf bytes.Buffer
			_, err := buf.ReadFrom(&ioLimitedReader{r: r.Body, limit: int64(maxBytes)})
			if err != nil {
				http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = io.NopCloser(strings.NewReader(buf.String()))
			next.ServeHTTP(w, r)
		})
	}
}

// ioLimitedReader wraps an io.Reader and returns io.EOF once limit bytes
// have been read, preventing a slow-read DOS from exhausting memory.
type ioLimitedReader struct {
	r     io.Reader
	limit int64
	n     int64
}

func (l *ioLimitedReader) Read(p []byte) (int, error) {
	if l.n >= l.limit {
		return 0, io.EOF
	}
	toRead := l.limit - l.n
	if int(toRead) > len(p) {
		toRead = int64(len(p))
	}
	n, err := l.r.Read(p[:toRead])
	l.n += int64(n)
	return n, err
}

// LoginThrottle implements a simple per-IP sliding-window brute-force
// guard for login and setup endpoints.
type LoginThrottle struct {
	mu      sync.Mutex
	entries map[string]*loginEntry
	limit   int
	window  time.Duration
}

type loginEntry struct {
	failedAt []time.Time
}

// NewLoginThrottle creates a throttle that allows `limit` failed attempts
// per `window` duration.
func NewLoginThrottle(limit int, window time.Duration) *LoginThrottle {
	return &LoginThrottle{
		entries: make(map[string]*loginEntry),
		limit:   limit,
		window:  window,
	}
}

// normalizeIP strips a ":port" suffix if present, so an "ip:port" and a
// bare "ip" for the same address land on the same throttle-map key
// regardless of which form a caller passes in.
func normalizeIP(s string) string {
	if host, _, err := net.SplitHostPort(s); err == nil {
		return host
	}
	return s
}

// Allow checks whether the caller from `ip` is within their failure quota.
func (lt *LoginThrottle) Allow(ip string) bool {
	ip = normalizeIP(ip)
	lt.mu.Lock()
	defer lt.mu.Unlock()

	e, ok := lt.entries[ip]
	if !ok {
		return true
	}

	now := time.Now()
	cutoff := now.Add(-lt.window)
	valid := make([]time.Time, 0, len(e.failedAt))
	for _, t := range e.failedAt {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	if len(valid) >= lt.limit {
		lt.entries[ip].failedAt = valid
		return false
	}
	return true
}

// RecordFailure records a failed authentication attempt for `ip`.
func (lt *LoginThrottle) RecordFailure(ip string) {
	ip = normalizeIP(ip)
	lt.mu.Lock()
	defer lt.mu.Unlock()
	e, ok := lt.entries[ip]
	if !ok {
		e = &loginEntry{}
		lt.entries[ip] = e
	}
	e.failedAt = append(e.failedAt, time.Now())
}

// LoginHandler wraps a handler and gates it with per-IP brute-force
// protection.  The wrapped handler should call RecordFailure on its own
// when credentials are invalid.
func (lt *LoginThrottle) LoginHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}
		if !lt.Allow(ip) {
			w.Header().Set("Retry-After", formatDuration(lt.window))
			http.Error(w, "Too many failed login attempts. Please try again later.", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// formatDuration rounds to the nearest second for human-readability.
func formatDuration(d time.Duration) string {
	seconds := int(d.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}
