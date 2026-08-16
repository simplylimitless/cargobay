package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/cargobay/backend/pkg/database"
)

// AuthContextKey is the key for user info in request context
type AuthContextKey string

const AuthUserKey AuthContextKey = "auth_user"

// User represents an authenticated user
type User struct {
	UserID      string
	Username    string
	Email       string
	Roles       []string
	Permissions []string
}

// RoleAndPermissionLookup provides a user's roles and their expanded
// permission set. Implemented by *rbac.RBAC; kept as an interface here to
// avoid an import cycle (rbac already depends on this package).
type RoleAndPermissionLookup interface {
	GetUserRoles(userID string) ([]string, error)
	GetUserRolePermissions(userID string) []string
}

// NewAuthMiddleware validates the bearer token against access_keys, loads
// the owning user and their RBAC roles/permissions, and attaches the result
// to the request context. Requests without a (valid) token proceed
// unauthenticated — routes that require a user should be wrapped in
// RequireAuth or rbac.RequirePermission.
func NewAuthMiddleware(db *database.Database, roles RoleAndPermissionLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				next.ServeHTTP(w, r)
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == authHeader {
				// Missing "Bearer " prefix — not a token we understand.
				next.ServeHTTP(w, r)
				return
			}

			key, err := db.ValidateAccessKey(token)
			if err != nil || key == nil {
				next.ServeHTTP(w, r)
				return
			}

			dbUser, err := db.GetUserByID(key.UserID)
			if err != nil || dbUser == nil || !dbUser.IsActive {
				next.ServeHTTP(w, r)
				return
			}

			userRoles, _ := roles.GetUserRoles(dbUser.UserID)
			user := &User{
				UserID:      dbUser.UserID,
				Username:    dbUser.Username,
				Email:       dbUser.Email,
				Roles:       userRoles,
				Permissions: roles.GetUserRolePermissions(dbUser.UserID),
			}

			ctx := context.WithValue(r.Context(), AuthUserKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUser retrieves the user from request context
func GetUser(r *http.Request) *User {
	user, ok := r.Context().Value(AuthUserKey).(*User)
	if !ok {
		return nil
	}
	return user
}

// RequireAuth blocks requests with no authenticated user (i.e. anonymous requests).
// It must run after WithUser has had a chance to populate the user context.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if GetUser(r) == nil {
			http.Error(w, "Unauthorized: authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole checks if user has the required role
func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetUser(r)
			if user == nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			hasRole := false
			for _, r := range user.Roles {
				if r == role {
					hasRole = true
					break
				}
			}

			if !hasRole {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermission checks if user has the required permission
func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetUser(r)
			if user == nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			hasPermission := false
			for _, p := range user.Permissions {
				if p == permission {
					hasPermission = true
					break
				}
			}

			if !hasPermission {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// GenerateAPIKey generates a new API key
func GenerateAPIKey() string {
	// In production, use crypto/rand for secure random generation
	return fmt.Sprintf("key_%s_%d", time.Now().Format("20060102150405"), time.Now().UnixNano())
}
