// Package auth provides authentication services including password hashing and login/logout.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"golang.org/x/crypto/bcrypt"
)

// LoginInput represents the input for a login request
type LoginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginOutput represents the output of a successful login
type LoginOutput struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt"`
	UserID       string    `json:"userId"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	Timezone     string    `json:"timezone"`
	Roles        []string  `json:"roles"`
	Permissions  []string  `json:"permissions"`
}

// VerifyPassword reports whether password matches the given bcrypt hash.
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// HashPassword hashes a plaintext password for storage.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// Login attempts to authenticate a user with username and password.
// Returns an access key if successful, nil otherwise.
func Login(db *database.Database, username, password string) (*LoginOutput, error) {
	// Get user by username
	user, err := db.GetUserByUsername(username)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup user: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Verify password
	if !VerifyPassword(user.PasswordHash, password) {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Check if user is active
	if !user.IsActive {
		return nil, fmt.Errorf("user account is deactivated")
	}

	// Get user roles
	roles, err := db.GetUserRoles(user.UserID)
	if err != nil {
		roles = []string{}
	}

	// Create an access key for the user
	expiresAt := time.Now().Add(24 * time.Hour)
	key, err := db.CreateAccessKey(user.UserID, "login_session", []string{"read", "write"}, &expiresAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create access key: %w", err)
	}

	// Get permissions for the user
	rolePermissions, err := db.GetUserRolePermissions(user.UserID)
	if err != nil {
		rolePermissions = []database.RolePermission{}
	}
	permissions := make([]string, len(rolePermissions))
	for i, rp := range rolePermissions {
		permissions[i] = rp.PermissionID
	}

	// Update last login time
	if err := db.UpdateUserLastLogin(user.UserID); err != nil {
		// Log but don't fail - this is just for auditing
	}

	var keyExpiresAt time.Time
	if key.ExpiresAt != nil {
		keyExpiresAt = *key.ExpiresAt
	}

	return &LoginOutput{
		AccessToken:  key.KeyHash,
		RefreshToken: "",
		ExpiresAt:    keyExpiresAt,
		UserID:       user.UserID,
		Username:     user.Username,
		Email:        user.Email,
		Timezone:     user.Timezone,
		Roles:        roles,
		Permissions:  permissions,
	}, nil
}

// Logout invalidates an access key
func Logout(db *database.Database, keyID string) error {
	return db.InvalidateAccessKey(keyID)
}

// tokenPrefix marks a value as a cargobay personal access token, so tokens
// are visually distinguishable from session tokens and greppable if one
// leaks into logs or config files.
const tokenPrefix = "cbpat_"

// GenerateToken creates a new cryptographically random personal access
// token. Unlike the legacy access-key generator (UUID + timestamp based),
// this draws directly from crypto/rand.
func GenerateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashToken returns the hex-encoded SHA-256 digest of a token, for storage
// and lookup. Tokens already carry 256 bits of entropy from crypto/rand, so
// this exists to protect against exposure via DB read access, not to resist
// brute force on a low-entropy secret (which is why no per-token salt is
// needed).
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
