package middleware

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
)

// MockRoleAndPermissionLookup is a mock implementation of RoleAndPermissionLookup
type MockRoleAndPermissionLookup struct {
	userRoles         []string
	userPermissions   []string
	getUserRolesFunc  func(string) ([]string, error)
	getUserPermsFunc  func(string) []string
}

// GetUserRoles implements RoleAndPermissionLookup interface
func (m *MockRoleAndPermissionLookup) GetUserRoles(userID string) ([]string, error) {
	if m.getUserRolesFunc != nil {
		return m.getUserRolesFunc(userID)
	}
	return m.userRoles, nil
}

// GetUserRolePermissions implements RoleAndPermissionLookup interface
func (m *MockRoleAndPermissionLookup) GetUserRolePermissions(userID string) []string {
	if m.getUserPermsFunc != nil {
		return m.getUserPermsFunc(userID)
	}
	return m.userPermissions
}

// MockDatabase is a mock implementation of authStore
type MockDatabase struct {
	users                  map[string]*database.UserRepository
	accessKeys             map[string]*database.AccessKey
	getUserByUsernameFunc  func(string) (*database.UserRepository, error)
	getUserByIDFunc        func(string) (*database.UserRepository, error)
	validateAccessKeyFunc  func(string) (*database.AccessKey, error)
}

// GetUserByUsername implements authStore interface
func (m *MockDatabase) GetUserByUsername(username string) (*database.UserRepository, error) {
	if m.getUserByUsernameFunc != nil {
		return m.getUserByUsernameFunc(username)
	}
	for _, u := range m.users {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, nil
}

// GetUserByID implements authStore interface
func (m *MockDatabase) GetUserByID(userID string) (*database.UserRepository, error) {
	if m.getUserByIDFunc != nil {
		return m.getUserByIDFunc(userID)
	}
	if u, ok := m.users[userID]; ok {
		return u, nil
	}
	return nil, nil
}

// ValidateAccessKey implements authStore interface
func (m *MockDatabase) ValidateAccessKey(key string) (*database.AccessKey, error) {
	if m.validateAccessKeyFunc != nil {
		return m.validateAccessKeyFunc(key)
	}
	if ak, ok := m.accessKeys[key]; ok {
		return ak, nil
	}
	return nil, nil
}

// TestNewAuthMiddlewareNoAuthHeader tests middleware with no Authorization header
func TestNewAuthMiddlewareNoAuthHeader(t *testing.T) {
	db := &MockDatabase{
		users: make(map[string]*database.UserRepository),
	}
	roles := &MockRoleAndPermissionLookup{}

	handler := NewAuthMiddleware(db, roles)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestNewAuthMiddlewareInvalidBasicAuth tests middleware with invalid Basic auth
func TestNewAuthMiddlewareInvalidBasicAuth(t *testing.T) {
	db := &MockDatabase{
		users: make(map[string]*database.UserRepository),
	}
	roles := &MockRoleAndPermissionLookup{}

	handler := NewAuthMiddleware(db, roles)

	// Invalid base64
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic invalid-base64!")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestNewAuthMiddlewareValidBasicAuth tests middleware with valid Basic auth
func TestNewAuthMiddlewareValidBasicAuth(t *testing.T) {
	db := &MockDatabase{
		users: map[string]*database.UserRepository{
			"user1": {
				UserID:       "user1",
				Username:     "testuser",
				Email:        "test@example.com",
				PasswordHash: "$2a$10$examplehash", // bcrypt hash
				IsActive:     true,
			},
		},
	}
	roles := &MockRoleAndPermissionLookup{
		userRoles:       []string{"admin"},
		userPermissions: []string{"read", "write"},
	}

	handler := NewAuthMiddleware(db, roles)

	// Valid basic auth: username:password
	authString := base64.StdEncoding.EncodeToString([]byte("testuser:password"))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic "+authString)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Check that user was added to context
	user := GetUser(req)
	assert.NotNil(t, user)
	assert.Equal(t, "testuser", user.Username)
	assert.Equal(t, []string{"admin"}, user.Roles)
	assert.Equal(t, []string{"read", "write"}, user.Permissions)
}

// TestNewAuthMiddlewareBasicAuthUserNotFound tests middleware with Basic auth for non-existent user
func TestNewAuthMiddlewareBasicAuthUserNotFound(t *testing.T) {
	db := &MockDatabase{
		users: make(map[string]*database.UserRepository),
	}
	roles := &MockRoleAndPermissionLookup{}

	handler := NewAuthMiddleware(db, roles)

	// Valid basic auth but user doesn't exist
	authString := base64.StdEncoding.EncodeToString([]byte("nonexistent:user"))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic "+authString)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, GetUser(req))
}

// TestNewAuthMiddlewareBasicAuthInactiveUser tests middleware with Basic auth for inactive user
func TestNewAuthMiddlewareBasicAuthInactiveUser(t *testing.T) {
	db := &MockDatabase{
		users: map[string]*database.UserRepository{
			"user1": {
				UserID:       "user1",
				Username:     "testuser",
				Email:        "test@example.com",
				PasswordHash: "$2a$10$examplehash",
				IsActive:     false,
			},
		},
	}
	roles := &MockRoleAndPermissionLookup{}

	handler := NewAuthMiddleware(db, roles)

	authString := base64.StdEncoding.EncodeToString([]byte("testuser:password"))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic "+authString)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, GetUser(req))
}

// TestNewAuthMiddlewareBearerToken tests middleware with Bearer token
func TestNewAuthMiddlewareBearerToken(t *testing.T) {
	db := &MockDatabase{
		users: map[string]*database.UserRepository{
			"user1": {
				UserID:       "user1",
				Username:     "testuser",
				Email:        "test@example.com",
				PasswordHash: "$2a$10$examplehash",
				IsActive:     true,
			},
		},
		accessKeys: map[string]*database.AccessKey{
			"valid-token": {
				UserID:   "user1",
				IsActive: true,
			},
		},
	}
	roles := &MockRoleAndPermissionLookup{
		userRoles:       []string{"user"},
		userPermissions: []string{"read"},
	}

	handler := NewAuthMiddleware(db, roles)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	user := GetUser(req)
	assert.NotNil(t, user)
	assert.Equal(t, "user1", user.UserID)
	assert.Equal(t, "testuser", user.Username)
}

// TestNewAuthMiddlewareInvalidBearerToken tests middleware with invalid Bearer token
func TestNewAuthMiddlewareInvalidBearerToken(t *testing.T) {
	db := &MockDatabase{
		accessKeys: make(map[string]*database.AccessKey),
	}
	roles := &MockRoleAndPermissionLookup{}

	handler := NewAuthMiddleware(db, roles)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, GetUser(req))
}

// TestNewAuthMiddlewareUnknownAuthType tests middleware with unknown auth type
func TestNewAuthMiddlewareUnknownAuthType(t *testing.T) {
	db := &MockDatabase{
		users: make(map[string]*database.UserRepository),
	}
	roles := &MockRoleAndPermissionLookup{}

	handler := NewAuthMiddleware(db, roles)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Digest nonce=abc123")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, GetUser(req))
}

// TestNewAuthMiddlewareEmptyAuthorizationHeader tests middleware with empty Authorization header
func TestNewAuthMiddlewareEmptyAuthorizationHeader(t *testing.T) {
	db := &MockDatabase{
		users: make(map[string]*database.UserRepository),
	}
	roles := &MockRoleAndPermissionLookup{}

	handler := NewAuthMiddleware(db, roles)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, GetUser(req))
}

// TestNewAuthMiddlewareBasicAuthMalformed tests middleware with malformed Basic auth
func TestNewAuthMiddlewareBasicAuthMalformed(t *testing.T) {
	db := &MockDatabase{
		users: make(map[string]*database.UserRepository),
	}
	roles := &MockRoleAndPermissionLookup{}

	handler := NewAuthMiddleware(db, roles)

	// Basic auth without colon separator
	authString := base64.StdEncoding.EncodeToString([]byte("invalidformat"))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic "+authString)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, GetUser(req))
}

// TestGetUser tests GetUser function
func TestGetUser(t *testing.T) {
	// Test with user in context
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	user := &User{
		UserID:      "user1",
		Username:    "testuser",
		Email:       "test@example.com",
		Roles:       []string{"admin"},
		Permissions: []string{"read", "write"},
	}
	req = req.WithContext(context.WithValue(req.Context(), AuthUserKey, user))

	retrievedUser := GetUser(req)
	assert.NotNil(t, retrievedUser)
	assert.Equal(t, "testuser", retrievedUser.Username)
}

// TestGetUserNoContext tests GetUser function with no context
func TestGetUserNoContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	user := GetUser(req)
	assert.Nil(t, user)
}

// TestGetUserWrongType tests GetUser function with wrong type in context
func TestGetUserWrongType(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), AuthUserKey, "not a user"))
	user := GetUser(req)
	assert.Nil(t, user)
}

// TestRequireAuthWithAuthenticatedUser tests RequireAuth with authenticated user
func TestRequireAuthWithAuthenticatedUser(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	user := &User{
		UserID:      "user1",
		Username:    "testuser",
		Email:       "test@example.com",
		Roles:       []string{"admin"},
		Permissions: []string{"read", "write"},
	}
	req = req.WithContext(context.WithValue(req.Context(), AuthUserKey, user))

	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestRequireAuthWithNoAuthenticatedUser tests RequireAuth with no authenticated user
func TestRequireAuthWithNoAuthenticatedUser(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "Unauthorized")
}

// TestRequireRoleWithMatchingRole tests RequireRole with matching role
func TestRequireRoleWithMatchingRole(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	user := &User{
		UserID:      "user1",
		Username:    "testuser",
		Email:       "test@example.com",
		Roles:       []string{"admin", "user"},
		Permissions: []string{"read", "write"},
	}
	req = req.WithContext(context.WithValue(req.Context(), AuthUserKey, user))

	handler := RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestRequireRoleWithNonMatchingRole tests RequireRole with non-matching role
func TestRequireRoleWithNonMatchingRole(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	user := &User{
		UserID:      "user1",
		Username:    "testuser",
		Email:       "test@example.com",
		Roles:       []string{"user"},
		Permissions: []string{"read", "write"},
	}
	req = req.WithContext(context.WithValue(req.Context(), AuthUserKey, user))

	handler := RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestRequireRoleWithNoUser tests RequireRole with no user
func TestRequireRoleWithNoUser(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	handler := RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestRequirePermissionWithMatchingPermission tests RequirePermission with matching permission
func TestRequirePermissionWithMatchingPermission(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	user := &User{
		UserID:      "user1",
		Username:    "testuser",
		Email:       "test@example.com",
		Roles:       []string{"admin"},
		Permissions: []string{"read", "write", "delete"},
	}
	req = req.WithContext(context.WithValue(req.Context(), AuthUserKey, user))

	handler := RequirePermission("delete")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestRequirePermissionWithNonMatchingPermission tests RequirePermission with non-matching permission
func TestRequirePermissionWithNonMatchingPermission(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	user := &User{
		UserID:      "user1",
		Username:    "testuser",
		Email:       "test@example.com",
		Roles:       []string{"user"},
		Permissions: []string{"read"},
	}
	req = req.WithContext(context.WithValue(req.Context(), AuthUserKey, user))

	handler := RequirePermission("delete")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestRequirePermissionWithNoUser tests RequirePermission with no user
func TestRequirePermissionWithNoUser(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	handler := RequirePermission("delete")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestGenerateAPIKey tests GenerateAPIKey function
func TestGenerateAPIKey(t *testing.T) {
	key := GenerateAPIKey()

	assert.Contains(t, key, "key_")
	// Key should contain timestamp and nanosecond
	assert.Regexp(t, `^key_\d{14}_\d+$`, key)
}

// TestGenerateAPIKeyUnique tests that GenerateAPIKey generates unique keys
func TestGenerateAPIKeyUnique(t *testing.T) {
	keys := make(map[string]bool)

	for i := 0; i < 100; i++ {
		key := GenerateAPIKey()
		assert.False(t, keys[key], "Key %s was generated more than once", key)
		keys[key] = true
	}
}

// TestAuthContextKeyIsString tests that AuthContextKey is a string type
func TestAuthContextKeyIsString(t *testing.T) {
	var key AuthContextKey = "test_key"
	assert.IsType(t, "", key)
}

// TestAuthUserKeyConstant tests AuthUserKey constant
func TestAuthUserKeyConstant(t *testing.T) {
	assert.Equal(t, "auth_user", string(AuthUserKey))
}

// TestUserStruct tests User struct
func TestUserStruct(t *testing.T) {
	user := &User{
		UserID:      "user1",
		Username:    "testuser",
		Email:       "test@example.com",
		Roles:       []string{"admin"},
		Permissions: []string{"read", "write"},
	}

	assert.Equal(t, "user1", user.UserID)
	assert.Equal(t, "testuser", user.Username)
	assert.Equal(t, "test@example.com", user.Email)
	assert.Equal(t, []string{"admin"}, user.Roles)
	assert.Equal(t, []string{"read", "write"}, user.Permissions)
}

// TestUserStructEmpty tests User struct with empty values
func TestUserStructEmpty(t *testing.T) {
	user := &User{}

	assert.Equal(t, "", user.UserID)
	assert.Equal(t, "", user.Username)
	assert.Equal(t, "", user.Email)
	assert.Nil(t, user.Roles)
	assert.Nil(t, user.Permissions)
}

// TestNewAuthMiddlewareMultipleMiddleware tests stacking multiple middleware
func TestNewAuthMiddlewareMultipleMiddleware(t *testing.T) {
	db := &MockDatabase{
		users: map[string]*database.UserRepository{
			"user1": {
				UserID:       "user1",
				Username:     "testuser",
				Email:        "test@example.com",
				PasswordHash: "$2a$10$examplehash",
				IsActive:     true,
			},
		},
	}
	roles := &MockRoleAndPermissionLookup{
		userRoles:       []string{"user"},
		userPermissions: []string{"read"},
	}

	authMiddleware := NewAuthMiddleware(db, roles)

	handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		if user != nil {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(user.Username))
		} else {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic dGVzdHVzZXI6cGFzc3dvcmQ=") // testuser:password
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "testuser", w.Body.String())
}

// TestNewAuthMiddlewareWithCustomRoleLookup tests middleware with custom role lookup
func TestNewAuthMiddlewareWithCustomRoleLookup(t *testing.T) {
	db := &MockDatabase{
		users: map[string]*database.UserRepository{
			"user1": {
				UserID:       "user1",
				Username:     "testuser",
				Email:        "test@example.com",
				PasswordHash: "$2a$10$examplehash",
				IsActive:     true,
			},
		},
	}
	roles := &MockRoleAndPermissionLookup{
		getUserRolesFunc: func(string) ([]string, error) {
			return []string{"custom_role"}, nil
		},
		getUserPermsFunc: func(string) []string {
			return []string{"custom_permission"}
		},
	}

	handler := NewAuthMiddleware(db, roles)

	authString := base64.StdEncoding.EncodeToString([]byte("testuser:password"))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic "+authString)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	user := GetUser(req)
	assert.NotNil(t, user)
	assert.Equal(t, []string{"custom_role"}, user.Roles)
	assert.Equal(t, []string{"custom_permission"}, user.Permissions)
}

// TestNewAuthMiddlewareDisabledAccessKey tests middleware with disabled access key
func TestNewAuthMiddlewareDisabledAccessKey(t *testing.T) {
	db := &MockDatabase{
		accessKeys: map[string]*database.AccessKey{
			"disabled-key": {
				UserID:   "user1",
				IsActive: false,
			},
		},
	}
	roles := &MockRoleAndPermissionLookup{}

	handler := NewAuthMiddleware(db, roles)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer disabled-key")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Nil(t, GetUser(req))
}

// TestNewAuthMiddlewareNilUser tests middleware with nil user after validation
func TestNewAuthMiddlewareNilUserAfterValidation(t *testing.T) {
	db := &MockDatabase{
		users: map[string]*database.UserRepository{},
	}
	roles := &MockRoleAndPermissionLookup{}

	handler := NewAuthMiddleware(db, roles)

	authString := base64.StdEncoding.EncodeToString([]byte("nonexistent:password"))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic "+authString)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestNewAuthMiddlewareUserNotFoundError tests middleware with error from GetUserByUsername
func TestNewAuthMiddlewareUserNotFoundError(t *testing.T) {
	db := &MockDatabase{
		getUserByUsernameFunc: func(string) (*database.UserRepository, error) {
			return nil, assert.AnError
		},
	}
	roles := &MockRoleAndPermissionLookup{}

	handler := NewAuthMiddleware(db, roles)

	authString := base64.StdEncoding.EncodeToString([]byte("testuser:password"))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic "+authString)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
