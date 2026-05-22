package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadState_FileNotFound(t *testing.T) {
	state, err := LoadState("/nonexistent/path/state.json")
	if err != nil {
		t.Fatalf("LoadState should return empty state for nonexistent file, got error: %v", err)
	}

	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if len(state.MachineCertStates) != 0 {
		t.Errorf("expected empty MachineCertStates, got %d entries", len(state.MachineCertStates))
	}
}

func TestLoadState_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	stateJSON := `{
  "machine_cert_states": {
    "mc-001": {
      "machine_certificate_id": "mc-001",
      "last_synced_revision": 3,
      "last_synced_fingerprint": "sha256:abc123",
      "last_deploy_status": "success",
      "last_deploy_at": "2024-01-15T10:30:00Z"
    },
    "mc-002": {
      "machine_certificate_id": "mc-002",
      "last_synced_revision": 1,
      "last_synced_fingerprint": "sha256:def456",
      "last_deploy_status": "failed",
      "last_deploy_at": "2024-01-14T08:00:00Z"
    }
  }
}`
	if err := os.WriteFile(path, []byte(stateJSON), 0600); err != nil {
		t.Fatal(err)
	}

	state, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if len(state.MachineCertStates) != 2 {
		t.Fatalf("expected 2 cert states, got %d", len(state.MachineCertStates))
	}

	mc1 := state.MachineCertStates["mc-001"]
	if mc1 == nil {
		t.Fatal("expected mc-001 state to exist")
	}
	if mc1.MachineCertificateID != "mc-001" {
		t.Errorf("MachineCertificateID = %q, want %q", mc1.MachineCertificateID, "mc-001")
	}
	if mc1.LastSyncedRevision != 3 {
		t.Errorf("LastSyncedRevision = %d, want %d", mc1.LastSyncedRevision, 3)
	}
	if mc1.LastSyncedFingerprint != "sha256:abc123" {
		t.Errorf("LastSyncedFingerprint = %q, want %q", mc1.LastSyncedFingerprint, "sha256:abc123")
	}
	if mc1.LastDeployStatus != "success" {
		t.Errorf("LastDeployStatus = %q, want %q", mc1.LastDeployStatus, "success")
	}
	if mc1.LastDeployAt != "2024-01-15T10:30:00Z" {
		t.Errorf("LastDeployAt = %q, want %q", mc1.LastDeployAt, "2024-01-15T10:30:00Z")
	}

	mc2 := state.MachineCertStates["mc-002"]
	if mc2 == nil {
		t.Fatal("expected mc-002 state to exist")
	}
	if mc2.LastDeployStatus != "failed" {
		t.Errorf("mc-002 LastDeployStatus = %q, want %q", mc2.LastDeployStatus, "failed")
	}
}

func TestLoadState_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := os.WriteFile(path, []byte("{invalid json"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadState(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoadState_NullMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// JSON with null machine_cert_states
	if err := os.WriteFile(path, []byte(`{"machine_cert_states": null}`), 0600); err != nil {
		t.Fatal(err)
	}

	state, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if state.MachineCertStates == nil {
		t.Fatal("expected non-nil MachineCertStates map")
	}
}

func TestSaveState_CreatesDirectoryAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "state.json")

	state := &AgentLocalState{
		MachineCertStates: map[string]*MachineCertState{
			"mc-001": {
				MachineCertificateID:  "mc-001",
				LastSyncedRevision:    5,
				LastSyncedFingerprint: "sha256:xyz789",
				LastDeployStatus:      "success",
				LastDeployAt:          "2024-02-01T12:00:00Z",
			},
		},
	}

	if err := SaveState(path, state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("state file was not created")
	}

	// Verify content is valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var parsed AgentLocalState
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("saved state is not valid JSON: %v", err)
	}
}

func TestSaveState_NilState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := SaveState(path, nil); err == nil {
		t.Fatal("expected error for nil state, got nil")
	}
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	original := &AgentLocalState{
		MachineCertStates: map[string]*MachineCertState{
			"mc-001": {
				MachineCertificateID:  "mc-001",
				LastSyncedRevision:    10,
				LastSyncedFingerprint: "sha256:fingerprint1",
				LastDeployStatus:      "success",
				LastDeployAt:          "2024-03-01T09:00:00Z",
			},
			"mc-002": {
				MachineCertificateID:  "mc-002",
				LastSyncedRevision:    2,
				LastSyncedFingerprint: "sha256:fingerprint2",
				LastDeployStatus:      "pending",
				LastDeployAt:          "",
			},
		},
	}

	if err := SaveState(path, original); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if len(loaded.MachineCertStates) != len(original.MachineCertStates) {
		t.Fatalf("state count mismatch: got %d, want %d",
			len(loaded.MachineCertStates), len(original.MachineCertStates))
	}

	for id, origState := range original.MachineCertStates {
		loadedState := loaded.MachineCertStates[id]
		if loadedState == nil {
			t.Fatalf("missing state for %s after round-trip", id)
		}
		if *loadedState != *origState {
			t.Errorf("state mismatch for %s:\n  got:  %+v\n  want: %+v", id, loadedState, origState)
		}
	}
}

func TestStateRoundTrip_EmptyState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	original := NewAgentLocalState()

	if err := SaveState(path, original); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if len(loaded.MachineCertStates) != 0 {
		t.Errorf("expected empty state, got %d entries", len(loaded.MachineCertStates))
	}
}

func TestGetCertState(t *testing.T) {
	state := NewAgentLocalState()

	// Non-existent key returns nil
	if got := state.GetCertState("nonexistent"); got != nil {
		t.Errorf("expected nil for nonexistent key, got %+v", got)
	}

	// Set and get
	certState := &MachineCertState{
		MachineCertificateID:  "mc-001",
		LastSyncedRevision:    1,
		LastSyncedFingerprint: "sha256:abc",
		LastDeployStatus:      "success",
		LastDeployAt:          "2024-01-01T00:00:00Z",
	}
	state.SetCertState("mc-001", certState)

	got := state.GetCertState("mc-001")
	if got == nil {
		t.Fatal("expected non-nil state after SetCertState")
	}
	if got.MachineCertificateID != "mc-001" {
		t.Errorf("MachineCertificateID = %q, want %q", got.MachineCertificateID, "mc-001")
	}
}

func TestSetCertState_NilMap(t *testing.T) {
	// Test that SetCertState initializes the map if nil
	state := &AgentLocalState{MachineCertStates: nil}

	certState := &MachineCertState{
		MachineCertificateID: "mc-001",
		LastSyncedRevision:   1,
	}
	state.SetCertState("mc-001", certState)

	if state.MachineCertStates == nil {
		t.Fatal("expected map to be initialized")
	}
	if state.MachineCertStates["mc-001"] != certState {
		t.Error("state was not set correctly")
	}
}
