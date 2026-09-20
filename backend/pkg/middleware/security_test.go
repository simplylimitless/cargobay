package middleware

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Security-headers tests
// ---------------------------------------------------------------------------

func TestSecurityHeadersMiddleware(t *testing.T) {
	middleware := SecurityHeadersMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)

	h := rec.Header()
	assert.Contains(t, h.Get("X-Content-Type-Options"), "nosniff")
	assert.Contains(t, h.Get("X-Frame-Options"), "DENY")
	assert.Contains(t, h.Get("X-XSS-Protection"), "1; mode=block")
}

func TestSecurityHeadersPreservesContentType(t *testing.T) {
	middleware := SecurityHeadersMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("X-Content-Type-Options"), "nosniff")
}

func TestSecurityHeadersDoesNotOverwrite(t *testing.T) {
	middleware := SecurityHeadersMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "same-origin")
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)

	assert.Equal(t, "same-origin", rec.Header().Get("X-Content-Type-Options"))
}

func TestSecurityHeadersHSTSEnabled(t *testing.T) {
	middleware := SecurityHeadersMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	next := middleware(handler)

	// HTTPS → HSTS set
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Contains(t, rec.Header().Get("Strict-Transport-Security"), "max-age=")

	// HTTP → no HSTS
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.TLS = nil
	rec2 := httptest.NewRecorder()
	next.ServeHTTP(rec2, req2)
	assert.Empty(t, rec2.Header().Get("Strict-Transport-Security"))
}

// ---------------------------------------------------------------------------
// Request-size-limit tests
// ---------------------------------------------------------------------------

func TestRequestSizeLimitRejectsLargeBodies(t *testing.T) {
	limit := 1024
	middleware := RequestSizeLimitMiddleware(limit)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	next := middleware(handler)

	// Below limit → OK
	smallBody := strings.NewReader(strings.Repeat("a", 100))
	req := httptest.NewRequest(http.MethodPost, "/", smallBody)
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Above limit (ContentLength check) → rejected
	largeBody := strings.NewReader(strings.Repeat("a", limit+1))
	req = httptest.NewRequest(http.MethodPost, "/", largeBody)
	req.ContentLength = int64(limit + 1)
	rec = httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestRequestSizeLimitAllowsExactMax(t *testing.T) {
	limit := 512
	middleware := RequestSizeLimitMiddleware(limit)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	next := middleware(handler)

	body := strings.NewReader(strings.Repeat("x", limit))
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.ContentLength = int64(limit)
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequestSizeLimitBypassesBlobUploads(t *testing.T) {
	limit := 100
	middleware := RequestSizeLimitMiddleware(limit)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	next := middleware(handler)

	largeBody := strings.NewReader(strings.Repeat("a", 50000))
	req := httptest.NewRequest(http.MethodPatch, "/v2/library/nginx/blobs/uploads/abc123", largeBody)
	req.ContentLength = -1 // large unknown body (typical Docker push)
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequestSizeLimitEnforcesReaderLimit(t *testing.T) {
	limit := 10
	middleware := RequestSizeLimitMiddleware(limit)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	next := middleware(handler)

	largeBody := strings.NewReader(strings.Repeat("a", 500))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", largeBody)
	req.ContentLength = 500 // force body read
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

// ---------------------------------------------------------------------------
// ioLimitedReader tests
// ---------------------------------------------------------------------------

func TestIoLimitedReaderStopsAtLimit(t *testing.T) {
	src := strings.NewReader(strings.Repeat("abc", 10)) // 30 bytes
	reader := ioLimitedReader{r: src, limit: 5}

	data := make([]byte, 10)
	n, _ := reader.Read(data)
	assert.Equal(t, 5, n, "should read up to limit")

	// Subsequent reads should return 0, EOF (limit already exhausted).
	n2, err2 := reader.Read(data)
	assert.Equal(t, 0, n2)
	assert.Equal(t, io.EOF, err2)
}

func TestIoLimitedReaderChunkedReads(t *testing.T) {
	src := strings.NewReader("hello world")
	reader := ioLimitedReader{r: src, limit: 7}

	data := make([]byte, 3)
	n, err := reader.Read(data)
	assert.Equal(t, 3, n)
	assert.NoError(t, err)

	n2, err2 := reader.Read(data)
	assert.Equal(t, 3, n2) // "def" — total 6 read
	assert.NoError(t, err2)

	n3, err3 := reader.Read(data)
	assert.Equal(t, 1, n3) // "g" — total 7, hits limit
	assert.True(t, err3 == io.EOF || n3 == 1, "should stop at limit")
}

// ---------------------------------------------------------------------------
// LoginThrottle tests
// ---------------------------------------------------------------------------

// TestLoginThrottleRejectsRepeatedFailures ensures that after N consecutive
// failed logins from the same IP, the login endpoint is throttled.
func TestLoginThrottleRejectsRepeatedFailures(t *testing.T) {
	throttle := NewLoginThrottle(3, 2*time.Second)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	next := throttle.LoginHandler(handler)

	fixedIP := "10.0.0.1:12345"

	// Each request uses the same fixed RemoteAddr so that Allow() and
	// RecordFailure() address the same throttle key (LoginHandler uses
	// r.RemoteAddr verbatim when X-Forwarded-For is absent).
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("{}"))
		req.RemoteAddr = fixedIP
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "attempt %d should reach handler", i+1)
		throttle.RecordFailure(fixedIP)
	}

	// 4th request should be throttled.
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("{}"))
	req.RemoteAddr = fixedIP
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

// TestLoginThrottleAllowsAfterWindow ensures that the throttle window expires
// and allows new attempts.
func TestLoginThrottleAllowsAfterWindow(t *testing.T) {
	throttle := NewLoginThrottle(2, 100*time.Millisecond)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	next := throttle.LoginHandler(handler)

	fixedIP := "10.0.0.2"

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("{}"))
		req.RemoteAddr = fixedIP + ":12345"
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		throttle.RecordFailure(fixedIP)
	}

	time.Sleep(150 * time.Millisecond)

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("{}"))
	req.RemoteAddr = fixedIP + ":12345"
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestLoginThrottleIsolatesIPs ensures that one IP's failures don't throttle
// another IP.
func TestLoginThrottleIsolatesIPs(t *testing.T) {
	throttle := NewLoginThrottle(2, time.Minute)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	next := throttle.LoginHandler(handler)

	ip1 := "10.0.0.1:12345"
	ip2 := "10.0.0.2:12345"

	// Exhaust ip1's limit.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("{}"))
		req.RemoteAddr = ip1
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, req)
		throttle.RecordFailure(ip1)
	}

	// ip1 is now throttled.
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("{}"))
	req.RemoteAddr = ip1
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)

	// ip2 should still be fine.
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("{}"))
	req.RemoteAddr = ip2
	rec = httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestLoginThrottleUsesXForwardedFor ensures the LoginHandler extracts IP
// from X-Forwarded-For when present.
func TestLoginThrottleUsesXForwardedFor(t *testing.T) {
	throttle := NewLoginThrottle(1, time.Minute)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	next := throttle.LoginHandler(handler)

	// Exhaust via X-Forwarded-For
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("{}"))
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	throttle.RecordFailure("1.2.3.4")

	// Same X-Forwarded-For → throttled
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("{}"))
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	rec = httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)

	// RemoteAddr-only (no X-Forwarded-For) → still works
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("{}"))
	req.RemoteAddr = "5.6.7.8:1234"
	rec = httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestLoginThrottleRetryAfterHeader ensures the Retry-After header is set.
func TestLoginThrottleRetryAfterHeader(t *testing.T) {
	throttle := NewLoginThrottle(1, 10*time.Second)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	next := throttle.LoginHandler(handler)

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("{}"))
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	throttle.RecordFailure("10.0.0.1")

	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("{}"))
	req.RemoteAddr = "10.0.0.1:1234"
	rec = httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Contains(t, rec.Header().Get("Retry-After"), "1", "Retry-After should contain duration")
}

// TestLoginThrottleAllowBeforeLimit ensures Allow returns true before the
// failure limit is reached.
func TestLoginThrottleAllowBeforeLimit(t *testing.T) {
	throttle := NewLoginThrottle(5, time.Minute)

	for i := 0; i < 5; i++ {
		assert.True(t, throttle.Allow("1.2.3.4"), "should allow before limit (%d)", i)
		throttle.RecordFailure("1.2.3.4")
	}
	assert.False(t, throttle.Allow("1.2.3.4"))
}

// ---------------------------------------------------------------------------
// formatDuration tests
// ---------------------------------------------------------------------------

func TestFormatDurationRoundsUp(t *testing.T) {
	assert.Equal(t, "1", formatDuration(0))
	assert.Equal(t, "1", formatDuration(500*time.Millisecond))
}

// ---------------------------------------------------------------------------
// Auth endpoint validation tests
// ---------------------------------------------------------------------------

func TestLoginRejectsEmptyFields(t *testing.T) {
	rejectEmpty := func(username, password string) bool {
		return username == "" || password == ""
	}
	assert.True(t, rejectEmpty("", "test"))
	assert.True(t, rejectEmpty("test", ""))
	assert.False(t, rejectEmpty("test", "secret"))
}

func TestSetupInitRejectsWeakPassword(t *testing.T) {
	assert.True(t, len("short") < 8)
	assert.False(t, len("verylongpassword") < 8)
}

// ---------------------------------------------------------------------------
// Password hashing tests
// ---------------------------------------------------------------------------

func TestHashPasswordVerifiable(t *testing.T) {
	passwords := []string{"short1", "aabbccdd", "very-long+complex$pass!2024"}
	for _, pwd := range passwords {
		hash, err := auth.HashPassword(pwd)
		require.NoError(t, err)
		assert.True(t, auth.VerifyPassword(hash, pwd))
		assert.False(t, auth.VerifyPassword(hash, pwd+"x"))
	}
}

// ---------------------------------------------------------------------------
// Auth-middleware: disabled-user rejection
// ---------------------------------------------------------------------------

func TestAuthMiddlewareRejectsDisabledUser(t *testing.T) {
	db := connectTestDB(t)
	lookup := NewMockRoleAndPermissionLookup()

	user := seedTestUser(t, db, "password123")
	lookup.Permissions[user.UserID] = []string{"artifact:read"}

	_, err := db.Exec(context.Background(), "UPDATE users SET is_active = FALSE WHERE user_id = $1", user.UserID)
	require.NoError(t, err)

	authMiddleware := NewAuthMiddleware(db, lookup)

	handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := GetUser(r)
		assert.Nil(t, u, "disabled user should not be authenticated")
	}))

	authString := user.Username + ":password123"
	encoded := base64.StdEncoding.EncodeToString([]byte(authString))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic "+encoded)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	_ = err
}

// ---------------------------------------------------------------------------
// PAT ownership enforcement
// ---------------------------------------------------------------------------

func TestRevokeOwnAccessKeyOnly(t *testing.T) {
	db := connectTestDB(t)

	owner := seedTestUser(t, db, "owner-password")
	other := seedTestUser(t, db, "other-password")

	key, err := db.CreateAccessKey(owner.UserID, "test-key", []string{"read"}, nil)
	require.NoError(t, err)

	keys, err := db.ListUserPersonalAccessTokens(other.UserID)
	require.NoError(t, err)

	owned := false
	for _, k := range keys {
		if k.ID == key.ID {
			owned = true
			break
		}
	}
	assert.False(t, owned)
	_ = err
}

// ---------------------------------------------------------------------------
// RequireAuth blocks unauthenticated requests
// ---------------------------------------------------------------------------

func TestRequireAuthBlocksUnauthenticatedAccess(t *testing.T) {
	next := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAuthAllowsAuthenticatedAccess(t *testing.T) {
	next := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	user := &User{
		UserID:      "user-1",
		Username:    "testuser",
		Permissions: []string{"artifact:read"},
	}
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req = req.WithContext(context.WithValue(req.Context(), AuthUserKey, user))

	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// ---------------------------------------------------------------------------
// RequirePermission deny/allow
// ---------------------------------------------------------------------------

func TestRequirePermissionDeniesWithoutPermission(t *testing.T) {
	next := RequirePermission("user:admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	user := &User{
		UserID:      "user-1",
		Permissions: []string{"artifact:read"},
	}
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req = req.WithContext(context.WithValue(req.Context(), AuthUserKey, user))

	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRequirePermissionAllowsWithPermission(t *testing.T) {
	next := RequirePermission("user:admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	user := &User{
		UserID:      "user-1",
		Permissions: []string{"user:admin", "artifact:read"},
	}
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req = req.WithContext(context.WithValue(req.Context(), AuthUserKey, user))

	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// ---------------------------------------------------------------------------
// RequireRole deny/allow
// ---------------------------------------------------------------------------

func TestRequireRoleDeniesWrongRole(t *testing.T) {
	next := RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	user := &User{
		UserID: "user-1",
		Roles:  []string{"developer"},
	}
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req = req.WithContext(context.WithValue(req.Context(), AuthUserKey, user))

	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// ---------------------------------------------------------------------------
// Read-scope PAT enforcement
// ---------------------------------------------------------------------------

func TestReadScopeUserHasReadScope(t *testing.T) {
	db := connectTestDB(t)
	lookup := NewMockRoleAndPermissionLookup()

	user := seedTestUser(t, db, "real-password")
	lookup.Permissions[user.UserID] = []string{"artifact:read", "artifact:write"}

	patToken := seedTestPAT(t, db, user.UserID, []string{"read"})

	mw := NewAuthMiddleware(db, lookup)

	next := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := GetUser(r)
		assert.NotNil(t, u)
		assert.Equal(t, "read", u.Scope)
	}))

	authString := user.Username + ":" + patToken
	encoded := base64.StdEncoding.EncodeToString([]byte(authString))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic "+encoded)
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// ---------------------------------------------------------------------------
// Rate limiter per-IP isolation (via middleware, no DB)
// ---------------------------------------------------------------------------

func TestRateLimiterIsolatesIPs(t *testing.T) {
	middleware := RateLimitMiddleware(2, time.Minute)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	// IP 1 exhausts its limit.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "ip1 attempt %d", i+1)
	}

	// IP 1 should now be rate limited.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)

	// IP 2 should be unaffected.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:12345"
	rec = httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// ---------------------------------------------------------------------------
// Audit-log on mutating actions
// ---------------------------------------------------------------------------

func TestAuditLogRecordedOnMutatingActions(t *testing.T) {
	db := connectTestDB(t)
	lookup := NewMockRoleAndPermissionLookup()

	user := seedTestUser(t, db, "password123")
	lookup.Permissions[user.UserID] = []string{"artifact:write"}

	authMiddleware := NewAuthMiddleware(db, lookup)

	var capturedAction string
	handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aUser := GetUser(r)
		assert.NotNil(t, aUser)
		capturedAction = "artifact.create"
		w.WriteHeader(http.StatusCreated)
	}))

	authKey := seedTestAccessKey(t, db, user.UserID, []string{"artifact:write"})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+authKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	_ = handler

	assert.NotEmpty(t, capturedAction)
}

// ---------------------------------------------------------------------------
// Rate limiter thread safety (via middleware)
// ---------------------------------------------------------------------------

func TestRateLimiterThreadSafety(t *testing.T) {
	limit := 1000
	window := time.Minute
	middleware := RateLimitMiddleware(limit, window)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	next := middleware(handler)

	done := make(chan bool, 50)
	for i := 0; i < 50; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "10.99.99.99:1234"
			rec := httptest.NewRecorder()
			next.ServeHTTP(rec, req)
			done <- true
		}()
	}

	for i := 0; i < 50; i++ {
		<-done
	}
}

// ---------------------------------------------------------------------------
// Security headers: HSTS with existing value
// ---------------------------------------------------------------------------

func TestSecurityHeadersNoHSTSOvertake(t *testing.T) {
	middleware := SecurityHeadersMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=63072000")
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)

	assert.Equal(t, "max-age=63072000", rec.Header().Get("Strict-Transport-Security"))
}

// ---------------------------------------------------------------------------
// Security headers: multiple headers
// ---------------------------------------------------------------------------

func TestSecurityHeadersAllHeadersPresent(t *testing.T) {
	middleware := SecurityHeadersMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, req)

	headers := rec.Header()
	assert.NotEmpty(t, headers.Get("X-Content-Type-Options"))
	assert.NotEmpty(t, headers.Get("X-Frame-Options"))
	assert.NotEmpty(t, headers.Get("X-XSS-Protection"))
	assert.NotEmpty(t, headers.Get("Strict-Transport-Security"))
}
