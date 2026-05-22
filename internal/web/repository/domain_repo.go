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

// DomainRepository handles domain monitor CRUD and monitor result storage.
type DomainRepository struct {
	db *sql.DB
}

// NewDomainRepository creates a new DomainRepository.
func NewDomainRepository(db *sql.DB) *DomainRepository {
	return &DomainRepository{db: db}
}

// Create creates a new domain monitor record.
func (r *DomainRepository) Create(ctx context.Context, domain *model.Domain) error {
	if domain.ID == "" {
		domain.ID = uuid.New().String()
	}

	now := time.Now().UTC()
	domain.CreatedAt = now
	domain.UpdatedAt = now

	if domain.Source == "" {
		domain.Source = "manual"
	}

	query := `INSERT INTO domains (
		id, name, source, thirdpart_dns_id, dns_record_type, dns_record_value,
		monitor_port, linked_machine_id, linked_certificate_id,
		linked_machine_certificate_id, monitor_enabled, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, query,
		domain.ID,
		domain.Name,
		domain.Source,
		domain.ThirdpartDNSID,
		domain.DNSRecordType,
		domain.DNSRecordValue,
		domain.MonitorPort,
		nullableString(domain.LinkedMachineID),
		nullableString(domain.LinkedCertificateID),
		nullableString(domain.LinkedMachineCertificateID),
		boolToInt(domain.MonitorEnabled),
		domain.CreatedAt.Format(time.RFC3339),
		domain.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to insert domain: %w", err)
	}

	return nil
}

// GetByID retrieves a domain by ID.
func (r *DomainRepository) GetByID(ctx context.Context, id string) (*model.Domain, error) {
	query := `SELECT id, name, source, thirdpart_dns_id, dns_record_type, dns_record_value,
		monitor_port, linked_machine_id, linked_certificate_id,
		linked_machine_certificate_id, monitor_enabled, created_at, updated_at
	FROM domains WHERE id = ?`

	row := r.db.QueryRowContext(ctx, query, id)
	return r.scanDomain(row)
}

// List returns domains with optional filtering.
func (r *DomainRepository) List(ctx context.Context, filter model.DomainFilter) ([]*model.Domain, error) {
	query := `SELECT id, name, source, thirdpart_dns_id, dns_record_type, dns_record_value,
		monitor_port, linked_machine_id, linked_certificate_id,
		linked_machine_certificate_id, monitor_enabled, created_at, updated_at
	FROM domains WHERE 1=1`

	var args []interface{}

	if filter.Name != "" {
		query += " AND name LIKE ?"
		args = append(args, "%"+filter.Name+"%")
	}

	if filter.Source != "" {
		query += " AND source = ?"
		args = append(args, filter.Source)
	}

	if filter.MonitorEnabled != nil {
		query += " AND monitor_enabled = ?"
		args = append(args, boolToInt(*filter.MonitorEnabled))
	}

	if filter.ThirdpartDNSID != "" {
		query += " AND thirdpart_dns_id = ?"
		args = append(args, filter.ThirdpartDNSID)
	}

	query += " ORDER BY created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query domains: %w", err)
	}
	defer rows.Close()

	var domains []*model.Domain
	for rows.Next() {
		domain, err := r.scanDomainFromRows(rows)
		if err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating domain rows: %w", err)
	}

	return domains, nil
}

// Update updates a domain record.
func (r *DomainRepository) Update(ctx context.Context, id string, updates map[string]interface{}) error {
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
		case "monitor_enabled":
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
	query := fmt.Sprintf("UPDATE domains SET %s WHERE id = ?", setClauses)

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update domain: %w", err)
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

// Delete deletes a domain record and its associated monitor results.
func (r *DomainRepository) Delete(ctx context.Context, id string) error {
	// Delete associated monitor results first
	_, err := r.db.ExecContext(ctx, "DELETE FROM domain_monitor_results WHERE domain_id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete domain monitor results: %w", err)
	}

	result, err := r.db.ExecContext(ctx, "DELETE FROM domains WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete domain: %w", err)
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

// SaveMonitorResult saves a domain monitor probe result.
func (r *DomainRepository) SaveMonitorResult(ctx context.Context, result *model.DomainMonitorResult) error {
	if result.ID == "" {
		result.ID = uuid.New().String()
	}

	resolvedIPsJSON, err := json.Marshal(result.ResolvedIPs)
	if err != nil {
		return fmt.Errorf("failed to marshal resolved_ips: %w", err)
	}

	var expireAt *string
	if result.ExpireAt != nil {
		s := result.ExpireAt.UTC().Format(time.RFC3339)
		expireAt = &s
	}

	query := `INSERT INTO domain_monitor_results (
		id, domain_id, checked_port, resolved_ips, tls_success,
		certificate_fingerprint_sha256, issuer, expire_at, days_remaining,
		domain_matched, chain_valid, error_message, checked_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = r.db.ExecContext(ctx, query,
		result.ID,
		result.DomainID,
		result.CheckedPort,
		string(resolvedIPsJSON),
		boolToInt(result.TLSSuccess),
		result.CertificateFingerprintSHA256,
		result.Issuer,
		expireAt,
		result.DaysRemaining,
		boolToInt(result.DomainMatched),
		boolToInt(result.ChainValid),
		result.ErrorMessage,
		result.CheckedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to insert domain monitor result: %w", err)
	}

	return nil
}

// GetLatestMonitorResult retrieves the most recent monitor result for a domain.
func (r *DomainRepository) GetLatestMonitorResult(ctx context.Context, domainID string) (*model.DomainMonitorResult, error) {
	query := `SELECT id, domain_id, checked_port, resolved_ips, tls_success,
		certificate_fingerprint_sha256, issuer, expire_at, days_remaining,
		domain_matched, chain_valid, error_message, checked_at
	FROM domain_monitor_results WHERE domain_id = ?
	ORDER BY checked_at DESC LIMIT 1`

	row := r.db.QueryRowContext(ctx, query, domainID)
	return r.scanMonitorResult(row)
}

// scanDomain scans a single row into a Domain model.
func (r *DomainRepository) scanDomain(row *sql.Row) (*model.Domain, error) {
	var domain model.Domain
	var createdAt, updatedAt string
	var monitorEnabled int
	var linkedMachineID, linkedCertificateID, linkedMachineCertificateID sql.NullString

	err := row.Scan(
		&domain.ID,
		&domain.Name,
		&domain.Source,
		&domain.ThirdpartDNSID,
		&domain.DNSRecordType,
		&domain.DNSRecordValue,
		&domain.MonitorPort,
		&linkedMachineID,
		&linkedCertificateID,
		&linkedMachineCertificateID,
		&monitorEnabled,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to scan domain: %w", err)
	}

	return r.populateDomain(&domain, createdAt, updatedAt, monitorEnabled, linkedMachineID, linkedCertificateID, linkedMachineCertificateID)
}

// scanDomainFromRows scans a row from sql.Rows into a Domain model.
func (r *DomainRepository) scanDomainFromRows(rows *sql.Rows) (*model.Domain, error) {
	var domain model.Domain
	var createdAt, updatedAt string
	var monitorEnabled int
	var linkedMachineID, linkedCertificateID, linkedMachineCertificateID sql.NullString

	err := rows.Scan(
		&domain.ID,
		&domain.Name,
		&domain.Source,
		&domain.ThirdpartDNSID,
		&domain.DNSRecordType,
		&domain.DNSRecordValue,
		&domain.MonitorPort,
		&linkedMachineID,
		&linkedCertificateID,
		&linkedMachineCertificateID,
		&monitorEnabled,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan domain row: %w", err)
	}

	return r.populateDomain(&domain, createdAt, updatedAt, monitorEnabled, linkedMachineID, linkedCertificateID, linkedMachineCertificateID)
}

// populateDomain fills in parsed fields on a Domain.
func (r *DomainRepository) populateDomain(
	domain *model.Domain,
	createdAt, updatedAt string,
	monitorEnabled int,
	linkedMachineID, linkedCertificateID, linkedMachineCertificateID sql.NullString,
) (*model.Domain, error) {
	var err error
	domain.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at: %w", err)
	}

	domain.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse updated_at: %w", err)
	}

	domain.MonitorEnabled = monitorEnabled == 1

	if linkedMachineID.Valid {
		domain.LinkedMachineID = linkedMachineID.String
	}
	if linkedCertificateID.Valid {
		domain.LinkedCertificateID = linkedCertificateID.String
	}
	if linkedMachineCertificateID.Valid {
		domain.LinkedMachineCertificateID = linkedMachineCertificateID.String
	}

	return domain, nil
}

// scanMonitorResult scans a single row into a DomainMonitorResult model.
func (r *DomainRepository) scanMonitorResult(row *sql.Row) (*model.DomainMonitorResult, error) {
	var result model.DomainMonitorResult
	var resolvedIPsJSON string
	var checkedAt string
	var expireAt sql.NullString
	var daysRemaining sql.NullInt64
	var tlsSuccess, domainMatched, chainValid int

	err := row.Scan(
		&result.ID,
		&result.DomainID,
		&result.CheckedPort,
		&resolvedIPsJSON,
		&tlsSuccess,
		&result.CertificateFingerprintSHA256,
		&result.Issuer,
		&expireAt,
		&daysRemaining,
		&domainMatched,
		&chainValid,
		&result.ErrorMessage,
		&checkedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to scan monitor result: %w", err)
	}

	// Parse resolved IPs
	if resolvedIPsJSON != "" {
		if err := json.Unmarshal([]byte(resolvedIPsJSON), &result.ResolvedIPs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal resolved_ips: %w", err)
		}
	}

	// Parse checked_at
	result.CheckedAt, err = time.Parse(time.RFC3339, checkedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse checked_at: %w", err)
	}

	// Parse expire_at
	if expireAt.Valid {
		t, err := time.Parse(time.RFC3339, expireAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse expire_at: %w", err)
		}
		result.ExpireAt = &t
	}

	// Parse days_remaining
	if daysRemaining.Valid {
		d := int(daysRemaining.Int64)
		result.DaysRemaining = &d
	}

	result.TLSSuccess = tlsSuccess == 1
	result.DomainMatched = domainMatched == 1
	result.ChainValid = chainValid == 1

	return &result, nil
}

// nullableString returns a sql.NullString for optional string fields.
func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}
