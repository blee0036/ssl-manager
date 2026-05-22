package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
	_, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	// Save config to complete full initialization
	serverCfg := &config.ServerConfig{ExternalURL: "https://test.example.com"}
	_, err = svc.SaveConfig(ctx, SaveConfigInput{Server: serverCfg})
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
	user, err := svc.CreateAdmin(ctx, CreateAdminInput{
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
	_, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create first admin: %v", err)
	}

	// Try to create second admin - should fail
	_, err = svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin2",
		Password: "password456",
	})
	if err != ErrAlreadyInitialized {
		t.Errorf("expected ErrAlreadyInitialized, got: %v", err)
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
			_, err := svc.CreateAdmin(ctx, tt.input)
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
	_, err := svc.CreateAdmin(ctx, CreateAdminInput{
		Username: "admin",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	// Save config
	externalURL := "https://ssl.example.com"
	cfg, err := svc.SaveConfig(ctx, SaveConfigInput{
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

	// Try to save config without creating admin first
	_, err := svc.SaveConfig(ctx, SaveConfigInput{
		Server: &config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
		},
	})
	if err != ErrInitNotComplete {
		t.Errorf("expected ErrInitNotComplete, got: %v", err)
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
	_, err := svc.CreateAdmin(ctx, CreateAdminInput{
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
