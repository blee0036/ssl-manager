package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"

	_ "github.com/glebarez/sqlite"
)

// Feature: ux-improvements-batch1, Property 14: Periodic Sync Only Processes Enabled Configs

// TestProperty_PeriodicSyncOnlyProcessesEnabledConfigs verifies that runDNSSyncAll
// only syncs DNS configs where enabled=true. Disabled configs must not have any
// domains created/synced.
//
// Strategy:
// 1. Set up in-memory DB with thirdpart_dns and domains tables
// 2. Create a mix of enabled and disabled DNS configs
// 3. Set up SchedulerService with real ThirdpartDNSService + mock CloudflareClient
// 4. Call runDNSSyncAll directly
// 5. Verify that only enabled configs have domains created (new records synced)
// 6. Verify disabled configs have NO domains created
//
// **Validates: Requirements 10.5**
func TestProperty_PeriodicSyncOnlyProcessesEnabledConfigs(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("runDNSSyncAll only syncs enabled configs; disabled configs produce no domains", prop.ForAll(
		func(numEnabled int, numDisabled int) bool {
			// Setup DB with all required tables
			db := setupDeletionSafetyTestDB(t)
			dnsRepo := repository.NewThirdpartDNSRepository(db)
			domainRepo := repository.NewDomainRepository(db)

			// Each config gets its own unique zone with a unique record
			// Build zones and records that mock client will return
			allZones := make([]Zone, 0, numEnabled+numDisabled)
			allRecords := make(map[string][]DNSRecord)

			for i := 0; i < numEnabled+numDisabled; i++ {
				zoneID := fmt.Sprintf("zone-%d", i)
				zoneName := fmt.Sprintf("config%d.com", i)
				allZones = append(allZones, Zone{ID: zoneID, Name: zoneName})
				allRecords[zoneID] = []DNSRecord{
					{ID: fmt.Sprintf("rec-%d", i), Name: fmt.Sprintf("www.config%d.com", i), Type: "A", Value: fmt.Sprintf("10.0.%d.1", i%256)},
				}
			}

			cfClient := &mockCloudflareClient{
				zones:   allZones,
				records: allRecords,
			}

			svc := NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, testRuntimeCfg())
			ctx := context.Background()

			// Track enabled and disabled config IDs
			enabledConfigIDs := make([]string, 0, numEnabled)
			disabledConfigIDs := make([]string, 0, numDisabled)

			// Create enabled configs
			for i := 0; i < numEnabled; i++ {
				zoneName := fmt.Sprintf("config%d.com", i)
				cfg, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
					Name:        fmt.Sprintf("Enabled-%d", i),
					Type:        "cloudflare",
					APIToken:    "test-token",
					MainDomains: []string{zoneName},
				})
				if err != nil {
					t.Logf("Failed to create enabled config %d: %v", i, err)
					return false
				}
				enabledConfigIDs = append(enabledConfigIDs, cfg.ID)
			}

			// Create disabled configs
			for i := 0; i < numDisabled; i++ {
				idx := numEnabled + i
				zoneName := fmt.Sprintf("config%d.com", idx)
				cfg, err := svc.CreateConfig(ctx, model.CreateThirdpartDNSInput{
					Name:        fmt.Sprintf("Disabled-%d", i),
					Type:        "cloudflare",
					APIToken:    "test-token",
					MainDomains: []string{zoneName},
				})
				if err != nil {
					t.Logf("Failed to create disabled config %d: %v", i, err)
					return false
				}
				// Disable the config
				disabled := false
				_, err = svc.UpdateConfig(ctx, cfg.ID, model.UpdateThirdpartDNSInput{
					Enabled: &disabled,
				})
				if err != nil {
					t.Logf("Failed to disable config %d: %v", i, err)
					return false
				}
				disabledConfigIDs = append(disabledConfigIDs, cfg.ID)
			}

			// Set up SchedulerService with the ThirdpartDNSService
			scheduler := &SchedulerService{
				thirdpartDNSService: svc,
				dnsRepo:             dnsRepo,
			}

			// Call runDNSSyncAll - this is the method under test
			scheduler.runDNSSyncAll(ctx)

			// Verify: enabled configs should have domains created
			for i, configID := range enabledConfigIDs {
				domains, err := domainRepo.List(ctx, model.DomainFilter{ThirdpartDNSID: configID})
				if err != nil {
					t.Logf("Failed to list domains for enabled config %d: %v", i, err)
					return false
				}
				if len(domains) == 0 {
					t.Logf("Enabled config %d (%s) should have domains synced, but has 0", i, configID)
					return false
				}
			}

			// Verify: disabled configs should have NO domains created
			for i, configID := range disabledConfigIDs {
				domains, err := domainRepo.List(ctx, model.DomainFilter{ThirdpartDNSID: configID})
				if err != nil {
					t.Logf("Failed to list domains for disabled config %d: %v", i, err)
					return false
				}
				if len(domains) != 0 {
					t.Logf("Disabled config %d (%s) should have 0 domains, but has %d", i, configID, len(domains))
					return false
				}
			}

			return true
		},
		gen.IntRange(1, 4), // numEnabled (at least 1 to verify sync works)
		gen.IntRange(1, 4), // numDisabled (at least 1 to verify skip behavior)
	))

	properties.TestingRun(t)
}
