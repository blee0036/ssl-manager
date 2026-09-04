package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

// MachineRepository handles machine CRUD operations.
type MachineRepository struct {
	db *sql.DB
}

// NewMachineRepository creates a new MachineRepository.
func NewMachineRepository(db *sql.DB) *MachineRepository {
	return &MachineRepository{db: db}
}

// Create creates a new machine record.
func (r *MachineRepository) Create(ctx context.Context, machine *model.Machine) error {
	if machine.ID == "" {
		machine.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	machine.CreatedAt = now
	machine.UpdatedAt = now
	if machine.Status == "" {
		machine.Status = "pending"
	}

	tagsJSON, err := json.Marshal(machine.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	_, err = r.db.ExecContext(ctx,
		`INSERT INTO machines (id, name, ip, hostname, os, arch, tags, remark, status, agent_version, agent_token_hash, agent_token_revoked_at, last_heartbeat_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		machine.ID,
		machine.Name,
		machine.IP,
		machine.Hostname,
		machine.OS,
		machine.Arch,
		string(tagsJSON),
		machine.Remark,
		machine.Status,
		machine.AgentVersion,
		machine.AgentTokenHash,
		formatNullableTime(machine.AgentTokenRevokedAt),
		formatNullableTime(machine.LastHeartbeatAt),
		machine.CreatedAt.UTC().Format(time.RFC3339),
		machine.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to create machine: %w", err)
	}
	return nil
}

// GetByID retrieves a machine by ID.
func (r *MachineRepository) GetByID(ctx context.Context, id string) (*model.Machine, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, ip, hostname, os, arch, tags, remark, status, agent_version, agent_token_hash, agent_token_revoked_at, last_heartbeat_at, created_at, updated_at
		 FROM machines WHERE id = ?`, id)
	return scanMachine(row)
}

// List returns machines with optional filtering.
func (r *MachineRepository) List(ctx context.Context, filter model.MachineFilter) ([]*model.Machine, error) {
	query := `SELECT id, name, ip, hostname, os, arch, tags, remark, status, agent_version, agent_token_hash, agent_token_revoked_at, last_heartbeat_at, created_at, updated_at FROM machines`
	var conditions []string
	var args []interface{}

	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.Search != "" {
		conditions = append(conditions, "(name LIKE ? OR ip LIKE ? OR hostname LIKE ?)")
		search := "%" + filter.Search + "%"
		args = append(args, search, search, search)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list machines: %w", err)
	}
	defer rows.Close()

	var machines []*model.Machine
	for rows.Next() {
		m, err := scanMachineFromRows(rows)
		if err != nil {
			return nil, err
		}
		machines = append(machines, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate machines: %w", err)
	}
	return machines, nil
}

// Update updates machine fields.
func (r *MachineRepository) Update(ctx context.Context, id string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	var setClauses []string
	var args []interface{}

	for key, value := range updates {
		switch key {
		case "name", "ip", "hostname", "os", "arch", "remark", "status", "agent_version":
			setClauses = append(setClauses, key+" = ?")
			args = append(args, value)
		case "tags":
			tagsJSON, err := json.Marshal(value)
			if err != nil {
				return fmt.Errorf("failed to marshal tags: %w", err)
			}
			setClauses = append(setClauses, "tags = ?")
			args = append(args, string(tagsJSON))
		default:
			return fmt.Errorf("unsupported update field: %s", key)
		}
	}

	setClauses = append(setClauses, "updated_at = ?")
	args = append(args, time.Now().UTC().Format(time.RFC3339))
	args = append(args, id)

	query := fmt.Sprintf("UPDATE machines SET %s WHERE id = ?", strings.Join(setClauses, ", "))
	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update machine: %w", err)
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

// Delete deletes a machine and the deployment data that belongs to it.
func (r *MachineRepository) Delete(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start machine deletion transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM deployment_logs
		 WHERE machine_id = ?
		    OR machine_certificate_id IN (
				SELECT id FROM machine_certificates WHERE machine_id = ?
			)`,
		id, id,
	); err != nil {
		return fmt.Errorf("failed to delete machine deployment logs: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE domains
		 SET linked_machine_id = CASE
				WHEN linked_machine_id = ? THEN ''
				ELSE linked_machine_id
			END,
			linked_machine_certificate_id = CASE
				WHEN linked_machine_certificate_id IN (
					SELECT id FROM machine_certificates WHERE machine_id = ?
				) THEN ''
				ELSE linked_machine_certificate_id
			END
		 WHERE linked_machine_id = ?
		    OR linked_machine_certificate_id IN (
				SELECT id FROM machine_certificates WHERE machine_id = ?
			)`,
		id, id, id, id,
	); err != nil {
		return fmt.Errorf("failed to clear machine domain links: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM machine_certificates WHERE machine_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete machine certificate configurations: %w", err)
	}

	result, err := tx.ExecContext(ctx, "DELETE FROM machines WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete machine: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit machine deletion: %w", err)
	}
	return nil
}

// UpdateHeartbeat updates heartbeat time and agent info.
func (r *MachineRepository) UpdateHeartbeat(ctx context.Context, id string, info model.HeartbeatInfo) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.db.ExecContext(ctx,
		`UPDATE machines SET last_heartbeat_at = ?, agent_version = ?, hostname = ?, ip = ?, os = ?, arch = ?, status = 'online', updated_at = ? WHERE id = ?`,
		now, info.AgentVersion, info.Hostname, info.IP, info.OS, info.Arch, now, id)
	if err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
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

// UpdateTokenHash updates the agent token hash.
func (r *MachineRepository) UpdateTokenHash(ctx context.Context, id string, tokenHash string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.db.ExecContext(ctx,
		`UPDATE machines SET agent_token_hash = ?, agent_token_revoked_at = NULL, updated_at = ? WHERE id = ?`,
		tokenHash, now, id)
	if err != nil {
		return fmt.Errorf("failed to update token hash: %w", err)
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

// RevokeToken marks the token as revoked.
func (r *MachineRepository) RevokeToken(ctx context.Context, id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.db.ExecContext(ctx,
		`UPDATE machines SET agent_token_revoked_at = ?, status = 'revoked', updated_at = ? WHERE id = ?`,
		now, now, id)
	if err != nil {
		return fmt.Errorf("failed to revoke token: %w", err)
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

// GetByTokenHash finds a machine by its token hash (for agent auth).
// Only returns machines with non-revoked tokens.
func (r *MachineRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*model.Machine, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, ip, hostname, os, arch, tags, remark, status, agent_version, agent_token_hash, agent_token_revoked_at, last_heartbeat_at, created_at, updated_at
		 FROM machines WHERE agent_token_hash = ? AND agent_token_revoked_at IS NULL`, tokenHash)
	return scanMachine(row)
}

// GetByTokenHashIncludingRevoked finds a machine by its token hash, including revoked tokens.
// Used to detect revoked token usage for alert triggering.
func (r *MachineRepository) GetByTokenHashIncludingRevoked(ctx context.Context, tokenHash string) (*model.Machine, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, ip, hostname, os, arch, tags, remark, status, agent_version, agent_token_hash, agent_token_revoked_at, last_heartbeat_at, created_at, updated_at
		 FROM machines WHERE agent_token_hash = ?`, tokenHash)
	return scanMachine(row)
}

// UpdateStatus updates machine status.
func (r *MachineRepository) UpdateStatus(ctx context.Context, id string, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.db.ExecContext(ctx,
		`UPDATE machines SET status = ?, updated_at = ? WHERE id = ?`,
		status, now, id)
	if err != nil {
		return fmt.Errorf("failed to update status: %w", err)
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

// ListByHeartbeatBefore returns machines whose last heartbeat is before the given time.
func (r *MachineRepository) ListByHeartbeatBefore(ctx context.Context, before time.Time) ([]*model.Machine, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, ip, hostname, os, arch, tags, remark, status, agent_version, agent_token_hash, agent_token_revoked_at, last_heartbeat_at, created_at, updated_at
		 FROM machines WHERE last_heartbeat_at IS NOT NULL AND last_heartbeat_at < ? AND status = 'online'`,
		before.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("failed to list machines by heartbeat: %w", err)
	}
	defer rows.Close()

	var machines []*model.Machine
	for rows.Next() {
		m, err := scanMachineFromRows(rows)
		if err != nil {
			return nil, err
		}
		machines = append(machines, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate machines: %w", err)
	}
	return machines, nil
}

// ListByStatus returns machines with the given status.
func (r *MachineRepository) ListByStatus(ctx context.Context, status string) ([]*model.Machine, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, ip, hostname, os, arch, tags, remark, status, agent_version, agent_token_hash, agent_token_revoked_at, last_heartbeat_at, created_at, updated_at
		 FROM machines WHERE status = ?`, status)
	if err != nil {
		return nil, fmt.Errorf("failed to list machines by status: %w", err)
	}
	defer rows.Close()

	var machines []*model.Machine
	for rows.Next() {
		m, err := scanMachineFromRows(rows)
		if err != nil {
			return nil, err
		}
		machines = append(machines, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate machines: %w", err)
	}
	return machines, nil
}

// --- Helper functions ---

func formatNullableTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func parseNullableTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s.String)
	if err != nil {
		return nil
	}
	return &t
}

func parseTags(s string) []string {
	if s == "" || s == "null" {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal([]byte(s), &tags); err != nil {
		return []string{}
	}
	return tags
}

type scannable interface {
	Scan(dest ...interface{}) error
}

func scanMachineFields(scanner scannable) (*model.Machine, error) {
	var m model.Machine
	var tagsStr string
	var revokedAt, heartbeatAt, createdAt, updatedAt sql.NullString

	err := scanner.Scan(
		&m.ID,
		&m.Name,
		&m.IP,
		&m.Hostname,
		&m.OS,
		&m.Arch,
		&tagsStr,
		&m.Remark,
		&m.Status,
		&m.AgentVersion,
		&m.AgentTokenHash,
		&revokedAt,
		&heartbeatAt,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to scan machine: %w", err)
	}

	m.Tags = parseTags(tagsStr)
	m.AgentTokenRevokedAt = parseNullableTime(revokedAt)
	m.LastHeartbeatAt = parseNullableTime(heartbeatAt)

	if createdAt.Valid {
		t, _ := time.Parse(time.RFC3339, createdAt.String)
		m.CreatedAt = t
	}
	if updatedAt.Valid {
		t, _ := time.Parse(time.RFC3339, updatedAt.String)
		m.UpdatedAt = t
	}

	return &m, nil
}

func scanMachine(row *sql.Row) (*model.Machine, error) {
	return scanMachineFields(row)
}

func scanMachineFromRows(rows *sql.Rows) (*model.Machine, error) {
	return scanMachineFields(rows)
}
