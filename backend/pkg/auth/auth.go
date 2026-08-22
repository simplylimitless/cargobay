// Package auth provides authentication services including password hashing and login/logout.
package auth

import (
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
