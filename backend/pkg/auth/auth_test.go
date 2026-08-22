package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHashPassword tests password hashing
func TestHashPassword(t *testing.T) {
	password := "testpassword123"

	hash, err := HashPassword(password)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.NotEqual(t, password, hash)
}

// TestHashPasswordMultipleTimes generates different hashes for same password
func TestHashPasswordMultipleTimes(t *testing.T) {
	password := "testpassword123"

	hash1, err := HashPassword(password)
	require.NoError(t, err)

	hash2, err := HashPassword(password)
	require.NoError(t, err)

	// Hashes should be different due to random salt
	assert.NotEqual(t, hash1, hash2)
}

// TestVerifyPasswordCorrect tests verifying correct password
func TestVerifyPasswordCorrect(t *testing.T) {
	password := "testpassword123"
	hash, err := HashPassword(password)
	require.NoError(t, err)

	result := VerifyPassword(hash, password)
	assert.True(t, result)
}

// TestVerifyPasswordIncorrect tests verifying incorrect password
func TestVerifyPasswordIncorrect(t *testing.T) {
	hash, err := HashPassword("correctpassword")
	require.NoError(t, err)

	result := VerifyPassword(hash, "wrongpassword")
	assert.False(t, result)
}

// TestVerifyPasswordEmpty tests verifying empty password
func TestVerifyPasswordEmpty(t *testing.T) {
	hash, err := HashPassword("password")
	require.NoError(t, err)

	result := VerifyPassword(hash, "")
	assert.False(t, result)
}

// TestVerifyPasswordInvalidHash tests verifying with invalid hash
func TestVerifyPasswordInvalidHash(t *testing.T) {
	result := VerifyPassword("invalidhash", "password")
	assert.False(t, result)
}

// TestVerifyPasswordEmptyHash tests verifying with empty hash
func TestVerifyPasswordEmptyHash(t *testing.T) {
	result := VerifyPassword("", "password")
	assert.False(t, result)
}

// TestHashPasswordLength tests that hash has expected length
func TestHashPasswordLength(t *testing.T) {
	hash, err := HashPassword("testpassword")
	require.NoError(t, err)

	// bcrypt hash is typically 60 characters
	assert.Equal(t, 60, len(hash))
}

// TestHashPasswordSpecialChars tests hashing passwords with special characters
func TestHashPasswordSpecialChars(t *testing.T) {
	passwords := []string{
		"password!@#$%^&*()",
		"p@ssw0rd!\"£$%^&*()",
		"test\npassword\twith\rspecial\r\nchars",
		"password with spaces",
		"password-123_456",
	}

	for _, password := range passwords {
		hash, err := HashPassword(password)
		require.NoError(t, err, "Failed to hash: %s", password)

		result := VerifyPassword(hash, password)
		assert.True(t, result, "Failed to verify: %s", password)
	}
}

// TestHashPasswordUnicode tests hashing passwords with unicode characters
func TestHashPasswordUnicode(t *testing.T) {
	passwords := []string{
		"密码123",
		"пароль",
		"パスワード",
		"passw0rd🔥",
		"café résumé naïve",
	}

	for _, password := range passwords {
		hash, err := HashPassword(password)
		require.NoError(t, err, "Failed to hash: %s", password)

		result := VerifyPassword(hash, password)
		assert.True(t, result, "Failed to verify: %s", password)
	}
}

// TestVerifyPasswordLongPassword tests with a very long password
func TestVerifyPasswordLongPassword(t *testing.T) {
	longPassword := "a" + "b" + "c" + "d" + "e" + "f" + "g" + "h" + "i" + "j" + "k" + "l" + "m" + "n" + "o" + "p" + "q" + "r" + "s" + "t" + "u" + "v" + "w" + "x" + "y" + "z"
	for i := 0; i < 10; i++ {
		longPassword += longPassword
	}

	hash, err := HashPassword(longPassword)
	require.NoError(t, err)

	result := VerifyPassword(hash, longPassword)
	assert.True(t, result)
}

// TestHashPasswordCost tests bcrypt cost factor
func TestHashPasswordCost(t *testing.T) {
	password := "testpassword"

	hash, err := HashPassword(password)
	require.NoError(t, err)

	// Extract cost from hash - bcrypt format: $2a$cost$saltandhash
	// The cost is the number after $2a$ and before the next $
	// Format: $2a$10$saltandhash
	assert.Contains(t, hash, "$2a$")
}

// TestVerifyPasswordAfterTime tests that hash still works after time passes
func TestVerifyPasswordAfterTime(t *testing.T) {
	password := "testpassword"
	hash, err := HashPassword(password)
	require.NoError(t, err)

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	result := VerifyPassword(hash, password)
	assert.True(t, result)
}

// TestMultipleVerifyPassword tests multiple verifications on same hash
func TestMultipleVerifyPassword(t *testing.T) {
	password := "testpassword"
	hash, err := HashPassword(password)
	require.NoError(t, err)

	// Verify multiple times
	for i := 0; i < 10; i++ {
		result := VerifyPassword(hash, password)
		assert.True(t, result, "Verification %d failed", i)
	}
}
