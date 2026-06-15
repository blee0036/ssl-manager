package service

import (
	"context"
	"fmt"

	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

const (
	// DefaultRetentionLimit is the maximum number of deployment logs to keep per machine certificate.
	DefaultRetentionLimit = 30
	// DefaultQueryLimit is the default limit for querying deployment logs.
	DefaultQueryLimit = 30
)

// DeploymentLogService handles deployment log business logic.
type DeploymentLogService struct {
	repo      *repository.DeploymentLogRepository
	sanitizer *Sanitizer
}

// NewDeploymentLogService creates a new DeploymentLogService.
func NewDeploymentLogService(repo *repository.DeploymentLogRepository, sanitizer *Sanitizer) *DeploymentLogService {
	return &DeploymentLogService{repo: repo, sanitizer: sanitizer}
}

// Create saves a deployment log and enforces the 30-record retention limit per machine certificate.
// It sanitizes and truncates all fields before persistence to prevent sensitive data leaks.
func (s *DeploymentLogService) Create(ctx context.Context, log *model.DeploymentLog) error {
	// Sanitize → truncate → re-sanitize (handled by SanitizeDeploymentLog)
	s.sanitizer.SanitizeDeploymentLog(log)

	if err := s.repo.Create(ctx, log); err != nil {
		return fmt.Errorf("failed to create deployment log: %w", err)
	}

	// Enforce retention limit: keep only the most recent 30 logs per machine certificate
	if err := s.repo.EnforceRetentionLimit(ctx, log.MachineCertificateID, DefaultRetentionLimit); err != nil {
		return fmt.Errorf("failed to enforce retention limit: %w", err)
	}

	return nil
}

// GetByMachineCertificateID returns deployment logs for a machine certificate in time DESC order.
func (s *DeploymentLogService) GetByMachineCertificateID(ctx context.Context, machineCertID string) ([]*model.DeploymentLog, error) {
	return s.repo.GetByMachineCertificateID(ctx, machineCertID, DefaultQueryLimit)
}

// GetByMachineID returns deployment logs for a machine in time DESC order.
func (s *DeploymentLogService) GetByMachineID(ctx context.Context, machineID string) ([]*model.DeploymentLog, error) {
	return s.repo.GetByMachineID(ctx, machineID, DefaultQueryLimit)
}
