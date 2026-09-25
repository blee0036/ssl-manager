package repository

import (
	"context"
	"testing"
	"time"
)

// insertOfflineCandidate inserts a machine with an explicit status and last heartbeat.
func insertOfflineCandidate(t *testing.T, repo *MachineRepository, id, status string, silentFor time.Duration) {
	t.Helper()

	now := time.Now().UTC()
	_, err := repo.db.Exec(
		`INSERT INTO machines (id, name, ip, status, agent_token_hash, last_heartbeat_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, id, "10.0.0.1", status, "hash-"+id,
		now.Add(-silentFor).Format(time.RFC3339),
		now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert machine %s: %v", id, err)
	}
}

func TestListOfflineAlertCandidates(t *testing.T) {
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	// Silent long enough to be past the cutoff.
	insertOfflineCandidate(t, repo, "m-online-stale", "online", 20*time.Minute)
	insertOfflineCandidate(t, repo, "m-offline-stale", "offline", 20*time.Minute)

	// Silence is intentional for these, so they must never be alert candidates.
	insertOfflineCandidate(t, repo, "m-pending", "pending", 20*time.Minute)
	insertOfflineCandidate(t, repo, "m-revoked", "revoked", 20*time.Minute)
	insertOfflineCandidate(t, repo, "m-disabled", "disabled", 20*time.Minute)

	// Reporting normally, so inside the cutoff.
	insertOfflineCandidate(t, repo, "m-fresh", "online", 5*time.Second)

	cutoff := time.Now().UTC().Add(-10 * time.Minute)
	machines, err := repo.ListOfflineAlertCandidates(ctx, cutoff)
	if err != nil {
		t.Fatalf("ListOfflineAlertCandidates failed: %v", err)
	}

	got := make(map[string]bool, len(machines))
	for _, m := range machines {
		got[m.ID] = true
	}

	// An already-offline machine must still be returned: the alert threshold is crossed
	// after the machine has been flipped to offline, so an online-only query would mean a
	// delayed alert never fires.
	for _, id := range []string{"m-online-stale", "m-offline-stale"} {
		if !got[id] {
			t.Errorf("expected %s to be an alert candidate", id)
		}
	}
	for _, id := range []string{"m-pending", "m-revoked", "m-disabled", "m-fresh"} {
		if got[id] {
			t.Errorf("did not expect %s to be an alert candidate", id)
		}
	}
}

func TestListOfflineAlertCandidates_SkipsMachinesWithoutHeartbeat(t *testing.T) {
	// A machine that has never reported has a NULL last_heartbeat_at and cannot be said to
	// have "gone offline", so it must be excluded regardless of status.
	db := setupMachineTestDB(t)
	repo := NewMachineRepository(db)
	ctx := context.Background()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO machines (id, name, ip, status, agent_token_hash, last_heartbeat_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, NULL, ?, ?)`,
		"m-never", "m-never", "10.0.0.9", "online", "hash-never", now, now,
	)
	if err != nil {
		t.Fatalf("failed to insert machine: %v", err)
	}

	machines, err := repo.ListOfflineAlertCandidates(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ListOfflineAlertCandidates failed: %v", err)
	}

	for _, m := range machines {
		if m.ID == "m-never" {
			t.Error("a machine with no heartbeat must not be an alert candidate")
		}
	}
}
