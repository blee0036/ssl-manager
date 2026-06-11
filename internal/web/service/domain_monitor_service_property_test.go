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

// Feature: ux-improvements-batch1, Property 9: Setting alert_ignored=true Always Calls Suppress

// suppressMockAlerter is a mock alerter that tracks SuppressActiveByTarget calls
// and can be configured to succeed or fail.
type suppressMockAlerter struct {
	suppressCalled     bool
	suppressTargetType string
	suppressTargetID   string
	suppressShouldFail bool
}

func (m *suppressMockAlerter) SendAlert(_ context.Context, _, _, _, _, _, _ string) error {
	return nil
}

func (m *suppressMockAlerter) AutoResolve(_ context.Context, _, _, _ string) {}

func (m *suppressMockAlerter) SuppressActiveByTarget(_ context.Context, targetType, targetID string) error {
	m.suppressCalled = true
	m.suppressTargetType = targetType
	m.suppressTargetID = targetID
	if m.suppressShouldFail {
		return errors.New("suppress failed: simulated error")
	}
	return nil
}

// setupSuppressTestDB creates an in-memory SQLite database with required tables
// for testing domain monitor Update with alert_ignored + suppress logic.
func setupSuppressTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	tables := []string{
		`CREATE TABLE IF NOT EXISTS domains (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			source TEXT DEFAULT 'manual',
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
		)`,
		`CREATE TABLE IF NOT EXISTS domain_monitor_results (
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
		)`,
	}

	for _, stmt := range tables {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			t.Fatalf("failed to create table: %v", err)
		}
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

// TestProperty_Suppress_AlertIgnored verifies that when Update is called with alert_ignored=true:
// 1. SuppressActiveByTarget is always called
// 2. If suppress succeeds: alert_ignored is persisted (readable as true after update)
// 3. If suppress fails: alert_ignored is NOT persisted (remains false) and Update returns error
//
// **Validates: Requirements 6.7**
func TestProperty_Suppress_AlertIgnored(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("Setting alert_ignored=true always calls SuppressActiveByTarget; success persists, failure rolls back", prop.ForAll(
		func(suppressShouldFail bool, domainName string, port int) bool {
			// Setup fresh DB and repo
			db := setupSuppressTestDB(t)
			domainRepo := repository.NewDomainRepository(db)

			// Create mock alerter with configurable success/failure
			alerter := &suppressMockAlerter{
				suppressShouldFail: suppressShouldFail,
			}

			// Create service with real repo and mock alerter
			svc := NewDomainMonitorService(domainRepo, nil, alerter, nil)

			ctx := context.Background()

			// Create a domain with alert_ignored=false
			domain := &model.Domain{
				Name:           domainName,
				Source:         "manual",
				MonitorPort:    port,
				MonitorEnabled: true,
				AlertIgnored:   false,
			}
			if err := domainRepo.Create(ctx, domain); err != nil {
				t.Logf("Failed to create domain: %v", err)
				return false
			}

			// Call Update with alert_ignored=true
			alertIgnoredTrue := true
			_, updateErr := svc.Update(ctx, domain.ID, model.UpdateDomainInput{
				AlertIgnored: &alertIgnoredTrue,
			})

			// Verify: SuppressActiveByTarget is ALWAYS called regardless of outcome
			if !alerter.suppressCalled {
				t.Logf("Expected SuppressActiveByTarget to be called, but it was not")
				return false
			}

			// Verify: correct target type and ID passed
			if alerter.suppressTargetType != "domain" {
				t.Logf("Expected targetType 'domain', got %q", alerter.suppressTargetType)
				return false
			}
			if alerter.suppressTargetID != domain.ID {
				t.Logf("Expected targetID %q, got %q", domain.ID, alerter.suppressTargetID)
				return false
			}

			// Read the domain back from DB to check persisted state
			readBack, readErr := domainRepo.GetByID(ctx, domain.ID)
			if readErr != nil {
				t.Logf("Failed to read domain back: %v", readErr)
				return false
			}

			if suppressShouldFail {
				// Suppress failed: Update should return error
				if updateErr == nil {
					t.Logf("Expected Update to return error when suppress fails, got nil")
					return false
				}
				// alert_ignored should NOT be persisted (remains false)
				if readBack.AlertIgnored {
					t.Logf("Expected alert_ignored to remain false after suppress failure, got true")
					return false
				}
			} else {
				// Suppress succeeded: Update should succeed
				if updateErr != nil {
					t.Logf("Expected Update to succeed when suppress succeeds, got error: %v", updateErr)
					return false
				}
				// alert_ignored should be persisted as true
				if !readBack.AlertIgnored {
					t.Logf("Expected alert_ignored to be true after successful suppress, got false")
					return false
				}
			}

			return true
		},
		gen.Bool(),                                                             // suppressShouldFail
		gen.Identifier().Map(func(s string) string {                           // domainName
			if len(s) > 20 {
				s = s[:20]
			}
			return fmt.Sprintf("%s.example.com", s)
		}),
		gen.IntRange(1, 65535), // port
	))

	properties.TestingRun(t)
}
