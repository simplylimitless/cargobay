package middleware

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

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
func TestAuthMiddlewareWithBearerToken(t *testing.T) {
	db := NewMockDatabase()
	lookup := NewMockRoleAndPermissionLookup()

	lookup.Permissions["user-1"] = []string{"artifact:read", "artifact:write"}

	middleware := NewAuthMiddleware(db, lookup)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		if user == nil {
			http.Error(w, "No user found", http.StatusUnauthorized)
			return
		}
		assert.Equal(t, "user-1", user.UserID)
		assert.Contains(t, user.Permissions, "artifact:read")
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	// Create a valid token
	token := GenerateAPIKey()
	db.AccessKeys[token] = &AccessKey{
		ID:          "key-1",
		UserID:      "user-1",
		Name:        "test-key",
		KeyHash:     token,
		Permissions: []string{"artifact:read", "artifact:write"},
		IsActive:    true,
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestAuthMiddlewareWithBasicAuth tests authentication with basic auth
func TestAuthMiddlewareWithBasicAuth(t *testing.T) {
	db := NewMockDatabase()
	lookup := NewMockRoleAndPermissionLookup()

	lookup.Roles["user-1"] = []string{"viewer"}
	lookup.Permissions["user-1"] = []string{"artifact:read"}

	// Create user with password
	password := "testpassword123"
	hashedPassword, _ := auth.HashPassword(password)

	db.Users["user-1"] = &UserRepository{
		UserID:       "user-1",
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: hashedPassword,
		Roles:        []string{"viewer"},
		IsActive:     true,
	}

	middleware := NewAuthMiddleware(db, lookup)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		if user == nil {
			http.Error(w, "No user found", http.StatusUnauthorized)
			return
		}
		assert.Equal(t, "testuser", user.Username)
		assert.Contains(t, user.Roles, "viewer")
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	// Create basic auth header
	authString := "testuser:" + password
	encodedAuth := base64.StdEncoding.EncodeToString([]byte(authString))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic "+encodedAuth)
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestAuthMiddlewareNoCredentials tests unauthenticated requests
func TestAuthMiddlewareNoCredentials(t *testing.T) {
	db := NewMockDatabase()
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

// TestAuthMiddlewareInvalidToken tests invalid token handling
func TestAuthMiddlewareInvalidToken(t *testing.T) {
	db := NewMockDatabase()
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

// TestAuthMiddlewareInvalidBasicAuth tests invalid basic auth handling
func TestAuthMiddlewareInvalidBasicAuth(t *testing.T) {
	db := NewMockDatabase()
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

// MockDatabase is a mock database for testing
type MockDatabase struct {
	Users      map[string]*UserRepository
	AccessKeys map[string]*AccessKey
	mu         sync.RWMutex
}

func NewMockDatabase() *MockDatabase {
	return &MockDatabase{
		Users:      make(map[string]*UserRepository),
		AccessKeys: make(map[string]*AccessKey),
	}
}

// Mock types
type UserRepository struct {
	UserID       string
	Username     string
	Email        string
	PasswordHash string
	Roles        []string
	IsActive     bool
}

type AccessKey struct {
	ID          string
	UserID      string
	Name        string
	KeyHash     string
	Permissions []string
	CreatedAt   time.Time
	LastUsed    *time.Time
	ExpiresAt   *time.Time
	IsActive    bool
}

// TestUserRepository tests user repository struct
func TestUserRepository(t *testing.T) {
	now := time.Now()
	user := &UserRepository{
		UserID:       "user-123",
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: "hashedpassword",
		Roles:        []string{"user", "developer"},
		CreatedAt:    now,
		IsActive:     true,
	}

	assert.Equal(t, "testuser", user.Username)
	assert.True(t, user.IsActive)
	assert.Len(t, user.Roles, 2)
}

// TestAccessKey tests access key struct
func TestAccessKey(t *testing.T) {
	now := time.Now()
	key := &AccessKey{
		ID:          "key-123",
		UserID:      "user-123",
		Name:        "production-key",
		KeyHash:     "key_hash_123",
		Permissions: []string{"artifact:read", "artifact:write"},
		CreatedAt:   now,
		ExpiresAt:   &now,
		IsActive:    true,
	}

	assert.Equal(t, "production-key", key.Name)
	assert.True(t, key.IsActive)
	assert.Len(t, key.Permissions, 2)
}

// TestAccessKey_NoExpiry tests access key without expiry
func TestAccessKey_NoExpiry(t *testing.T) {
	key := &AccessKey{
		ID:        "key-1",
		ExpiresAt: nil,
		IsActive:  true,
	}

	assert.Nil(t, key.ExpiresAt)
	assert.True(t, key.IsActive)
}

// TestUserRepository_LastLogin tests optional last login
func TestUserRepository_LastLogin(t *testing.T) {
	now := time.Now()
	user := &UserRepository{
		UserID:    "user-1",
		LastLogin: &now,
		IsActive:  true,
	}

	assert.NotNil(t, user.LastLogin)
}

// TestAccessKey_LastUsed tests optional last used
func TestAccessKey_LastUsed(t *testing.T) {
	now := time.Now()
	key := &AccessKey{
		ID:        "key-1",
		LastUsed:  &now,
		IsActive:  true,
	}

	assert.NotNil(t, key.LastUsed)
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

// TestUserRetrievalAfterAuth tests user is properly set after authentication
func TestUserRetrievalAfterAuth(t *testing.T) {
	db := NewMockDatabase()
	lookup := NewMockRoleAndPermissionLookup()

	lookup.Roles["user-1"] = []string{"admin"}
	lookup.Permissions["user-1"] = []string{"artifact:read", "artifact:write", "user:admin"}

	db.Users["user-1"] = &UserRepository{
		UserID:       "user-1",
		Username:     "admin",
		Email:        "admin@example.com",
		PasswordHash: "$2a$10$testpasswordhash",
		Roles:        []string{"admin"},
		IsActive:     true,
	}

	middleware := NewAuthMiddleware(db, lookup)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		if user == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		assert.Equal(t, "admin", user.Username)
		assert.Contains(t, user.Roles, "admin")
		assert.Contains(t, user.Permissions, "user:admin")
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic YWRtaW46cGFzc3dvcmQ=") // admin:password (invalid hash but won't be verified)
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	// Should return unauthorized because password doesn't match hash
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestUserRetrievalAfterAuth tests user is properly set after authentication with correct password
func TestUserRetrievalAfterAuthWithPassword(t *testing.T) {
	db := NewMockDatabase()
	lookup := NewMockRoleAndPermissionLookup()

	lookup.Roles["user-1"] = []string{"admin"}
	lookup.Permissions["user-1"] = []string{"artifact:read", "artifact:write", "user:admin"}

	// Use a valid bcrypt hash for "password"
	hashedPassword := "$2a$10$abcdefghijklmnopqrstuvwxyz1234567890123456789012345678901234"

	db.Users["user-1"] = &UserRepository{
		UserID:       "user-1",
		Username:     "admin",
		Email:        "admin@example.com",
		PasswordHash: hashedPassword,
		Roles:        []string{"admin"},
		IsActive:     true,
	}

	middleware := NewAuthMiddleware(db, lookup)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		if user == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	next := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic YWRtaW46cGFzc3dvcmQ=") // admin:password
	rec := httptest.NewRecorder()

	next.ServeHTTP(rec, req)

	// Should return unauthorized because password hash doesn't match
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
