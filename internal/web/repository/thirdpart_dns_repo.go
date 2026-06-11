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

// ThirdpartDNSRepository handles thirdpart_dns and thirdpart_dns_sync_logs CRUD.
type ThirdpartDNSRepository struct {
	db *sql.DB
}

// NewThirdpartDNSRepository creates a new ThirdpartDNSRepository.
func NewThirdpartDNSRepository(db *sql.DB) *ThirdpartDNSRepository {
	return &ThirdpartDNSRepository{db: db}
}

// Create creates a new thirdpart_dns configuration record.
func (r *ThirdpartDNSRepository) Create(ctx context.Context, config *model.ThirdpartDNS) error {
	if config.ID == "" {
		config.ID = uuid.New().String()
	}

	now := time.Now().UTC()
	config.CreatedAt = now
	config.UpdatedAt = now

	mainDomainsJSON, err := json.Marshal(config.MainDomains)
	if err != nil {
		return fmt.Errorf("failed to marshal main_domains: %w", err)
	}

	query := `INSERT INTO thirdpart_dns (
		id, name, type, api_token, config_json, main_domains, enabled, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = r.db.ExecContext(ctx, query,
		config.ID,
		config.Name,
		config.Type,
		config.APIToken,
		config.ConfigJSON,
		string(mainDomainsJSON),
		boolToInt(config.Enabled),
		config.CreatedAt.Format(time.RFC3339),
		config.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to insert thirdpart_dns: %w", err)
	}

	return nil
}

// GetByID retrieves a thirdpart_dns configuration by ID.
func (r *ThirdpartDNSRepository) GetByID(ctx context.Context, id string) (*model.ThirdpartDNS, error) {
	query := `SELECT id, name, type, api_token, config_json, main_domains, enabled, created_at, updated_at
	FROM thirdpart_dns WHERE id = ?`

	row := r.db.QueryRowContext(ctx, query, id)
	return r.scanConfig(row)
}

// List returns all thirdpart_dns configurations.
func (r *ThirdpartDNSRepository) List(ctx context.Context) ([]*model.ThirdpartDNS, error) {
	query := `SELECT id, name, type, api_token, config_json, main_domains, enabled, created_at, updated_at
	FROM thirdpart_dns ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query thirdpart_dns: %w", err)
	}
	defer rows.Close()

	var configs []*model.ThirdpartDNS
	for rows.Next() {
		cfg, err := r.scanConfigFromRows(rows)
		if err != nil {
			return nil, err
		}
		configs = append(configs, cfg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating thirdpart_dns rows: %w", err)
	}

	return configs, nil
}

// Update updates a thirdpart_dns configuration.
func (r *ThirdpartDNSRepository) Update(ctx context.Context, id string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	updates["updated_at"] = time.Now().UTC().Format(time.RFC3339)

	setClauses := ""
	var args []interface{}

	for key, value := range updates {
		if setClauses != "" {
			setClauses += ", "
		}
		setClauses += key + " = ?"

		switch key {
		case "main_domains":
			if domains, ok := value.([]string); ok {
				domainsJSON, err := json.Marshal(domains)
				if err != nil {
					return fmt.Errorf("failed to marshal main_domains: %w", err)
				}
				args = append(args, string(domainsJSON))
			} else {
				args = append(args, value)
			}
		case "enabled":
			if b, ok := value.(bool); ok {
				args = append(args, boolToInt(b))
			} else {
				args = append(args, value)
			}
		default:
			args = append(args, value)
		}
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE thirdpart_dns SET %s WHERE id = ?", setClauses)

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update thirdpart_dns: %w", err)
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

// Delete deletes a thirdpart_dns configuration and its sync logs.
func (r *ThirdpartDNSRepository) Delete(ctx context.Context, id string) error {
	// Delete associated sync logs first
	_, err := r.db.ExecContext(ctx, "DELETE FROM thirdpart_dns_sync_logs WHERE thirdpart_dns_id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete sync logs: %w", err)
	}

	result, err := r.db.ExecContext(ctx, "DELETE FROM thirdpart_dns WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete thirdpart_dns: %w", err)
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

// SaveSyncLog saves a sync log entry for a thirdpart_dns configuration.
func (r *ThirdpartDNSRepository) SaveSyncLog(ctx context.Context, log *model.ThirdpartDNSSyncLog) error {
	if log.ID == "" {
		log.ID = uuid.New().String()
	}

	query := `INSERT INTO thirdpart_dns_sync_logs (
		id, thirdpart_dns_id, records_count, status, error_message, new_domains, updated_domains, removed_domains, synced_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, query,
		log.ID,
		log.ThirdpartDNSID,
		log.RecordsCount,
		log.Status,
		log.ErrorMessage,
		log.NewDomains,
		log.UpdatedDomains,
		log.RemovedDomains,
		log.SyncedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to insert sync log: %w", err)
	}

	return nil
}

// GetSyncLogs retrieves sync logs for a thirdpart_dns configuration, ordered by time descending.
func (r *ThirdpartDNSRepository) GetSyncLogs(ctx context.Context, thirdpartDNSID string) ([]*model.ThirdpartDNSSyncLog, error) {
	query := `SELECT id, thirdpart_dns_id, records_count, status, error_message,
	COALESCE(new_domains, '[]'), COALESCE(updated_domains, '[]'), COALESCE(removed_domains, '[]'),
	synced_at
	FROM thirdpart_dns_sync_logs WHERE thirdpart_dns_id = ?
	ORDER BY synced_at DESC`

	rows, err := r.db.QueryContext(ctx, query, thirdpartDNSID)
	if err != nil {
		return nil, fmt.Errorf("failed to query sync logs: %w", err)
	}
	defer rows.Close()

	var logs []*model.ThirdpartDNSSyncLog
	for rows.Next() {
		l, err := r.scanSyncLogFromRows(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sync log rows: %w", err)
	}

	return logs, nil
}

// scanConfig scans a single row into a ThirdpartDNS model.
func (r *ThirdpartDNSRepository) scanConfig(row *sql.Row) (*model.ThirdpartDNS, error) {
	var cfg model.ThirdpartDNS
	var mainDomainsJSON string
	var createdAt, updatedAt string
	var enabled int

	err := row.Scan(
		&cfg.ID,
		&cfg.Name,
		&cfg.Type,
		&cfg.APIToken,
		&cfg.ConfigJSON,
		&mainDomainsJSON,
		&enabled,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to scan thirdpart_dns: %w", err)
	}

	return r.populateConfig(&cfg, mainDomainsJSON, createdAt, updatedAt, enabled)
}

// scanConfigFromRows scans a row from sql.Rows into a ThirdpartDNS model.
func (r *ThirdpartDNSRepository) scanConfigFromRows(rows *sql.Rows) (*model.ThirdpartDNS, error) {
	var cfg model.ThirdpartDNS
	var mainDomainsJSON string
	var createdAt, updatedAt string
	var enabled int

	err := rows.Scan(
		&cfg.ID,
		&cfg.Name,
		&cfg.Type,
		&cfg.APIToken,
		&cfg.ConfigJSON,
		&mainDomainsJSON,
		&enabled,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan thirdpart_dns row: %w", err)
	}

	return r.populateConfig(&cfg, mainDomainsJSON, createdAt, updatedAt, enabled)
}

// populateConfig fills in parsed fields on a ThirdpartDNS.
func (r *ThirdpartDNSRepository) populateConfig(
	cfg *model.ThirdpartDNS,
	mainDomainsJSON, createdAt, updatedAt string,
	enabled int,
) (*model.ThirdpartDNS, error) {
	if err := json.Unmarshal([]byte(mainDomainsJSON), &cfg.MainDomains); err != nil {
		return nil, fmt.Errorf("failed to unmarshal main_domains: %w", err)
	}

	var err error
	cfg.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at: %w", err)
	}

	cfg.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse updated_at: %w", err)
	}

	cfg.Enabled = enabled == 1

	return cfg, nil
}

// scanSyncLogFromRows scans a row from sql.Rows into a ThirdpartDNSSyncLog model.
func (r *ThirdpartDNSRepository) scanSyncLogFromRows(rows *sql.Rows) (*model.ThirdpartDNSSyncLog, error) {
	var l model.ThirdpartDNSSyncLog
	var syncedAt string

	err := rows.Scan(
		&l.ID,
		&l.ThirdpartDNSID,
		&l.RecordsCount,
		&l.Status,
		&l.ErrorMessage,
		&l.NewDomains,
		&l.UpdatedDomains,
		&l.RemovedDomains,
		&syncedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan sync log row: %w", err)
	}

	l.SyncedAt, err = time.Parse(time.RFC3339, syncedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse synced_at: %w", err)
	}

	return &l, nil
}
