package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/glebarez/sqlite"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/database"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// setupPropertyTestService creates a fresh InitService with temp DB and config path for property testing.
func setupPropertyTestService(t *testing.T) *InitService {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := database.NewDB(tmpDir)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")
	return NewInitService(db, userRepo, configPath, nil)
}

// TestProperty1_InitTokenGenerationIntegrity verifies Init Token generation integrity:
// - Generated token is at least 32 bytes (64 hex characters)
// - Stored value is SHA256 hash, not plaintext token
// - admin_id in init_state matches the created user ID
// - pending_init is true after creation
// - expires_at is approximately 30 minutes in the future
//
// **Validates: Requirements 1.1, 1.6, 1.11**
func TestProperty1_InitTokenGenerationIntegrity(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 50
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Generator for valid usernames (alphanumeric, 3-20 chars)
	genUsername := gen.RegexMatch(`[a-zA-Z][a-zA-Z0-9]{2,19}`)

	// Generator for valid passwords (6-30 chars)
	genPassword := gen.RegexMatch(`[a-zA-Z0-9!@#$%]{6,30}`)

	// Property: token is at least 32 bytes (64 hex characters)
	properties.Property("token is at least 32 bytes (64 hex chars)", prop.ForAll(
		func(username, password string) bool {
			svc := setupPropertyTestService(t)
			ctx := context.Background()

			_, plainToken, err := svc.CreateAdmin(ctx, CreateAdminInput{
				Username: username,
				Password: password,
			})
			if err != nil {
				t.Logf("CreateAdmin error: %v", err)
				return false
			}

			// plainToken is hex-encoded 32 bytes → 64 hex characters
			tokenBytes, err := hex.DecodeString(plainToken)
			if err != nil {
				t.Logf("token is not valid hex: %v", err)
				return false
			}
			return len(tokenBytes) >= 32
		},
		genUsername,
		genPassword,
	))

	// Property: stored hash is SHA256(plainToken), not the plaintext token
	properties.Property("stored value is SHA256 hash not plaintext", prop.ForAll(
		func(username, password string) bool {
			svc := setupPropertyTestService(t)
			ctx := context.Background()

			_, plainToken, err := svc.CreateAdmin(ctx, CreateAdminInput{
				Username: username,
				Password: password,
			})
			if err != nil {
				t.Logf("CreateAdmin error: %v", err)
				return false
			}

			// Query the DB for the pending init_state
			state, err := svc.db.GetPendingInitState(nil)
			if err != nil || state == nil {
				t.Logf("GetPendingInitState error: %v, state: %v", err, state)
				return false
			}

			// Stored token_hash must NOT be the plaintext token
			if state.TokenHash == plainToken {
				t.Log("FAIL: stored value is plaintext token, not hash")
				return false
			}

			// Stored token_hash must be SHA256(plainToken)
			expectedHash := computeSHA256Hex(plainToken)
			if state.TokenHash != expectedHash {
				t.Logf("FAIL: stored hash %q != expected SHA256 %q", state.TokenHash, expectedHash)
				return false
			}

			return true
		},
		genUsername,
		genPassword,
	))

	// Property: admin_id in init_state matches the created user's ID
	properties.Property("admin_id matches created user ID", prop.ForAll(
		func(username, password string) bool {
			svc := setupPropertyTestService(t)
			ctx := context.Background()

			user, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
				Username: username,
				Password: password,
			})
			if err != nil {
				t.Logf("CreateAdmin error: %v", err)
				return false
			}

			state, err := svc.db.GetPendingInitState(nil)
			if err != nil || state == nil {
				t.Logf("GetPendingInitState error: %v, state: %v", err, state)
				return false
			}

			return state.AdminID == user.ID
		},
		genUsername,
		genPassword,
	))

	// Property: pending_init is true after creation
	properties.Property("pending_init is true after creation", prop.ForAll(
		func(username, password string) bool {
			svc := setupPropertyTestService(t)
			ctx := context.Background()

			_, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
				Username: username,
				Password: password,
			})
			if err != nil {
				t.Logf("CreateAdmin error: %v", err)
				return false
			}

			state, err := svc.db.GetPendingInitState(nil)
			if err != nil || state == nil {
				t.Logf("GetPendingInitState error: %v, state: %v", err, state)
				return false
			}

			return state.PendingInit == true
		},
		genUsername,
		genPassword,
	))

	// Property: expires_at is approximately 30 minutes in the future (between 29 and 31 minutes)
	properties.Property("expires_at is ~30 minutes in the future", prop.ForAll(
		func(username, password string) bool {
			svc := setupPropertyTestService(t)
			ctx := context.Background()

			before := time.Now()
			_, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
				Username: username,
				Password: password,
			})
			if err != nil {
				t.Logf("CreateAdmin error: %v", err)
				return false
			}
			after := time.Now()

			state, err := svc.db.GetPendingInitState(nil)
			if err != nil || state == nil {
				t.Logf("GetPendingInitState error: %v, state: %v", err, state)
				return false
			}

			// expires_at should be between (before + 29min) and (after + 31min)
			// Using a generous window to account for test execution time
			minExpiry := before.Add(29 * time.Minute)
			maxExpiry := after.Add(31 * time.Minute)

			if state.ExpiresAt.Before(minExpiry) {
				t.Logf("FAIL: expires_at %v is before min %v", state.ExpiresAt, minExpiry)
				return false
			}
			if state.ExpiresAt.After(maxExpiry) {
				t.Logf("FAIL: expires_at %v is after max %v", state.ExpiresAt, maxExpiry)
				return false
			}

			return true
		},
		genUsername,
		genPassword,
	))

	// Property: completed_at is nil after creation (not yet completed)
	properties.Property("completed_at is nil after creation", prop.ForAll(
		func(username, password string) bool {
			svc := setupPropertyTestService(t)
			ctx := context.Background()

			_, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
				Username: username,
				Password: password,
			})
			if err != nil {
				t.Logf("CreateAdmin error: %v", err)
				return false
			}

			state, err := svc.db.GetPendingInitState(nil)
			if err != nil || state == nil {
				t.Logf("GetPendingInitState error: %v, state: %v", err, state)
				return false
			}

			return state.CompletedAt == nil
		},
		genUsername,
		genPassword,
	))

	// Property: each CreateAdmin invocation generates a unique token (no token reuse)
	properties.Property("each invocation generates unique token", prop.ForAll(
		func(username1, password1, username2, password2 string) bool {
			// Run two separate CreateAdmin calls (each with its own fresh DB)
			// and verify they produce different tokens
			svc1 := setupPropertyTestService(t)
			svc2 := setupPropertyTestService(t)
			ctx := context.Background()

			_, token1, err := svc1.CreateAdmin(ctx, CreateAdminInput{
				Username: username1,
				Password: password1,
			})
			if err != nil {
				t.Logf("CreateAdmin 1 error: %v", err)
				return false
			}

			_, token2, err := svc2.CreateAdmin(ctx, CreateAdminInput{
				Username: username2,
				Password: password2,
			})
			if err != nil {
				t.Logf("CreateAdmin 2 error: %v", err)
				return false
			}

			// Tokens should differ (crypto/rand ensures uniqueness)
			return token1 != token2
		},
		genUsername,
		genPassword,
		genUsername,
		genPassword,
	))

	properties.TestingRun(t)
}

// TestProperty2_InitTokenValidationCorrectness verifies Init Token validation correctness (Round-trip):
// - SHA256(T) matches only the original token T
// - Any other token T' (T' != T) does NOT produce the same hash
// - The sha256Hex function is deterministic (same input always produces same output)
//
// **Validates: Requirements 1.2, 1.3**
func TestProperty2_InitTokenValidationCorrectness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 200
	parameters.Rng.Seed(123)

	properties := gopter.NewProperties(parameters)

	// Generator for arbitrary token strings (simulating hex-encoded 32-byte tokens)
	genToken := gen.RegexMatch(`[0-9a-f]{64}`)

	// Generator for arbitrary non-empty strings (to test mismatches)
	genAnyString := gen.RegexMatch(`[a-zA-Z0-9._\-]{1,128}`)

	// Property: sha256Hex(T) is deterministic — same input always produces same hash
	properties.Property("sha256Hex is deterministic", prop.ForAll(
		func(token string) bool {
			hash1 := sha256Hex(token)
			hash2 := sha256Hex(token)
			return hash1 == hash2
		},
		genToken,
	))

	// Property: sha256Hex(T) matches independent SHA256 computation
	properties.Property("sha256Hex matches independent SHA256 computation", prop.ForAll(
		func(token string) bool {
			hash := sha256Hex(token)
			expected := computeSHA256Hex(token)
			return hash == expected
		},
		genToken,
	))

	// Property: for any two distinct tokens T1 != T2, sha256Hex(T1) != sha256Hex(T2)
	// (collision resistance — probabilistically verified)
	properties.Property("distinct tokens produce distinct hashes", prop.ForAll(
		func(token1, token2 string) bool {
			if token1 == token2 {
				// Skip identical inputs — not a valid test case for collision resistance
				return true
			}
			hash1 := sha256Hex(token1)
			hash2 := sha256Hex(token2)
			return hash1 != hash2
		},
		genToken,
		genToken,
	))

	// Property: validating with original token succeeds (hash comparison)
	// Simulates the SaveConfig token verification: compute SHA256 of submitted token and compare to stored hash
	properties.Property("validation accepts only original token", prop.ForAll(
		func(token string) bool {
			// Simulate CreateAdmin: store the hash
			storedHash := sha256Hex(token)

			// Simulate SaveConfig: verify submitted token
			submittedHash := sha256Hex(token)

			return submittedHash == storedHash
		},
		genToken,
	))

	// Property: validating with any different token fails (hash mismatch)
	properties.Property("validation rejects different tokens", prop.ForAll(
		func(originalToken, otherToken string) bool {
			if originalToken == otherToken {
				// Skip identical inputs
				return true
			}

			// Simulate CreateAdmin: store the hash of original token
			storedHash := sha256Hex(originalToken)

			// Simulate SaveConfig: attacker submits a different token
			attackerHash := sha256Hex(otherToken)

			// Must NOT match
			return attackerHash != storedHash
		},
		genToken,
		genAnyString,
	))

	// Property: sha256Hex output is always a 64-character hex string
	properties.Property("sha256Hex output is always 64-char hex", prop.ForAll(
		func(token string) bool {
			hash := sha256Hex(token)
			if len(hash) != 64 {
				t.Logf("FAIL: hash length %d != 64", len(hash))
				return false
			}
			// Verify it's valid hex
			_, err := hex.DecodeString(hash)
			if err != nil {
				t.Logf("FAIL: hash is not valid hex: %v", err)
				return false
			}
			return true
		},
		genAnyString,
	))

	// Property: sha256Hex(T) != T for any non-trivial input (hash is not identity)
	properties.Property("hash is never equal to input", prop.ForAll(
		func(token string) bool {
			hash := sha256Hex(token)
			return hash != token
		},
		genAnyString,
	))

	properties.TestingRun(t)
}

// TestProperty3_InitTokenOneTimeUse verifies Init Token one-time use:
// - After successful config save, pending_init=false
// - After successful config save, token_hash is cleared to empty string
// - After successful config save, completed_at is set (non-nil)
// - After successful config save, the init_state row is preserved (not deleted)
// - Subsequent request with the same token is rejected (ErrInvalidInitToken)
//
// **Validates: Requirements 1.4, 1.7**
func TestProperty3_InitTokenOneTimeUse(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 50
	parameters.Rng.Seed(456)

	properties := gopter.NewProperties(parameters)

	// Generator for valid usernames (alphanumeric, 3-20 chars)
	genUsername := gen.RegexMatch(`[a-zA-Z][a-zA-Z0-9]{2,19}`)

	// Generator for valid passwords (6-30 chars)
	genPassword := gen.RegexMatch(`[a-zA-Z0-9!@#$%]{6,30}`)

	// Generator for valid external URLs
	genExternalURL := gen.RegexMatch(`https://[a-z]{3,10}\\.example\\.com`)

	// Property: after successful SaveConfig, pending_init becomes false
	properties.Property("pending_init is false after successful SaveConfig", prop.ForAll(
		func(username, password, externalURL string) bool {
			svc := setupPropertyTestService(t)
			ctx := context.Background()

			_, plainToken, err := svc.CreateAdmin(ctx, CreateAdminInput{
				Username: username,
				Password: password,
			})
			if err != nil {
				t.Logf("CreateAdmin error: %v", err)
				return false
			}

			// Save config with valid token
			_, err = svc.SaveConfig(ctx, plainToken, SaveConfigInput{
				Server: &config.ServerConfig{ExternalURL: externalURL},
			})
			if err != nil {
				t.Logf("SaveConfig error: %v", err)
				return false
			}

			// After save, there should be no pending init_state
			pendingState, err := svc.db.GetPendingInitState(nil)
			if err != nil {
				t.Logf("GetPendingInitState error: %v", err)
				return false
			}
			if pendingState != nil {
				t.Log("FAIL: pending_init is still true after successful SaveConfig")
				return false
			}

			// Verify completed record exists
			hasCompleted, err := svc.db.HasCompletedInitState(nil)
			if err != nil {
				t.Logf("HasCompletedInitState error: %v", err)
				return false
			}
			if !hasCompleted {
				t.Log("FAIL: no completed init_state record found after SaveConfig")
				return false
			}

			return true
		},
		genUsername,
		genPassword,
		genExternalURL,
	))

	// Property: after successful SaveConfig, token_hash is cleared to empty string
	properties.Property("token_hash is empty after successful SaveConfig", prop.ForAll(
		func(username, password, externalURL string) bool {
			svc := setupPropertyTestService(t)
			ctx := context.Background()

			_, plainToken, err := svc.CreateAdmin(ctx, CreateAdminInput{
				Username: username,
				Password: password,
			})
			if err != nil {
				t.Logf("CreateAdmin error: %v", err)
				return false
			}

			// Get the init state ID before completing
			pendingBefore, err := svc.db.GetPendingInitState(nil)
			if err != nil || pendingBefore == nil {
				t.Logf("GetPendingInitState before error: %v", err)
				return false
			}
			stateID := pendingBefore.ID

			// Save config with valid token
			_, err = svc.SaveConfig(ctx, plainToken, SaveConfigInput{
				Server: &config.ServerConfig{ExternalURL: externalURL},
			})
			if err != nil {
				t.Logf("SaveConfig error: %v", err)
				return false
			}

			// Directly query the DB for the completed record to check token_hash
			var tokenHash string
			err = svc.db.DB.QueryRow(
				`SELECT token_hash FROM init_state WHERE id = ?`, stateID,
			).Scan(&tokenHash)
			if err != nil {
				t.Logf("query token_hash error: %v", err)
				return false
			}

			if tokenHash != "" {
				t.Logf("FAIL: token_hash is %q, expected empty string", tokenHash)
				return false
			}

			return true
		},
		genUsername,
		genPassword,
		genExternalURL,
	))

	// Property: after successful SaveConfig, completed_at is set (non-nil)
	properties.Property("completed_at is set after successful SaveConfig", prop.ForAll(
		func(username, password, externalURL string) bool {
			svc := setupPropertyTestService(t)
			ctx := context.Background()

			_, plainToken, err := svc.CreateAdmin(ctx, CreateAdminInput{
				Username: username,
				Password: password,
			})
			if err != nil {
				t.Logf("CreateAdmin error: %v", err)
				return false
			}

			// Get the state ID before completing
			pendingBefore, err := svc.db.GetPendingInitState(nil)
			if err != nil || pendingBefore == nil {
				t.Logf("GetPendingInitState before error: %v", err)
				return false
			}
			stateID := pendingBefore.ID

			beforeSave := time.Now()

			// Save config with valid token
			_, err = svc.SaveConfig(ctx, plainToken, SaveConfigInput{
				Server: &config.ServerConfig{ExternalURL: externalURL},
			})
			if err != nil {
				t.Logf("SaveConfig error: %v", err)
				return false
			}

			afterSave := time.Now()

			// Query the completed_at value directly
			var completedAtStr string
			err = svc.db.DB.QueryRow(
				`SELECT completed_at FROM init_state WHERE id = ?`, stateID,
			).Scan(&completedAtStr)
			if err != nil {
				t.Logf("query completed_at error: %v", err)
				return false
			}

			if completedAtStr == "" {
				t.Log("FAIL: completed_at is empty after successful SaveConfig")
				return false
			}

			// Parse and verify it's within reasonable time bounds
			completedAt, err := time.Parse(time.RFC3339, completedAtStr)
			if err != nil {
				t.Logf("FAIL: completed_at %q is not valid RFC3339: %v", completedAtStr, err)
				return false
			}

			// completed_at should be between beforeSave and afterSave (with 1s tolerance)
			if completedAt.Before(beforeSave.Add(-1 * time.Second)) {
				t.Logf("FAIL: completed_at %v is before beforeSave %v", completedAt, beforeSave)
				return false
			}
			if completedAt.After(afterSave.Add(1 * time.Second)) {
				t.Logf("FAIL: completed_at %v is after afterSave %v", completedAt, afterSave)
				return false
			}

			return true
		},
		genUsername,
		genPassword,
		genExternalURL,
	))

	// Property: init_state row is preserved after completion (not deleted)
	properties.Property("init_state row is preserved after completion", prop.ForAll(
		func(username, password, externalURL string) bool {
			svc := setupPropertyTestService(t)
			ctx := context.Background()

			_, plainToken, err := svc.CreateAdmin(ctx, CreateAdminInput{
				Username: username,
				Password: password,
			})
			if err != nil {
				t.Logf("CreateAdmin error: %v", err)
				return false
			}

			// Get the state ID
			pendingBefore, err := svc.db.GetPendingInitState(nil)
			if err != nil || pendingBefore == nil {
				t.Logf("GetPendingInitState before error: %v", err)
				return false
			}
			stateID := pendingBefore.ID

			// Save config
			_, err = svc.SaveConfig(ctx, plainToken, SaveConfigInput{
				Server: &config.ServerConfig{ExternalURL: externalURL},
			})
			if err != nil {
				t.Logf("SaveConfig error: %v", err)
				return false
			}

			// Verify the row still exists in the DB (not deleted)
			var count int
			err = svc.db.DB.QueryRow(
				`SELECT COUNT(*) FROM init_state WHERE id = ?`, stateID,
			).Scan(&count)
			if err != nil {
				t.Logf("count query error: %v", err)
				return false
			}
			if count != 1 {
				t.Logf("FAIL: expected 1 row for state ID %s, got %d", stateID, count)
				return false
			}

			return true
		},
		genUsername,
		genPassword,
		genExternalURL,
	))

	// Property: subsequent request with the same token is rejected after SaveConfig
	properties.Property("same token is rejected after successful use", prop.ForAll(
		func(username, password, externalURL string) bool {
			svc := setupPropertyTestService(t)
			ctx := context.Background()

			_, plainToken, err := svc.CreateAdmin(ctx, CreateAdminInput{
				Username: username,
				Password: password,
			})
			if err != nil {
				t.Logf("CreateAdmin error: %v", err)
				return false
			}

			// First SaveConfig — should succeed
			_, err = svc.SaveConfig(ctx, plainToken, SaveConfigInput{
				Server: &config.ServerConfig{ExternalURL: externalURL},
			})
			if err != nil {
				t.Logf("first SaveConfig error: %v", err)
				return false
			}

			// Second SaveConfig with same token — should be rejected
			_, err = svc.SaveConfig(ctx, plainToken, SaveConfigInput{
				Server: &config.ServerConfig{ExternalURL: externalURL},
			})
			if err == nil {
				t.Log("FAIL: second SaveConfig with same token succeeded, should have been rejected")
				return false
			}

			// The error should be ErrInvalidInitToken (token_hash is now empty, so hash won't match)
			if !errors.Is(err, ErrInvalidInitToken) {
				t.Logf("FAIL: expected ErrInvalidInitToken, got: %v", err)
				return false
			}

			return true
		},
		genUsername,
		genPassword,
		genExternalURL,
	))

	// Property: a different token is also rejected after the original is consumed
	properties.Property("different token is also rejected after original consumed", prop.ForAll(
		func(username, password, externalURL string) bool {
			svc := setupPropertyTestService(t)
			ctx := context.Background()

			_, plainToken, err := svc.CreateAdmin(ctx, CreateAdminInput{
				Username: username,
				Password: password,
			})
			if err != nil {
				t.Logf("CreateAdmin error: %v", err)
				return false
			}

			// Use the real token to complete initialization
			_, err = svc.SaveConfig(ctx, plainToken, SaveConfigInput{
				Server: &config.ServerConfig{ExternalURL: externalURL},
			})
			if err != nil {
				t.Logf("SaveConfig error: %v", err)
				return false
			}

			// Try with a completely fabricated token — should also be rejected
			fakeToken := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			_, err = svc.SaveConfig(ctx, fakeToken, SaveConfigInput{
				Server: &config.ServerConfig{ExternalURL: externalURL},
			})
			if err == nil {
				t.Log("FAIL: SaveConfig with fake token succeeded after initialization completed")
				return false
			}

			if !errors.Is(err, ErrInvalidInitToken) {
				t.Logf("FAIL: expected ErrInvalidInitToken, got: %v", err)
				return false
			}

			return true
		},
		genUsername,
		genPassword,
		genExternalURL,
	))

	properties.TestingRun(t)
}

// computeSHA256Hex computes SHA256 hash of the input and returns hex-encoded result.
// This is a test-local helper to independently verify the hash computation.
func computeSHA256Hex(input string) string {
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])
}
