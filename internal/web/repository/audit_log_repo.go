package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

// AuditLogFilter defines filter criteria for querying audit logs.
type AuditLogFilter struct {
	ActorType  string `json:"actor_type,omitempty"`
	ActorID    string `json:"actor_id,omitempty"`
	Action     string `json:"action,omitempty"`
	TargetType string `json:"target_type,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	DateFrom   string `json:"date_from,omitempty"` // format: 2006-01-02
	DateTo     string `json:"date_to,omitempty"`   // format: 2006-01-02
	Limit      int    `json:"limit,omitempty"`
	Offset     int    `json:"offset,omitempty"`
}

// AuditLogRepository handles audit log CRUD operations.
type AuditLogRepository struct {
	db *sql.DB
}

// NewAuditLogRepository creates a new AuditLogRepository.
func NewAuditLogRepository(db *sql.DB) *AuditLogRepository {
	return &AuditLogRepository{db: db}
}

// Create saves a new audit log record.
func (r *AuditLogRepository) Create(ctx context.Context, log *model.AuditLog) error {
	if log.ID == "" {
		log.ID = uuid.New().String()
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO audit_logs (id, actor_type, actor_id, action, target_type, target_id, detail, ip, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.ID,
		log.ActorType,
		log.ActorID,
		log.Action,
		log.TargetType,
		log.TargetID,
		log.Detail,
		log.IP,
		log.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to create audit log: %w", err)
	}
	return nil
}

// CreateAuditLog implements the AuditRepository interface used by the audit middleware.
func (r *AuditLogRepository) CreateAuditLog(ctx context.Context, log *model.AuditLog) error {
	return r.Create(ctx, log)
}

// List returns audit logs matching the filter, ordered by created_at DESC.
func (r *AuditLogRepository) List(ctx context.Context, filter AuditLogFilter) ([]*model.AuditLog, error) {
	query := `SELECT id, actor_type, actor_id, action, target_type, target_id, detail, ip, created_at
		FROM audit_logs WHERE 1=1`
	var args []interface{}

	if filter.ActorType != "" {
		query += " AND actor_type = ?"
		args = append(args, filter.ActorType)
	}
	if filter.ActorID != "" {
		query += " AND actor_id = ?"
		args = append(args, filter.ActorID)
	}
	if filter.Action != "" {
		query += " AND action LIKE ?"
		args = append(args, "%"+filter.Action+"%")
	}
	if filter.TargetType != "" {
		query += " AND target_type = ?"
		args = append(args, filter.TargetType)
	}
	if filter.TargetID != "" {
		query += " AND target_id = ?"
		args = append(args, filter.TargetID)
	}
	if filter.DateFrom != "" {
		query += " AND created_at >= ?"
		args = append(args, filter.DateFrom+"T00:00:00Z")
	}
	if filter.DateTo != "" {
		query += " AND created_at <= ?"
		args = append(args, filter.DateTo+"T23:59:59Z")
	}

	query += " ORDER BY created_at DESC"

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	query += " LIMIT ?"
	args = append(args, limit)

	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*model.AuditLog
	for rows.Next() {
		log, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate audit logs: %w", err)
	}
	return logs, nil
}

// scanAuditLog scans a single audit log row.
func scanAuditLog(rows *sql.Rows) (*model.AuditLog, error) {
	var log model.AuditLog
	var createdAt string

	err := rows.Scan(
		&log.ID,
		&log.ActorType,
		&log.ActorID,
		&log.Action,
		&log.TargetType,
		&log.TargetID,
		&log.Detail,
		&log.IP,
		&createdAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan audit log: %w", err)
	}

	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		log.CreatedAt = t
	}

	return &log, nil
}

// Count returns the total number of audit logs matching the filter (ignoring limit/offset).
func (r *AuditLogRepository) Count(ctx context.Context, filter AuditLogFilter) (int, error) {
	query := `SELECT COUNT(*) FROM audit_logs WHERE 1=1`
	var args []interface{}

	if filter.ActorType != "" {
		query += " AND actor_type = ?"
		args = append(args, filter.ActorType)
	}
	if filter.ActorID != "" {
		query += " AND actor_id = ?"
		args = append(args, filter.ActorID)
	}
	if filter.Action != "" {
		query += " AND action LIKE ?"
		args = append(args, "%"+filter.Action+"%")
	}
	if filter.TargetType != "" {
		query += " AND target_type = ?"
		args = append(args, filter.TargetType)
	}
	if filter.TargetID != "" {
		query += " AND target_id = ?"
		args = append(args, filter.TargetID)
	}
	if filter.DateFrom != "" {
		query += " AND created_at >= ?"
		args = append(args, filter.DateFrom+"T00:00:00Z")
	}
	if filter.DateTo != "" {
		query += " AND created_at <= ?"
		args = append(args, filter.DateTo+"T23:59:59Z")
	}

	var count int
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count audit logs: %w", err)
	}
	return count, nil
}
