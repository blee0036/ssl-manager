package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

// CertificateRepository handles certificate metadata CRUD and file storage.
type CertificateRepository struct {
	db      *sql.DB
	dataDir string // base data directory (e.g., "./data")
}

// NewCertificateRepository creates a new CertificateRepository.
func NewCertificateRepository(db *sql.DB, dataDir string) *CertificateRepository {
	return &CertificateRepository{
		db:      db,
		dataDir: dataDir,
	}
}

// CertDirPath returns the path to a certificate's file directory.
func (r *CertificateRepository) CertDirPath(id string) string {
	return filepath.Join(r.dataDir, "certificates", id)
}

// Create creates a new certificate metadata record and its file directory.
func (r *CertificateRepository) Create(ctx context.Context, cert *model.Certificate) error {
	if cert.ID == "" {
		cert.ID = uuid.New().String()
	}

	now := time.Now().UTC()
	cert.CreatedAt = now
	cert.UpdatedAt = now
	cert.CertDirPath = filepath.Join("certificates", cert.ID)

	domainsJSON, err := json.Marshal(cert.Domains)
	if err != nil {
		return fmt.Errorf("failed to marshal domains: %w", err)
	}

	var lastRenewAt *string
	if cert.LastRenewAt != nil {
		s := cert.LastRenewAt.UTC().Format(time.RFC3339)
		lastRenewAt = &s
	}

	query := `INSERT INTO certificates (
		id, name, domains, source, expire_at, auto_renew, issuer,
		fingerprint_sha256, chain_valid, cert_dir_path, thirdpart_dns_id,
		last_renew_at, renew_status, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = r.db.ExecContext(ctx, query,
		cert.ID,
		cert.Name,
		string(domainsJSON),
		cert.Source,
		cert.ExpireAt.UTC().Format(time.RFC3339),
		boolToInt(cert.AutoRenew),
		cert.Issuer,
		cert.FingerprintSHA256,
		boolToInt(cert.ChainValid),
		cert.CertDirPath,
		cert.ThirdpartDNSID,
		lastRenewAt,
		cert.RenewStatus,
		cert.CreatedAt.UTC().Format(time.RFC3339),
		cert.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to insert certificate: %w", err)
	}

	// Create the certificate file directory
	dirPath := r.CertDirPath(cert.ID)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		if _, cleanupErr := r.db.ExecContext(ctx, "DELETE FROM certificates WHERE id = ?", cert.ID); cleanupErr != nil {
			return fmt.Errorf("failed to create certificate directory: %w; failed to remove certificate record: %v", err, cleanupErr)
		}
		return fmt.Errorf("failed to create certificate directory: %w", err)
	}

	return nil
}

// GetByID retrieves certificate metadata by ID.
func (r *CertificateRepository) GetByID(ctx context.Context, id string) (*model.Certificate, error) {
	query := `SELECT id, name, domains, source, expire_at, auto_renew, issuer,
		fingerprint_sha256, chain_valid, cert_dir_path, thirdpart_dns_id,
		last_renew_at, renew_status, created_at, updated_at
	FROM certificates WHERE id = ?`

	row := r.db.QueryRowContext(ctx, query, id)
	return r.scanCertificate(row)
}

// List returns certificates with optional filtering.
func (r *CertificateRepository) List(ctx context.Context, filter model.CertFilter) ([]*model.Certificate, error) {
	query := `SELECT id, name, domains, source, expire_at, auto_renew, issuer,
		fingerprint_sha256, chain_valid, cert_dir_path, thirdpart_dns_id,
		last_renew_at, renew_status, created_at, updated_at
	FROM certificates WHERE 1=1`

	var args []interface{}

	if filter.Source != "" {
		query += " AND source = ?"
		args = append(args, filter.Source)
	}

	if filter.AutoRenew != nil {
		query += " AND auto_renew = ?"
		args = append(args, boolToInt(*filter.AutoRenew))
	}

	if filter.ExpiringSoon {
		// Default to 15 days for expiring soon filter
		deadline := time.Now().UTC().Add(15 * 24 * time.Hour).Format(time.RFC3339)
		query += " AND expire_at <= ?"
		args = append(args, deadline)
	}

	query += " ORDER BY created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query certificates: %w", err)
	}
	defer rows.Close()

	var certs []*model.Certificate
	for rows.Next() {
		cert, err := r.scanCertificateFromRows(rows)
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating certificate rows: %w", err)
	}

	return certs, nil
}

// Update updates certificate metadata fields.
func (r *CertificateRepository) Update(ctx context.Context, id string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	// Always update updated_at
	updates["updated_at"] = time.Now().UTC().Format(time.RFC3339)

	setClauses := ""
	var args []interface{}

	for key, value := range updates {
		if setClauses != "" {
			setClauses += ", "
		}
		setClauses += key + " = ?"

		switch key {
		case "domains":
			// domains should be stored as JSON array
			if domains, ok := value.([]string); ok {
				domainsJSON, err := json.Marshal(domains)
				if err != nil {
					return fmt.Errorf("failed to marshal domains: %w", err)
				}
				args = append(args, string(domainsJSON))
			} else {
				args = append(args, value)
			}
		case "auto_renew", "chain_valid":
			if b, ok := value.(bool); ok {
				args = append(args, boolToInt(b))
			} else {
				args = append(args, value)
			}
		case "expire_at":
			if t, ok := value.(time.Time); ok {
				args = append(args, t.UTC().Format(time.RFC3339))
			} else {
				args = append(args, value)
			}
		case "last_renew_at":
			if t, ok := value.(*time.Time); ok && t != nil {
				args = append(args, t.UTC().Format(time.RFC3339))
			} else if t, ok := value.(time.Time); ok {
				args = append(args, t.UTC().Format(time.RFC3339))
			} else {
				args = append(args, value)
			}
		default:
			args = append(args, value)
		}
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE certificates SET %s WHERE id = ?", setClauses)

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update certificate: %w", err)
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

// Delete deletes certificate metadata and its file directory.
func (r *CertificateRepository) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, "DELETE FROM certificates WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete certificate: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	// Remove the certificate file directory
	if err := r.DeleteCertFiles(id); err != nil {
		return fmt.Errorf("failed to delete certificate files: %w", err)
	}

	return nil
}

// SaveCertFiles saves certificate PEM files to the certificate directory.
func (r *CertificateRepository) SaveCertFiles(id string, certPEM, chainPEM, fullchainPEM, privkeyPEM []byte) error {
	dirPath := r.CertDirPath(id)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("failed to create certificate directory: %w", err)
	}

	// Write cert files with 0644 permissions
	files := map[string][]byte{
		"cert.pem":      certPEM,
		"chain.pem":     chainPEM,
		"fullchain.pem": fullchainPEM,
	}

	for name, data := range files {
		if data == nil {
			continue
		}
		path := filepath.Join(dirPath, name)
		if err := os.WriteFile(path, data, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	// Write private key with 0600 permissions
	if privkeyPEM != nil {
		path := filepath.Join(dirPath, "privkey.pem")
		if err := os.WriteFile(path, privkeyPEM, 0600); err != nil {
			return fmt.Errorf("failed to write privkey.pem: %w", err)
		}
	}

	return nil
}

// ReadCertFiles reads certificate PEM files from the certificate directory.
func (r *CertificateRepository) ReadCertFiles(id string) (certPEM, chainPEM, fullchainPEM, privkeyPEM []byte, err error) {
	dirPath := r.CertDirPath(id)

	certPEM, err = os.ReadFile(filepath.Join(dirPath, "cert.pem"))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, nil, nil, fmt.Errorf("failed to read cert.pem: %w", err)
	}

	chainPEM, err = os.ReadFile(filepath.Join(dirPath, "chain.pem"))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, nil, nil, fmt.Errorf("failed to read chain.pem: %w", err)
	}

	fullchainPEM, err = os.ReadFile(filepath.Join(dirPath, "fullchain.pem"))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, nil, nil, fmt.Errorf("failed to read fullchain.pem: %w", err)
	}

	privkeyPEM, err = os.ReadFile(filepath.Join(dirPath, "privkey.pem"))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, nil, nil, fmt.Errorf("failed to read privkey.pem: %w", err)
	}

	return certPEM, chainPEM, fullchainPEM, privkeyPEM, nil
}

// DeleteCertFiles removes the certificate file directory.
func (r *CertificateRepository) DeleteCertFiles(id string) error {
	dirPath := r.CertDirPath(id)
	if err := os.RemoveAll(dirPath); err != nil {
		return fmt.Errorf("failed to remove certificate directory: %w", err)
	}
	return nil
}

// ListExpiringSoon returns certificates expiring within the given number of days.
func (r *CertificateRepository) ListExpiringSoon(ctx context.Context, days int) ([]*model.Certificate, error) {
	deadline := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)

	query := `SELECT id, name, domains, source, expire_at, auto_renew, issuer,
		fingerprint_sha256, chain_valid, cert_dir_path, thirdpart_dns_id,
		last_renew_at, renew_status, created_at, updated_at
	FROM certificates WHERE expire_at <= ? ORDER BY expire_at ASC`

	rows, err := r.db.QueryContext(ctx, query, deadline)
	if err != nil {
		return nil, fmt.Errorf("failed to query expiring certificates: %w", err)
	}
	defer rows.Close()

	var certs []*model.Certificate
	for rows.Next() {
		cert, err := r.scanCertificateFromRows(rows)
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating certificate rows: %w", err)
	}

	return certs, nil
}

// ListFailedAutoRenewal returns Cloudflare DNS certificates whose previous
// automatic renewal failed at or before retryBefore. Their updated_at value is
// written together with the failed status and therefore acts as the persisted
// timestamp for the once-per-day retry schedule.
func (r *CertificateRepository) ListFailedAutoRenewal(ctx context.Context, retryBefore time.Time) ([]*model.Certificate, error) {
	query := `SELECT id, name, domains, source, expire_at, auto_renew, issuer,
		fingerprint_sha256, chain_valid, cert_dir_path, thirdpart_dns_id,
		last_renew_at, renew_status, created_at, updated_at
	FROM certificates
	WHERE auto_renew = 1
		AND source = 'certbot_cloudflare_dns'
		AND (renew_status = 'failed' OR renew_status LIKE 'failed:%')
		AND updated_at <= ?
	ORDER BY updated_at ASC`

	rows, err := r.db.QueryContext(ctx, query, retryBefore.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("failed to query failed auto-renewal certificates: %w", err)
	}
	defer rows.Close()

	var certs []*model.Certificate
	for rows.Next() {
		cert, err := r.scanCertificateFromRows(rows)
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating failed auto-renewal certificate rows: %w", err)
	}

	return certs, nil
}

// scanCertificate scans a single row into a Certificate model.
func (r *CertificateRepository) scanCertificate(row *sql.Row) (*model.Certificate, error) {
	var cert model.Certificate
	var domainsJSON string
	var expireAt, createdAt, updatedAt string
	var lastRenewAt *string
	var autoRenew, chainValid int

	err := row.Scan(
		&cert.ID,
		&cert.Name,
		&domainsJSON,
		&cert.Source,
		&expireAt,
		&autoRenew,
		&cert.Issuer,
		&cert.FingerprintSHA256,
		&chainValid,
		&cert.CertDirPath,
		&cert.ThirdpartDNSID,
		&lastRenewAt,
		&cert.RenewStatus,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to scan certificate: %w", err)
	}

	return r.populateCertificate(&cert, domainsJSON, expireAt, createdAt, updatedAt, lastRenewAt, autoRenew, chainValid)
}

// scanCertificateFromRows scans a row from sql.Rows into a Certificate model.
func (r *CertificateRepository) scanCertificateFromRows(rows *sql.Rows) (*model.Certificate, error) {
	var cert model.Certificate
	var domainsJSON string
	var expireAt, createdAt, updatedAt string
	var lastRenewAt *string
	var autoRenew, chainValid int

	err := rows.Scan(
		&cert.ID,
		&cert.Name,
		&domainsJSON,
		&cert.Source,
		&expireAt,
		&autoRenew,
		&cert.Issuer,
		&cert.FingerprintSHA256,
		&chainValid,
		&cert.CertDirPath,
		&cert.ThirdpartDNSID,
		&lastRenewAt,
		&cert.RenewStatus,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan certificate row: %w", err)
	}

	return r.populateCertificate(&cert, domainsJSON, expireAt, createdAt, updatedAt, lastRenewAt, autoRenew, chainValid)
}

// populateCertificate fills in parsed fields on a Certificate.
func (r *CertificateRepository) populateCertificate(
	cert *model.Certificate,
	domainsJSON, expireAt, createdAt, updatedAt string,
	lastRenewAt *string,
	autoRenew, chainValid int,
) (*model.Certificate, error) {
	// Parse domains JSON
	if err := json.Unmarshal([]byte(domainsJSON), &cert.Domains); err != nil {
		return nil, fmt.Errorf("failed to unmarshal domains: %w", err)
	}

	// Parse time fields
	var err error
	cert.ExpireAt, err = time.Parse(time.RFC3339, expireAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse expire_at: %w", err)
	}

	cert.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at: %w", err)
	}

	cert.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse updated_at: %w", err)
	}

	if lastRenewAt != nil {
		t, err := time.Parse(time.RFC3339, *lastRenewAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse last_renew_at: %w", err)
		}
		cert.LastRenewAt = &t
	}

	cert.AutoRenew = autoRenew == 1
	cert.ChainValid = chainValid == 1

	return cert, nil
}
