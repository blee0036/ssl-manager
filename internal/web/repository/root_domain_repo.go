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

// rootDomainColumns is the canonical column list for root_domains SELECTs.
// days_remaining is intentionally NOT included: it is a non-persistent field
// computed at read time by the service layer.
const rootDomainColumns = `id, name, source, registrable_domain, expiry_date, last_checked_at,
	last_status, last_error, monitor_enabled, alert_ignored, created_at, updated_at`

// rootDomainSortByWhitelist maps allowed sort_by values to safe SQL expressions.
// Aligns with DomainRepository.sortByWhitelist conventions.
var rootDomainSortByWhitelist = map[string]string{
	"name":            "LOWER(RTRIM(name, '.'))",
	"source":          "source",
	"expiry_date":     "COALESCE(strftime('%s', expiry_date), 0)",
	"last_checked_at": "COALESCE(strftime('%s', last_checked_at), 0)",
	"created_at":      "COALESCE(strftime('%s', created_at), 0)",
}

// RootDomainRepository handles root domain (registration expiry) CRUD, listing,
// and WHOIS result persistence. It is fully independent of DomainRepository
// (TLS certificate monitoring).
type RootDomainRepository struct {
	db *sql.DB
}

// NewRootDomainRepository creates a new RootDomainRepository.
func NewRootDomainRepository(db *sql.DB) *RootDomainRepository {
	return &RootDomainRepository{db: db}
}

// Create inserts a new root domain record. It generates a uuid when ID is empty,
// defaults source to "manual", and stamps created_at/updated_at with UTC now.
// Relies on the UNIQUE index on registrable_domain to enforce dedup.
func (r *RootDomainRepository) Create(ctx context.Context, rd *model.RootDomain) error {
	if rd.ID == "" {
		rd.ID = uuid.New().String()
	}

	now := time.Now().UTC()
	rd.CreatedAt = now
	rd.UpdatedAt = now

	if rd.Source == "" {
		rd.Source = "manual"
	}

	query := `INSERT INTO root_domains (
		id, name, source, registrable_domain, expiry_date, last_checked_at,
		last_status, last_error, monitor_enabled, alert_ignored, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, query,
		rd.ID,
		rd.Name,
		rd.Source,
		rd.RegistrableDomain,
		nullableTimeRFC3339(rd.ExpiryDate),
		nullableTimeRFC3339(rd.LastCheckedAt),
		rd.LastStatus,
		rd.LastError,
		boolToInt(rd.MonitorEnabled),
		boolToInt(rd.AlertIgnored),
		rd.CreatedAt.Format(time.RFC3339),
		rd.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to insert root domain: %w", err)
	}

	return nil
}

// CreateIfNotExists atomically inserts a new root domain unless one with the same
// registrable_domain already exists, reporting whether a NEW row was created.
//
// It is the race-safe, idempotent dedup primitive for the root-domain write paths
// (manual add / Cloudflare import / DNS-cadence reconcile). A plain
// "GetByRegistrableDomain then Create" sequence has a check-then-act race: two
// concurrent callers (e.g. two imports, or an import racing the reconcile) can
// both miss, then one insert wins and the other trips the UNIQUE index on
// registrable_domain. This method instead performs a single
// `INSERT ... ON CONFLICT(registrable_domain) DO NOTHING`, so a conflict is NOT an
// error — it simply means the registrable domain was already present. In that case
// the existing row is left untouched and the method returns (created=false,
// err=nil). Only a genuine DB failure returns a non-nil error.
//
// Field handling mirrors Create exactly: a uuid is generated when ID is empty,
// created_at/updated_at are stamped with UTC now, source defaults to "manual", the
// two bool flags are stored as INTEGER, and the two nullable times as nullable
// RFC3339 strings.
func (r *RootDomainRepository) CreateIfNotExists(ctx context.Context, rd *model.RootDomain) (bool, error) {
	if rd.ID == "" {
		rd.ID = uuid.New().String()
	}

	now := time.Now().UTC()
	rd.CreatedAt = now
	rd.UpdatedAt = now

	if rd.Source == "" {
		rd.Source = "manual"
	}

	query := `INSERT INTO root_domains (
		id, name, source, registrable_domain, expiry_date, last_checked_at,
		last_status, last_error, monitor_enabled, alert_ignored, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(registrable_domain) DO NOTHING`

	result, err := r.db.ExecContext(ctx, query,
		rd.ID,
		rd.Name,
		rd.Source,
		rd.RegistrableDomain,
		nullableTimeRFC3339(rd.ExpiryDate),
		nullableTimeRFC3339(rd.LastCheckedAt),
		rd.LastStatus,
		rd.LastError,
		boolToInt(rd.MonitorEnabled),
		boolToInt(rd.AlertIgnored),
		rd.CreatedAt.Format(time.RFC3339),
		rd.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return false, fmt.Errorf("failed to insert root domain: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to get rows affected: %w", err)
	}

	// rowsAffected == 0 means the ON CONFLICT(registrable_domain) DO NOTHING clause
	// skipped the insert because the registrable domain already existed. That is the
	// idempotent "already exists" outcome (created=false), not an error.
	return rowsAffected > 0, nil
}

// GetByID retrieves a root domain by ID. Returns sql.ErrNoRows when not found.
func (r *RootDomainRepository) GetByID(ctx context.Context, id string) (*model.RootDomain, error) {
	query := "SELECT " + rootDomainColumns + " FROM root_domains WHERE id = ?"
	row := r.db.QueryRowContext(ctx, query, id)
	return r.scanRootDomain(row)
}

// GetByRegistrableDomain retrieves a root domain by its registrable domain
// (eTLD+1). Returns sql.ErrNoRows when not found. Used for dedup checks.
func (r *RootDomainRepository) GetByRegistrableDomain(ctx context.Context, registrable string) (*model.RootDomain, error) {
	query := "SELECT " + rootDomainColumns + " FROM root_domains WHERE registrable_domain = ?"
	row := r.db.QueryRowContext(ctx, query, registrable)
	return r.scanRootDomain(row)
}

// Update applies a partial update to a root domain record. It always refreshes
// updated_at and converts boolean flags to INTEGER for SQLite storage.
// Returns sql.ErrNoRows when no row matches the id.
func (r *RootDomainRepository) Update(ctx context.Context, id string, updates map[string]interface{}) error {
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
	query := fmt.Sprintf("UPDATE root_domains SET %s WHERE id = ?", setClauses)

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update root domain: %w", err)
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

// Delete removes a root domain record (and, inherently, its inlined expiry data).
// Returns sql.ErrNoRows when no row matches the id.
func (r *RootDomainRepository) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, "DELETE FROM root_domains WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete root domain: %w", err)
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

// List returns root domains matching the given simple filter, newest first.
// Used by reconcile and internal enumeration.
func (r *RootDomainRepository) List(ctx context.Context, filter model.RootDomainFilter) ([]*model.RootDomain, error) {
	query := "SELECT " + rootDomainColumns + " FROM root_domains WHERE 1=1"

	var args []interface{}

	if filter.Source != "" {
		query += " AND source = ?"
		args = append(args, filter.Source)
	}

	if filter.MonitorEnabled != nil {
		query += " AND monitor_enabled = ?"
		args = append(args, boolToInt(*filter.MonitorEnabled))
	}

	query += " ORDER BY created_at DESC, id ASC"

	return r.queryRootDomains(ctx, query, args...)
}

// ListEnabled returns all root domains with monitor_enabled = 1, used by the
// periodic expiry refresh scheduler.
func (r *RootDomainRepository) ListEnabled(ctx context.Context) ([]*model.RootDomain, error) {
	query := "SELECT " + rootDomainColumns + " FROM root_domains WHERE monitor_enabled = 1 ORDER BY created_at ASC, id ASC"
	return r.queryRootDomains(ctx, query)
}

// ListWithSort returns root domains with server-side filtering, whitelist sorting
// and pagination, plus the total count (before pagination).
// thresholdDays drives the expiring/ok filter_status predicates; it is supplied
// by the service from the runtime global config.
func (r *RootDomainRepository) ListWithSort(ctx context.Context, params model.RootDomainListParams, thresholdDays int) ([]*model.RootDomain, int, error) {
	if thresholdDays <= 0 {
		thresholdDays = 14
	}

	whereClause, args := buildRootDomainWhereClause(params, thresholdDays)

	// COUNT query (before pagination)
	countSQL := "SELECT COUNT(*) FROM root_domains WHERE " + whereClause
	var total int
	if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count root domains: %w", err)
	}

	orderByClause := buildRootDomainOrderByClause(params.SortBy, params.SortOrder)

	// Pagination defaults (aligned with DomainRepository.ListWithSort)
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

	dataSQL := fmt.Sprintf("SELECT %s FROM root_domains WHERE %s %s LIMIT ? OFFSET ?",
		rootDomainColumns, whereClause, orderByClause)

	dataArgs := make([]interface{}, len(args))
	copy(dataArgs, args)
	dataArgs = append(dataArgs, perPage, offset)

	rootDomains, err := r.queryRootDomains(ctx, dataSQL, dataArgs...)
	if err != nil {
		return nil, 0, err
	}

	return rootDomains, total, nil
}

// SaveExpiryResult persists a WHOIS check outcome by inlining it into the
// root_domains row. When expiry is nil (a failed check), expiry_date is left
// untouched so the previously known value is preserved (requirements 4.5/4.6/7.2);
// only last_checked_at/last_status/last_error/updated_at are updated.
// Returns sql.ErrNoRows when no row matches the id.
func (r *RootDomainRepository) SaveExpiryResult(ctx context.Context, id string, expiry *time.Time, checkedAt time.Time, status, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	checkedAtStr := checkedAt.UTC().Format(time.RFC3339)

	var query string
	var args []interface{}

	if expiry == nil {
		query = `UPDATE root_domains SET last_checked_at = ?, last_status = ?, last_error = ?, updated_at = ? WHERE id = ?`
		args = []interface{}{checkedAtStr, status, errMsg, now, id}
	} else {
		query = `UPDATE root_domains SET expiry_date = ?, last_checked_at = ?, last_status = ?, last_error = ?, updated_at = ? WHERE id = ?`
		args = []interface{}{expiry.UTC().Format(time.RFC3339), checkedAtStr, status, errMsg, now, id}
	}

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to save expiry result: %w", err)
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

// queryRootDomains runs a SELECT (using rootDomainColumns) and scans all rows.
func (r *RootDomainRepository) queryRootDomains(ctx context.Context, query string, args ...interface{}) ([]*model.RootDomain, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query root domains: %w", err)
	}
	defer rows.Close()

	var rootDomains []*model.RootDomain
	for rows.Next() {
		rd, err := r.scanRootDomainFromRows(rows)
		if err != nil {
			return nil, err
		}
		rootDomains = append(rootDomains, rd)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating root domain rows: %w", err)
	}

	return rootDomains, nil
}

// scanRootDomain scans a single row into a RootDomain model.
func (r *RootDomainRepository) scanRootDomain(row *sql.Row) (*model.RootDomain, error) {
	var rd model.RootDomain
	var expiryDate, lastCheckedAt sql.NullString
	var createdAt, updatedAt string
	var monitorEnabled, alertIgnored int

	err := row.Scan(
		&rd.ID,
		&rd.Name,
		&rd.Source,
		&rd.RegistrableDomain,
		&expiryDate,
		&lastCheckedAt,
		&rd.LastStatus,
		&rd.LastError,
		&monitorEnabled,
		&alertIgnored,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to scan root domain: %w", err)
	}

	return r.populateRootDomain(&rd, expiryDate, lastCheckedAt, createdAt, updatedAt, monitorEnabled, alertIgnored)
}

// scanRootDomainFromRows scans a row from sql.Rows into a RootDomain model.
func (r *RootDomainRepository) scanRootDomainFromRows(rows *sql.Rows) (*model.RootDomain, error) {
	var rd model.RootDomain
	var expiryDate, lastCheckedAt sql.NullString
	var createdAt, updatedAt string
	var monitorEnabled, alertIgnored int

	err := rows.Scan(
		&rd.ID,
		&rd.Name,
		&rd.Source,
		&rd.RegistrableDomain,
		&expiryDate,
		&lastCheckedAt,
		&rd.LastStatus,
		&rd.LastError,
		&monitorEnabled,
		&alertIgnored,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan root domain row: %w", err)
	}

	return r.populateRootDomain(&rd, expiryDate, lastCheckedAt, createdAt, updatedAt, monitorEnabled, alertIgnored)
}

// populateRootDomain parses the string/int columns onto a RootDomain, normalizing
// timestamps to UTC (round-trip consistency, requirements 4.3/4.7).
func (r *RootDomainRepository) populateRootDomain(
	rd *model.RootDomain,
	expiryDate, lastCheckedAt sql.NullString,
	createdAt, updatedAt string,
	monitorEnabled, alertIgnored int,
) (*model.RootDomain, error) {
	if expiryDate.Valid {
		t, err := time.Parse(time.RFC3339, expiryDate.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse expiry_date: %w", err)
		}
		tu := t.UTC()
		rd.ExpiryDate = &tu
	}

	if lastCheckedAt.Valid {
		t, err := time.Parse(time.RFC3339, lastCheckedAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse last_checked_at: %w", err)
		}
		tu := t.UTC()
		rd.LastCheckedAt = &tu
	}

	var err error
	rd.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at: %w", err)
	}

	rd.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse updated_at: %w", err)
	}

	rd.MonitorEnabled = monitorEnabled != 0
	rd.AlertIgnored = alertIgnored != 0

	return rd, nil
}

// buildRootDomainWhereClause constructs the WHERE clause and args from
// RootDomainListParams. All conditions are AND-combined; empty conditions
// yield "1=1". thresholdDays drives the expiring/ok status predicates.
func buildRootDomainWhereClause(params model.RootDomainListParams, thresholdDays int) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	if params.Name != "" {
		conditions = append(conditions, "name LIKE ?")
		args = append(args, "%"+params.Name+"%")
	}
	if params.Source != "" {
		conditions = append(conditions, "source = ?")
		args = append(args, params.Source)
	}
	if params.MonitorEnabled != nil {
		if *params.MonitorEnabled {
			conditions = append(conditions, "monitor_enabled = 1")
		} else {
			conditions = append(conditions, "monitor_enabled = 0")
		}
	}
	if params.AlertIgnored != nil {
		if *params.AlertIgnored {
			conditions = append(conditions, "alert_ignored = 1")
		} else {
			conditions = append(conditions, "alert_ignored = 0")
		}
	}

	if predicate, ok := rootDomainStatusPredicate(params.FilterStatus, thresholdDays); ok {
		conditions = append(conditions, predicate)
	}

	if len(conditions) == 0 {
		return "1=1", args
	}
	return strings.Join(conditions, " AND "), args
}

// rootDomainStatusPredicate returns the SQL predicate for a filter_status value.
//
// The expired/expiring/ok predicates MIRROR the service's truncated-toward-zero
// days_remaining tiering (computeDaysRemaining = int((expiry-now)/24h)), which is
// the same contract the alerts (Property 8) and the frontend (expiryState.ts) use.
// They must NOT compare the raw timestamp against "now": that disagrees with the
// truncated day count near day boundaries (e.g. an expiry 12h out has
// days_remaining==0 and is "expired" to the UI/alerts, but a raw ">now" test would
// wrongly classify it as "expiring"). Instead we partition the timeline at the +1
// day and +(threshold+1) days boundaries, which is exactly where int((expiry-now)/24h)
// changes tier (SQLite '+N days' adds N*86400s in UTC):
//
//	days_remaining <= 0             ⟺ (expiry - now) <  1 day
//	0 < days_remaining <= threshold ⟺ 1 day <= (expiry - now) < (threshold+1) days
//	days_remaining >  threshold     ⟺ (expiry - now) >= (threshold+1) days
//
// thresholdDays is an int (injection-safe) embedded into the SQLite date modifier.
// Returns ("", false) for unrecognized statuses.
func rootDomainStatusPredicate(status string, thresholdDays int) (string, bool) {
	switch status {
	case "enabled":
		return "monitor_enabled = 1", true
	case "disabled":
		return "monitor_enabled = 0", true
	case "ignored":
		return "alert_ignored = 1", true
	case "unknown":
		return "expiry_date IS NULL", true
	case "expired":
		// days_remaining <= 0  ⟺  (expiry - now) < 1 day
		return "expiry_date IS NOT NULL AND strftime('%s', expiry_date) < strftime('%s', 'now', '+1 days')", true
	case "expiring":
		// 0 < days_remaining <= thresholdDays  ⟺  1 day <= (expiry - now) < (thresholdDays+1) days
		return fmt.Sprintf(
			"expiry_date IS NOT NULL AND strftime('%%s', expiry_date) >= strftime('%%s', 'now', '+1 days') AND strftime('%%s', expiry_date) < strftime('%%s', 'now', '+%d days')",
			thresholdDays+1,
		), true
	case "ok":
		// days_remaining > thresholdDays  ⟺  (expiry - now) >= (thresholdDays+1) days
		return fmt.Sprintf(
			"expiry_date IS NOT NULL AND strftime('%%s', expiry_date) >= strftime('%%s', 'now', '+%d days')",
			thresholdDays+1,
		), true
	default:
		return "", false
	}
}

// buildRootDomainOrderByClause returns the ORDER BY clause for a given sort_by
// and sort_order. Unknown sort_by falls back to newest-first. A stable id tie
// breaker is always appended for deterministic pagination.
func buildRootDomainOrderByClause(sortBy, sortOrder string) string {
	expr, ok := rootDomainSortByWhitelist[sortBy]
	if !ok {
		return "ORDER BY created_at DESC, id ASC"
	}
	direction := "ASC"
	if strings.ToLower(sortOrder) == "desc" {
		direction = "DESC"
	}
	return fmt.Sprintf("ORDER BY %s %s, id ASC", expr, direction)
}

// nullableTimeRFC3339 converts an optional time to a nullable RFC3339 string
// (normalized to UTC) for storage. Nil yields SQL NULL.
func nullableTimeRFC3339(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: t.UTC().Format(time.RFC3339), Valid: true}
}
