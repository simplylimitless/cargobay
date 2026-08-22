package middleware

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/auth"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockRoleAndPermissionLookup is a mock implementation
type MockRoleAndPermissionLookup struct {
	Roles       map[string][]string
	Permissions map[string][]string
}

func NewMockRoleAndPermissionLookup() *MockRoleAndPermissionLookup {
	return &MockRoleAndPermissionLookup{
		Roles:       make(map[string][]string),
		Permissions: make(map[string][]string),
	}
}

func (m *MockRoleAndPermissionLookup) GetUserRoles(userID string) ([]string, error) {
	if roles, ok := m.Roles[userID]; ok {
		return roles, nil
	}
	return []string{}, nil
}

func (m *MockRoleAndPermissionLookup) GetUserRolePermissions(userID string) []string {
	if perms, ok := m.Permissions[userID]; ok {
		return perms
	}
	return []string{}
}

// connectTestDB connects to the docker-compose Postgres instance. The
// auth-success paths in NewAuthMiddleware issue real queries, so those
// tests need a live, connected *database.Database — not the unconnected
// instances used elsewhere (see pkg/rbac/rbac_test.go for that pattern).
// Skipped when no database is reachable.
func connectTestDB(t *testing.T) *database.Database {
	t.Helper()
	db := database.New("postgres://cargobay:password@localhost:5432/cargobay")
	if err := db.Connect(); err != nil {
		t.Skipf("no postgres available: %v", err)
	}
	t.Cleanup(func() { db.Disconnect() })
	return db
}

// seedTestUser creates a real user row (cleaned up via t.Cleanup) for tests
// that exercise NewAuthMiddleware's Basic-auth success path.
func seedTestUser(t *testing.T, db *database.Database, password string) *database.UserRepository {
	t.Helper()
	hash, err := auth.HashPassword(password)
	require.NoError(t, err)

	username := fmt.Sprintf("test-user-%d", time.Now().UnixNano())
	user, err := db.CreateUser(username, username+"@example.com", hash, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Exec(context.Background(), "DELETE FROM users WHERE user_id = $1", user.UserID)
	})
	return user
}

// seedTestAccessKey creates a real access key row (cleaned up via
// t.Cleanup) for tests that exercise NewAuthMiddleware's Bearer-token
// success path. Returns the bearer token to send in the Authorization
// header (access_keys.key_hash is compared in plaintext).
func seedTestAccessKey(t *testing.T, db *database.Database, userID string, permissions []string) string {
	t.Helper()
	key, err := db.CreateAccessKey(userID, "test-key", permissions, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Exec(context.Background(), "DELETE FROM access_keys WHERE id = $1", key.ID)
	})
	return key.KeyHash
}

// TestNewRateLimiter creates a new rate limiter and tests its initialization
func TestNewRateLimiter(t *testing.T) {
	limit := 100
	window := time.Minute
	rl := NewRateLimiter(limit, window)

	assert.NotNil(t, rl)
	assert.Equal(t, limit, rl.limit)
	assert.Equal(t, window, rl.window)
	assert.NotNil(t, rl.requests)
}

// TestRateLimitMiddleware tests rate limiting middleware
func TestRateLimitMiddleware(t *testing.T) {
	limit := 5
	window := time.Minute
	middleware := RateLimitMiddleware(limit, window)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	// Make requests
	for i := 0; i < limit; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		rec := httptest.NewRecorder()

		next.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("X-RateLimit-Limit"), "5")
	}

	// Make one more request - should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Contains(t, rec.Body.String(), "Rate limit exceeded")
}

// TestRateLimitMiddlewareWithXForwardedFor tests using X-Forwarded-For header
func TestRateLimitMiddlewareWithXForwardedFor(t *testing.T) {
	limit := 3
	window := time.Minute
	middleware := RateLimitMiddleware(limit, window)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	// Make requests with X-Forwarded-For
	for i := 0; i < limit; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", "192.168.1.1")
		rec := httptest.NewRecorder()

		next.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	}

	// Make one more request - should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "192.168.1.1")
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

// TestGetRateLimitHeaders tests rate limit headers generation
func TestGetRateLimitHeaders(t *testing.T) {
	limit := 10
	window := time.Minute
	now := time.Now()

	info := &rateInfo{
		count:     5,
		windowEnd: now.Add(window),
	}

	headers := GetRateLimitHeaders(info, limit)

	assert.Equal(t, "10", headers.Get("X-RateLimit-Limit"))
	assert.Equal(t, "5", headers.Get("X-RateLimit-Remaining"))
}

// TestNormalizePath tests path normalization
func TestNormalizePath(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path should be normalized
		assert.Equal(t, "/api/v1/test", r.URL.Path)
		assert.Equal(t, "", r.URL.RawPath)
		w.WriteHeader(http.StatusOK)
	})

	next := NormalizePath(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.URL.RawPath = "/api/v1/test" // Set raw path to test normalization
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestAuthMiddlewareWithBearerToken tests authentication with bearer token
// against a live, seeded database.
func TestAuthMiddlewareWithBearerToken(t *testing.T) {
	db := connectTestDB(t)
	lookup := NewMockRoleAndPermissionLookup()

	user := seedTestUser(t, db, "testpassword123")
	lookup.Permissions[user.UserID] = []string{"artifact:read", "artifact:write"}
	token := seedTestAccessKey(t, db, user.UserID, []string{"artifact:read", "artifact:write"})

	middleware := NewAuthMiddleware(db, lookup)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authedUser := GetUser(r)
		if authedUser == nil {
			http.Error(w, "No user found", http.StatusUnauthorized)
			return
		}
		assert.Equal(t, user.UserID, authedUser.UserID)
		assert.Contains(t, authedUser.Permissions, "artifact:read")
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestAuthMiddlewareWithBasicAuth tests authentication with basic auth
// against a live, seeded database.
func TestAuthMiddlewareWithBasicAuth(t *testing.T) {
	db := connectTestDB(t)
	lookup := NewMockRoleAndPermissionLookup()

	password := "testpassword123"
	user := seedTestUser(t, db, password)
	lookup.Roles[user.UserID] = []string{"viewer"}
	lookup.Permissions[user.UserID] = []string{"artifact:read"}

	middleware := NewAuthMiddleware(db, lookup)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authedUser := GetUser(r)
		if authedUser == nil {
			http.Error(w, "No user found", http.StatusUnauthorized)
			return
		}
		assert.Equal(t, user.Username, authedUser.Username)
		assert.Contains(t, authedUser.Roles, "viewer")
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	// Create basic auth header
	authString := user.Username + ":" + password
	encodedAuth := base64.StdEncoding.EncodeToString([]byte(authString))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic "+encodedAuth)
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestAuthMiddlewareNoCredentials tests unauthenticated requests. No
// Authorization header means NewAuthMiddleware returns before ever
// querying the database, so an unconnected instance is safe here.
func TestAuthMiddlewareNoCredentials(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")
	lookup := NewMockRoleAndPermissionLookup()

	middleware := NewAuthMiddleware(db, lookup)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		assert.Nil(t, user)
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestAuthMiddlewareInvalidToken tests invalid token handling. Uses a
// live DB since ValidateAccessKey is actually queried, but no row will
// match so the request proceeds unauthenticated.
func TestAuthMiddlewareInvalidToken(t *testing.T) {
	db := connectTestDB(t)
	lookup := NewMockRoleAndPermissionLookup()

	middleware := NewAuthMiddleware(db, lookup)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		assert.Nil(t, user)
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestAuthMiddlewareInvalidBasicAuth tests invalid basic auth handling.
// The base64 decode fails before any query, so an unconnected instance
// is safe here.
func TestAuthMiddlewareInvalidBasicAuth(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")
	lookup := NewMockRoleAndPermissionLookup()

	middleware := NewAuthMiddleware(db, lookup)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		assert.Nil(t, user)
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic invalid-base64")
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestRequireAuth tests authentication requirement
func TestRequireAuth(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	next := RequireAuth(handler)

	// Request without auth context
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "Unauthorized")
}

// TestRequireRole tests role requirement
func TestRequireRole(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	next := RequireRole("admin")(handler)

	// Request without user context
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestRequireRoleWithRole tests role requirement with valid role
func TestRequireRoleWithRole(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	next := RequireRole("admin")(handler)

	// Create request with user who has admin role
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	user := &User{
		UserID:      "user-1",
		Username:    "admin",
		Email:       "admin@example.com",
		Roles:       []string{"admin", "viewer"},
		Permissions: []string{"artifact:read", "artifact:write", "user:admin"},
	}
	ctx := context.WithValue(req.Context(), AuthUserKey, user)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestRequireRoleWithoutRole tests role requirement without required role
func TestRequireRoleWithoutRole(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	next := RequireRole("admin")(handler)

	// Create request with user who does not have admin role
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	user := &User{
		UserID:      "user-1",
		Username:    "viewer",
		Email:       "viewer@example.com",
		Roles:       []string{"viewer"},
		Permissions: []string{"artifact:read"},
	}
	ctx := context.WithValue(req.Context(), AuthUserKey, user)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// TestRequirePermission tests permission requirement
func TestRequirePermission(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	next := RequirePermission("artifact:admin")(handler)

	// Create request without user context
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestRequirePermissionWithPermission tests permission requirement with valid permission
func TestRequirePermissionWithPermission(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	next := RequirePermission("artifact:read")(handler)

	// Create request with user who has the required permission
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	user := &User{
		UserID:      "user-1",
		Username:    "viewer",
		Email:       "viewer@example.com",
		Roles:       []string{"viewer"},
		Permissions: []string{"artifact:read", "artifact:search"},
	}
	ctx := context.WithValue(req.Context(), AuthUserKey, user)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestRequirePermissionWithoutPermission tests permission requirement without valid permission
func TestRequirePermissionWithoutPermission(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	next := RequirePermission("artifact:admin")(handler)

	// Create request with user who does not have the required permission
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	user := &User{
		UserID:      "user-1",
		Username:    "viewer",
		Email:       "viewer@example.com",
		Roles:       []string{"viewer"},
		Permissions: []string{"artifact:read", "artifact:search"},
	}
	ctx := context.WithValue(req.Context(), AuthUserKey, user)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// TestGenerateAPIKey tests API key generation
func TestGenerateAPIKey(t *testing.T) {
	key := GenerateAPIKey()

	assert.NotEmpty(t, key)
	assert.Contains(t, key, "key_")
}

// TestRateLimitConcurrency tests rate limiter thread safety
func TestRateLimitConcurrency(t *testing.T) {
	limit := 100
	window := time.Minute
	rl := NewRateLimiter(limit, window)

	done := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		go func(index int) {
			key := "concurrent-key"
			now := time.Now()

			rl.mu.Lock()
			info, exists := rl.requests[key]

			if !exists || now.After(info.windowEnd) {
				info = &rateInfo{
					count:     1,
					windowEnd: now.Add(window),
				}
				rl.requests[key] = info
			} else {
				info.count++
			}
			rl.mu.Unlock()
			done <- true
		}(i)
	}

	for i := 0; i < 20; i++ {
		<-done
	}

	rl.mu.Lock()
	info, exists := rl.requests["concurrent-key"]
	rl.mu.Unlock()

	assert.True(t, exists)
	assert.Equal(t, 20, info.count)
}

// TestGetUser tests user retrieval from context
func TestGetUser(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	user := &User{
		UserID:   "user-1",
		Username: "testuser",
		Email:    "test@example.com",
	}

	ctx := context.WithValue(req.Context(), AuthUserKey, user)
	req = req.WithContext(ctx)

	retrieved := GetUser(req)
	assert.NotNil(t, retrieved)
	assert.Equal(t, "user-1", retrieved.UserID)
	assert.Equal(t, "testuser", retrieved.Username)
}

// TestGetUserNoContext tests user retrieval when no user in context
func TestGetUserNoContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	user := GetUser(req)
	assert.Nil(t, user)
}

// TestRateLimitWindowExpiration tests window expiration behavior
func TestRateLimitWindowExpiration(t *testing.T) {
	limit := 2
	window := 100 * time.Millisecond
	middleware := RateLimitMiddleware(limit, window)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	// Make requests within the window
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "127.0.0.1:12345"
	rec1 := httptest.NewRecorder()
	next.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "127.0.0.1:12345"
	rec2 := httptest.NewRecorder()
	next.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusOK, rec2.Code)

	// Make request after window expires
	time.Sleep(window + 10*time.Millisecond)

	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.RemoteAddr = "127.0.0.1:12345"
	rec3 := httptest.NewRecorder()
	next.ServeHTTP(rec3, req3)
	assert.Equal(t, http.StatusOK, rec3.Code) // Should be allowed again
}

// TestUserRetrievalAfterAuth tests that a wrong password is rejected
// against a live, seeded database.
func TestUserRetrievalAfterAuth(t *testing.T) {
	db := connectTestDB(t)
	lookup := NewMockRoleAndPermissionLookup()

	user := seedTestUser(t, db, "correct-password")
	lookup.Roles[user.UserID] = []string{"admin"}
	lookup.Permissions[user.UserID] = []string{"artifact:read", "artifact:write", "user:admin"}

	middleware := NewAuthMiddleware(db, lookup)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authedUser := GetUser(r)
		if authedUser == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	authString := user.Username + ":wrong-password"
	encodedAuth := base64.StdEncoding.EncodeToString([]byte(authString))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic "+encodedAuth)
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	// Should return unauthorized because the password doesn't match
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestUserRetrievalAfterAuthWithPassword tests that a correct password is
// accepted and the user is populated in context, against a live, seeded
// database.
func TestUserRetrievalAfterAuthWithPassword(t *testing.T) {
	db := connectTestDB(t)
	lookup := NewMockRoleAndPermissionLookup()

	password := "correct-password"
	user := seedTestUser(t, db, password)
	lookup.Roles[user.UserID] = []string{"admin"}
	lookup.Permissions[user.UserID] = []string{"artifact:read", "artifact:write", "user:admin"}

	middleware := NewAuthMiddleware(db, lookup)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authedUser := GetUser(r)
		if authedUser == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		assert.Equal(t, user.Username, authedUser.Username)
		assert.Contains(t, authedUser.Roles, "admin")
		assert.Contains(t, authedUser.Permissions, "user:admin")
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	authString := user.Username + ":" + password
	encodedAuth := base64.StdEncoding.EncodeToString([]byte(authString))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic "+encodedAuth)
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}
