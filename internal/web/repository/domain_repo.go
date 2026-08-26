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

// filterStatusPredicates 定义每个 filter_status 值对应的 SQL 条件。
// 所有条件假设 d 为 domains 别名，dmr 为 LEFT JOIN latest domain_monitor_results 别名。
var filterStatusPredicates = map[string]string{
	"enabled":      "d.monitor_enabled = 1",
	"disabled":     "d.monitor_enabled = 0",
	"ignored":      "d.alert_ignored = 1",
	"tls_ok":       "dmr.id IS NOT NULL AND dmr.tls_success = 1",
	"tls_error":    "dmr.id IS NOT NULL AND dmr.tls_success = 0",
	"unchecked":    "dmr.id IS NULL",
	"matched":      "dmr.id IS NOT NULL AND dmr.domain_matched = 1",
	"unmatched":    "dmr.id IS NOT NULL AND dmr.domain_matched = 0",
	"expiring_30d": "dmr.expire_at IS NOT NULL AND strftime('%s', dmr.expire_at) > strftime('%s', 'now') AND strftime('%s', dmr.expire_at) <= strftime('%s', 'now', '+30 days')",
	"expired":      "dmr.expire_at IS NOT NULL AND strftime('%s', dmr.expire_at) <= strftime('%s', 'now')",
}

// sortByWhitelist maps allowed sort_by parameter values to safe SQL expressions.
var sortByWhitelist = map[string]string{
	"name":            "LOWER(RTRIM(d.name, '.'))",
	"source":          "d.source",
	"monitor_port":    "d.monitor_port",
	"tls_success":     "COALESCE(dmr.tls_success, -1)",
	"domain_matched":  "COALESCE(dmr.domain_matched, -1)",
	"expire_at":       "COALESCE(strftime('%s', dmr.expire_at), 0)",
	"checked_at":      "COALESCE(strftime('%s', dmr.checked_at), 0)",
	"monitor_enabled": "d.monitor_enabled",
	"alert_ignored":   "d.alert_ignored",
}

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
		id, name, source, thirdpart_dns_id, dns_record_id, dns_record_type, dns_record_value,
		monitor_port, linked_machine_id, linked_certificate_id,
		linked_machine_certificate_id, monitor_enabled, alert_ignored, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, query,
		domain.ID,
		domain.Name,
		domain.Source,
		domain.ThirdpartDNSID,
		domain.DNSRecordID,
		domain.DNSRecordType,
		domain.DNSRecordValue,
		domain.MonitorPort,
		nullableString(domain.LinkedMachineID),
		nullableString(domain.LinkedCertificateID),
		nullableString(domain.LinkedMachineCertificateID),
		boolToInt(domain.MonitorEnabled),
		boolToInt(domain.AlertIgnored),
		domain.CreatedAt.Format(time.RFC3339),
		domain.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to insert domain: %w", err)
	}

	return nil
}

// DeleteZoneOnlyCloudflareRecords removes domains rows that were created by the
// (now-removed) Cloudflare apex auto-sync logic: it added a TLS/SSL monitor for
// every Cloudflare Zone's root domain purely because the Zone existed, with no
// regard for whether that hostname actually resolves via any A/AAAA/CNAME
// record. That was a design error — a hostname should only be monitored for
// TLS when it has a real DNS record backing it (which is exactly what
// ThirdpartDNSService.syncToLocalDomains already handles).
//
// The signature that uniquely identifies such a leftover row is
// source='cloudflare' AND thirdpart_dns_id=” AND dns_record_id=”. Every
// legitimate cloudflare-sourced row — whether freshly synced or pre-existing
// legacy data missing only dns_record_id — always has thirdpart_dns_id set
// (ThirdpartDNSService.syncToLocalDomains's Create always populates it), so
// this signature can never match a real DNS-record-synced domain or a
// manually-created one (Source="manual").
//
// Any domain_monitor_results rows for a matched domain are deleted first (the
// same cascade DomainRepository.Delete performs for a single row), avoiding a
// foreign key violation under PRAGMA foreign_keys=ON. Returns the number of
// domains rows removed. Idempotent: once no row matches the signature, this is
// a no-op on every subsequent call.
func (r *DomainRepository) DeleteZoneOnlyCloudflareRecords(ctx context.Context) (int64, error) {
	const matchClause = `source = 'cloudflare' AND thirdpart_dns_id = '' AND dns_record_id = ''`

	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM domain_monitor_results WHERE domain_id IN (SELECT id FROM domains WHERE `+matchClause+`)`,
	); err != nil {
		return 0, fmt.Errorf("failed to delete monitor results for zone-only cloudflare domains: %w", err)
	}

	result, err := r.db.ExecContext(ctx, `DELETE FROM domains WHERE `+matchClause)
	if err != nil {
		return 0, fmt.Errorf("failed to delete zone-only cloudflare domains: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}
	return rowsAffected, nil
}

// GetByID retrieves a domain by ID.
func (r *DomainRepository) GetByID(ctx context.Context, id string) (*model.Domain, error) {
	query := `SELECT id, name, source, thirdpart_dns_id, dns_record_id, dns_record_type, dns_record_value,
		monitor_port, linked_machine_id, linked_certificate_id,
		linked_machine_certificate_id, monitor_enabled, alert_ignored, created_at, updated_at
	FROM domains WHERE id = ?`

	row := r.db.QueryRowContext(ctx, query, id)
	return r.scanDomain(row)
}

// List returns domains with optional filtering.
func (r *DomainRepository) List(ctx context.Context, filter model.DomainFilter) ([]*model.Domain, error) {
	query := `SELECT id, name, source, thirdpart_dns_id, dns_record_id, dns_record_type, dns_record_value,
		monitor_port, linked_machine_id, linked_certificate_id,
		linked_machine_certificate_id, monitor_enabled, alert_ignored, created_at, updated_at
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
		case "monitor_enabled", "alert_ignored":
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

// GetLatestMonitorResultsBatch retrieves the latest monitor result for each of the given domain IDs
// in a single query. Returns a map of domainID → *DomainMonitorResult (only for domains that have results).
func (r *DomainRepository) GetLatestMonitorResultsBatch(ctx context.Context, domainIDs []string) (map[string]*model.DomainMonitorResult, error) {
	if len(domainIDs) == 0 {
		return map[string]*model.DomainMonitorResult{}, nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(domainIDs))
	args := make([]interface{}, len(domainIDs))
	for i, id := range domainIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	// Use a correlated subquery to get only the latest result per domain
	query := fmt.Sprintf(`SELECT dmr.id, dmr.domain_id, dmr.checked_port, dmr.resolved_ips, dmr.tls_success,
		dmr.certificate_fingerprint_sha256, dmr.issuer, dmr.expire_at, dmr.days_remaining,
		dmr.domain_matched, dmr.chain_valid, dmr.error_message, dmr.checked_at
	FROM domain_monitor_results dmr
	WHERE dmr.domain_id IN (%s)
	  AND dmr.id = (SELECT id FROM domain_monitor_results WHERE domain_id = dmr.domain_id ORDER BY checked_at DESC LIMIT 1)`,
		strings.Join(placeholders, ","))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to batch query monitor results: %w", err)
	}
	defer rows.Close()

	results := make(map[string]*model.DomainMonitorResult, len(domainIDs))
	for rows.Next() {
		result, err := r.scanMonitorResultFromRows(rows)
		if err != nil {
			return nil, err
		}
		results[result.DomainID] = result
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating monitor result rows: %w", err)
	}

	return results, nil
}

// scanMonitorResultFromRows scans a row from sql.Rows into a DomainMonitorResult model.
func (r *DomainRepository) scanMonitorResultFromRows(rows *sql.Rows) (*model.DomainMonitorResult, error) {
	var result model.DomainMonitorResult
	var resolvedIPsJSON string
	var checkedAt string
	var expireAt sql.NullString
	var daysRemaining sql.NullInt64
	var tlsSuccess, domainMatched, chainValid int

	err := rows.Scan(
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
		return nil, fmt.Errorf("failed to scan monitor result row: %w", err)
	}

	if resolvedIPsJSON != "" {
		if err := json.Unmarshal([]byte(resolvedIPsJSON), &result.ResolvedIPs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal resolved_ips: %w", err)
		}
	}

	result.CheckedAt, err = time.Parse(time.RFC3339, checkedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse checked_at: %w", err)
	}

	if expireAt.Valid {
		t, err := time.Parse(time.RFC3339, expireAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse expire_at: %w", err)
		}
		result.ExpireAt = &t
	}

	if daysRemaining.Valid {
		d := int(daysRemaining.Int64)
		result.DaysRemaining = &d
	}

	result.TLSSuccess = tlsSuccess == 1
	result.DomainMatched = domainMatched == 1
	result.ChainValid = chainValid == 1

	return &result, nil
}

// scanDomain scans a single row into a Domain model.
func (r *DomainRepository) scanDomain(row *sql.Row) (*model.Domain, error) {
	var domain model.Domain
	var createdAt, updatedAt string
	var monitorEnabled, alertIgnored int
	var linkedMachineID, linkedCertificateID, linkedMachineCertificateID sql.NullString

	err := row.Scan(
		&domain.ID,
		&domain.Name,
		&domain.Source,
		&domain.ThirdpartDNSID,
		&domain.DNSRecordID,
		&domain.DNSRecordType,
		&domain.DNSRecordValue,
		&domain.MonitorPort,
		&linkedMachineID,
		&linkedCertificateID,
		&linkedMachineCertificateID,
		&monitorEnabled,
		&alertIgnored,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to scan domain: %w", err)
	}

	return r.populateDomain(&domain, createdAt, updatedAt, monitorEnabled, alertIgnored, linkedMachineID, linkedCertificateID, linkedMachineCertificateID)
}

// scanDomainFromRows scans a row from sql.Rows into a Domain model.
func (r *DomainRepository) scanDomainFromRows(rows *sql.Rows) (*model.Domain, error) {
	var domain model.Domain
	var createdAt, updatedAt string
	var monitorEnabled, alertIgnored int
	var linkedMachineID, linkedCertificateID, linkedMachineCertificateID sql.NullString

	err := rows.Scan(
		&domain.ID,
		&domain.Name,
		&domain.Source,
		&domain.ThirdpartDNSID,
		&domain.DNSRecordID,
		&domain.DNSRecordType,
		&domain.DNSRecordValue,
		&domain.MonitorPort,
		&linkedMachineID,
		&linkedCertificateID,
		&linkedMachineCertificateID,
		&monitorEnabled,
		&alertIgnored,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan domain row: %w", err)
	}

	return r.populateDomain(&domain, createdAt, updatedAt, monitorEnabled, alertIgnored, linkedMachineID, linkedCertificateID, linkedMachineCertificateID)
}

// populateDomain fills in parsed fields on a Domain.
func (r *DomainRepository) populateDomain(
	domain *model.Domain,
	createdAt, updatedAt string,
	monitorEnabled, alertIgnored int,
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

	domain.MonitorEnabled = monitorEnabled != 0
	domain.AlertIgnored = alertIgnored != 0

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

// buildWhereClause constructs the WHERE clause and args from DomainListParams.
// All conditions are combined with AND. Empty/missing conditions yield "1=1".
func buildWhereClause(params model.DomainListParams) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	if params.Name != "" {
		conditions = append(conditions, "d.name LIKE ?")
		args = append(args, "%"+params.Name+"%")
	}
	if params.Source != "" {
		conditions = append(conditions, "d.source = ?")
		args = append(args, params.Source)
	}
	if params.ThirdpartDNSID != "" {
		conditions = append(conditions, "d.thirdpart_dns_id = ?")
		args = append(args, params.ThirdpartDNSID)
	}
	if params.MonitorEnabled != nil {
		if *params.MonitorEnabled {
			conditions = append(conditions, "d.monitor_enabled = 1")
		} else {
			conditions = append(conditions, "d.monitor_enabled = 0")
		}
	}
	if params.AlertIgnored != nil {
		if *params.AlertIgnored {
			conditions = append(conditions, "d.alert_ignored = 1")
		} else {
			conditions = append(conditions, "d.alert_ignored = 0")
		}
	}

	// Status filter predicate (AND combined with above)
	if predicate, ok := filterStatusPredicates[params.FilterStatus]; ok {
		conditions = append(conditions, predicate)
	}

	if len(conditions) == 0 {
		return "1=1", args
	}
	return strings.Join(conditions, " AND "), args
}

// buildOrderByClause returns the ORDER BY clause for a given sort_by and sort_order.
// If sort_by is not in the whitelist, falls back to the default multi-level sort.
func buildOrderByClause(sortBy, sortOrder string) string {
	expr, ok := sortByWhitelist[sortBy]
	if !ok {
		return buildDefaultOrderBy()
	}
	direction := "ASC"
	if strings.ToLower(sortOrder) == "desc" {
		direction = "DESC"
	}
	return fmt.Sprintf("ORDER BY %s %s, d.id ASC", expr, direction)
}

// buildDefaultOrderBy returns the default multi-level sort ORDER BY clause.
func buildDefaultOrderBy() string {
	return `ORDER BY
  CASE WHEN d.alert_ignored = 1 THEN 3 WHEN d.monitor_enabled = 0 THEN 2 WHEN (dmr.id IS NULL OR dmr.tls_success = 0 OR dmr.domain_matched = 0 OR dmr.chain_valid = 0 OR dmr.error_message LIKE 'fingerprint mismatch:%' OR strftime('%s', dmr.expire_at) <= strftime('%s', 'now') OR (dmr.expire_at IS NOT NULL AND strftime('%s', dmr.expire_at) <= strftime('%s', 'now', '+30 days'))) THEN 0 ELSE 1 END ASC,
  CASE WHEN d.alert_ignored = 1 THEN 99 WHEN d.monitor_enabled = 0 THEN 99 WHEN strftime('%s', dmr.expire_at) <= strftime('%s', 'now') THEN 0 WHEN dmr.tls_success = 0 AND dmr.error_message != '' THEN 1 WHEN dmr.error_message LIKE 'fingerprint mismatch:%' THEN 2 WHEN dmr.chain_valid = 0 THEN 3 WHEN dmr.domain_matched = 0 THEN 4 WHEN strftime('%s', dmr.expire_at) <= strftime('%s', 'now', '+30 days') THEN 5 WHEN dmr.id IS NULL THEN 6 ELSE 99 END ASC,
  CASE WHEN d.alert_ignored = 0 AND d.monitor_enabled = 1 AND NOT (dmr.id IS NULL OR dmr.tls_success = 0 OR dmr.domain_matched = 0 OR dmr.chain_valid = 0 OR dmr.error_message LIKE 'fingerprint mismatch:%' OR strftime('%s', dmr.expire_at) <= strftime('%s', 'now') OR (dmr.expire_at IS NOT NULL AND strftime('%s', dmr.expire_at) <= strftime('%s', 'now', '+30 days'))) THEN COALESCE(strftime('%s', dmr.expire_at), 9223372036854775807) ELSE 9223372036854775807 END ASC,
  LOWER(RTRIM(d.name, '.')) ASC,
  d.monitor_port ASC,
  d.id ASC`
}

// ListWithSort returns domains with server-side sorting, filtering, and pagination.
// Only SELECTs domain table columns. The LEFT JOIN on domain_monitor_results (latest row)
// is used solely for filtering and sorting purposes.
// Returns ([]*model.Domain, totalCount, error).
func (r *DomainRepository) ListWithSort(ctx context.Context, params model.DomainListParams) ([]*model.Domain, int, error) {
	whereClause, args := buildWhereClause(params)

	joinClause := `LEFT JOIN domain_monitor_results dmr ON dmr.domain_id = d.id
  AND dmr.id = (SELECT id FROM domain_monitor_results WHERE domain_id = d.id ORDER BY checked_at DESC LIMIT 1)`

	// COUNT query (before pagination)
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM domains d %s WHERE %s", joinClause, whereClause)
	var total int
	if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count domains: %w", err)
	}

	// Build ORDER BY
	orderByClause := buildOrderByClause(params.SortBy, params.SortOrder)

	// Pagination defaults
	perPage := params.PerPage
	if perPage <= 0 {
		perPage = 50
	}
	if perPage > 100 {
		perPage = 100
	}
	page := params.Page
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * perPage

	// Data query: only SELECT domain columns
	dataSQL := fmt.Sprintf(`SELECT d.id, d.name, d.source, d.thirdpart_dns_id, d.dns_record_id, d.dns_record_type, d.dns_record_value,
       d.monitor_port, d.linked_machine_id, d.linked_certificate_id,
       d.linked_machine_certificate_id, d.monitor_enabled, d.alert_ignored,
       d.created_at, d.updated_at
FROM domains d %s
WHERE %s
%s
LIMIT ? OFFSET ?`, joinClause, whereClause, orderByClause)

	// Append pagination args after the WHERE args
	dataArgs := make([]interface{}, len(args))
	copy(dataArgs, args)
	dataArgs = append(dataArgs, perPage, offset)

	rows, err := r.db.QueryContext(ctx, dataSQL, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query domains with sort: %w", err)
	}
	defer rows.Close()

	var domains []*model.Domain
	for rows.Next() {
		domain, err := r.scanDomainFromRows(rows)
		if err != nil {
			return nil, 0, err
		}
		domains = append(domains, domain)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating domain rows: %w", err)
	}

	return domains, total, nil
}

// nullableString returns a sql.NullString for optional string fields.
func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}
