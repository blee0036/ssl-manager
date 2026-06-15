package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"golang.org/x/crypto/bcrypt"
)

// CreateUserTx creates a new user within a transaction.
// The user.PasswordHash field should contain the plain-text password;
// it will be hashed before storage.
// If tx is nil, the operation runs on the DB directly.
func (db *DB) CreateUserTx(tx *sql.Tx, user *model.User) error {
	exec := db.getExecutor(tx)

	// Hash the password
	hash, err := bcrypt.GenerateFromPassword([]byte(user.PasswordHash), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Generate ID if not set
	if user.ID == "" {
		user.ID = uuid.New().String()
	}

	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now
	user.Enabled = true

	_, err = exec.Exec(
		`INSERT INTO users (id, username, password_hash, role, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		user.ID,
		user.Username,
		string(hash),
		user.Role,
		boolToInt(user.Enabled),
		user.CreatedAt.Format(time.RFC3339),
		user.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	// Store the hash back so the caller can see it
	user.PasswordHash = string(hash)
	return nil
}

// DeleteUserTx deletes a user by ID within a transaction.
// If tx is nil, the operation runs on the DB directly.
func (db *DB) DeleteUserTx(tx *sql.Tx, userID string) error {
	exec := db.getExecutor(tx)

	result, err := exec.Exec(`DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("user not found: %s", userID)
	}
	return nil
}
