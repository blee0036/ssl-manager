package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AgentLocalState represents the Agent's local state persisted to disk.
// This state survives Agent restarts and tracks deployment progress.
type AgentLocalState struct {
	MachineCertStates map[string]*MachineCertState `json:"machine_cert_states"`
}

// MachineCertState tracks the deployment state of a single machine certificate.
type MachineCertState struct {
	MachineCertificateID  string `json:"machine_certificate_id"`
	LastSyncedRevision    int    `json:"last_synced_revision"`
	LastSyncedFingerprint string `json:"last_synced_fingerprint"`
	LastDeployStatus      string `json:"last_deploy_status"`
	LastDeployAt          string `json:"last_deploy_at"`
}

// NewAgentLocalState creates a new empty AgentLocalState.
func NewAgentLocalState() *AgentLocalState {
	return &AgentLocalState{
		MachineCertStates: make(map[string]*MachineCertState),
	}
}

// LoadState reads and parses the local state JSON file from the given path.
// If the file does not exist, it returns a new empty state (not an error).
func LoadState(path string) (*AgentLocalState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewAgentLocalState(), nil
		}
		return nil, fmt.Errorf("failed to read agent state file: %w", err)
	}

	state := NewAgentLocalState()
	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("failed to parse agent state file: %w", err)
	}

	// Ensure the map is initialized even if JSON had null
	if state.MachineCertStates == nil {
		state.MachineCertStates = make(map[string]*MachineCertState)
	}

	return state, nil
}

// SaveState writes the AgentLocalState to the given path in JSON format.
// It creates parent directories if they don't exist and sets file permissions to 0600.
func SaveState(path string, state *AgentLocalState) error {
	if state == nil {
		return fmt.Errorf("state is nil")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create agent state directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal agent state: %w", err)
	}

	// Append newline for POSIX compliance
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write agent state file: %w", err)
	}

	return nil
}

// GetCertState returns the state for a given machine certificate ID.
// Returns nil if no state exists for that ID.
func (s *AgentLocalState) GetCertState(machineCertID string) *MachineCertState {
	if s.MachineCertStates == nil {
		return nil
	}
	return s.MachineCertStates[machineCertID]
}

// SetCertState sets or updates the state for a given machine certificate ID.
func (s *AgentLocalState) SetCertState(machineCertID string, state *MachineCertState) {
	if s.MachineCertStates == nil {
		s.MachineCertStates = make(map[string]*MachineCertState)
	}
	s.MachineCertStates[machineCertID] = state
}
