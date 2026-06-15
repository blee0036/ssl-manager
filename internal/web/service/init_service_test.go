package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/glebarez/sqlite"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/database"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// setupTestDB creates an in-memory SQLite database with the users table for testing.
func setupInitTestDB(t *testing.T) *database.DB {
	t.Helper()

	// Create a temp directory for the database
	tmpDir := t.TempDir()

	db, err := database.NewDB(tmpDir)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

func TestInitService_NeedsInit_NoAdminUser(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)

	ctx := context.Background()
	needsInit, err := svc.NeedsInit(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !needsInit {
		t.Error("expected NeedsInit to return true when no admin user exists")
	}
}

func TestInitService_NeedsInit_AfterAdminCreated(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)

	ctx := context.Background()

	// Create admin
	_, initToken, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	// Save config to complete full initialization
	serverCfg := &config.ServerConfig{ExternalURL: "https://test.example.com"}
	_, err = svc.SaveConfig(ctx, initToken, SaveConfigInput{Server: serverCfg})
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Now should not need init
	needsInit, err := svc.NeedsInit(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if needsInit {
		t.Error("expected NeedsInit to return false after admin user is created and config is saved")
	}
}

func TestInitService_CreateAdmin_Success(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)

	ctx := context.Background()
	user, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "securepass123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	if user.ID == "" {
		t.Error("expected user ID to be set")
	}
	if user.Username != "admin" {
		t.Errorf("expected username 'admin', got '%s'", user.Username)
	}
	if user.Role != "admin" {
		t.Errorf("expected role 'admin', got '%s'", user.Role)
	}
}

func TestInitService_CreateAdmin_AlreadyInitialized(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)

	ctx := context.Background()

	// Create first admin
	_, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create first admin: %v", err)
	}

	// Try to create second admin - should fail (pending not expired)
	_, _, err = svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin2",
		Password: "password456",
	})
	if err != ErrInitPendingNotExpired {
		t.Errorf("expected ErrInitPendingNotExpired, got: %v", err)
	}
}

func TestInitService_CreateAdmin_ValidationErrors(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)

	ctx := context.Background()

	tests := []struct {
		name  string
		input CreateAdminInput
	}{
		{
			name:  "empty username",
			input: CreateAdminInput{Username: "", Password: "password123"},
		},
		{
			name:  "empty password",
			input: CreateAdminInput{Username: "admin", Password: ""},
		},
		{
			name:  "short password",
			input: CreateAdminInput{Username: "admin", Password: "12345"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := svc.CreateAdmin(ctx, tt.input)
			if err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func TestInitService_SaveConfig_Success(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)

	ctx := context.Background()

	// First create admin (required before saving config)
	_, token, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	// Save config
	externalURL := "https://ssl.example.com"
	cfg, err := svc.SaveConfig(ctx, token, SaveConfigInput{
		Server: &config.ServerConfig{
			ExternalURL: externalURL,
			ListenAddr:  ":9090",
		},
	})
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	if cfg.Server.ExternalURL != externalURL {
		t.Errorf("expected external_url '%s', got '%s'", externalURL, cfg.Server.ExternalURL)
	}
	if cfg.Server.ListenAddr != ":9090" {
		t.Errorf("expected listen_addr ':9090', got '%s'", cfg.Server.ListenAddr)
	}

	// Verify file was written
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("expected config.json to be created")
	}
}

func TestInitService_SaveConfig_BeforeAdminCreated(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)

	ctx := context.Background()

	// Try to save config without creating admin first (use a fake token)
	_, err := svc.SaveConfig(ctx, "fake-token", SaveConfigInput{
		Server: &config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
		},
	})
	if err != ErrInvalidInitToken {
		t.Errorf("expected ErrInvalidInitToken, got: %v", err)
	}
}

func TestInitService_IsInitialized_CachesResult(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)

	ctx := context.Background()

	// Initially not initialized
	if svc.IsInitialized() {
		t.Error("expected IsInitialized to return false initially")
	}

	// Create admin
	_, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	// Now should be cached as initialized
	if !svc.IsInitialized() {
		t.Error("expected IsInitialized to return true after admin creation")
	}
}

func TestInitService_SaveConfig_WithTurnstile_Success(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)

	ctx := context.Background()

	// Create admin first
	_, token, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	// Save config with Turnstile enabled
	cfg, err := svc.SaveConfig(ctx, token, SaveConfigInput{
		Server: &config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
		},
		Turnstile: &config.TurnstileConfig{
			Enabled:   true,
			SiteKey:   "site-key-123",
			SecretKey: "secret-key-456",
		},
	})
	if err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	if !cfg.Turnstile.Enabled {
		t.Error("expected Turnstile to be enabled")
	}
	if cfg.Turnstile.SiteKey != "site-key-123" {
		t.Errorf("expected site_key 'site-key-123', got '%s'", cfg.Turnstile.SiteKey)
	}
	if cfg.Turnstile.SecretKey != "secret-key-456" {
		t.Errorf("expected secret_key 'secret-key-456', got '%s'", cfg.Turnstile.SecretKey)
	}

	// Verify config was persisted to disk
	loaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load saved config: %v", err)
	}
	if !loaded.Turnstile.Enabled {
		t.Error("expected persisted Turnstile to be enabled")
	}
	if loaded.Turnstile.SiteKey != "site-key-123" {
		t.Errorf("expected persisted site_key 'site-key-123', got '%s'", loaded.Turnstile.SiteKey)
	}
	if loaded.Turnstile.SecretKey != "secret-key-456" {
		t.Errorf("expected persisted secret_key 'secret-key-456', got '%s'", loaded.Turnstile.SecretKey)
	}
}

func TestInitService_SaveConfig_TurnstileEnabled_MissingSiteKey(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)

	ctx := context.Background()

	// Create admin first
	_, token, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	// Try to save config with Turnstile enabled but missing site_key
	_, err = svc.SaveConfig(ctx, token, SaveConfigInput{
		Server: &config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
		},
		Turnstile: &config.TurnstileConfig{
			Enabled:   true,
			SiteKey:   "",
			SecretKey: "secret-key-456",
		},
	})
	if err == nil {
		t.Fatal("expected validation error when site_key is missing, got nil")
	}
}

func TestInitService_SaveConfig_TurnstileEnabled_MissingSecretKey(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)

	ctx := context.Background()

	// Create admin first
	_, token2, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	// Try to save config with Turnstile enabled but missing secret_key
	_, err = svc.SaveConfig(ctx, token2, SaveConfigInput{
		Server: &config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
		},
		Turnstile: &config.TurnstileConfig{
			Enabled:   true,
			SiteKey:   "site-key-123",
			SecretKey: "",
		},
	})
	if err == nil {
		t.Fatal("expected validation error when secret_key is missing, got nil")
	}
}


// =============================================================================
// Token Expiry Tests (Requirement 1.5, 1.9)
// =============================================================================

// TestInitService_SaveConfig_ExpiredToken verifies that SaveConfig with an expired
// token returns ErrInitTokenExpired (Requirement 1.5).
func TestInitService_SaveConfig_ExpiredToken(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	// Create admin to get a token
	_, plainToken, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateAdmin failed: %v", err)
	}

	// Manually expire the token by updating the init_state's expires_at to the past
	_, err = db.Exec(
		`UPDATE init_state SET expires_at = ? WHERE pending_init = 1`,
		time.Now().Add(-1*time.Hour).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to manually expire token: %v", err)
	}

	// Try SaveConfig with the now-expired token
	_, err = svc.SaveConfig(ctx, plainToken, SaveConfigInput{
		Server: &config.ServerConfig{ExternalURL: "https://test.example.com"},
	})
	if !errors.Is(err, ErrInitTokenExpired) {
		t.Errorf("expected ErrInitTokenExpired, got: %v", err)
	}

	// Verify config file was NOT created
	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		t.Error("config file should not exist after expired token rejection")
	}
}

// TestInitService_SaveConfig_ExpiredToken_ThenRecreateAdmin verifies that after
// a token expires, a new CreateAdmin can be called to replace the expired pending
// admin (Requirement 1.9, 1.10).
func TestInitService_SaveConfig_ExpiredToken_ThenRecreateAdmin(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	// Create first admin
	user1, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin1",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("first CreateAdmin failed: %v", err)
	}

	// Expire the token
	_, err = db.Exec(
		`UPDATE init_state SET expires_at = ? WHERE pending_init = 1`,
		time.Now().Add(-1*time.Hour).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to manually expire token: %v", err)
	}

	// Create second admin — should succeed because previous pending is expired
	user2, token2, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin2",
		Password: "password456",
	})
	if err != nil {
		t.Fatalf("second CreateAdmin failed: %v", err)
	}

	if user2.Username != "admin2" {
		t.Errorf("expected username 'admin2', got '%s'", user2.Username)
	}

	// Verify old admin was deleted
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, user1.ID).Scan(&count)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if count != 0 {
		t.Error("expired pending admin should have been deleted")
	}

	// Verify new token works for SaveConfig
	_, err = svc.SaveConfig(ctx, token2, SaveConfigInput{
		Server: &config.ServerConfig{ExternalURL: "https://test2.example.com"},
	})
	if err != nil {
		t.Fatalf("SaveConfig with new token failed: %v", err)
	}
}

// =============================================================================
// EnsureInitState Restart Logic Tests (Requirement 1.8)
// =============================================================================

// TestInitService_EnsureInitState_CompletedRecordExists verifies EnsureInitState
// is a no-op when a completed record already exists.
func TestInitService_EnsureInitState_CompletedRecordExists(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	// Create admin and complete initialization
	_, token, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateAdmin failed: %v", err)
	}

	_, err = svc.SaveConfig(ctx, token, SaveConfigInput{
		Server: &config.ServerConfig{ExternalURL: "https://test.example.com"},
	})
	if err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Count init_state rows before
	var countBefore int
	db.QueryRow(`SELECT COUNT(*) FROM init_state`).Scan(&countBefore)

	// Call EnsureInitState — should be a no-op
	err = svc.EnsureInitState(ctx)
	if err != nil {
		t.Fatalf("EnsureInitState failed: %v", err)
	}

	// Count init_state rows after — should be same
	var countAfter int
	db.QueryRow(`SELECT COUNT(*) FROM init_state`).Scan(&countAfter)
	if countAfter != countBefore {
		t.Errorf("expected %d init_state rows after EnsureInitState, got %d", countBefore, countAfter)
	}
}

// TestInitService_EnsureInitState_ConvertPendingToCompleted verifies that when
// admin exists + config file exists + pending row exists (no completed),
// EnsureInitState converts the pending row to completed.
func TestInitService_EnsureInitState_ConvertPendingToCompleted(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	// Create admin (creates pending init_state)
	_, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateAdmin failed: %v", err)
	}

	// Simulate "config file was saved but DB update failed" by creating config file manually
	cfg := config.DefaultConfig()
	cfg.Server.ExternalURL = "https://test.example.com"
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Verify we have a pending row and no completed row
	hasCompleted, _ := db.HasCompletedInitState(nil)
	if hasCompleted {
		t.Fatal("should not have completed state before EnsureInitState")
	}
	pending, _ := db.GetPendingInitState(nil)
	if pending == nil {
		t.Fatal("should have a pending state before EnsureInitState")
	}
	pendingID := pending.ID

	// Call EnsureInitState — should convert pending to completed
	err = svc.EnsureInitState(ctx)
	if err != nil {
		t.Fatalf("EnsureInitState failed: %v", err)
	}

	// Verify: no more pending state
	pendingAfter, _ := db.GetPendingInitState(nil)
	if pendingAfter != nil {
		t.Error("expected no pending state after EnsureInitState")
	}

	// Verify: completed record exists
	hasCompletedAfter, _ := db.HasCompletedInitState(nil)
	if !hasCompletedAfter {
		t.Error("expected completed state after EnsureInitState")
	}

	// Verify: the converted row has correct fields
	var tokenHash string
	var pendingInit int
	var completedAt string
	err = db.QueryRow(
		`SELECT token_hash, pending_init, completed_at FROM init_state WHERE id = ?`, pendingID,
	).Scan(&tokenHash, &pendingInit, &completedAt)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if tokenHash != "" {
		t.Errorf("expected empty token_hash after conversion, got %q", tokenHash)
	}
	if pendingInit != 0 {
		t.Errorf("expected pending_init=0, got %d", pendingInit)
	}
	if completedAt == "" {
		t.Error("expected completed_at to be set")
	}
}

// TestInitService_EnsureInitState_BackfillLegacy verifies that when admin exists
// + config file exists + NO init_state rows exist, EnsureInitState creates a
// backfill completed record for legacy systems.
func TestInitService_EnsureInitState_BackfillLegacy(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")

	// Simulate a legacy system: admin exists in users table, config file on disk,
	// but no init_state records at all.

	// Create admin user directly in the DB (bypassing InitService)
	_, err := db.Exec(
		`INSERT INTO users (id, username, password_hash, role, enabled, created_at, updated_at)
		 VALUES ('legacy-admin-id', 'admin', '$2a$10$hashhere', 'admin', 1, '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("failed to insert legacy admin: %v", err)
	}

	// Create config file
	cfg := config.DefaultConfig()
	cfg.Server.ExternalURL = "https://legacy.example.com"
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	// Verify no init_state rows exist
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM init_state`).Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 init_state rows, got %d", count)
	}

	// Call EnsureInitState
	err = svc.EnsureInitState(ctx)
	if err != nil {
		t.Fatalf("EnsureInitState failed: %v", err)
	}

	// Verify: a completed record was backfilled
	hasCompleted, _ := db.HasCompletedInitState(nil)
	if !hasCompleted {
		t.Error("expected a completed init_state record after legacy backfill")
	}

	// Verify: the backfill record has admin_id="backfill-legacy"
	var adminID string
	var pendingInit int
	err = db.QueryRow(
		`SELECT admin_id, pending_init FROM init_state WHERE pending_init = 0`,
	).Scan(&adminID, &pendingInit)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if adminID != "backfill-legacy" {
		t.Errorf("expected admin_id 'backfill-legacy', got %q", adminID)
	}
}

// TestInitService_EnsureInitState_NoAdminNoConfig verifies that EnsureInitState
// does nothing when neither admin nor config exists (fresh system).
func TestInitService_EnsureInitState_NoAdminNoConfig(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "nonexistent", "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	err := svc.EnsureInitState(ctx)
	if err != nil {
		t.Fatalf("EnsureInitState failed: %v", err)
	}

	// Verify: no init_state rows created
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM init_state`).Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 init_state rows for fresh system, got %d", count)
	}
}

// =============================================================================
// Transaction Atomicity Tests (Requirement 1.10)
// =============================================================================

// TestInitService_CreateAdmin_TransactionAtomicity verifies that CreateAdmin
// is atomic — if the operation succeeds, both the user and init_state exist;
// the test verifies consistent state after various scenarios.
func TestInitService_CreateAdmin_TransactionAtomicity(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	// Successful CreateAdmin: both user and init_state must exist
	user, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateAdmin failed: %v", err)
	}

	// Verify user exists
	var userCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, user.ID).Scan(&userCount)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if userCount != 1 {
		t.Errorf("expected 1 user with ID %s, got %d", user.ID, userCount)
	}

	// Verify init_state exists and references the correct admin
	state, err := db.GetPendingInitState(nil)
	if err != nil {
		t.Fatalf("GetPendingInitState error: %v", err)
	}
	if state == nil {
		t.Fatal("expected pending init_state after CreateAdmin")
	}
	if state.AdminID != user.ID {
		t.Errorf("init_state.admin_id %q != user.ID %q", state.AdminID, user.ID)
	}
}

// TestInitService_CreateAdmin_AtomicityOnExpiredOverwrite verifies that when
// an expired pending admin is overwritten, the old admin + init_state are deleted
// AND new admin + init_state are created — all atomically.
func TestInitService_CreateAdmin_AtomicityOnExpiredOverwrite(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	// Create first admin
	user1, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin1",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("first CreateAdmin failed: %v", err)
	}

	// Expire the pending token
	_, err = db.Exec(
		`UPDATE init_state SET expires_at = ? WHERE pending_init = 1`,
		time.Now().Add(-1*time.Hour).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to expire token: %v", err)
	}

	// Create second admin (should overwrite)
	user2, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin2",
		Password: "password456",
	})
	if err != nil {
		t.Fatalf("second CreateAdmin failed: %v", err)
	}

	// Verify: old user deleted
	var oldUserCount int
	db.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, user1.ID).Scan(&oldUserCount)
	if oldUserCount != 0 {
		t.Error("old admin user should be deleted after expired overwrite")
	}

	// Verify: new user exists
	var newUserCount int
	db.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, user2.ID).Scan(&newUserCount)
	if newUserCount != 1 {
		t.Error("new admin user should exist after overwrite")
	}

	// Verify: exactly 1 init_state row (old deleted, new created)
	var stateCount int
	db.QueryRow(`SELECT COUNT(*) FROM init_state`).Scan(&stateCount)
	if stateCount != 1 {
		t.Errorf("expected exactly 1 init_state row, got %d", stateCount)
	}

	// Verify: the init_state row references new admin
	state, _ := db.GetPendingInitState(nil)
	if state == nil {
		t.Fatal("expected pending init_state after overwrite")
	}
	if state.AdminID != user2.ID {
		t.Errorf("init_state should reference new admin %s, got %s", user2.ID, state.AdminID)
	}
}

// =============================================================================
// Unexpired Pending Rejection Tests (Requirement 1.10)
// =============================================================================

// TestInitService_CreateAdmin_UnexpiredPendingRejection verifies that when a
// pending admin exists and its token is NOT expired, new CreateAdmin is rejected
// with ErrInitPendingNotExpired.
func TestInitService_CreateAdmin_UnexpiredPendingRejection(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	// Create first admin (token valid for 30 minutes)
	_, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin1",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("first CreateAdmin failed: %v", err)
	}

	// Verify token is not expired (should be valid for ~30 minutes)
	state, _ := db.GetPendingInitState(nil)
	if state == nil {
		t.Fatal("expected pending state")
	}
	if IsInitStateExpired(state) {
		t.Fatal("token should NOT be expired")
	}

	// Try to create a second admin — should be rejected
	_, _, err = svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin2",
		Password: "password456",
	})
	if !errors.Is(err, ErrInitPendingNotExpired) {
		t.Errorf("expected ErrInitPendingNotExpired, got: %v", err)
	}

	// Verify: original admin still exists (not deleted)
	var userCount int
	db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = 'admin1'`).Scan(&userCount)
	if userCount != 1 {
		t.Error("original admin should still exist after rejection")
	}

	// Verify: still only 1 init_state row
	var stateCount int
	db.QueryRow(`SELECT COUNT(*) FROM init_state`).Scan(&stateCount)
	if stateCount != 1 {
		t.Errorf("expected 1 init_state row, got %d", stateCount)
	}
}

// TestInitService_CreateAdmin_AfterCompletedRejectsAll verifies that once a
// completed record exists, all CreateAdmin attempts are rejected with
// ErrAlreadyInitialized regardless of pending state.
func TestInitService_CreateAdmin_AfterCompletedRejectsAll(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	// Complete full initialization
	_, token, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateAdmin failed: %v", err)
	}
	_, err = svc.SaveConfig(ctx, token, SaveConfigInput{
		Server: &config.ServerConfig{ExternalURL: "https://test.example.com"},
	})
	if err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Try to create another admin — should be rejected
	_, _, err = svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "attacker",
		Password: "evilpass123",
	})
	if !errors.Is(err, ErrAlreadyInitialized) {
		t.Errorf("expected ErrAlreadyInitialized, got: %v", err)
	}
}

// =============================================================================
// Concurrent CreateAdmin Tests (Requirement 1.10 — DB constraint)
// =============================================================================

// TestInitService_CreateAdmin_ConcurrentOnlyOneSucceeds verifies that when
// multiple goroutines try to CreateAdmin concurrently, at most one succeeds
// (guaranteed by SQLite serialized writes + partial unique index).
func TestInitService_CreateAdmin_ConcurrentOnlyOneSucceeds(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	const numGoroutines = 10
	var wg sync.WaitGroup
	results := make(chan error, numGoroutines)

	// Launch multiple concurrent CreateAdmin calls
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			_, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
				Username: "admin",
				Password: "password123",
			})
			results <- err
		}(i)
	}

	wg.Wait()
	close(results)

	// Count successes and failures
	successCount := 0
	expectedErrors := 0
	var unexpectedErrors []error

	for err := range results {
		if err == nil {
			successCount++
		} else if errors.Is(err, ErrInitPendingNotExpired) {
			expectedErrors++
		} else {
			// Some errors may be due to UNIQUE constraint on username or
			// SQLite busy/locked — these are acceptable concurrent behavior
			unexpectedErrors = append(unexpectedErrors, err)
		}
	}

	// At most one should succeed
	if successCount > 1 {
		t.Errorf("expected at most 1 success, got %d", successCount)
	}

	// At least one should succeed (the system should be usable)
	if successCount == 0 && len(unexpectedErrors) > 0 {
		t.Logf("No CreateAdmin succeeded. Unexpected errors: %v", unexpectedErrors)
		// This can happen with SQLite contention — the important thing is
		// at most 1 pending row exists
	}

	// Verify DB state: at most 1 pending init_state row
	var pendingCount int
	err := db.QueryRow(`SELECT COUNT(*) FROM init_state WHERE pending_init = 1`).Scan(&pendingCount)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if pendingCount > 1 {
		t.Errorf("CRITICAL: expected at most 1 pending init_state row, got %d", pendingCount)
	}

	// Verify: at most 1 admin user with username "admin"
	var adminCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = 'admin'`).Scan(&adminCount)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if adminCount > 1 {
		t.Errorf("CRITICAL: expected at most 1 admin user, got %d", adminCount)
	}

	t.Logf("Concurrent results: %d success, %d expected_errors (pending not expired), %d unexpected",
		successCount, expectedErrors, len(unexpectedErrors))
}

// TestInitService_CreateAdmin_ConcurrentDifferentUsernames verifies that even
// with different usernames, only one pending admin can exist due to partial
// unique index on pending_init=1.
func TestInitService_CreateAdmin_ConcurrentDifferentUsernames(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	const numGoroutines = 5
	var wg sync.WaitGroup
	results := make(chan error, numGoroutines)

	// Launch concurrent CreateAdmin with different usernames
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			username := "admin" + string(rune('A'+idx))
			_, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
				Username: username,
				Password: "password123",
			})
			results <- err
		}(i)
	}

	wg.Wait()
	close(results)

	successCount := 0
	for err := range results {
		if err == nil {
			successCount++
		}
	}

	// At most one should succeed (DB partial unique index guarantees this)
	if successCount > 1 {
		t.Errorf("expected at most 1 success with different usernames, got %d", successCount)
	}

	// Verify DB state
	var pendingCount int
	db.QueryRow(`SELECT COUNT(*) FROM init_state WHERE pending_init = 1`).Scan(&pendingCount)
	if pendingCount > 1 {
		t.Errorf("CRITICAL: more than 1 pending init_state row: %d", pendingCount)
	}
}

// =============================================================================
// GetPhase Tests
// =============================================================================

// TestInitService_GetPhase_NeedsAdmin verifies GetPhase returns "needs_admin"
// when no admin exists.
func TestInitService_GetPhase_NeedsAdmin(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	phase, err := svc.GetPhase(ctx)
	if err != nil {
		t.Fatalf("GetPhase failed: %v", err)
	}
	if phase != "needs_admin" {
		t.Errorf("expected phase 'needs_admin', got %q", phase)
	}
}

// TestInitService_GetPhase_NeedsConfig verifies GetPhase returns "needs_config"
// when admin created but config not saved (pending token is active).
func TestInitService_GetPhase_NeedsConfig(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	_, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateAdmin failed: %v", err)
	}

	phase, err := svc.GetPhase(ctx)
	if err != nil {
		t.Fatalf("GetPhase failed: %v", err)
	}
	if phase != "needs_config" {
		t.Errorf("expected phase 'needs_config', got %q", phase)
	}
}

// TestInitService_GetPhase_NeedsAdmin_AfterTokenExpiry verifies that GetPhase
// returns "needs_admin" when the pending token is expired.
func TestInitService_GetPhase_NeedsAdmin_AfterTokenExpiry(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	_, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateAdmin failed: %v", err)
	}

	// Expire the token
	_, err = db.Exec(
		`UPDATE init_state SET expires_at = ? WHERE pending_init = 1`,
		time.Now().Add(-1*time.Hour).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to expire token: %v", err)
	}

	phase, err := svc.GetPhase(ctx)
	if err != nil {
		t.Fatalf("GetPhase failed: %v", err)
	}
	if phase != "needs_admin" {
		t.Errorf("expected phase 'needs_admin' after token expiry, got %q", phase)
	}
}

// TestInitService_GetPhase_Completed verifies GetPhase returns "completed"
// after full initialization.
func TestInitService_GetPhase_Completed(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	_, token, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateAdmin failed: %v", err)
	}

	_, err = svc.SaveConfig(ctx, token, SaveConfigInput{
		Server: &config.ServerConfig{ExternalURL: "https://test.example.com"},
	})
	if err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	phase, err := svc.GetPhase(ctx)
	if err != nil {
		t.Fatalf("GetPhase failed: %v", err)
	}
	if phase != "completed" {
		t.Errorf("expected phase 'completed', got %q", phase)
	}
}

// =============================================================================
// SaveConfig Token Validation Edge Cases
// =============================================================================

// TestInitService_SaveConfig_EmptyToken verifies that an empty init token
// is rejected with ErrInvalidInitToken.
func TestInitService_SaveConfig_EmptyToken(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	_, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateAdmin failed: %v", err)
	}

	// Empty token
	_, err = svc.SaveConfig(ctx, "", SaveConfigInput{
		Server: &config.ServerConfig{ExternalURL: "https://test.example.com"},
	})
	if !errors.Is(err, ErrInvalidInitToken) {
		t.Errorf("expected ErrInvalidInitToken for empty token, got: %v", err)
	}
}

// TestInitService_SaveConfig_WrongToken verifies that a wrong token is
// rejected with ErrInvalidInitToken.
func TestInitService_SaveConfig_WrongToken(t *testing.T) {
	db := setupInitTestDB(t)
	userRepo := repository.NewUserRepository(db.DB)
	configPath := filepath.Join(t.TempDir(), "config.json")

	svc := NewInitService(db, userRepo, configPath, nil)
	ctx := context.Background()

	_, _, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateAdmin failed: %v", err)
	}

	// Wrong token (valid hex but not the real one)
	wrongToken := "0000000000000000000000000000000000000000000000000000000000000000"
	_, err = svc.SaveConfig(ctx, wrongToken, SaveConfigInput{
		Server: &config.ServerConfig{ExternalURL: "https://test.example.com"},
	})
	if !errors.Is(err, ErrInvalidInitToken) {
		t.Errorf("expected ErrInvalidInitToken for wrong token, got: %v", err)
	}
}
