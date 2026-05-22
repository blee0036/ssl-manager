package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"golang.org/x/crypto/bcrypt"
)

// UserRepository provides CRUD operations for users.
type UserRepository struct {
	db *sql.DB
}

// NewUserRepository creates a new UserRepository.
func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create creates a new user with bcrypt-hashed password.
// The user.PasswordHash field should contain the plain-text password;
// it will be hashed before storage.
func (r *UserRepository) Create(ctx context.Context, user *model.User) error {
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

	_, err = r.db.ExecContext(ctx,
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

// GetByUsername retrieves a user by username.
func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, enabled, created_at, updated_at
		 FROM users WHERE username = ?`, username)

	return scanUser(row)
}

// GetByID retrieves a user by ID.
func (r *UserRepository) GetByID(ctx context.Context, id string) (*model.User, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, enabled, created_at, updated_at
		 FROM users WHERE id = ?`, id)

	return scanUser(row)
}

// List returns all users.
func (r *UserRepository) List(ctx context.Context) ([]*model.User, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, username, password_hash, role, enabled, created_at, updated_at
		 FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []*model.User
	for rows.Next() {
		user, err := scanUserFromRows(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate users: %w", err)
	}
	return users, nil
}

// Update updates user fields (name, role).
func (r *UserRepository) Update(ctx context.Context, id string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	query := "UPDATE users SET "
	args := make([]interface{}, 0, len(updates)+2)
	first := true

	for key, value := range updates {
		if !first {
			query += ", "
		}
		query += key + " = ?"
		args = append(args, value)
		first = false
	}

	query += ", updated_at = ? WHERE id = ?"
	args = append(args, time.Now().Format(time.RFC3339), id)

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("user not found: %s", id)
	}
	return nil
}

// Disable disables a user account (sets enabled=0).
func (r *UserRepository) Disable(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE users SET enabled = 0, updated_at = ? WHERE id = ?`,
		time.Now().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("failed to disable user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("user not found: %s", id)
	}
	return nil
}

// UpdatePassword updates a user's password (stores bcrypt hash).
func (r *UserRepository) UpdatePassword(ctx context.Context, id string, passwordHash string) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		passwordHash, time.Now().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("user not found: %s", id)
	}
	return nil
}

// scanUser scans a single user row from *sql.Row.
func scanUser(row *sql.Row) (*model.User, error) {
	var user model.User
	var enabled int
	var createdAt, updatedAt string

	err := row.Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.Role,
		&enabled,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to scan user: %w", err)
	}

	user.Enabled = enabled == 1
	user.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	user.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &user, nil
}

// scanUserFromRows scans a single user from *sql.Rows.
func scanUserFromRows(rows *sql.Rows) (*model.User, error) {
	var user model.User
	var enabled int
	var createdAt, updatedAt string

	err := rows.Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.Role,
		&enabled,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan user: %w", err)
	}

	user.Enabled = enabled == 1
	user.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	user.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &user, nil
}


