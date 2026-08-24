package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

// setupDomainTestDB creates a test DB with the domains and domain_monitor_results tables.
func setupDomainTestDB(t *testing.T) *DomainRepository {
	t.Helper()
	db := setupTestDB(t)

	// Add domains table
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS domains (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		source TEXT DEFAULT 'manual' CHECK(source IN ('manual', 'certificate', 'cloudflare')),
		thirdpart_dns_id TEXT DEFAULT '',
		dns_record_id TEXT DEFAULT '',
		dns_record_type TEXT DEFAULT '',
		dns_record_value TEXT DEFAULT '',
		monitor_port INTEGER NOT NULL DEFAULT 443,
		linked_machine_id TEXT,
		linked_certificate_id TEXT,
		linked_machine_certificate_id TEXT,
		monitor_enabled INTEGER NOT NULL DEFAULT 1,
		alert_ignored INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create domains table: %v", err)
	}

	// Add domain_monitor_results table
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS domain_monitor_results (
		id TEXT PRIMARY KEY,
		domain_id TEXT NOT NULL REFERENCES domains(id),
		checked_port INTEGER NOT NULL,
		resolved_ips TEXT DEFAULT '',
		tls_success INTEGER NOT NULL DEFAULT 0,
		certificate_fingerprint_sha256 TEXT DEFAULT '',
		issuer TEXT DEFAULT '',
		expire_at TEXT,
		days_remaining INTEGER,
		domain_matched INTEGER NOT NULL DEFAULT 0,
		chain_valid INTEGER NOT NULL DEFAULT 0,
		error_message TEXT DEFAULT '',
		checked_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create domain_monitor_results table: %v", err)
	}

	return NewDomainRepository(db)
}

func newTestDomain() *model.Domain {
	return &model.Domain{
		Name:           "example.com",
		Source:         "manual",
		MonitorPort:    443,
		MonitorEnabled: true,
	}
}

func TestDomainRepo_Create(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	domain := newTestDomain()
	err := repo.Create(ctx, domain)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if domain.ID == "" {
		t.Fatal("expected ID to be generated")
	}
	if domain.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
	if domain.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set")
	}
}

func TestDomainRepo_Create_WithLinkedFields(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	domain := &model.Domain{
		Name:                       "test.example.com",
		Source:                     "manual",
		MonitorPort:                8443,
		LinkedMachineID:            "machine-1",
		LinkedCertificateID:        "cert-1",
		LinkedMachineCertificateID: "mc-1",
		MonitorEnabled:             true,
	}

	err := repo.Create(ctx, domain)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Retrieve and verify
	got, err := repo.GetByID(ctx, domain.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if got.LinkedMachineID != "machine-1" {
		t.Errorf("expected LinkedMachineID 'machine-1', got '%s'", got.LinkedMachineID)
	}
	if got.LinkedCertificateID != "cert-1" {
		t.Errorf("expected LinkedCertificateID 'cert-1', got '%s'", got.LinkedCertificateID)
	}
	if got.LinkedMachineCertificateID != "mc-1" {
		t.Errorf("expected LinkedMachineCertificateID 'mc-1', got '%s'", got.LinkedMachineCertificateID)
	}
}

func TestDomainRepo_GetByID(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	domain := newTestDomain()
	if err := repo.Create(ctx, domain); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByID(ctx, domain.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if got.ID != domain.ID {
		t.Errorf("expected ID %s, got %s", domain.ID, got.ID)
	}
	if got.Name != "example.com" {
		t.Errorf("expected Name 'example.com', got '%s'", got.Name)
	}
	if got.MonitorPort != 443 {
		t.Errorf("expected MonitorPort 443, got %d", got.MonitorPort)
	}
	if !got.MonitorEnabled {
		t.Error("expected MonitorEnabled to be true")
	}
}

func TestDomainRepo_GetByID_NotFound(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent-id")
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestDomainRepo_List(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	// Create multiple domains
	d1 := &model.Domain{Name: "a.example.com", Source: "manual", MonitorPort: 443, MonitorEnabled: true}
	d2 := &model.Domain{Name: "b.example.com", Source: "cloudflare", MonitorPort: 443, MonitorEnabled: false}
	d3 := &model.Domain{Name: "c.example.com", Source: "manual", MonitorPort: 8443, MonitorEnabled: true}

	for _, d := range []*model.Domain{d1, d2, d3} {
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	// List all
	domains, err := repo.List(ctx, model.DomainFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(domains) != 3 {
		t.Fatalf("expected 3 domains, got %d", len(domains))
	}

	// Filter by source
	domains, err = repo.List(ctx, model.DomainFilter{Source: "manual"})
	if err != nil {
		t.Fatalf("List with source filter failed: %v", err)
	}
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains with source=manual, got %d", len(domains))
	}

	// Filter by monitor_enabled
	enabled := true
	domains, err = repo.List(ctx, model.DomainFilter{MonitorEnabled: &enabled})
	if err != nil {
		t.Fatalf("List with monitor_enabled filter failed: %v", err)
	}
	if len(domains) != 2 {
		t.Fatalf("expected 2 enabled domains, got %d", len(domains))
	}
}

func TestDomainRepo_Update(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	domain := newTestDomain()
	if err := repo.Create(ctx, domain); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Update monitor port and enabled status
	updates := map[string]interface{}{
		"monitor_port":    8443,
		"monitor_enabled": false,
	}

	if err := repo.Update(ctx, domain.ID, updates); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, err := repo.GetByID(ctx, domain.ID)
	if err != nil {
		t.Fatalf("GetByID after update failed: %v", err)
	}
	if got.MonitorPort != 8443 {
		t.Errorf("expected MonitorPort 8443, got %d", got.MonitorPort)
	}
	if got.MonitorEnabled {
		t.Error("expected MonitorEnabled to be false after update")
	}
}

func TestDomainRepo_Update_NotFound(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	err := repo.Update(ctx, "nonexistent-id", map[string]interface{}{"monitor_port": 8443})
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestDomainRepo_Delete(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	domain := newTestDomain()
	if err := repo.Create(ctx, domain); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := repo.Delete(ctx, domain.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err := repo.GetByID(ctx, domain.ID)
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows after delete, got %v", err)
	}
}

func TestDomainRepo_Delete_NotFound(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	err := repo.Delete(ctx, "nonexistent-id")
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestDomainRepo_Delete_CascadesMonitorResults(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	domain := newTestDomain()
	if err := repo.Create(ctx, domain); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Save a monitor result
	result := &model.DomainMonitorResult{
		DomainID:    domain.ID,
		CheckedPort: 443,
		TLSSuccess:  true,
		CheckedAt:   time.Now().UTC(),
	}
	if err := repo.SaveMonitorResult(ctx, result); err != nil {
		t.Fatalf("SaveMonitorResult failed: %v", err)
	}

	// Delete domain should also delete results
	if err := repo.Delete(ctx, domain.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify monitor result is gone
	_, err := repo.GetLatestMonitorResult(ctx, domain.ID)
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows for monitor result after domain delete, got %v", err)
	}
}

func TestDomainRepo_SaveMonitorResult(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	domain := newTestDomain()
	if err := repo.Create(ctx, domain); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	expireAt := time.Now().Add(90 * 24 * time.Hour).UTC().Truncate(time.Second)
	daysRemaining := 90

	result := &model.DomainMonitorResult{
		DomainID:                     domain.ID,
		CheckedPort:                  443,
		ResolvedIPs:                  []string{"1.2.3.4", "5.6.7.8"},
		TLSSuccess:                   true,
		CertificateFingerprintSHA256: "abc123def456",
		Issuer:                       "Let's Encrypt",
		ExpireAt:                     &expireAt,
		DaysRemaining:                &daysRemaining,
		DomainMatched:                true,
		ChainValid:                   true,
		CheckedAt:                    time.Now().UTC().Truncate(time.Second),
	}

	err := repo.SaveMonitorResult(ctx, result)
	if err != nil {
		t.Fatalf("SaveMonitorResult failed: %v", err)
	}

	if result.ID == "" {
		t.Fatal("expected ID to be generated")
	}
}

func TestDomainRepo_GetLatestMonitorResult(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	domain := newTestDomain()
	if err := repo.Create(ctx, domain); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	// Save multiple results
	for i := 0; i < 3; i++ {
		result := &model.DomainMonitorResult{
			DomainID:    domain.ID,
			CheckedPort: 443,
			ResolvedIPs: []string{"1.2.3.4"},
			TLSSuccess:  true,
			Issuer:      fmt.Sprintf("issuer-%d", i),
			CheckedAt:   now.Add(time.Duration(i) * time.Minute),
		}
		if err := repo.SaveMonitorResult(ctx, result); err != nil {
			t.Fatalf("SaveMonitorResult %d failed: %v", i, err)
		}
	}

	// Get latest should return the most recent one
	latest, err := repo.GetLatestMonitorResult(ctx, domain.ID)
	if err != nil {
		t.Fatalf("GetLatestMonitorResult failed: %v", err)
	}

	if latest.Issuer != "issuer-2" {
		t.Errorf("expected latest issuer 'issuer-2', got '%s'", latest.Issuer)
	}
}

func TestDomainRepo_GetLatestMonitorResult_NotFound(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	_, err := repo.GetLatestMonitorResult(ctx, "nonexistent-domain")
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestDomainRepo_SaveMonitorResult_WithNilOptionalFields(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	domain := newTestDomain()
	if err := repo.Create(ctx, domain); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Save result with nil optional fields (DNS failure case)
	result := &model.DomainMonitorResult{
		DomainID:     domain.ID,
		CheckedPort:  443,
		TLSSuccess:   false,
		ErrorMessage: "DNS resolution failed",
		CheckedAt:    time.Now().UTC().Truncate(time.Second),
	}

	err := repo.SaveMonitorResult(ctx, result)
	if err != nil {
		t.Fatalf("SaveMonitorResult failed: %v", err)
	}

	// Retrieve and verify nil fields
	got, err := repo.GetLatestMonitorResult(ctx, domain.ID)
	if err != nil {
		t.Fatalf("GetLatestMonitorResult failed: %v", err)
	}

	if got.ExpireAt != nil {
		t.Error("expected ExpireAt to be nil")
	}
	if got.DaysRemaining != nil {
		t.Error("expected DaysRemaining to be nil")
	}
	if got.TLSSuccess {
		t.Error("expected TLSSuccess to be false")
	}
	if got.ErrorMessage != "DNS resolution failed" {
		t.Errorf("expected error message 'DNS resolution failed', got '%s'", got.ErrorMessage)
	}
}

// -----------------------------------------------------------------------------
// Task 2.2 (cloudflare-domain-auto-sync spec): CreateIfNotExists unit tests
// -----------------------------------------------------------------------------
//
// setupDomainTestDBWithUniqueIndex is used ONLY by the tests below. Unlike the
// package-level setupDomainTestDB (used by every other test in this file),
// which intentionally omits any uniqueness constraint on domains.name,
// CreateIfNotExists's `ON CONFLICT(LOWER(RTRIM(name, '.'))) DO NOTHING` clause
// requires the idx_domains_name_normalized UNIQUE index (mirroring
// internal/database/migrate.go) to exist as its exact conflict target — without
// it, SQLite has no matching unique constraint to trigger ON CONFLICT, and the
// insert would either fail or (worse) silently insert a duplicate row instead
// of exercising the dedup path under test.

// setupDomainTestDBWithUniqueIndex creates a test DB with the domains table
// PLUS the idx_domains_name_normalized UNIQUE index on LOWER(RTRIM(name, '.')).
func setupDomainTestDBWithUniqueIndex(t *testing.T) *DomainRepository {
	t.Helper()
	db := setupTestDB(t)

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS domains (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		source TEXT DEFAULT 'manual' CHECK(source IN ('manual', 'certificate', 'cloudflare')),
		thirdpart_dns_id TEXT DEFAULT '',
		dns_record_id TEXT DEFAULT '',
		dns_record_type TEXT DEFAULT '',
		dns_record_value TEXT DEFAULT '',
		monitor_port INTEGER NOT NULL DEFAULT 443,
		linked_machine_id TEXT,
		linked_certificate_id TEXT,
		linked_machine_certificate_id TEXT,
		monitor_enabled INTEGER NOT NULL DEFAULT 1,
		alert_ignored INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create domains table: %v", err)
	}

	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_domains_name_normalized ON domains(LOWER(RTRIM(name, '.')))`); err != nil {
		t.Fatalf("failed to create idx_domains_name_normalized: %v", err)
	}

	return NewDomainRepository(db)
}

// countDomainsByNormalizedName counts how many domains rows match the given
// name after normalization (LOWER+RTRIM '.'), the same comparison
// CreateIfNotExists's unique index uses.
func countDomainsByNormalizedName(t *testing.T, repo *DomainRepository, name string) int {
	t.Helper()
	var n int
	err := repo.db.QueryRow(
		`SELECT COUNT(*) FROM domains WHERE LOWER(RTRIM(name, '.')) = LOWER(RTRIM(?, '.'))`,
		name,
	).Scan(&n)
	if err != nil {
		t.Fatalf("failed to count domains by normalized name %q: %v", name, err)
	}
	return n
}

// TestDomainRepo_CreateIfNotExists_SecondCallDoesNotOverwrite verifies that
// calling CreateIfNotExists a second time with the same name returns
// created=false and leaves the original record's fields untouched.
//
// Requirements: 2.2, 2.3
func TestDomainRepo_CreateIfNotExists_SecondCallDoesNotOverwrite(t *testing.T) {
	repo := setupDomainTestDBWithUniqueIndex(t)
	ctx := context.Background()

	first := &model.Domain{
		Name:           "example.com",
		Source:         "cloudflare",
		MonitorPort:    443,
		MonitorEnabled: true,
	}
	created, err := repo.CreateIfNotExists(ctx, first)
	if err != nil {
		t.Fatalf("first CreateIfNotExists failed: %v", err)
	}
	if !created {
		t.Fatal("expected first CreateIfNotExists to report created=true")
	}

	// Second call for the same name, with deliberately different field values so
	// an overwrite would be detectable.
	second := &model.Domain{
		Name:           "example.com",
		Source:         "cloudflare",
		MonitorPort:    8443,
		MonitorEnabled: false,
		AlertIgnored:   true,
	}
	created, err = repo.CreateIfNotExists(ctx, second)
	if err != nil {
		t.Fatalf("second CreateIfNotExists failed: %v", err)
	}
	if created {
		t.Fatal("expected second CreateIfNotExists to report created=false")
	}

	if got := countDomainsByNormalizedName(t, repo, "example.com"); got != 1 {
		t.Fatalf("expected exactly 1 domain row for 'example.com', got %d", got)
	}

	got, err := repo.GetByID(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetByID for original record failed: %v", err)
	}
	if got.MonitorPort != 443 {
		t.Errorf("expected original MonitorPort 443 to remain unchanged, got %d", got.MonitorPort)
	}
	if !got.MonitorEnabled {
		t.Error("expected original MonitorEnabled to remain true (must not be overwritten)")
	}
	if got.AlertIgnored {
		t.Error("expected original AlertIgnored to remain false (must not be overwritten)")
	}
}

// TestDomainRepo_CreateIfNotExists_NormalizesCaseAndTrailingDot verifies that
// "Example.COM." and "example.com" are treated as the same record: the second
// call is a no-op dedup hit, not a new row.
//
// Requirements: 2.2, 2.3
func TestDomainRepo_CreateIfNotExists_NormalizesCaseAndTrailingDot(t *testing.T) {
	repo := setupDomainTestDBWithUniqueIndex(t)
	ctx := context.Background()

	first := &model.Domain{
		Name:           "example.com",
		Source:         "cloudflare",
		MonitorPort:    443,
		MonitorEnabled: true,
	}
	created, err := repo.CreateIfNotExists(ctx, first)
	if err != nil {
		t.Fatalf("first CreateIfNotExists failed: %v", err)
	}
	if !created {
		t.Fatal("expected first CreateIfNotExists to report created=true")
	}

	second := &model.Domain{
		Name:           "Example.COM.",
		Source:         "cloudflare",
		MonitorPort:    443,
		MonitorEnabled: true,
	}
	created, err = repo.CreateIfNotExists(ctx, second)
	if err != nil {
		t.Fatalf("second CreateIfNotExists (case/trailing-dot variant) failed: %v", err)
	}
	if created {
		t.Fatal("expected 'Example.COM.' to be treated as a duplicate of 'example.com' (created=false)")
	}

	if got := countDomainsByNormalizedName(t, repo, "example.com"); got != 1 {
		t.Fatalf("expected exactly 1 domain row after case/trailing-dot variant insert, got %d", got)
	}
	if got := countDomainsByNormalizedName(t, repo, "Example.COM."); got != 1 {
		t.Fatalf("expected exactly 1 domain row when queried via the case/trailing-dot variant name, got %d", got)
	}
}

// TestDomainRepo_CreateIfNotExists_DoesNotOverwriteExistingManualSourceRecord
// verifies that calling CreateIfNotExists with Source="cloudflare" for a name
// that already has a Source="manual" record returns created=false and does not
// overwrite the existing record's Source.
//
// Requirements: 2.2, 2.3
func TestDomainRepo_CreateIfNotExists_DoesNotOverwriteExistingManualSourceRecord(t *testing.T) {
	repo := setupDomainTestDBWithUniqueIndex(t)
	ctx := context.Background()

	manual := &model.Domain{
		Name:           "example.com",
		Source:         "manual",
		MonitorPort:    443,
		MonitorEnabled: true,
	}
	if err := repo.Create(ctx, manual); err != nil {
		t.Fatalf("failed to seed manual domain: %v", err)
	}

	autoSync := &model.Domain{
		Name:           "example.com",
		Source:         "cloudflare",
		MonitorPort:    443,
		MonitorEnabled: true,
	}
	created, err := repo.CreateIfNotExists(ctx, autoSync)
	if err != nil {
		t.Fatalf("CreateIfNotExists against existing manual record failed: %v", err)
	}
	if created {
		t.Fatal("expected CreateIfNotExists to report created=false when a manual-source record with the same name already exists")
	}

	if got := countDomainsByNormalizedName(t, repo, "example.com"); got != 1 {
		t.Fatalf("expected exactly 1 domain row for 'example.com', got %d", got)
	}

	got, err := repo.GetByID(ctx, manual.ID)
	if err != nil {
		t.Fatalf("GetByID for original manual record failed: %v", err)
	}
	if got.Source != "manual" {
		t.Errorf("expected original record's Source to remain 'manual', got %q (must not be overwritten by cloudflare auto-sync)", got.Source)
	}
}
