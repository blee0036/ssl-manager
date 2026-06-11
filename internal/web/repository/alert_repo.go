package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

// AlertRepository handles alert CRUD operations.
type AlertRepository struct {
	db *sql.DB
}

// NewAlertRepository creates a new AlertRepository.
func NewAlertRepository(db *sql.DB) *AlertRepository {
	return &AlertRepository{db: db}
}

// Create saves a new alert record.
func (r *AlertRepository) Create(ctx context.Context, alert *model.Alert) error {
	if alert.ID == "" {
		alert.ID = uuid.New().String()
	}
	if alert.CreatedAt.IsZero() {
		alert.CreatedAt = time.Now().UTC()
	}
	if alert.Status == "" {
		alert.Status = "active"
	}

	sentChannels := strings.Join(alert.SentChannels, ",")

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO alerts (id, level, type, title, content, status, target_type, target_id, sent_channels, created_at, resolved_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		alert.ID,
		alert.Level,
		alert.Type,
		alert.Title,
		alert.Content,
		alert.Status,
		alert.TargetType,
		alert.TargetID,
		sentChannels,
		alert.CreatedAt.UTC().Format(time.RFC3339),
		formatOptionalTime(alert.ResolvedAt),
	)
	if err != nil {
		return fmt.Errorf("failed to create alert: %w", err)
	}
	return nil
}

// GetByID retrieves an alert by ID.
func (r *AlertRepository) GetByID(ctx context.Context, id string) (*model.Alert, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, level, type, title, content, status, target_type, target_id, sent_channels, created_at, resolved_at
		 FROM alerts WHERE id = ?`, id)

	return r.scanAlert(row)
}

// List returns alerts with optional filtering, ordered by created_at DESC.
func (r *AlertRepository) List(ctx context.Context, filter model.AlertFilter) ([]*model.Alert, error) {
	query := `SELECT id, level, type, title, content, status, target_type, target_id, sent_channels, created_at, resolved_at FROM alerts`
	var conditions []string
	var args []interface{}

	if filter.Level != "" {
		conditions = append(conditions, "level = ?")
		args = append(args, filter.Level)
	}
	if filter.Type != "" {
		conditions = append(conditions, "type = ?")
		args = append(args, filter.Type)
	}
	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, filter.Status)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query alerts: %w", err)
	}
	defer rows.Close()

	var alerts []*model.Alert
	for rows.Next() {
		alert, err := r.scanAlertFromRows(rows)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, alert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate alerts: %w", err)
	}
	return alerts, nil
}

// FindActiveByTarget finds an active (unresolved) alert for a specific target and alert type.
// Used for suppression logic.
func (r *AlertRepository) FindActiveByTarget(ctx context.Context, targetType, targetID, alertType string) (*model.Alert, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, level, type, title, content, status, target_type, target_id, sent_channels, created_at, resolved_at
		 FROM alerts WHERE target_type = ? AND target_id = ? AND type = ? AND status = 'active'
		 ORDER BY created_at DESC LIMIT 1`,
		targetType, targetID, alertType)

	alert, err := r.scanAlert(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return alert, nil
}

// UpdateStatus updates the status and resolved_at of an alert.
func (r *AlertRepository) UpdateStatus(ctx context.Context, id, status string, resolvedAt *time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE alerts SET status = ?, resolved_at = ? WHERE id = ?`,
		status, formatOptionalTime(resolvedAt), id)
	if err != nil {
		return fmt.Errorf("failed to update alert status: %w", err)
	}
	return nil
}

// SuppressActiveByTarget sets all active alerts for a given target to 'suppressed' status.
func (r *AlertRepository) SuppressActiveByTarget(ctx context.Context, targetType, targetID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.ExecContext(ctx,
		`UPDATE alerts SET status = 'suppressed', resolved_at = ? WHERE target_type = ? AND target_id = ? AND status = 'active'`,
		now, targetType, targetID)
	if err != nil {
		return fmt.Errorf("failed to suppress active alerts: %w", err)
	}
	return nil
}

// --- Helper functions ---

func (r *AlertRepository) scanAlert(row *sql.Row) (*model.Alert, error) {
	var alert model.Alert
	var sentChannelsStr string
	var createdAt string
	var resolvedAt sql.NullString

	err := row.Scan(
		&alert.ID,
		&alert.Level,
		&alert.Type,
		&alert.Title,
		&alert.Content,
		&alert.Status,
		&alert.TargetType,
		&alert.TargetID,
		&sentChannelsStr,
		&createdAt,
		&resolvedAt,
	)
	if err != nil {
		return nil, err
	}

	if sentChannelsStr != "" {
		alert.SentChannels = strings.Split(sentChannelsStr, ",")
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		alert.CreatedAt = t
	}
	if resolvedAt.Valid && resolvedAt.String != "" {
		if t, err := time.Parse(time.RFC3339, resolvedAt.String); err == nil {
			alert.ResolvedAt = &t
		}
	}

	return &alert, nil
}

func (r *AlertRepository) scanAlertFromRows(rows *sql.Rows) (*model.Alert, error) {
	var alert model.Alert
	var sentChannelsStr string
	var createdAt string
	var resolvedAt sql.NullString

	err := rows.Scan(
		&alert.ID,
		&alert.Level,
		&alert.Type,
		&alert.Title,
		&alert.Content,
		&alert.Status,
		&alert.TargetType,
		&alert.TargetID,
		&sentChannelsStr,
		&createdAt,
		&resolvedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan alert: %w", err)
	}

	if sentChannelsStr != "" {
		alert.SentChannels = strings.Split(sentChannelsStr, ",")
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		alert.CreatedAt = t
	}
	if resolvedAt.Valid && resolvedAt.String != "" {
		if t, err := time.Parse(time.RFC3339, resolvedAt.String); err == nil {
			alert.ResolvedAt = &t
		}
	}

	return &alert, nil
}

func formatOptionalTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// NotificationChannelRepository handles notification channel CRUD operations.
type NotificationChannelRepository struct {
	db *sql.DB
}

// NewNotificationChannelRepository creates a new NotificationChannelRepository.
func NewNotificationChannelRepository(db *sql.DB) *NotificationChannelRepository {
	return &NotificationChannelRepository{db: db}
}

// Create saves a new notification channel.
func (r *NotificationChannelRepository) Create(ctx context.Context, ch *model.NotificationChannel) error {
	if ch.ID == "" {
		ch.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	if ch.CreatedAt.IsZero() {
		ch.CreatedAt = now
	}
	if ch.UpdatedAt.IsZero() {
		ch.UpdatedAt = now
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO notification_channels (id, type, name, config_json, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ch.ID,
		ch.Type,
		ch.Name,
		ch.ConfigJSON,
		boolToInt(ch.Enabled),
		ch.CreatedAt.UTC().Format(time.RFC3339),
		ch.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to create notification channel: %w", err)
	}
	return nil
}

// GetByID retrieves a notification channel by ID.
func (r *NotificationChannelRepository) GetByID(ctx context.Context, id string) (*model.NotificationChannel, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, type, name, config_json, enabled, created_at, updated_at
		 FROM notification_channels WHERE id = ?`, id)

	return r.scanChannel(row)
}

// List returns all notification channels.
func (r *NotificationChannelRepository) List(ctx context.Context) ([]*model.NotificationChannel, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, type, name, config_json, enabled, created_at, updated_at
		 FROM notification_channels ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to query notification channels: %w", err)
	}
	defer rows.Close()

	var channels []*model.NotificationChannel
	for rows.Next() {
		ch, err := r.scanChannelFromRows(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate notification channels: %w", err)
	}
	return channels, nil
}

// ListEnabled returns all enabled notification channels.
func (r *NotificationChannelRepository) ListEnabled(ctx context.Context) ([]*model.NotificationChannel, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, type, name, config_json, enabled, created_at, updated_at
		 FROM notification_channels WHERE enabled = 1 ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to query enabled notification channels: %w", err)
	}
	defer rows.Close()

	var channels []*model.NotificationChannel
	for rows.Next() {
		ch, err := r.scanChannelFromRows(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate enabled notification channels: %w", err)
	}
	return channels, nil
}

// Update updates a notification channel.
func (r *NotificationChannelRepository) Update(ctx context.Context, id string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	var setClauses []string
	var args []interface{}

	for key, val := range updates {
		setClauses = append(setClauses, key+" = ?")
		args = append(args, val)
	}

	// Always update updated_at
	setClauses = append(setClauses, "updated_at = ?")
	args = append(args, time.Now().UTC().Format(time.RFC3339))

	args = append(args, id)

	query := fmt.Sprintf("UPDATE notification_channels SET %s WHERE id = ?", strings.Join(setClauses, ", "))
	_, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update notification channel: %w", err)
	}
	return nil
}

// Delete deletes a notification channel by ID.
func (r *NotificationChannelRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM notification_channels WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete notification channel: %w", err)
	}
	return nil
}

// --- Helper functions ---

func (r *NotificationChannelRepository) scanChannel(row *sql.Row) (*model.NotificationChannel, error) {
	var ch model.NotificationChannel
	var enabled int
	var createdAt, updatedAt string

	err := row.Scan(
		&ch.ID,
		&ch.Type,
		&ch.Name,
		&ch.ConfigJSON,
		&enabled,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, err
	}

	ch.Enabled = enabled == 1
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		ch.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		ch.UpdatedAt = t
	}

	return &ch, nil
}

func (r *NotificationChannelRepository) scanChannelFromRows(rows *sql.Rows) (*model.NotificationChannel, error) {
	var ch model.NotificationChannel
	var enabled int
	var createdAt, updatedAt string

	err := rows.Scan(
		&ch.ID,
		&ch.Type,
		&ch.Name,
		&ch.ConfigJSON,
		&enabled,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan notification channel: %w", err)
	}

	ch.Enabled = enabled == 1
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		ch.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		ch.UpdatedAt = t
	}

	return &ch, nil
}
