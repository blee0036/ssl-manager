package database

import (
	"database/sql"
	"fmt"
	"time"
)

// InitState represents a row in the init_state table.
type InitState struct {
	ID          string
	AdminID     string
	TokenHash   string
	ExpiresAt   time.Time
	PendingInit bool
	CompletedAt *time.Time
}

// executor is an interface satisfied by both *sql.DB and *sql.Tx,
// allowing repository methods to work within or outside a transaction.
type executor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

// getExecutor returns the provided tx if non-nil, otherwise the DB itself.
func (db *DB) getExecutor(tx *sql.Tx) executor {
	if tx != nil {
		return tx
	}
	return db.DB
}

// InsertInitState inserts a new init_state record.
// If tx is nil, the operation runs on the DB directly.
func (db *DB) InsertInitState(tx *sql.Tx, state *InitState) error {
	exec := db.getExecutor(tx)

	completedAt := sql.NullString{}
	if state.CompletedAt != nil {
		completedAt.Valid = true
		completedAt.String = state.CompletedAt.Format(time.RFC3339)
	}

	_, err := exec.Exec(
		`INSERT INTO init_state (id, admin_id, token_hash, expires_at, pending_init, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		state.ID,
		state.AdminID,
		state.TokenHash,
		state.ExpiresAt.Format(time.RFC3339),
		boolToInt(state.PendingInit),
		completedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert init_state: %w", err)
	}
	return nil
}

// GetPendingInitState retrieves the init_state record where pending_init=1.
// Returns nil, nil if no pending record exists.
// If tx is nil, the operation runs on the DB directly.
func (db *DB) GetPendingInitState(tx *sql.Tx) (*InitState, error) {
	exec := db.getExecutor(tx)

	row := exec.QueryRow(
		`SELECT id, admin_id, token_hash, expires_at, pending_init, completed_at
		 FROM init_state WHERE pending_init = 1 LIMIT 1`)

	state, err := scanInitState(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get pending init_state: %w", err)
	}
	return state, nil
}

// HasCompletedInitState checks if any init_state record with pending_init=0 exists.
// If tx is nil, the operation runs on the DB directly.
func (db *DB) HasCompletedInitState(tx *sql.Tx) (bool, error) {
	exec := db.getExecutor(tx)

	var count int
	err := exec.QueryRow(`SELECT COUNT(*) FROM init_state WHERE pending_init = 0`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check completed init_state: %w", err)
	}
	return count > 0, nil
}

// UpdateInitStateToCompleted marks an init_state record as completed:
// sets pending_init=0, completed_at=now(), token_hash=''.
// If tx is nil, the operation runs on the DB directly.
func (db *DB) UpdateInitStateToCompleted(tx *sql.Tx, id string) error {
	exec := db.getExecutor(tx)

	now := time.Now().Format(time.RFC3339)
	result, err := exec.Exec(
		`UPDATE init_state SET pending_init = 0, completed_at = ?, token_hash = '' WHERE id = ?`,
		now, id)
	if err != nil {
		return fmt.Errorf("failed to update init_state to completed: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("init_state not found: %s", id)
	}
	return nil
}

// ConsumeInitToken atomically marks an init_state record as completed,
// but ONLY if it is still pending with the expected token hash.
// This prevents concurrent SaveConfig requests from both succeeding.
// Returns ErrInitTokenAlreadyConsumed if the row was already consumed or doesn't match.
func (db *DB) ConsumeInitToken(tx *sql.Tx, id string, expectedTokenHash string) error {
	exec := db.getExecutor(tx)

	now := time.Now().Format(time.RFC3339)
	result, err := exec.Exec(
		`UPDATE init_state SET pending_init = 0, completed_at = ?, token_hash = '' WHERE id = ? AND pending_init = 1 AND token_hash = ?`,
		now, id, expectedTokenHash)
	if err != nil {
		return fmt.Errorf("failed to consume init token: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrInitTokenAlreadyConsumed
	}
	return nil
}

// ErrInitTokenAlreadyConsumed indicates the init token was already consumed by another request.
var ErrInitTokenAlreadyConsumed = fmt.Errorf("init token already consumed")

// DeleteInitState deletes an init_state record by ID.
// If tx is nil, the operation runs on the DB directly.
func (db *DB) DeleteInitState(tx *sql.Tx, id string) error {
	exec := db.getExecutor(tx)

	result, err := exec.Exec(`DELETE FROM init_state WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete init_state: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("init_state not found: %s", id)
	}
	return nil
}

// scanInitState scans a single init_state row from *sql.Row.
func scanInitState(row *sql.Row) (*InitState, error) {
	var state InitState
	var pendingInit int
	var expiresAt string
	var completedAt sql.NullString

	err := row.Scan(
		&state.ID,
		&state.AdminID,
		&state.TokenHash,
		&expiresAt,
		&pendingInit,
		&completedAt,
	)
	if err != nil {
		return nil, err
	}

	state.PendingInit = pendingInit == 1
	state.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)

	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339, completedAt.String)
		state.CompletedAt = &t
	}

	return &state, nil
}

// boolToInt converts a boolean to an integer (0 or 1) for SQLite storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
