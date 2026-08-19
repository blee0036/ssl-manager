package service

import (
	"context"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

var testJWTSecret = []byte("test-secret-key-for-unit-tests")

// setupAuthTestDB creates a UserRepository backed by the shared test DB.
func setupAuthTestDB(t *testing.T) *repository.UserRepository {
	t.Helper()
	db := setupTestDB(t)
	return repository.NewUserRepository(db)
}

// createAuthTestUser creates a user in the test database and returns it.
func createAuthTestUser(t *testing.T, repo *repository.UserRepository, username, password, role string) *model.User {
	t.Helper()
	user := &model.User{
		Username:     username,
		PasswordHash: password, // UserRepository.Create hashes this
		Role:         role,
	}
	err := repo.Create(context.Background(), user)
	if err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	return user
}

func TestAuthLogin_Success(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	createAuthTestUser(t, userRepo, "admin", "password123", "admin")

	token, err := svc.Login(context.Background(), "admin", "password123", "", "")
	if err != nil {
		t.Fatalf("expected successful login, got error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Validate the token
	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}
	if claims.Username != "admin" {
		t.Errorf("expected username 'admin', got '%s'", claims.Username)
	}
	if claims.Role != "admin" {
		t.Errorf("expected role 'admin', got '%s'", claims.Role)
	}
}

func TestAuthLogin_WrongPassword(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	createAuthTestUser(t, userRepo, "admin", "password123", "admin")

	_, err := svc.Login(context.Background(), "admin", "wrongpassword", "", "")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestAuthLogin_NonExistentUser(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	_, err := svc.Login(context.Background(), "nonexistent", "password123", "", "")
	if err == nil {
		t.Fatal("expected error for non-existent user")
	}
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestAuthLogin_SameErrorForWrongUsernameAndPassword(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	createAuthTestUser(t, userRepo, "admin", "password123", "admin")

	// Wrong password
	_, err1 := svc.Login(context.Background(), "admin", "wrongpassword", "", "")
	// Non-existent user
	_, err2 := svc.Login(context.Background(), "nonexistent", "password123", "", "")

	// Both should return the same generic error
	if err1 == nil || err2 == nil {
		t.Fatal("expected errors for both cases")
	}
	if err1.Error() != err2.Error() {
		t.Errorf("expected same error message for both cases, got '%s' and '%s'", err1.Error(), err2.Error())
	}
}

func TestAuthLogin_DisabledUser(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	user := createAuthTestUser(t, userRepo, "disabled_user", "password123", "user")

	// Disable the user by ID
	err := userRepo.Disable(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("failed to disable user: %v", err)
	}

	_, err = svc.Login(context.Background(), "disabled_user", "password123", "", "")
	if err == nil {
		t.Fatal("expected error for disabled user")
	}
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestAuthLoginReadonly_Enabled(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{
		Readonly: config.ReadonlyConfig{
			Enabled:      true,
			ViewPassword: "readonly-pass",
		},
	}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	token, err := svc.LoginReadonly(context.Background(), "readonly-pass", "", "")
	if err != nil {
		t.Fatalf("expected successful readonly login, got error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Validate the token has readonly role
	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}
	if claims.Role != "readonly" {
		t.Errorf("expected role 'readonly', got '%s'", claims.Role)
	}
	if claims.Username != "readonly" {
		t.Errorf("expected username 'readonly', got '%s'", claims.Username)
	}
}

func TestAuthLoginReadonly_Disabled(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{
		Readonly: config.ReadonlyConfig{
			Enabled: false,
		},
	}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	_, err := svc.LoginReadonly(context.Background(), "any-password", "", "")
	if err == nil {
		t.Fatal("expected error when readonly is disabled")
	}
	if err != ErrReadonlyDisabled {
		t.Errorf("expected ErrReadonlyDisabled, got: %v", err)
	}
}

func TestAuthLoginReadonly_WrongPassword(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{
		Readonly: config.ReadonlyConfig{
			Enabled:      true,
			ViewPassword: "correct-pass",
		},
	}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	_, err := svc.LoginReadonly(context.Background(), "wrong-pass", "", "")
	if err == nil {
		t.Fatal("expected error for wrong readonly password")
	}
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestAuthValidateToken_Valid(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	createAuthTestUser(t, userRepo, "testuser", "password123", "user")

	token, err := svc.Login(context.Background(), "testuser", "password123", "", "")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}
	if claims.Username != "testuser" {
		t.Errorf("expected username 'testuser', got '%s'", claims.Username)
	}
	if claims.Role != "user" {
		t.Errorf("expected role 'user', got '%s'", claims.Role)
	}
}

func TestAuthValidateToken_Invalid(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	_, err := svc.ValidateToken("invalid-token-string")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	if err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken, got: %v", err)
	}
}

func TestAuthValidateToken_WrongSecret(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	createAuthTestUser(t, userRepo, "testuser", "password123", "user")

	token, err := svc.Login(context.Background(), "testuser", "password123", "", "")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	// Create a service with a different secret
	svc2 := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), []byte("different-secret"))
	_, err = svc2.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for token signed with different secret")
	}
	if err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken, got: %v", err)
	}
}

func TestAuthInvalidateUserSessions(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	userID := "user-123"

	// Before invalidation, session should be valid
	pastTime := time.Now().Add(-1 * time.Hour)
	if !svc.IsSessionValid(userID, pastTime) {
		t.Fatal("expected session to be valid before invalidation")
	}

	// Invalidate sessions
	svc.InvalidateUserSessions(userID)

	// Token issued before invalidation should be invalid
	if svc.IsSessionValid(userID, pastTime) {
		t.Fatal("expected session issued before invalidation to be invalid")
	}

	// Token issued after invalidation should be valid
	futureTime := time.Now().Add(1 * time.Second)
	if !svc.IsSessionValid(userID, futureTime) {
		t.Fatal("expected session issued after invalidation to be valid")
	}
}

func TestAuthIsSessionValid_NoInvalidation(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	// Any session should be valid if no invalidation has occurred
	if !svc.IsSessionValid("any-user", time.Now().Add(-24*time.Hour)) {
		t.Fatal("expected session to be valid when no invalidation exists")
	}
}

func TestAuthLogin_TokenExpiry(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{}
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	createAuthTestUser(t, userRepo, "testuser", "password123", "user")

	token, err := svc.Login(context.Background(), "testuser", "password123", "", "")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	// Validate token and check expiry is ~24 hours from now
	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}

	expectedExpiry := time.Now().Add(24 * time.Hour)
	diff := claims.ExpiresAt.Time.Sub(expectedExpiry)
	if diff > 5*time.Second || diff < -5*time.Second {
		t.Errorf("expected token expiry ~24h from now, got diff: %v", diff)
	}
}

// TestAuthLogin_SessionExpiry_UsesConfiguredHours verifies that the JWT's expiry
// claim reflects the configured auth.session_expiry_hours, and that it is read
// fresh from the runtime config on every login (not cached at construction time).
func TestAuthLogin_SessionExpiry_UsesConfiguredHours(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{Auth: config.AuthConfig{SessionExpiryHours: 2}}
	runtimeCfg := config.NewRuntimeConfig(cfg)
	svc := NewAuthService(userRepo, runtimeCfg, testJWTSecret)

	createAuthTestUser(t, userRepo, "admin", "password123", "admin")

	before := time.Now()
	token, err := svc.Login(context.Background(), "admin", "password123", "", "")
	if err != nil {
		t.Fatalf("expected successful login, got error: %v", err)
	}

	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}

	got := claims.ExpiresAt.Time.Sub(before)
	// Allow a small tolerance for test execution time.
	if got < 2*time.Hour-time.Minute || got > 2*time.Hour+time.Minute {
		t.Errorf("expected token expiry ~2h from now, got %v", got)
	}

	// Update the runtime config and confirm the NEXT login picks up the new value
	// without recreating the service (config changes take effect live).
	runtimeCfg.Update(&config.Config{Auth: config.AuthConfig{SessionExpiryHours: 1}})

	before2 := time.Now()
	token2, err := svc.Login(context.Background(), "admin", "password123", "", "")
	if err != nil {
		t.Fatalf("expected successful second login, got error: %v", err)
	}
	claims2, err := svc.ValidateToken(token2)
	if err != nil {
		t.Fatalf("expected valid second token, got error: %v", err)
	}
	got2 := claims2.ExpiresAt.Time.Sub(before2)
	if got2 < 1*time.Hour-time.Minute || got2 > 1*time.Hour+time.Minute {
		t.Errorf("expected updated token expiry ~1h from now, got %v", got2)
	}
}

// TestAuthLogin_SessionExpiry_FallsBackWhenUnset verifies that a zero-value
// Auth.SessionExpiryHours (e.g. an older config loaded before this field
// existed, or a directly-constructed zero-value Config as used by other tests
// in this file) falls back to the documented 24-hour default rather than
// producing an immediately-expired or zero-duration token.
func TestAuthLogin_SessionExpiry_FallsBackWhenUnset(t *testing.T) {
	userRepo := setupAuthTestDB(t)
	cfg := &config.Config{} // zero-value: Auth.SessionExpiryHours == 0
	svc := NewAuthService(userRepo, config.NewRuntimeConfig(cfg), testJWTSecret)

	createAuthTestUser(t, userRepo, "admin", "password123", "admin")

	before := time.Now()
	token, err := svc.Login(context.Background(), "admin", "password123", "", "")
	if err != nil {
		t.Fatalf("expected successful login, got error: %v", err)
	}
	claims, err := svc.ValidateToken(token)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}

	got := claims.ExpiresAt.Time.Sub(before)
	if got < 24*time.Hour-time.Minute || got > 24*time.Hour+time.Minute {
		t.Errorf("expected fallback token expiry ~24h from now, got %v", got)
	}
}
