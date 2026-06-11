package database

import (
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// Feature: ux-improvements-batch1, Property 6: alert_ignored Round-Trip Persistence
// **Validates: Requirements 6.1**

// TestProperty_MigrateIdempotency verifies that calling Migrate() multiple times
// (random number 1-5 times) on the same database always succeeds (idempotent).
func TestProperty_MigrateIdempotency(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("Migrate() called N times (1-5) always succeeds", prop.ForAll(
		func(n int) bool {
			db := openTestDB(t)

			for i := 0; i < n; i++ {
				if err := db.Migrate(); err != nil {
					t.Logf("Migrate() call %d/%d failed: %v", i+1, n, err)
					return false
				}
			}
			return true
		},
		gen.IntRange(1, 5),
	))

	properties.TestingRun(t)
}

// TestProperty_AlertIgnoredRoundTrip verifies that after migration, the alert_ignored
// column can store and retrieve random bool values (0/1) correctly.
func TestProperty_AlertIgnoredRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("alert_ignored stores and retrieves bool values correctly", prop.ForAll(
		func(alertIgnored bool) bool {
			db := openTestDB(t)
			if err := db.Migrate(); err != nil {
				t.Logf("Migrate() failed: %v", err)
				return false
			}

			// Convert bool to int for storage
			val := 0
			if alertIgnored {
				val = 1
			}

			// Insert a domain with the generated alert_ignored value
			_, err := db.Exec(`INSERT INTO domains (id, name, source, monitor_port, monitor_enabled, alert_ignored, created_at, updated_at)
				VALUES ('test-id', 'example.com', 'manual', 443, 1, ?, '2024-01-01', '2024-01-01')`, val)
			if err != nil {
				t.Logf("INSERT failed: %v", err)
				return false
			}

			// Read back and verify
			var readVal int
			err = db.QueryRow(`SELECT alert_ignored FROM domains WHERE id = 'test-id'`).Scan(&readVal)
			if err != nil {
				t.Logf("SELECT failed: %v", err)
				return false
			}

			readBool := readVal != 0
			return readBool == alertIgnored
		},
		gen.Bool(),
	))

	properties.TestingRun(t)
}

// TestProperty_DNSRecordIDRoundTrip verifies that after migration, the dns_record_id
// column can store and retrieve random string values correctly.
func TestProperty_DNSRecordIDRoundTrip(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("dns_record_id stores and retrieves string values correctly", prop.ForAll(
		func(dnsRecordID string) bool {
			db := openTestDB(t)
			if err := db.Migrate(); err != nil {
				t.Logf("Migrate() failed: %v", err)
				return false
			}

			// Insert a domain with the generated dns_record_id value
			_, err := db.Exec(`INSERT INTO domains (id, name, source, monitor_port, monitor_enabled, alert_ignored, dns_record_id, created_at, updated_at)
				VALUES ('test-id', 'example.com', 'manual', 443, 1, 0, ?, '2024-01-01', '2024-01-01')`, dnsRecordID)
			if err != nil {
				t.Logf("INSERT failed: %v", err)
				return false
			}

			// Read back and verify
			var readVal string
			err = db.QueryRow(`SELECT dns_record_id FROM domains WHERE id = 'test-id'`).Scan(&readVal)
			if err != nil {
				t.Logf("SELECT failed: %v", err)
				return false
			}

			return readVal == dnsRecordID
		},
		gen.AlphaString(),
	))

	properties.TestingRun(t)
}
