package middleware

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/auth"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
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
	// Scope is "full" for password-authenticated and session-token requests,
	// or "read" when the request authenticated with a read-only personal
	// access token. RBAC checks that key off the request (see
	// rbac.HasEffectivePermission) use this to deny write/delete/admin
	// permissions even if the underlying user's roles would otherwise allow
	// them.
	Scope string
}

// RoleAndPermissionLookup provides a user's roles and their expanded
// permission set. Implemented by *rbac.RBAC; kept as an interface here to
// avoid an import cycle (rbac already depends on this package).
type RoleAndPermissionLookup interface {
	GetUserRoles(userID string) ([]string, error)
	GetUserRolePermissions(userID string) []string
}

// authStore is the subset of *database.Database's methods NewAuthMiddleware
// depends on. Narrowed to an interface so tests can substitute a fake.
type authStore interface {
	GetUserByUsername(username string) (*database.UserRepository, error)
	GetUserByID(userID string) (*database.UserRepository, error)
	ValidateAccessKey(keyHash string) (*database.AccessKey, error)
}

// NewAuthMiddleware validates the caller's credentials — either a bearer
// token against access_keys, or HTTP Basic username/password against the
// users table (used by `docker login`/`docker push`, which speak Basic
// auth rather than this app's session tokens) — loads the owning user and
// their RBAC roles/permissions, and attaches the result to the request
// context. Requests without (valid) credentials proceed unauthenticated —
// routes that require a user should be wrapped in RequireAuth or
// rbac.RequirePermission.
func NewAuthMiddleware(db authStore, roles RoleAndPermissionLookup) func(http.Handler) http.Handler {
	loadUser := func(dbUser *database.UserRepository, scope string) *User {
		userRoles, _ := roles.GetUserRoles(dbUser.UserID)
		return &User{
			UserID:      dbUser.UserID,
			Username:    dbUser.Username,
			Email:       dbUser.Email,
			Roles:       userRoles,
			Permissions: roles.GetUserRolePermissions(dbUser.UserID),
			Scope:       scope,
		}
	}

	resolveToken := func(w http.ResponseWriter, r *http.Request, next http.Handler, token string) {
		key, err := db.ValidateAccessKey(auth.HashToken(token))
		if err != nil || key == nil {
			// Fall back to the legacy unhashed lookup for login-session
			// tokens, which are still stored as their raw value.
			key, err = db.ValidateAccessKey(token)
			if err != nil || key == nil {
				next.ServeHTTP(w, r)
				return
			}
		}

		dbUser, err := db.GetUserByID(key.UserID)
		if err != nil || dbUser == nil || !dbUser.IsActive {
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), AuthUserKey, loadUser(dbUser, accessKeyScope(key)))
		next.ServeHTTP(w, r.WithContext(ctx))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				// NuGet's native client (nuget push / dotnet nuget push)
				// sends its API key via X-NuGet-ApiKey rather than
				// Authorization — accept it as a bearer-equivalent personal
				// access token so those clients can publish without needing
				// non-standard configuration.
				if apiKey := r.Header.Get("X-NuGet-ApiKey"); apiKey != "" {
					resolveToken(w, r, next, apiKey)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			if basicCreds := strings.TrimPrefix(authHeader, "Basic "); basicCreds != authHeader {
				decoded, err := base64.StdEncoding.DecodeString(basicCreds)
				if err != nil {
					next.ServeHTTP(w, r)
					return
				}
				username, password, ok := strings.Cut(string(decoded), ":")
				if !ok {
					next.ServeHTTP(w, r)
					return
				}
				dbUser, err := db.GetUserByUsername(username)
				if err != nil || dbUser == nil || !dbUser.IsActive {
					next.ServeHTTP(w, r)
					return
				}

				scope := "full"
				if !auth.VerifyPassword(dbUser.PasswordHash, password) {
					// Not the account password — check whether it's a
					// personal access token belonging to this same user
					// (the docker login/k3s imagePullSecret convention:
					// real username + PAT as the password).
					key, err := db.ValidateAccessKey(auth.HashToken(password))
					if err != nil || key == nil || key.UserID != dbUser.UserID {
						next.ServeHTTP(w, r)
						return
					}
					scope = accessKeyScope(key)
				}

				ctx := context.WithValue(r.Context(), AuthUserKey, loadUser(dbUser, scope))
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == authHeader {
				// Neither "Bearer " nor "Basic " prefix — not a credential we understand.
				next.ServeHTTP(w, r)
				return
			}

			resolveToken(w, r, next, token)
		})
	}
}

// accessKeyScope derives a request scope from an access key's stored
// permissions: any key carrying "write" (login sessions, and PATs created
// with read-write scope) is "full"; a key that only carries "read" is
// scoped down to "read".
func accessKeyScope(key *database.AccessKey) string {
	for _, p := range key.Permissions {
		if p == "write" {
			return "full"
		}
	}
	return "read"
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
