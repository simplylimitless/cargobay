package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AuthMiddleware validates authentication tokens
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Public endpoints that don't require auth
		publicPaths := []string{
			"/health",
			"/metrics",
			"/npm/-/",
			"/maven/-/",
		}

		path := r.URL.Path
		for _, pubPath := range publicPaths {
			if strings.HasPrefix(path, pubPath) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Extract and validate token
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Unauthorized: Missing authorization header", http.StatusUnauthorized)
			return
		}

		// Parse Bearer token
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Unauthorized: Invalid authorization format", http.StatusUnauthorized)
			return
		}

		// In production, validate JWT or lookup in database
		// For now, accept any token for development
		// In production, use: db.ValidateAccessKey(tokenHash)

		next.ServeHTTP(w, r)
	})
}

// AuthContextKey is the key for user info in request context
type AuthContextKey string

const AuthUserKey AuthContextKey = "auth_user"

// User represents an authenticated user
type User struct {
	UserID     string
	Username   string
	Email      string
	Roles      []string
	Permissions []string
}

// WithUser adds user info to request context
func WithUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			next.ServeHTTP(w, r)
			return
		}

		// In production, decode JWT or lookup user
		// For now, create a mock user
		user := &User{
			UserID:      "mock-user-id",
			Username:    "developer",
			Email:       "dev@example.com",
			Roles:       []string{"developer"},
			Permissions: []string{"read"},
		}

		ctx := context.WithValue(r.Context(), AuthUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
