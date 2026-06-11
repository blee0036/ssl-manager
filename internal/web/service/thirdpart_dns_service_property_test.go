package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"

	_ "github.com/glebarez/sqlite"
)

// Feature: ux-improvements-batch1, Property 12: Sync Deletion Safety

// setupDeletionSafetyTestDB creates a test DB with all tables needed for sync deletion tests,
// including domain_monitor_results (required by domainRepo.Delete cascade).
func setupDeletionSafetyTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db := setupThirdpartDNSServiceTestDB(t)

	// Add domain_monitor_results table (needed for domainRepo.Delete cascade)
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS domain_monitor_results (
		id TEXT PRIMARY KEY,
		domain_id TEXT NOT NULL,
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

	return db
}

// TestProperty_SyncDeletionSafety_OnlyDeletesMatchingRecords verifies that the delete phase
// of syncToLocalDomains only removes records that satisfy ALL conditions:
// 1. source == "cloudflare"
// 2. thirdpart_dns_id == current config ID
// 3. within main_domains scope
// 4. dns_record_id not in sync results
//
// Records that fail any condition must NOT be deleted.
//
// **Validates: Requirements 8.1, 8.2, 8.4, 8.5**
func TestProperty_SyncDeletionSafety_OnlyDeletesMatchingRecords(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("Only domains matching ALL deletion conditions are deleted", prop.ForAll(
		func(numSurvivingRecords int, numDisappearingRecords int, hasManualDomain bool, hasOtherConfigDomain bool, hasOutOfScopeDomain bool) bool {
			db := setupDeletionSafetyTestDB(t)
			dnsRepo := repository.NewThirdpartDNSRepository(db)
			domainRepo := repository.NewDomainRepository(db)

			cfClient := &mockCloudflareClient{
				zones: []Zone{
					{ID: "zone-1", Name: "example.com"},
				},
			}

			svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())
			ctx := context.Background()

			// Create a DNS config with main_domains = ["example.com"]
			config, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
				Name:        "Test Config",
				Type:        "cloudflare",
				APIToken:    "test-token",
				MainDomains: []string{"example.com"},
			})
			if err != nil {
				t.Logf("CreateConfig failed: %v", err)
				return false
			}

			// Build records that should SURVIVE (still present in sync results)
			var syncRecords []DNSRecord
			for i := 0; i < numSurvivingRecords; i++ {
				recID := fmt.Sprintf("rec-survive-%d", i)
				name := fmt.Sprintf("s%d.example.com", i)
				syncRecords = append(syncRecords, DNSRecord{
					ID: recID, Name: name, Type: "A", Value: fmt.Sprintf("1.2.3.%d", i%256),
				})
				// Pre-create in DB as existing synced domain
				d := &model.Domain{
					Name:           name,
					Source:         "cloudflare",
					ThirdpartDNSID: config.ID,
					DNSRecordID:    recID,
					DNSRecordType:  "A",
					DNSRecordValue: fmt.Sprintf("1.2.3.%d", i%256),
					MonitorPort:    443,
					MonitorEnabled: true,
				}
				if err := domainRepo.Create(ctx, d); err != nil {
					t.Logf("Failed to create surviving domain: %v", err)
					return false
				}
			}

			// Build records that DISAPPEARED from sync (should be deleted)
			for i := 0; i < numDisappearingRecords; i++ {
				recID := fmt.Sprintf("rec-gone-%d", i)
				name := fmt.Sprintf("gone%d.example.com", i)
				// In DB but NOT in syncRecords
				d := &model.Domain{
					Name:           name,
					Source:         "cloudflare",
					ThirdpartDNSID: config.ID,
					DNSRecordID:    recID,
					DNSRecordType:  "A",
					DNSRecordValue: "9.9.9.9",
					MonitorPort:    443,
					MonitorEnabled: true,
				}
				if err := domainRepo.Create(ctx, d); err != nil {
					t.Logf("Failed to create disappearing domain: %v", err)
					return false
				}
			}

			// Optionally add a manual domain (source != cloudflare) — must NEVER be deleted
			if hasManualDomain {
				d := &model.Domain{
					Name:           "manual.example.com",
					Source:         "manual",
					ThirdpartDNSID: "",
					DNSRecordID:    "",
					DNSRecordType:  "",
					DNSRecordValue: "",
					MonitorPort:    443,
					MonitorEnabled: true,
				}
				if err := domainRepo.Create(ctx, d); err != nil {
					t.Logf("Failed to create manual domain: %v", err)
					return false
				}
			}

			// Optionally add a domain from a DIFFERENT config — must NEVER be deleted
			if hasOtherConfigDomain {
				d := &model.Domain{
					Name:           "other.example.com",
					Source:         "cloudflare",
					ThirdpartDNSID: "other-config-id",
					DNSRecordID:    "rec-other-1",
					DNSRecordType:  "A",
					DNSRecordValue: "10.10.10.10",
					MonitorPort:    443,
					MonitorEnabled: true,
				}
				if err := domainRepo.Create(ctx, d); err != nil {
					t.Logf("Failed to create other-config domain: %v", err)
					return false
				}
			}

			// Optionally add a domain outside main_domains scope — must NEVER be deleted
			if hasOutOfScopeDomain {
				d := &model.Domain{
					Name:           "out-of-scope.other.org",
					Source:         "cloudflare",
					ThirdpartDNSID: config.ID,
					DNSRecordID:    "rec-outscope-1",
					DNSRecordType:  "A",
					DNSRecordValue: "11.11.11.11",
					MonitorPort:    443,
					MonitorEnabled: true,
				}
				if err := domainRepo.Create(ctx, d); err != nil {
					t.Logf("Failed to create out-of-scope domain: %v", err)
					return false
				}
			}

			// Configure mock to return the surviving records
			cfClient.records = map[string][]DNSRecord{
				"zone-1": syncRecords,
			}

			// Run sync
			result, err := svc.SyncRecords(ctx, config.ID)
			if err != nil {
				t.Logf("SyncRecords failed: %v", err)
				return false
			}

			// Verify: disappeared records should be deleted
			if len(result.RemovedDomains) != numDisappearingRecords {
				t.Logf("Expected %d removed domains, got %d", numDisappearingRecords, len(result.RemovedDomains))
				return false
			}

			// Verify: surviving records still exist
			for i := 0; i < numSurvivingRecords; i++ {
				name := fmt.Sprintf("s%d.example.com", i)
				domains, _ := domainRepo.List(ctx, model.DomainFilter{Source: "cloudflare", ThirdpartDNSID: config.ID})
				found := false
				for _, d := range domains {
					if d.Name == name {
						found = true
						break
					}
				}
				if !found {
					t.Logf("Surviving domain %s was incorrectly deleted", name)
					return false
				}
			}

			// Verify: manual domain still exists
			if hasManualDomain {
				domains, _ := domainRepo.List(ctx, model.DomainFilter{Source: "manual"})
				found := false
				for _, d := range domains {
					if d.Name == "manual.example.com" {
						found = true
						break
					}
				}
				if !found {
					t.Logf("Manual domain was incorrectly deleted")
					return false
				}
			}

			// Verify: other config domain still exists
			if hasOtherConfigDomain {
				domains, _ := domainRepo.List(ctx, model.DomainFilter{Source: "cloudflare", ThirdpartDNSID: "other-config-id"})
				found := false
				for _, d := range domains {
					if d.Name == "other.example.com" {
						found = true
						break
					}
				}
				if !found {
					t.Logf("Other-config domain was incorrectly deleted")
					return false
				}
			}

			// Verify: out-of-scope domain still exists
			if hasOutOfScopeDomain {
				domains, _ := domainRepo.List(ctx, model.DomainFilter{Source: "cloudflare", ThirdpartDNSID: config.ID})
				found := false
				for _, d := range domains {
					if d.Name == "out-of-scope.other.org" {
						found = true
						break
					}
				}
				if !found {
					t.Logf("Out-of-scope domain was incorrectly deleted")
					return false
				}
			}

			return true
		},
		gen.IntRange(0, 5),  // numSurvivingRecords
		gen.IntRange(0, 5),  // numDisappearingRecords
		gen.Bool(),          // hasManualDomain
		gen.Bool(),          // hasOtherConfigDomain
		gen.Bool(),          // hasOutOfScopeDomain
	))

	properties.TestingRun(t)
}

// TestProperty_SyncDeletionSafety_IncompleteFetchNoChanges verifies that when
// FetchResult.Complete==false (triggered by records with empty IDs), NO local changes
// are made: no creates, no updates, no deletes.
//
// **Validates: Requirements 8.1, 8.2, 8.4, 8.5**
func TestProperty_SyncDeletionSafety_IncompleteFetchNoChanges(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(123)

	properties := gopter.NewProperties(parameters)

	properties.Property("Complete==false means NO local changes (no creates, no updates, no deletes)", prop.ForAll(
		func(numExisting int, numNewRecords int) bool {
			db := setupDeletionSafetyTestDB(t)
			dnsRepo := repository.NewThirdpartDNSRepository(db)
			domainRepo := repository.NewDomainRepository(db)

			// Mock that returns records with empty IDs (triggers Complete=false)
			var recordsWithEmptyID []DNSRecord
			for i := 0; i < numNewRecords; i++ {
				recordsWithEmptyID = append(recordsWithEmptyID, DNSRecord{
					ID:    "", // Empty ID triggers Complete=false
					Name:  fmt.Sprintf("new%d.example.com", i),
					Type:  "A",
					Value: fmt.Sprintf("2.2.2.%d", i%256),
				})
			}

			cfClient := &mockCloudflareClient{
				zones: []Zone{
					{ID: "zone-1", Name: "example.com"},
				},
				records: map[string][]DNSRecord{
					"zone-1": recordsWithEmptyID,
				},
			}

			svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())
			ctx := context.Background()

			// Create a DNS config
			config, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
				Name:        "Incomplete Test",
				Type:        "cloudflare",
				APIToken:    "test-token",
				MainDomains: []string{"example.com"},
			})
			if err != nil {
				t.Logf("CreateConfig failed: %v", err)
				return false
			}

			// Pre-create some existing domains in DB (should remain unchanged after sync)
			for i := 0; i < numExisting; i++ {
				d := &model.Domain{
					Name:           fmt.Sprintf("existing%d.example.com", i),
					Source:         "cloudflare",
					ThirdpartDNSID: config.ID,
					DNSRecordID:    fmt.Sprintf("rec-existing-%d", i),
					DNSRecordType:  "A",
					DNSRecordValue: fmt.Sprintf("3.3.3.%d", i%256),
					MonitorPort:    443,
					MonitorEnabled: true,
				}
				if err := domainRepo.Create(ctx, d); err != nil {
					t.Logf("Failed to create existing domain: %v", err)
					return false
				}
			}

			// Count domains before sync
			domainsBefore, err := domainRepo.List(ctx, model.DomainFilter{})
			if err != nil {
				t.Logf("Failed to list domains before: %v", err)
				return false
			}
			countBefore := len(domainsBefore)

			// Run sync — should fail because Complete==false
			_, syncErr := svc.SyncRecords(ctx, config.ID)

			// Sync must return an error (incomplete fetch)
			if syncErr == nil {
				t.Logf("Expected SyncRecords to return error for incomplete fetch, got nil")
				return false
			}

			// Count domains after sync — must be unchanged
			domainsAfter, err := domainRepo.List(ctx, model.DomainFilter{})
			if err != nil {
				t.Logf("Failed to list domains after: %v", err)
				return false
			}
			countAfter := len(domainsAfter)

			if countBefore != countAfter {
				t.Logf("Domain count changed! before=%d, after=%d (expected no change)", countBefore, countAfter)
				return false
			}

			// Also verify each existing domain is unchanged (values preserved)
			for i := 0; i < numExisting; i++ {
				expectedName := fmt.Sprintf("existing%d.example.com", i)
				found := false
				for _, d := range domainsAfter {
					if d.Name == expectedName && d.DNSRecordValue == fmt.Sprintf("3.3.3.%d", i%256) {
						found = true
						break
					}
				}
				if !found {
					t.Logf("Existing domain %s was modified or deleted", expectedName)
					return false
				}
			}

			return true
		},
		gen.IntRange(0, 5), // numExisting domains in DB
		gen.IntRange(1, 5), // numNewRecords with empty IDs (must be >= 1 to trigger incomplete)
	))

	properties.TestingRun(t)
}


// Feature: ux-improvements-batch1, Property 15: No Concurrent Sync for Same Config

// TestProperty_NoConcurrentSyncForSameConfig verifies that when a sync is already
// in progress for a config (simulated via pre-loading syncingConfigs), a concurrent
// sync request for the SAME config returns ErrSyncInProgress. After releasing the lock,
// the sync proceeds normally (no longer returns ErrSyncInProgress).
//
// Strategy: manually LoadOrStore into syncingConfigs to simulate in-progress state,
// then verify SyncRecords returns ErrSyncInProgress. Delete the entry and verify
// SyncRecords no longer returns ErrSyncInProgress. Also verify that locking one config
// does not block a different config.
//
// **Validates: Requirements 10.8**
func TestProperty_NoConcurrentSyncForSameConfig(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("concurrent sync for same config returns ErrSyncInProgress; different config is unaffected", prop.ForAll(
		func(configName string) bool {
			// Setup DB with required tables
			db := setupDeletionSafetyTestDB(t)
			dnsRepo := repository.NewThirdpartDNSRepository(db)
			domainRepo := repository.NewDomainRepository(db)

			// Use a mock client that returns valid data (zones + records)
			cfClient := &mockCloudflareClient{
				zones:   []Zone{{ID: "zone-1", Name: "example.com"}},
				records: map[string][]DNSRecord{"zone-1": {{ID: "rec-1", Name: "test.example.com", Type: "A", Value: "1.2.3.4"}}},
			}

			svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())
			ctx := context.Background()

			// Create a DNS config in the database to make it a valid config
			dnsConfig, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
				Name:        configName,
				Type:        "cloudflare",
				APIToken:    "test-token-abc123",
				MainDomains: []string{"example.com"},
			})
			if err != nil {
				t.Logf("Failed to create config: %v", err)
				return false
			}

			// === Test 1: Pre-load syncingConfigs to simulate in-progress sync ===
			svc.syncingConfigs.Store(dnsConfig.ID, true)

			// Attempt to sync the same config — should get ErrSyncInProgress
			_, syncErr := svc.SyncRecords(ctx, dnsConfig.ID)
			if !errors.Is(syncErr, ErrSyncInProgress) {
				t.Logf("Expected ErrSyncInProgress for locked config, got: %v", syncErr)
				return false
			}

			// === Test 2: Release the lock and verify sync proceeds ===
			svc.syncingConfigs.Delete(dnsConfig.ID)

			// Now SyncRecords should NOT return ErrSyncInProgress
			// (it should succeed since the config is valid and enabled)
			_, syncErr = svc.SyncRecords(ctx, dnsConfig.ID)
			if errors.Is(syncErr, ErrSyncInProgress) {
				t.Logf("After releasing lock, still got ErrSyncInProgress")
				return false
			}

			// === Test 3: Lock one config, verify a DIFFERENT config ID is not blocked ===
			// Re-lock the first config
			svc.syncingConfigs.Store(dnsConfig.ID, true)

			// Attempt to sync a non-existent config ID — should NOT get ErrSyncInProgress
			// (it will get a different error like "config not found", but NOT ErrSyncInProgress)
			_, syncErr = svc.SyncRecords(ctx, "non-existent-config-id")
			if errors.Is(syncErr, ErrSyncInProgress) {
				t.Logf("Locking config %q should not block config %q", dnsConfig.ID, "non-existent-config-id")
				return false
			}

			// Cleanup
			svc.syncingConfigs.Delete(dnsConfig.ID)

			return true
		},
		gen.Identifier().Map(func(s string) string { // configName
			if len(s) > 15 {
				s = s[:15]
			}
			return "cfg-" + s
		}),
	))

	properties.TestingRun(t)
}
