package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/glebarez/sqlite"
)

// DB wraps *sql.DB to provide database operations.
type DB struct {
	*sql.DB
	dataDir string
}

// NewDB creates a new database connection. It ensures the data directory exists,
// opens the SQLite3 database, enables WAL mode and foreign keys, and runs migrations.
func NewDB(dataDir string) (*DB, error) {
	// Create data directory if not exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory %s: %w", dataDir, err)
	}

	dbPath := filepath.Join(dataDir, "data.sqlite3")
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Verify connection
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Enable WAL mode and foreign keys via PRAGMA
	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}
	if _, err := sqlDB.Exec("PRAGMA foreign_keys=ON"); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	db := &DB{
		DB:      sqlDB,
		dataDir: dataDir,
	}

	// Run migrations
	if err := db.Migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.DB.Close()
}

// HasAdminUser checks if any admin user exists in the database.
// This is used during the init flow to determine if the system needs initialization.
func (db *DB) HasAdminUser() (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin' AND enabled = 1").Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check admin user: %w", err)
	}
	return count > 0, nil
}
