package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// MachineCertificateService handles machine certificate deployment config business logic.
type MachineCertificateService struct {
	mcRepo *repository.MachineCertificateRepository
}

// NewMachineCertificateService creates a new MachineCertificateService.
func NewMachineCertificateService(mcRepo *repository.MachineCertificateRepository) *MachineCertificateService {
	return &MachineCertificateService{
		mcRepo: mcRepo,
	}
}

// Create creates a new machine certificate deployment config.
// Validates that cert_path and private_key_path are non-empty.
// Sets initial config_revision to 1.
func (s *MachineCertificateService) Create(ctx context.Context, input model.CreateMachineCertInput) (*model.MachineCertificate, error) {
	// Validate paths are non-empty
	if err := validatePaths(input.CertPath, input.PrivateKeyPath); err != nil {
		return nil, err
	}

	mc := &model.MachineCertificate{
		MachineID:          input.MachineID,
		CertificateID:      input.CertificateID,
		CertPath:           input.CertPath,
		PrivateKeyPath:     input.PrivateKeyPath,
		PostDeployCommands: input.PostDeployCommands,
		ConfigRevision:     1,
		LastDeployStatus:   "pending",
	}

	if err := s.mcRepo.Create(ctx, mc); err != nil {
		return nil, fmt.Errorf("failed to create machine certificate: %w", err)
	}

	return mc, nil
}

// Update updates an existing machine certificate deployment config.
// Validates paths if provided, increments config_revision, and marks as pending sync.
func (s *MachineCertificateService) Update(ctx context.Context, id string, input model.UpdateMachineCertInput) (*model.MachineCertificate, error) {
	// Validate paths if provided
	if input.CertPath != nil && strings.TrimSpace(*input.CertPath) == "" {
		return nil, fmt.Errorf("cert_path cannot be empty")
	}
	if input.PrivateKeyPath != nil && strings.TrimSpace(*input.PrivateKeyPath) == "" {
		return nil, fmt.Errorf("private_key_path cannot be empty")
	}

	mc, err := s.mcRepo.Update(ctx, id, input)
	if err != nil {
		return nil, fmt.Errorf("failed to update machine certificate: %w", err)
	}

	return mc, nil
}

// Delete deletes a machine certificate deployment config.
func (s *MachineCertificateService) Delete(ctx context.Context, id string) error {
	if err := s.mcRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete machine certificate: %w", err)
	}
	return nil
}

// GetByMachineID retrieves all machine certificates for a given machine.
func (s *MachineCertificateService) GetByMachineID(ctx context.Context, machineID string) ([]*model.MachineCertificate, error) {
	return s.mcRepo.GetByMachineID(ctx, machineID)
}

// TriggerManualDeploy marks a machine certificate as pending sync and increments config_revision.
// This forces the Agent to deploy on next poll, even if the certificate fingerprint hasn't changed.
func (s *MachineCertificateService) TriggerManualDeploy(ctx context.Context, machineCertID string) error {
	if err := s.mcRepo.TriggerManualDeploy(ctx, machineCertID); err != nil {
		return fmt.Errorf("failed to trigger manual deploy: %w", err)
	}
	return nil
}

// MarkPendingSync marks all machine certificates for a given certificate as pending sync.
func (s *MachineCertificateService) MarkPendingSync(ctx context.Context, certificateID string) error {
	if err := s.mcRepo.MarkPendingSync(ctx, certificateID); err != nil {
		return fmt.Errorf("failed to mark pending sync: %w", err)
	}
	return nil
}

// validatePaths validates that cert_path and private_key_path are non-empty.
func validatePaths(certPath, privateKeyPath string) error {
	if strings.TrimSpace(certPath) == "" {
		return fmt.Errorf("cert_path cannot be empty")
	}
	if strings.TrimSpace(privateKeyPath) == "" {
		return fmt.Errorf("private_key_path cannot be empty")
	}
	return nil
}
