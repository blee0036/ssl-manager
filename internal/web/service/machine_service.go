package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// MachineService handles machine business logic.
type MachineService struct {
	machineRepo *repository.MachineRepository
	runtimeCfg  *config.RuntimeConfig
}

// NewMachineService creates a new MachineService.
func NewMachineService(machineRepo *repository.MachineRepository, runtimeCfg *config.RuntimeConfig) *MachineService {
	return &MachineService{
		machineRepo: machineRepo,
		runtimeCfg:  runtimeCfg,
	}
}

// Create creates a new machine and generates a unique Agent Token.
// Returns the machine and the plaintext token (only shown once).
func (s *MachineService) Create(ctx context.Context, input model.CreateMachineInput) (*model.Machine, string, error) {
	if input.Name == "" {
		return nil, "", errors.New("machine name is required")
	}
	if input.IP == "" {
		return nil, "", errors.New("machine IP is required")
	}

	// Generate a unique agent token
	token, tokenHash, err := generateToken()
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate agent token: %w", err)
	}

	machine := &model.Machine{
		Name:           input.Name,
		IP:             input.IP,
		Tags:           input.Tags,
		Remark:         input.Remark,
		AgentTokenHash: tokenHash,
	}

	if machine.Tags == nil {
		machine.Tags = []string{}
	}

	if err := s.machineRepo.Create(ctx, machine); err != nil {
		return nil, "", fmt.Errorf("failed to create machine: %w", err)
	}

	return machine, token, nil
}

// GetByID retrieves a machine by ID.
func (s *MachineService) GetByID(ctx context.Context, id string) (*model.Machine, error) {
	machine, err := s.machineRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("machine not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get machine: %w", err)
	}
	return machine, nil
}

// List returns machines with optional filtering.
func (s *MachineService) List(ctx context.Context, filter model.MachineFilter) ([]*model.Machine, error) {
	machines, err := s.machineRepo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list machines: %w", err)
	}
	return machines, nil
}

// Update updates machine fields.
func (s *MachineService) Update(ctx context.Context, id string, input model.UpdateMachineInput) (*model.Machine, error) {
	// Verify machine exists
	_, err := s.machineRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("machine not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get machine: %w", err)
	}

	updates := make(map[string]interface{})
	if input.Name != nil {
		updates["name"] = *input.Name
	}
	if input.IP != nil {
		updates["ip"] = *input.IP
	}
	if input.Tags != nil {
		updates["tags"] = input.Tags
	}
	if input.Remark != nil {
		updates["remark"] = *input.Remark
	}

	if len(updates) > 0 {
		if err := s.machineRepo.Update(ctx, id, updates); err != nil {
			return nil, fmt.Errorf("failed to update machine: %w", err)
		}
	}

	// Return updated machine
	machine, err := s.machineRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get updated machine: %w", err)
	}
	return machine, nil
}

// Delete deletes a machine.
func (s *MachineService) Delete(ctx context.Context, id string) error {
	err := s.machineRepo.Delete(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("machine not found: %s", id)
		}
		return fmt.Errorf("failed to delete machine: %w", err)
	}
	return nil
}

// GenerateToken generates a new token for a machine (revokes old one).
// Returns the plaintext token (only shown once).
func (s *MachineService) GenerateToken(ctx context.Context, machineID string) (string, error) {
	// Verify machine exists
	_, err := s.machineRepo.GetByID(ctx, machineID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("machine not found: %s", machineID)
		}
		return "", fmt.Errorf("failed to get machine: %w", err)
	}

	// Generate new token
	token, tokenHash, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("failed to generate agent token: %w", err)
	}

	// Update token hash (this also clears revoked_at)
	if err := s.machineRepo.UpdateTokenHash(ctx, machineID, tokenHash); err != nil {
		return "", fmt.Errorf("failed to update token hash: %w", err)
	}

	return token, nil
}

// RevokeToken revokes the current token for a machine.
func (s *MachineService) RevokeToken(ctx context.Context, machineID string) error {
	// Verify machine exists
	_, err := s.machineRepo.GetByID(ctx, machineID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("machine not found: %s", machineID)
		}
		return fmt.Errorf("failed to get machine: %w", err)
	}

	if err := s.machineRepo.RevokeToken(ctx, machineID); err != nil {
		return fmt.Errorf("failed to revoke token: %w", err)
	}
	return nil
}

// GetInstallCommand generates the installation command for a machine.
func (s *MachineService) GetInstallCommand(ctx context.Context, machineID string, token string) (string, error) {
	// Verify machine exists
	_, err := s.machineRepo.GetByID(ctx, machineID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("machine not found: %s", machineID)
		}
		return "", fmt.Errorf("failed to get machine: %w", err)
	}

	externalURL := s.runtimeCfg.Get().Server.ExternalURL

	cmd := fmt.Sprintf(
		"curl -sSL %s/api/agent/install.sh | bash -s -- --server-url %s --machine-id %s --agent-token %s",
		externalURL, externalURL, machineID, token,
	)

	return cmd, nil
}

// UpdateHeartbeat updates machine heartbeat info.
func (s *MachineService) UpdateHeartbeat(ctx context.Context, machineID string, info model.HeartbeatInfo) error {
	err := s.machineRepo.UpdateHeartbeat(ctx, machineID, info)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("machine not found: %s", machineID)
		}
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}
	return nil
}

// generateToken generates a 32-byte random token and returns the hex-encoded
// plaintext token and its SHA256 hash.
func generateToken() (plaintext string, hash string, err error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	plaintext = hex.EncodeToString(tokenBytes)
	hash = HashToken(plaintext)
	return plaintext, hash, nil
}

// HashToken computes the SHA256 hash of a token string and returns it as hex.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
