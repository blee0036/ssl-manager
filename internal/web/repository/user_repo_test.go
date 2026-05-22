package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/sqlite"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"golang.org/x/crypto/bcrypt"
)

func setupUserTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL CHECK(role IN ('admin', 'user')),
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create users table: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCreateAndGetByID(t *testing.T) {
	db := setupUserTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	user := &model.User{
		Username:     "admin",
		PasswordHash: "secret123",
		Role:         "admin",
	}

	err := repo.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if user.ID == "" {
		t.Fatal("expected user ID to be set")
	}

	got, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if got.Username != "admin" {
		t.Errorf("expected username 'admin', got %q", got.Username)
	}
	if got.Role != "admin" {
		t.Errorf("expected role 'admin', got %q", got.Role)
	}
	if !got.Enabled {
		t.Error("expected user to be enabled")
	}
}

func TestGetByUsername(t *testing.T) {
	db := setupUserTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	user := &model.User{
		Username:     "testuser",
		PasswordHash: "password",
		Role:         "user",
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByUsername(ctx, "testuser")
	if err != nil {
		t.Fatalf("GetByUsername failed: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("expected ID %q, got %q", user.ID, got.ID)
	}
}

func TestGetByUsername_NotFound(t *testing.T) {
	db := setupUserTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	_, err := repo.GetByUsername(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent user")
	}
}

func TestListUsers(t *testing.T) {
	db := setupUserTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	// Create two users
	user1 := &model.User{Username: "user1", PasswordHash: "pass1", Role: "admin"}
	user2 := &model.User{Username: "user2", PasswordHash: "pass2", Role: "user"}

	if err := repo.Create(ctx, user1); err != nil {
		t.Fatalf("Create user1 failed: %v", err)
	}
	if err := repo.Create(ctx, user2); err != nil {
		t.Fatalf("Create user2 failed: %v", err)
	}

	users, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
}

func TestDisableUser(t *testing.T) {
	db := setupUserTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	user := &model.User{Username: "toDisable", PasswordHash: "pass", Role: "user"}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := repo.Disable(ctx, user.ID); err != nil {
		t.Fatalf("Disable failed: %v", err)
	}

	got, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Enabled {
		t.Error("expected user to be disabled")
	}
}

func TestPasswordStoredAsBcryptHash(t *testing.T) {
	db := setupUserTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	plainPassword := "mySecretPassword"
	user := &model.User{
		Username:     "hashtest",
		PasswordHash: plainPassword,
		Role:         "user",
	}

	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByUsername(ctx, "hashtest")
	if err != nil {
		t.Fatalf("GetByUsername failed: %v", err)
	}

	// The stored hash should NOT be the plain password
	if got.PasswordHash == plainPassword {
		t.Fatal("password stored as plaintext, expected bcrypt hash")
	}

	// The stored hash should be a valid bcrypt hash that matches the original password
	if err := bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte(plainPassword)); err != nil {
		t.Fatalf("stored hash does not match original password: %v", err)
	}
}
