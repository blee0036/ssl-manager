package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DashboardStats holds the aggregated statistics for the dashboard.
type DashboardStats struct {
	CertificatesTotal      int  `json:"certificates_total"`
	CertificatesExpiring15d int  `json:"certificates_expiring_15d"`
	CertificatesExpired    int  `json:"certificates_expired"`
	MachinesOnline         int  `json:"machines_online"`
	MachinesOffline        int  `json:"machines_offline"`
	DeployFailures24h      int  `json:"deploy_failures_24h"`
	RenewFailures24h       int  `json:"renew_failures_24h"`
	DomainAnomalies        int  `json:"domain_anomalies"`
	HasAnomalies           bool `json:"has_anomalies"`
}

// DashboardService provides dashboard statistics by querying the database directly.
type DashboardService struct {
	db *sql.DB
}

// NewDashboardService creates a new DashboardService.
func NewDashboardService(db *sql.DB) *DashboardService {
	return &DashboardService{db: db}
}

// GetStats returns aggregated dashboard statistics.
func (s *DashboardService) GetStats(ctx context.Context) (*DashboardStats, error) {
	now := time.Now().UTC()
	stats := &DashboardStats{}

	// 1. Certificate statistics
	if err := s.getCertificateStats(ctx, now, stats); err != nil {
		return nil, fmt.Errorf("failed to get certificate stats: %w", err)
	}

	// 2. Machine statistics
	if err := s.getMachineStats(ctx, stats); err != nil {
		return nil, fmt.Errorf("failed to get machine stats: %w", err)
	}

	// 3. Deployment failures in last 24 hours
	if err := s.getDeployFailures24h(ctx, now, stats); err != nil {
		return nil, fmt.Errorf("failed to get deploy failures: %w", err)
	}

	// 4. Renew failures in last 24 hours
	if err := s.getRenewFailures24h(ctx, now, stats); err != nil {
		return nil, fmt.Errorf("failed to get renew failures: %w", err)
	}

	// 5. Domain anomalies
	if err := s.getDomainAnomalies(ctx, stats); err != nil {
		return nil, fmt.Errorf("failed to get domain anomalies: %w", err)
	}

	// Determine if there are any anomalies that need attention
	stats.HasAnomalies = stats.CertificatesExpiring15d > 0 ||
		stats.CertificatesExpired > 0 ||
		stats.MachinesOffline > 0 ||
		stats.DeployFailures24h > 0 ||
		stats.RenewFailures24h > 0 ||
		stats.DomainAnomalies > 0

	return stats, nil
}

// getCertificateStats queries certificate total, expiring in 15 days, and expired counts.
func (s *DashboardService) getCertificateStats(ctx context.Context, now time.Time, stats *DashboardStats) error {
	// Total certificates
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM certificates").Scan(&stats.CertificatesTotal)
	if err != nil {
		return err
	}

	// Certificates expiring within 15 days (not yet expired)
	nowStr := now.Format(time.RFC3339)
	in15d := now.Add(15 * 24 * time.Hour).Format(time.RFC3339)
	err = s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM certificates WHERE expire_at > ? AND expire_at <= ?",
		nowStr, in15d,
	).Scan(&stats.CertificatesExpiring15d)
	if err != nil {
		return err
	}

	// Expired certificates
	err = s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM certificates WHERE expire_at <= ?",
		nowStr,
	).Scan(&stats.CertificatesExpired)
	if err != nil {
		return err
	}

	return nil
}

// getMachineStats queries online and offline machine counts.
func (s *DashboardService) getMachineStats(ctx context.Context, stats *DashboardStats) error {
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM machines WHERE status = 'online'",
	).Scan(&stats.MachinesOnline)
	if err != nil {
		return err
	}

	err = s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM machines WHERE status = 'offline'",
	).Scan(&stats.MachinesOffline)
	if err != nil {
		return err
	}

	return nil
}

// getDeployFailures24h queries deployment failures in the last 24 hours.
func (s *DashboardService) getDeployFailures24h(ctx context.Context, now time.Time, stats *DashboardStats) error {
	since := now.Add(-24 * time.Hour).Format(time.RFC3339)
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM deployment_logs WHERE status = 'failed' AND created_at >= ?",
		since,
	).Scan(&stats.DeployFailures24h)
	if err != nil {
		return err
	}
	return nil
}

// getRenewFailures24h queries renew failures in the last 24 hours.
// Renew failures are tracked via certificates with renew_status = 'failed' and
// alerts of type containing 'renew' with level 'critical' in the last 24 hours.
func (s *DashboardService) getRenewFailures24h(ctx context.Context, now time.Time, stats *DashboardStats) error {
	since := now.Add(-24 * time.Hour).Format(time.RFC3339)
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM alerts WHERE type LIKE '%renew%' AND level IN ('warning', 'critical') AND created_at >= ?",
		since,
	).Scan(&stats.RenewFailures24h)
	if err != nil {
		return err
	}
	return nil
}

// getDomainAnomalies queries domains with SSL anomalies.
// A domain is anomalous if its latest monitor result shows:
// - TLS handshake failed (tls_success = 0)
// - Domain name mismatch (domain_matched = 0)
// - Certificate fingerprint mismatch with linked certificate (error_message LIKE 'fingerprint mismatch:%')
func (s *DashboardService) getDomainAnomalies(ctx context.Context, stats *DashboardStats) error {
	query := `SELECT COUNT(DISTINCT d.id) FROM domains d
		INNER JOIN domain_monitor_results dmr ON dmr.domain_id = d.id
		WHERE d.monitor_enabled = 1
		AND dmr.id = (
			SELECT id FROM domain_monitor_results
			WHERE domain_id = d.id
			ORDER BY checked_at DESC
			LIMIT 1
		)
		AND (dmr.tls_success = 0 OR dmr.domain_matched = 0 OR dmr.error_message LIKE 'fingerprint mismatch:%')`

	err := s.db.QueryRowContext(ctx, query).Scan(&stats.DomainAnomalies)
	if err != nil {
		return err
	}
	return nil
}
