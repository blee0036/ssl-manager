package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

// createRootDomainsSchema adds the root_domains table plus its indexes (mirroring
// internal/database/migrate.go) to a DB produced by the shared setupTestDB helper
// from testhelper_test.go.
//
// It uses a distinct name from the unit-test file's helpers so both files coexist
// in the repository package without redeclaration and without touching shared
// helpers.
func createRootDomainsSchema(t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS root_domains (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT 'manual' CHECK(source IN ('manual', 'cloudflare')),
		registrable_domain TEXT NOT NULL,
		expiry_date TEXT,
		expiry_source TEXT NOT NULL DEFAULT 'whois' CHECK(expiry_source IN ('whois', 'manual')),
		last_checked_at TEXT,
		last_status TEXT NOT NULL DEFAULT '',
		last_error TEXT NOT NULL DEFAULT '',
		monitor_enabled INTEGER NOT NULL DEFAULT 1,
		alert_ignored INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("failed to create root_domains table: %v", err)
	}

	indexes := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_root_domains_registrable ON root_domains(registrable_domain)`,
		`CREATE INDEX IF NOT EXISTS idx_root_domains_enabled ON root_domains(monitor_enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_root_domains_expiry ON root_domains(expiry_date)`,
	}
	for _, stmt := range indexes {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create root_domains index: %v", err)
		}
	}
}

// Feature: domain-expiry-monitor, Property 5: 到期日持久化往返一致（UTC）
// **Validates: Requirements 4.3, 4.7**

func TestProperty_RootDomainExpiryRoundTripUTC(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Expiry timestamps are generated at second precision because RFC3339 storage
	// truncates sub-second components. Range: [1970-01-01, 2100-01-01) — within
	// RFC3339's 4-digit-year window and covering realistic registration expiries.
	const maxUnix = int64(4102444800) // 2100-01-01T00:00:00Z
	// Fixed-zone offsets across the full real-world span [-12h, +14h] exercise the
	// "any timezone" requirement.
	const minOffsetSec = -12 * 3600
	const maxOffsetSec = 14 * 3600

	properties.Property("SaveExpiryResult persists an any-timezone expiry and GetByID reads it back as the equivalent UTC instant", prop.ForAll(
		func(unixSec int64, offsetSec int) bool {
			db := setupTestDB(t)
			createRootDomainsSchema(t, db)
			repo := NewRootDomainRepository(db)
			ctx := context.Background()

			// Build the expiry in an arbitrary fixed timezone at second precision.
			loc := time.FixedZone("gen", offsetSec)
			expiry := time.Unix(unixSec, 0).In(loc)

			// Create a root domain (expiry initially unknown).
			unique := uuid.New().String()[:8]
			rd := &model.RootDomain{
				Name:              fmt.Sprintf("example-%s.com", unique),
				Source:            "manual",
				RegistrableDomain: fmt.Sprintf("example-%s.com", unique),
				MonitorEnabled:    true,
			}
			if err := repo.Create(ctx, rd); err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}

			// Persist the expiry via SaveExpiryResult (success path).
			checkedAt := time.Now().UTC()
			if err := repo.SaveExpiryResult(ctx, rd.ID, &expiry, checkedAt, "success", ""); err != nil {
				t.Logf("SaveExpiryResult failed: %v", err)
				return false
			}

			// Read back and assert round-trip consistency.
			got, err := repo.GetByID(ctx, rd.ID)
			if err != nil {
				t.Logf("GetByID failed: %v", err)
				return false
			}
			if got.ExpiryDate == nil {
				t.Logf("expected non-nil ExpiryDate after SaveExpiryResult")
				return false
			}

			// Instant equivalence: Equal compares the time instant, ignoring
			// location, so a UTC-normalized read still equals the original.
			if !got.ExpiryDate.Equal(expiry) {
				t.Logf("round-trip instant mismatch: input=%v (utc=%v) got=%v",
					expiry, expiry.UTC(), got.ExpiryDate.UTC())
				return false
			}

			// Normalized to UTC on read.
			if got.ExpiryDate.Location().String() != "UTC" {
				t.Logf("expected ExpiryDate normalized to UTC, got location %q", got.ExpiryDate.Location().String())
				return false
			}

			return true
		},
		gen.Int64Range(0, maxUnix),
		gen.IntRange(minOffsetSec, maxOffsetSec),
	))

	properties.TestingRun(t)
}

// Feature: domain-expiry-monitor, Property 14: 删除往返
// **Validates: Requirements 8.4**

func TestProperty_RootDomainDeleteRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Optional expiry timestamps use second precision within [1970, 2100) so the
	// generated record covers both the "known expiry" and "unknown expiry" shapes.
	const maxUnix = int64(4102444800) // 2100-01-01T00:00:00Z

	properties.Property("Delete removes a created root domain so a subsequent GetByID reports sql.ErrNoRows", prop.ForAll(
		func(sourceIsCloudflare, monitorEnabled, alertIgnored, hasExpiry bool, unixSec int64) bool {
			db := setupTestDB(t)
			createRootDomainsSchema(t, db)
			repo := NewRootDomainRepository(db)
			ctx := context.Background()

			// source must satisfy the CHECK(source IN ('manual','cloudflare')) constraint.
			source := "manual"
			if sourceIsCloudflare {
				source = "cloudflare"
			}

			// A unique registrable domain keeps the insert clear of the UNIQUE index
			// even though each iteration already runs on a fresh in-memory DB.
			unique := uuid.New().String()[:8]

			// Optionally attach a known expiry to exercise records both with and
			// without inlined WHOIS data.
			var expiry *time.Time
			if hasExpiry {
				e := time.Unix(unixSec, 0).UTC()
				expiry = &e
			}

			rd := &model.RootDomain{
				Name:              fmt.Sprintf("example-%s.com", unique),
				Source:            source,
				RegistrableDomain: fmt.Sprintf("example-%s.com", unique),
				MonitorEnabled:    monitorEnabled,
				AlertIgnored:      alertIgnored,
				ExpiryDate:        expiry,
			}
			if err := repo.Create(ctx, rd); err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}

			// Precondition: the record exists before deletion.
			if _, err := repo.GetByID(ctx, rd.ID); err != nil {
				t.Logf("expected created root domain to exist before delete, GetByID err: %v", err)
				return false
			}

			// Delete of an existing record must succeed (return nil).
			if err := repo.Delete(ctx, rd.ID); err != nil {
				t.Logf("Delete returned error for existing record: %v", err)
				return false
			}

			// Postcondition: the root domain (and its inlined expiry data) is gone,
			// so GetByID reports not-found via sql.ErrNoRows.
			_, err := repo.GetByID(ctx, rd.ID)
			if !errors.Is(err, sql.ErrNoRows) {
				t.Logf("expected sql.ErrNoRows after Delete, got: %v", err)
				return false
			}

			return true
		},
		gen.Bool(),
		gen.Bool(),
		gen.Bool(),
		gen.Bool(),
		gen.Int64Range(0, maxUnix),
	))

	properties.TestingRun(t)
}
