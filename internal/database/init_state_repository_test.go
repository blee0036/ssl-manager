package database

import (
	"database/sql"
	"testing"
	"time"
)

func setupInitStateTestDB(t *testing.T) *DB {
	t.Helper()
	db := openTestDB(t)
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}
	return db
}

func TestInsertInitState_Basic(t *testing.T) {
	db := setupInitStateTestDB(t)

	state := &InitState{
		ID:          "test-id-1",
		AdminID:     "admin-1",
		TokenHash:   "abc123hash",
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		PendingInit: true,
	}

	err := db.InsertInitState(nil, state)
	if err != nil {
		t.Fatalf("InsertInitState failed: %v", err)
	}

	// Verify it was inserted
	var id, adminID, tokenHash string
	var pendingInit int
	err = db.QueryRow(`SELECT id, admin_id, token_hash, pending_init FROM init_state WHERE id = ?`, state.ID).
		Scan(&id, &adminID, &tokenHash, &pendingInit)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if id != "test-id-1" || adminID != "admin-1" || tokenHash != "abc123hash" || pendingInit != 1 {
		t.Errorf("unexpected values: id=%s adminID=%s tokenHash=%s pendingInit=%d", id, adminID, tokenHash, pendingInit)
	}
}

func TestInsertInitState_WithTx(t *testing.T) {
	db := setupInitStateTestDB(t)

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}

	state := &InitState{
		ID:          "test-id-tx",
		AdminID:     "admin-tx",
		TokenHash:   "txhash",
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		PendingInit: true,
	}

	err = db.InsertInitState(tx, state)
	if err != nil {
		tx.Rollback()
		t.Fatalf("InsertInitState with tx failed: %v", err)
	}

	// Before commit, shouldn't be visible outside tx
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM init_state WHERE id = ?`, state.ID).Scan(&count)
	if count != 0 {
		t.Error("expected row not visible before commit")
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// After commit, should be visible
	db.QueryRow(`SELECT COUNT(*) FROM init_state WHERE id = ?`, state.ID).Scan(&count)
	if count != 1 {
		t.Error("expected row visible after commit")
	}
}

func TestInsertInitState_UniqueConstraint(t *testing.T) {
	db := setupInitStateTestDB(t)

	state1 := &InitState{
		ID:          "test-id-1",
		AdminID:     "admin-1",
		TokenHash:   "hash1",
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		PendingInit: true,
	}
	if err := db.InsertInitState(nil, state1); err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// Try to insert another pending_init=1 record — should fail due to partial unique index
	state2 := &InitState{
		ID:          "test-id-2",
		AdminID:     "admin-2",
		TokenHash:   "hash2",
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		PendingInit: true,
	}
	err := db.InsertInitState(nil, state2)
	if err == nil {
		t.Fatal("expected unique constraint violation for second pending_init=1 insert")
	}
}

func TestInsertInitState_MultipleCompleted(t *testing.T) {
	db := setupInitStateTestDB(t)

	// Multiple completed (pending_init=0) records should be allowed
	now := time.Now()
	state1 := &InitState{
		ID:          "completed-1",
		AdminID:     "admin-1",
		TokenHash:   "",
		ExpiresAt:   now,
		PendingInit: false,
		CompletedAt: &now,
	}
	state2 := &InitState{
		ID:          "completed-2",
		AdminID:     "admin-2",
		TokenHash:   "",
		ExpiresAt:   now,
		PendingInit: false,
		CompletedAt: &now,
	}

	if err := db.InsertInitState(nil, state1); err != nil {
		t.Fatalf("first completed insert failed: %v", err)
	}
	if err := db.InsertInitState(nil, state2); err != nil {
		t.Fatalf("second completed insert failed: %v", err)
	}
}

func TestGetPendingInitState_NoPending(t *testing.T) {
	db := setupInitStateTestDB(t)

	state, err := db.GetPendingInitState(nil)
	if err != nil {
		t.Fatalf("GetPendingInitState failed: %v", err)
	}
	if state != nil {
		t.Error("expected nil when no pending state exists")
	}
}

func TestGetPendingInitState_ReturnsPending(t *testing.T) {
	db := setupInitStateTestDB(t)

	expiresAt := time.Now().Add(30 * time.Minute).Truncate(time.Second)
	state := &InitState{
		ID:          "pending-1",
		AdminID:     "admin-1",
		TokenHash:   "myhash",
		ExpiresAt:   expiresAt,
		PendingInit: true,
	}
	if err := db.InsertInitState(nil, state); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	result, err := db.GetPendingInitState(nil)
	if err != nil {
		t.Fatalf("GetPendingInitState failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ID != "pending-1" || result.AdminID != "admin-1" || result.TokenHash != "myhash" {
		t.Errorf("unexpected result: %+v", result)
	}
	if !result.PendingInit {
		t.Error("expected PendingInit=true")
	}
}

func TestGetPendingInitState_IgnoresCompleted(t *testing.T) {
	db := setupInitStateTestDB(t)

	now := time.Now()
	completed := &InitState{
		ID:          "completed-1",
		AdminID:     "admin-1",
		TokenHash:   "",
		ExpiresAt:   now,
		PendingInit: false,
		CompletedAt: &now,
	}
	if err := db.InsertInitState(nil, completed); err != nil {
		t.Fatalf("insert completed failed: %v", err)
	}

	result, err := db.GetPendingInitState(nil)
	if err != nil {
		t.Fatalf("GetPendingInitState failed: %v", err)
	}
	if result != nil {
		t.Error("expected nil when only completed records exist")
	}
}

func TestHasCompletedInitState_NoRecords(t *testing.T) {
	db := setupInitStateTestDB(t)

	has, err := db.HasCompletedInitState(nil)
	if err != nil {
		t.Fatalf("HasCompletedInitState failed: %v", err)
	}
	if has {
		t.Error("expected false when no records exist")
	}
}

func TestHasCompletedInitState_OnlyPending(t *testing.T) {
	db := setupInitStateTestDB(t)

	state := &InitState{
		ID:          "pending-1",
		AdminID:     "admin-1",
		TokenHash:   "hash",
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		PendingInit: true,
	}
	if err := db.InsertInitState(nil, state); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	has, err := db.HasCompletedInitState(nil)
	if err != nil {
		t.Fatalf("HasCompletedInitState failed: %v", err)
	}
	if has {
		t.Error("expected false when only pending records exist")
	}
}

func TestHasCompletedInitState_WithCompleted(t *testing.T) {
	db := setupInitStateTestDB(t)

	now := time.Now()
	state := &InitState{
		ID:          "completed-1",
		AdminID:     "admin-1",
		TokenHash:   "",
		ExpiresAt:   now,
		PendingInit: false,
		CompletedAt: &now,
	}
	if err := db.InsertInitState(nil, state); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	has, err := db.HasCompletedInitState(nil)
	if err != nil {
		t.Fatalf("HasCompletedInitState failed: %v", err)
	}
	if !has {
		t.Error("expected true when completed records exist")
	}
}

func TestUpdateInitStateToCompleted(t *testing.T) {
	db := setupInitStateTestDB(t)

	state := &InitState{
		ID:          "pending-1",
		AdminID:     "admin-1",
		TokenHash:   "myhash",
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		PendingInit: true,
	}
	if err := db.InsertInitState(nil, state); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	err := db.UpdateInitStateToCompleted(nil, "pending-1")
	if err != nil {
		t.Fatalf("UpdateInitStateToCompleted failed: %v", err)
	}

	// Verify the record is updated
	var pendingInit int
	var tokenHash string
	var completedAt sql.NullString
	err = db.QueryRow(`SELECT pending_init, token_hash, completed_at FROM init_state WHERE id = ?`, "pending-1").
		Scan(&pendingInit, &tokenHash, &completedAt)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if pendingInit != 0 {
		t.Errorf("expected pending_init=0, got %d", pendingInit)
	}
	if tokenHash != "" {
		t.Errorf("expected token_hash='', got %q", tokenHash)
	}
	if !completedAt.Valid {
		t.Error("expected completed_at to be set")
	}
}

func TestUpdateInitStateToCompleted_NotFound(t *testing.T) {
	db := setupInitStateTestDB(t)

	err := db.UpdateInitStateToCompleted(nil, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestDeleteInitState(t *testing.T) {
	db := setupInitStateTestDB(t)

	state := &InitState{
		ID:          "to-delete",
		AdminID:     "admin-1",
		TokenHash:   "hash",
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		PendingInit: true,
	}
	if err := db.InsertInitState(nil, state); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	err := db.DeleteInitState(nil, "to-delete")
	if err != nil {
		t.Fatalf("DeleteInitState failed: %v", err)
	}

	// Verify deletion
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM init_state WHERE id = ?`, "to-delete").Scan(&count)
	if count != 0 {
		t.Error("expected row to be deleted")
	}
}

func TestDeleteInitState_NotFound(t *testing.T) {
	db := setupInitStateTestDB(t)

	err := db.DeleteInitState(nil, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestGetPendingInitState_WithTx(t *testing.T) {
	db := setupInitStateTestDB(t)

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}
	defer tx.Rollback()

	state := &InitState{
		ID:          "tx-pending",
		AdminID:     "admin-tx",
		TokenHash:   "txhash",
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		PendingInit: true,
	}
	if err := db.InsertInitState(tx, state); err != nil {
		t.Fatalf("insert in tx failed: %v", err)
	}

	// Should be visible within the same tx
	result, err := db.GetPendingInitState(tx)
	if err != nil {
		t.Fatalf("GetPendingInitState in tx failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result within tx")
	}
	if result.ID != "tx-pending" {
		t.Errorf("expected ID=tx-pending, got %s", result.ID)
	}
}

func TestInsertInitState_WithCompletedAt(t *testing.T) {
	db := setupInitStateTestDB(t)

	completedAt := time.Now().Truncate(time.Second)
	state := &InitState{
		ID:          "with-completed",
		AdminID:     "admin-1",
		TokenHash:   "",
		ExpiresAt:   time.Now(),
		PendingInit: false,
		CompletedAt: &completedAt,
	}

	if err := db.InsertInitState(nil, state); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// Verify completed_at was stored
	var storedCompletedAt sql.NullString
	err := db.QueryRow(`SELECT completed_at FROM init_state WHERE id = ?`, state.ID).Scan(&storedCompletedAt)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !storedCompletedAt.Valid {
		t.Fatal("expected completed_at to be set")
	}
	parsed, _ := time.Parse(time.RFC3339, storedCompletedAt.String)
	if !parsed.Equal(completedAt) {
		t.Errorf("expected completed_at=%v, got %v", completedAt, parsed)
	}
}
