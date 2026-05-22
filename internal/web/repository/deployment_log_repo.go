package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

// DeploymentLogRepository handles deployment log CRUD operations.
type DeploymentLogRepository struct {
	db *sql.DB
}

// NewDeploymentLogRepository creates a new DeploymentLogRepository.
func NewDeploymentLogRepository(db *sql.DB) *DeploymentLogRepository {
	return &DeploymentLogRepository{db: db}
}

// Create saves a new deployment log record.
func (r *DeploymentLogRepository) Create(ctx context.Context, log *model.DeploymentLog) error {
	if log.ID == "" {
		log.ID = uuid.New().String()
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}

	commandOutputsJSON, err := json.Marshal(log.CommandOutputs)
	if err != nil {
		return fmt.Errorf("failed to marshal command_outputs: %w", err)
	}

	_, err = r.db.ExecContext(ctx,
		`INSERT INTO deployment_logs (id, machine_certificate_id, machine_id, certificate_id, status, cert_fingerprint_sha256, cert_path, private_key_path, command_outputs, error_message, started_at, finished_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.ID,
		log.MachineCertificateID,
		log.MachineID,
		log.CertificateID,
		log.Status,
		log.CertFingerprintSHA256,
		log.CertPath,
		log.PrivateKeyPath,
		string(commandOutputsJSON),
		log.ErrorMessage,
		log.StartedAt.UTC().Format(time.RFC3339),
		log.FinishedAt.UTC().Format(time.RFC3339),
		log.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to create deployment log: %w", err)
	}
	return nil
}

// GetByMachineCertificateID returns deployment logs for a machine certificate, ordered by created_at DESC.
func (r *DeploymentLogRepository) GetByMachineCertificateID(ctx context.Context, machineCertID string, limit int) ([]*model.DeploymentLog, error) {
	query := `SELECT id, machine_certificate_id, machine_id, certificate_id, status, cert_fingerprint_sha256, cert_path, private_key_path, command_outputs, error_message, started_at, finished_at, created_at
		FROM deployment_logs WHERE machine_certificate_id = ? ORDER BY created_at DESC`
	var args []interface{}
	args = append(args, machineCertID)

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query deployment logs: %w", err)
	}
	defer rows.Close()

	return r.scanLogs(rows)
}

// GetByMachineID returns deployment logs for a machine, ordered by created_at DESC.
func (r *DeploymentLogRepository) GetByMachineID(ctx context.Context, machineID string, limit int) ([]*model.DeploymentLog, error) {
	query := `SELECT id, machine_certificate_id, machine_id, certificate_id, status, cert_fingerprint_sha256, cert_path, private_key_path, command_outputs, error_message, started_at, finished_at, created_at
		FROM deployment_logs WHERE machine_id = ? ORDER BY created_at DESC`
	var args []interface{}
	args = append(args, machineID)

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query deployment logs by machine: %w", err)
	}
	defer rows.Close()

	return r.scanLogs(rows)
}

// EnforceRetentionLimit deletes the oldest logs beyond the maxLogs limit for a given machine certificate.
func (r *DeploymentLogRepository) EnforceRetentionLimit(ctx context.Context, machineCertID string, maxLogs int) error {
	// Delete logs that are beyond the retention limit, keeping only the newest maxLogs records
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM deployment_logs WHERE id IN (
			SELECT id FROM deployment_logs
			WHERE machine_certificate_id = ?
			ORDER BY created_at DESC
			LIMIT -1 OFFSET ?
		)`,
		machineCertID, maxLogs)
	if err != nil {
		return fmt.Errorf("failed to enforce retention limit: %w", err)
	}
	return nil
}

// --- Helper functions ---

func (r *DeploymentLogRepository) scanLogs(rows *sql.Rows) ([]*model.DeploymentLog, error) {
	var logs []*model.DeploymentLog
	for rows.Next() {
		log, err := r.scanLogFromRows(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate deployment logs: %w", err)
	}
	return logs, nil
}

func (r *DeploymentLogRepository) scanLogFromRows(rows *sql.Rows) (*model.DeploymentLog, error) {
	var log model.DeploymentLog
	var commandOutputsJSON string
	var startedAt, finishedAt, createdAt string

	err := rows.Scan(
		&log.ID,
		&log.MachineCertificateID,
		&log.MachineID,
		&log.CertificateID,
		&log.Status,
		&log.CertFingerprintSHA256,
		&log.CertPath,
		&log.PrivateKeyPath,
		&commandOutputsJSON,
		&log.ErrorMessage,
		&startedAt,
		&finishedAt,
		&createdAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan deployment log: %w", err)
	}

	// Parse command outputs JSON
	if commandOutputsJSON != "" {
		if err := json.Unmarshal([]byte(commandOutputsJSON), &log.CommandOutputs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal command_outputs: %w", err)
		}
	}

	// Parse time fields
	if t, err := time.Parse(time.RFC3339, startedAt); err == nil {
		log.StartedAt = t
	}
	if t, err := time.Parse(time.RFC3339, finishedAt); err == nil {
		log.FinishedAt = t
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		log.CreatedAt = t
	}

	return &log, nil
}
