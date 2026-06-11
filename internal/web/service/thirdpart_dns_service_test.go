package service

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// testRuntimeCfg creates a RuntimeConfig with default port 443 for testing.
func testRuntimeCfg() *config.RuntimeConfig {
	cfg := config.DefaultConfig()
	cfg.DomainMonitor.DefaultPort = 443
	return config.NewRuntimeConfig(cfg)
}

// mockCloudflareClient is a mock implementation of CloudflareClient for testing.
type mockCloudflareClient struct {
	verifyErr  error
	zones      []Zone
	zonesErr   error
	records    map[string][]DNSRecord // keyed by zoneID
	recordsErr error
}

func (m *mockCloudflareClient) VerifyToken(ctx context.Context, token string) error {
	return m.verifyErr
}

func (m *mockCloudflareClient) ListZones(ctx context.Context, token string) ([]Zone, error) {
	if m.zonesErr != nil {
		return nil, m.zonesErr
	}
	return m.zones, nil
}

func (m *mockCloudflareClient) ListDNSRecords(ctx context.Context, token string, zoneID string, types []string) ([]DNSRecord, error) {
	if m.recordsErr != nil {
		return nil, m.recordsErr
	}
	if m.records != nil {
		return m.records[zoneID], nil
	}
	return nil, nil
}

// setupThirdpartDNSServiceTestDB creates a test DB with required tables.
func setupThirdpartDNSServiceTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db := setupTestDB(t)

	// Add thirdpart_dns table
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS thirdpart_dns (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'cloudflare' CHECK(type IN ('cloudflare')),
		api_token TEXT NOT NULL,
		config_json TEXT NOT NULL DEFAULT '{}',
		main_domains TEXT NOT NULL DEFAULT '[]',
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create thirdpart_dns table: %v", err)
	}

	// Add thirdpart_dns_sync_logs table
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS thirdpart_dns_sync_logs (
		id TEXT PRIMARY KEY,
		thirdpart_dns_id TEXT NOT NULL,
		records_count INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL CHECK(status IN ('success', 'failed')),
		error_message TEXT DEFAULT '',
		new_domains TEXT DEFAULT '[]',
		updated_domains TEXT DEFAULT '[]',
		removed_domains TEXT DEFAULT '[]',
		synced_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create thirdpart_dns_sync_logs table: %v", err)
	}

	// Add domains table
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS domains (
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

	return db
}

func TestThirdpartDNSService_CreateConfig(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	cfClient := &mockCloudflareClient{}
	svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())

	ctx := context.Background()

	config, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
		Name:        "My Cloudflare",
		Type:        "cloudflare",
		APIToken:    "test-token",
		ConfigJSON:  `{"zone_id": "abc"}`,
		MainDomains: []string{"example.com"},
	})
	if err != nil {
		t.Fatalf("CreateConfig failed: %v", err)
	}

	if config.ID == "" {
		t.Fatal("expected ID to be generated")
	}
	if config.Name != "My Cloudflare" {
		t.Errorf("expected name 'My Cloudflare', got '%s'", config.Name)
	}
	if !config.Enabled {
		t.Error("expected Enabled to be true by default")
	}
}

func TestThirdpartDNSService_CreateConfig_EmptyName(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	svc := NewThirdpartDNSService(dnsRepo, domainRepo, nil, nil, testRuntimeCfg())

	ctx := context.Background()

	_, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
		Name:     "",
		Type:     "cloudflare",
		APIToken: "token",
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestThirdpartDNSService_CreateConfig_EmptyToken(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	svc := NewThirdpartDNSService(dnsRepo, domainRepo, nil, nil, testRuntimeCfg())

	ctx := context.Background()

	_, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
		Name:     "Test",
		Type:     "cloudflare",
		APIToken: "",
	})
	if err == nil {
		t.Fatal("expected error for empty api_token")
	}
}

func TestThirdpartDNSService_CreateConfig_UnsupportedType(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	svc := NewThirdpartDNSService(dnsRepo, domainRepo, nil, nil, testRuntimeCfg())

	ctx := context.Background()

	_, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
		Name:     "Test",
		Type:     "route53",
		APIToken: "token",
	})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestThirdpartDNSService_CreateConfig_TokenVerificationFails(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	cfClient := &mockCloudflareClient{
		verifyErr: fmt.Errorf("invalid token"),
	}
	svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())

	ctx := context.Background()

	_, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
		Name:     "Test",
		Type:     "cloudflare",
		APIToken: "bad-token",
	})
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestThirdpartDNSService_UpdateConfig(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	cfClient := &mockCloudflareClient{}
	svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())

	ctx := context.Background()

	// Create a config first
	config, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
		Name:        "Original",
		Type:        "cloudflare",
		APIToken:    "token-1",
		MainDomains: []string{"example.com"},
	})
	if err != nil {
		t.Fatalf("CreateConfig failed: %v", err)
	}

	// Update it
	newName := "Updated"
	newDomains := []string{"new.com", "other.com"}
	updated, err := svc.UpdateConfig(ctx, config.ID, model.UpdateThirdpartDNSInput{
		Name:        &newName,
		MainDomains: newDomains,
	})
	if err != nil {
		t.Fatalf("UpdateConfig failed: %v", err)
	}

	if updated.Name != "Updated" {
		t.Errorf("expected name 'Updated', got '%s'", updated.Name)
	}
	if len(updated.MainDomains) != 2 {
		t.Errorf("expected 2 main domains, got %d", len(updated.MainDomains))
	}
}

func TestThirdpartDNSService_UpdateConfig_NotFound(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	svc := NewThirdpartDNSService(dnsRepo, domainRepo, nil, nil, testRuntimeCfg())

	ctx := context.Background()

	newName := "Test"
	_, err := svc.UpdateConfig(ctx, "nonexistent", model.UpdateThirdpartDNSInput{
		Name: &newName,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent config")
	}
}

func TestThirdpartDNSService_DeleteConfig(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	cfClient := &mockCloudflareClient{}
	svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())

	ctx := context.Background()

	config, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
		Name:     "To Delete",
		Type:     "cloudflare",
		APIToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateConfig failed: %v", err)
	}

	if err := svc.DeleteConfig(ctx, config.ID); err != nil {
		t.Fatalf("DeleteConfig failed: %v", err)
	}

	// Verify it's gone
	configs, err := svc.ListConfigs(ctx)
	if err != nil {
		t.Fatalf("ListConfigs failed: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("expected 0 configs after delete, got %d", len(configs))
	}
}

func TestThirdpartDNSService_ListConfigs(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	cfClient := &mockCloudflareClient{}
	svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())

	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
			Name:     fmt.Sprintf("Config %d", i),
			Type:     "cloudflare",
			APIToken: fmt.Sprintf("token-%d", i),
		})
		if err != nil {
			t.Fatalf("CreateConfig %d failed: %v", i, err)
		}
	}

	configs, err := svc.ListConfigs(ctx)
	if err != nil {
		t.Fatalf("ListConfigs failed: %v", err)
	}
	if len(configs) != 3 {
		t.Errorf("expected 3 configs, got %d", len(configs))
	}
}

func TestThirdpartDNSService_SyncRecords_FullSync(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)

	cfClient := &mockCloudflareClient{
		zones: []Zone{
			{ID: "zone-1", Name: "example.com"},
			{ID: "zone-2", Name: "test.com"},
		},
		records: map[string][]DNSRecord{
			"zone-1": {
				{ID: "rec-1", Name: "www.example.com", Type: "A", Value: "1.2.3.4"},
				{ID: "rec-2", Name: "api.example.com", Type: "CNAME", Value: "lb.example.com"},
			},
			"zone-2": {
				{ID: "rec-3", Name: "app.test.com", Type: "A", Value: "5.6.7.8"},
			},
		},
	}

	svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())
	ctx := context.Background()

	// Create config with empty main_domains (full sync)
	config, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
		Name:        "Full Sync",
		Type:        "cloudflare",
		APIToken:    "token",
		MainDomains: []string{},
	})
	if err != nil {
		t.Fatalf("CreateConfig failed: %v", err)
	}

	// Sync records
	result, err := svc.SyncRecords(ctx, config.ID)
	if err != nil {
		t.Fatalf("SyncRecords failed: %v", err)
	}

	if result.RecordsCount != 3 {
		t.Errorf("expected 3 records, got %d", result.RecordsCount)
	}
	if len(result.NewDomains) != 3 {
		t.Errorf("expected 3 new domains, got %d", len(result.NewDomains))
	}

	// Verify domains were created
	domains, err := domainRepo.List(ctx, model.DomainFilter{ThirdpartDNSID: config.ID})
	if err != nil {
		t.Fatalf("List domains failed: %v", err)
	}
	if len(domains) != 3 {
		t.Errorf("expected 3 domains in DB, got %d", len(domains))
	}

	// Verify sync log was created
	logs, err := svc.GetSyncLogs(ctx, config.ID)
	if err != nil {
		t.Fatalf("GetSyncLogs failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 sync log, got %d", len(logs))
	}
	if logs[0].Status != "success" {
		t.Errorf("expected sync log status 'success', got '%s'", logs[0].Status)
	}
	if logs[0].RecordsCount != 3 {
		t.Errorf("expected sync log records_count 3, got %d", logs[0].RecordsCount)
	}
}

func TestThirdpartDNSService_SyncRecords_FilteredByMainDomains(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)

	cfClient := &mockCloudflareClient{
		zones: []Zone{
			{ID: "zone-1", Name: "example.com"},
			{ID: "zone-2", Name: "other.com"},
		},
		records: map[string][]DNSRecord{
			"zone-1": {
				{ID: "rec-4", Name: "www.example.com", Type: "A", Value: "1.2.3.4"},
				{ID: "rec-5", Name: "api.example.com", Type: "AAAA", Value: "::1"},
			},
			"zone-2": {
				{ID: "rec-6", Name: "app.other.com", Type: "A", Value: "5.6.7.8"},
			},
		},
	}

	svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())
	ctx := context.Background()

	// Create config with specific main_domains (filtered sync)
	config, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
		Name:        "Filtered Sync",
		Type:        "cloudflare",
		APIToken:    "token",
		MainDomains: []string{"example.com"},
	})
	if err != nil {
		t.Fatalf("CreateConfig failed: %v", err)
	}

	// Sync records - should only get example.com records
	result, err := svc.SyncRecords(ctx, config.ID)
	if err != nil {
		t.Fatalf("SyncRecords failed: %v", err)
	}

	if result.RecordsCount != 2 {
		t.Errorf("expected 2 records (only example.com), got %d", result.RecordsCount)
	}
	if len(result.NewDomains) != 2 {
		t.Errorf("expected 2 new domains, got %d", len(result.NewDomains))
	}

	// Verify only example.com domains were created
	domains, err := domainRepo.List(ctx, model.DomainFilter{ThirdpartDNSID: config.ID})
	if err != nil {
		t.Fatalf("List domains failed: %v", err)
	}
	if len(domains) != 2 {
		t.Errorf("expected 2 domains in DB, got %d", len(domains))
	}
}

func TestThirdpartDNSService_SyncRecords_UpdatesExisting(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)

	cfClient := &mockCloudflareClient{
		zones: []Zone{
			{ID: "zone-1", Name: "example.com"},
		},
		records: map[string][]DNSRecord{
			"zone-1": {
				{ID: "rec-7", Name: "www.example.com", Type: "A", Value: "9.9.9.9"},
			},
		},
	}

	svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())
	ctx := context.Background()

	// Create config
	config, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
		Name:        "Update Test",
		Type:        "cloudflare",
		APIToken:    "token",
		MainDomains: []string{},
	})
	if err != nil {
		t.Fatalf("CreateConfig failed: %v", err)
	}

	// First sync - creates the domain
	result1, err := svc.SyncRecords(ctx, config.ID)
	if err != nil {
		t.Fatalf("First SyncRecords failed: %v", err)
	}
	if len(result1.NewDomains) != 1 {
		t.Errorf("expected 1 new domain on first sync, got %d", len(result1.NewDomains))
	}

	// Change the record value
	cfClient.records["zone-1"] = []DNSRecord{
		{ID: "rec-7", Name: "www.example.com", Type: "A", Value: "10.10.10.10"},
	}

	// Second sync - should update the existing domain
	result2, err := svc.SyncRecords(ctx, config.ID)
	if err != nil {
		t.Fatalf("Second SyncRecords failed: %v", err)
	}
	if len(result2.NewDomains) != 0 {
		t.Errorf("expected 0 new domains on second sync, got %d", len(result2.NewDomains))
	}
	if len(result2.UpdatedDomains) != 1 {
		t.Errorf("expected 1 updated domain on second sync, got %d", len(result2.UpdatedDomains))
	}

	// Verify the domain was updated
	domains, err := domainRepo.List(ctx, model.DomainFilter{ThirdpartDNSID: config.ID})
	if err != nil {
		t.Fatalf("List domains failed: %v", err)
	}
	if len(domains) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(domains))
	}
	if domains[0].DNSRecordValue != "10.10.10.10" {
		t.Errorf("expected updated value '10.10.10.10', got '%s'", domains[0].DNSRecordValue)
	}
}

func TestThirdpartDNSService_SyncRecords_DisabledConfig(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	cfClient := &mockCloudflareClient{}
	svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())

	ctx := context.Background()

	// Create and disable config
	config, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
		Name:     "Disabled",
		Type:     "cloudflare",
		APIToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateConfig failed: %v", err)
	}

	disabled := false
	_, err = svc.UpdateConfig(ctx, config.ID, model.UpdateThirdpartDNSInput{
		Enabled: &disabled,
	})
	if err != nil {
		t.Fatalf("UpdateConfig failed: %v", err)
	}

	// Sync should fail for disabled config
	_, err = svc.SyncRecords(ctx, config.ID)
	if err == nil {
		t.Fatal("expected error for disabled config")
	}
}

func TestThirdpartDNSService_SyncRecords_APIFailure(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)

	cfClient := &mockCloudflareClient{
		zonesErr: fmt.Errorf("API rate limit exceeded"),
	}

	alertSender := &mockAlertSender{}
	svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, alertSender, testRuntimeCfg())
	ctx := context.Background()

	// Create config
	config, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
		Name:     "Fail Test",
		Type:     "cloudflare",
		APIToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateConfig failed: %v", err)
	}

	// Sync should fail
	_, err = svc.SyncRecords(ctx, config.ID)
	if err == nil {
		t.Fatal("expected error for API failure")
	}

	// Verify failure sync log was created
	logs, err := svc.GetSyncLogs(ctx, config.ID)
	if err != nil {
		t.Fatalf("GetSyncLogs failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 sync log, got %d", len(logs))
	}
	if logs[0].Status != "failed" {
		t.Errorf("expected sync log status 'failed', got '%s'", logs[0].Status)
	}
	if logs[0].ErrorMessage == "" {
		t.Error("expected error message in sync log")
	}

	// Verify alert was sent
	if len(alertSender.alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alertSender.alerts))
	}
	if alertSender.alerts[0].AlertType != "dns_sync_failed" {
		t.Errorf("expected alert type 'dns_sync_failed', got '%s'", alertSender.alerts[0].AlertType)
	}
}

func TestThirdpartDNSService_SyncRecords_DomainSourceIsCloudflare(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)

	cfClient := &mockCloudflareClient{
		zones: []Zone{
			{ID: "zone-1", Name: "example.com"},
		},
		records: map[string][]DNSRecord{
			"zone-1": {
				{ID: "rec-8", Name: "www.example.com", Type: "A", Value: "1.2.3.4"},
			},
		},
	}

	svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())
	ctx := context.Background()

	config, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
		Name:        "Source Test",
		Type:        "cloudflare",
		APIToken:    "token",
		MainDomains: []string{},
	})
	if err != nil {
		t.Fatalf("CreateConfig failed: %v", err)
	}

	_, err = svc.SyncRecords(ctx, config.ID)
	if err != nil {
		t.Fatalf("SyncRecords failed: %v", err)
	}

	// Verify domain source is 'cloudflare'
	domains, err := domainRepo.List(ctx, model.DomainFilter{ThirdpartDNSID: config.ID})
	if err != nil {
		t.Fatalf("List domains failed: %v", err)
	}
	if len(domains) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(domains))
	}
	if domains[0].Source != "cloudflare" {
		t.Errorf("expected domain source 'cloudflare', got '%s'", domains[0].Source)
	}
	if domains[0].MonitorEnabled != true {
		t.Error("expected MonitorEnabled to be true")
	}
	if domains[0].MonitorPort != 443 {
		t.Errorf("expected MonitorPort 443, got %d", domains[0].MonitorPort)
	}
}

func TestThirdpartDNSService_GetSyncLogs(t *testing.T) {
	db := setupThirdpartDNSServiceTestDB(t)
	dnsRepo := repository.NewThirdpartDNSRepository(db)
	domainRepo := repository.NewDomainRepository(db)
	cfClient := &mockCloudflareClient{
		zones:   []Zone{{ID: "zone-1", Name: "example.com"}},
		records: map[string][]DNSRecord{"zone-1": {{ID: "rec-9", Name: "www.example.com", Type: "A", Value: "1.2.3.4"}}},
	}
	svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())

	ctx := context.Background()

	config, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
		Name:     "Log Test",
		Type:     "cloudflare",
		APIToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateConfig failed: %v", err)
	}

	// Sync twice
	_, err = svc.SyncRecords(ctx, config.ID)
	if err != nil {
		t.Fatalf("First SyncRecords failed: %v", err)
	}
	_, err = svc.SyncRecords(ctx, config.ID)
	if err != nil {
		t.Fatalf("Second SyncRecords failed: %v", err)
	}

	logs, err := svc.GetSyncLogs(ctx, config.ID)
	if err != nil {
		t.Fatalf("GetSyncLogs failed: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 sync logs, got %d", len(logs))
	}
}
