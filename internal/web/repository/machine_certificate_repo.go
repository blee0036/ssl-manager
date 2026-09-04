package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

// MachineCertificateRepository handles machine certificate deployment config CRUD operations.
type MachineCertificateRepository struct {
	db *sql.DB
}

// NewMachineCertificateRepository creates a new MachineCertificateRepository.
func NewMachineCertificateRepository(db *sql.DB) *MachineCertificateRepository {
	return &MachineCertificateRepository{db: db}
}

// Create creates a new machine certificate deployment config record.
func (r *MachineCertificateRepository) Create(ctx context.Context, mc *model.MachineCertificate) error {
	if mc.ID == "" {
		mc.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	mc.CreatedAt = now
	mc.UpdatedAt = now
	if mc.ConfigRevision == 0 {
		mc.ConfigRevision = 1
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO machine_certificates (id, machine_id, certificate_id, cert_path, private_key_path, post_deploy_commands, config_revision, last_deploy_status, last_deploy_at, last_deploy_message, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mc.ID,
		mc.MachineID,
		mc.CertificateID,
		mc.CertPath,
		mc.PrivateKeyPath,
		mc.PostDeployCommands,
		mc.ConfigRevision,
		mc.LastDeployStatus,
		formatNullableTime(mc.LastDeployAt),
		mc.LastDeployMessage,
		mc.CreatedAt.UTC().Format(time.RFC3339),
		mc.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to create machine certificate: %w", err)
	}
	return nil
}

// GetByID retrieves a machine certificate by ID.
func (r *MachineCertificateRepository) GetByID(ctx context.Context, id string) (*model.MachineCertificate, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, machine_id, certificate_id, cert_path, private_key_path, post_deploy_commands, config_revision, last_deploy_status, last_deploy_at, last_deploy_message, created_at, updated_at
		 FROM machine_certificates WHERE id = ?`, id)
	return scanMachineCertificate(row)
}

// GetByMachineID retrieves all machine certificates for a given machine.
func (r *MachineCertificateRepository) GetByMachineID(ctx context.Context, machineID string) ([]*model.MachineCertificate, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, machine_id, certificate_id, cert_path, private_key_path, post_deploy_commands, config_revision, last_deploy_status, last_deploy_at, last_deploy_message, created_at, updated_at
		 FROM machine_certificates WHERE machine_id = ? ORDER BY created_at DESC`, machineID)
	if err != nil {
		return nil, fmt.Errorf("failed to query machine certificates: %w", err)
	}
	defer rows.Close()

	var results []*model.MachineCertificate
	for rows.Next() {
		mc, err := scanMachineCertificateFromRows(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, mc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate machine certificates: %w", err)
	}
	return results, nil
}

// Update updates a machine certificate deployment config.
func (r *MachineCertificateRepository) Update(ctx context.Context, id string, input model.UpdateMachineCertInput) (*model.MachineCertificate, error) {
	// First get the current record to build the update
	current, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply updates
	if input.CertPath != nil {
		current.CertPath = *input.CertPath
	}
	if input.PrivateKeyPath != nil {
		current.PrivateKeyPath = *input.PrivateKeyPath
	}
	if input.PostDeployCommands != nil {
		current.PostDeployCommands = *input.PostDeployCommands
	}

	// Increment config_revision and mark as pending
	current.ConfigRevision++
	current.LastDeployStatus = "pending"
	current.UpdatedAt = time.Now().UTC()

	_, err = r.db.ExecContext(ctx,
		`UPDATE machine_certificates SET cert_path = ?, private_key_path = ?, post_deploy_commands = ?, config_revision = ?, last_deploy_status = ?, updated_at = ? WHERE id = ?`,
		current.CertPath,
		current.PrivateKeyPath,
		current.PostDeployCommands,
		current.ConfigRevision,
		current.LastDeployStatus,
		current.UpdatedAt.UTC().Format(time.RFC3339),
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update machine certificate: %w", err)
	}

	return current, nil
}

// Delete deletes a machine certificate deployment config and its deployment logs.
func (r *MachineCertificateRepository) Delete(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start machine certificate deletion transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM deployment_logs WHERE machine_certificate_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete machine certificate deployment logs: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		"UPDATE domains SET linked_machine_certificate_id = '' WHERE linked_machine_certificate_id = ?",
		id,
	); err != nil {
		return fmt.Errorf("failed to clear machine certificate domain links: %w", err)
	}

	result, err := tx.ExecContext(ctx, "DELETE FROM machine_certificates WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete machine certificate: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit machine certificate deletion: %w", err)
	}
	return nil
}

// MarkPendingSync marks all machine certificates for a given certificate as pending sync
// and increments their config_revision.
func (r *MachineCertificateRepository) MarkPendingSync(ctx context.Context, certificateID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.ExecContext(ctx,
		`UPDATE machine_certificates SET last_deploy_status = 'pending', config_revision = config_revision + 1, updated_at = ? WHERE certificate_id = ?`,
		now, certificateID)
	if err != nil {
		return fmt.Errorf("failed to mark machine certificates as pending sync: %w", err)
	}
	return nil
}

// IncrementRevision increments the config_revision for a specific machine certificate.
func (r *MachineCertificateRepository) IncrementRevision(ctx context.Context, id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.db.ExecContext(ctx,
		`UPDATE machine_certificates SET config_revision = config_revision + 1, updated_at = ? WHERE id = ?`,
		now, id)
	if err != nil {
		return fmt.Errorf("failed to increment config revision: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// TriggerManualDeploy marks a machine certificate as pending and increments config_revision.
func (r *MachineCertificateRepository) TriggerManualDeploy(ctx context.Context, id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.db.ExecContext(ctx,
		`UPDATE machine_certificates SET last_deploy_status = 'pending', config_revision = config_revision + 1, updated_at = ? WHERE id = ?`,
		now, id)
	if err != nil {
		return fmt.Errorf("failed to trigger manual deploy: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdateDeployStatus updates the deployment status, timestamp, and message for a machine certificate.
func (r *MachineCertificateRepository) UpdateDeployStatus(ctx context.Context, id string, status string, message string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.db.ExecContext(ctx,
		`UPDATE machine_certificates SET last_deploy_status = ?, last_deploy_at = ?, last_deploy_message = ?, updated_at = ? WHERE id = ?`,
		status, now, message, now, id)
	if err != nil {
		return fmt.Errorf("failed to update deploy status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- Helper functions ---

func scanMachineCertificateFields(scanner scannable) (*model.MachineCertificate, error) {
	var mc model.MachineCertificate
	var lastDeployAt, createdAt, updatedAt sql.NullString

	err := scanner.Scan(
		&mc.ID,
		&mc.MachineID,
		&mc.CertificateID,
		&mc.CertPath,
		&mc.PrivateKeyPath,
		&mc.PostDeployCommands,
		&mc.ConfigRevision,
		&mc.LastDeployStatus,
		&lastDeployAt,
		&mc.LastDeployMessage,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to scan machine certificate: %w", err)
	}

	mc.LastDeployAt = parseNullableTime(lastDeployAt)

	if createdAt.Valid {
		t, _ := time.Parse(time.RFC3339, createdAt.String)
		mc.CreatedAt = t
	}
	if updatedAt.Valid {
		t, _ := time.Parse(time.RFC3339, updatedAt.String)
		mc.UpdatedAt = t
	}

	return &mc, nil
}

func scanMachineCertificate(row *sql.Row) (*model.MachineCertificate, error) {
	return scanMachineCertificateFields(row)
}

func scanMachineCertificateFromRows(rows *sql.Rows) (*model.MachineCertificate, error) {
	return scanMachineCertificateFields(rows)
}

// CountByCertificateIDs returns a map of certificate_id -> count of machine_certificates for each certificate.
func (r *MachineCertificateRepository) CountByCertificateIDs(ctx context.Context, certIDs []string) (map[string]int, error) {
	result := make(map[string]int)
	if len(certIDs) == 0 {
		return result, nil
	}

	// Build query with placeholders
	placeholders := ""
	args := make([]interface{}, len(certIDs))
	for i, id := range certIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = id
	}

	query := fmt.Sprintf("SELECT certificate_id, COUNT(*) FROM machine_certificates WHERE certificate_id IN (%s) GROUP BY certificate_id", placeholders)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to count machine certificates: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var certID string
		var count int
		if err := rows.Scan(&certID, &count); err != nil {
			return nil, fmt.Errorf("failed to scan count: %w", err)
		}
		result[certID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate counts: %w", err)
	}

	return result, nil
}
