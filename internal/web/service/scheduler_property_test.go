package service

import (
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestProperty12_RenewalThresholdDetection verifies that the renewal threshold detection
// logic correctly identifies certificates that need renewal based on auto_renew flag,
// days until expiry, and the configured default_before_days threshold.
//
// **Validates: Requirements 6.1, 12.1**
func TestProperty12_RenewalThresholdDetection(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Property 1: When auto_renew=true AND days_until_expiry <= default_before_days,
	// the certificate should be identified as needing renewal.
	// We generate defaultBeforeDays and a non-positive offset to ensure daysUntilExpiry <= threshold.
	properties.Property("renewal triggered when days_until_expiry <= default_before_days and auto_renew=true", prop.ForAll(
		func(defaultBeforeDays int, offset int) bool {
			// offset is 0 or negative, so daysUntilExpiry = defaultBeforeDays + offset <= defaultBeforeDays
			daysUntilExpiry := defaultBeforeDays + offset

			now := time.Now().UTC()
			expireAt := now.Add(time.Duration(daysUntilExpiry) * 24 * time.Hour)

			result := CertificateNeedsRenewal(true, expireAt, now, defaultBeforeDays)
			if !result {
				t.Logf("Expected renewal needed: auto_renew=true, daysUntilExpiry=%d, defaultBeforeDays=%d",
					daysUntilExpiry, defaultBeforeDays)
				return false
			}
			return true
		},
		gen.IntRange(7, 90),   // defaultBeforeDays
		gen.IntRange(-120, 0), // offset (ensures daysUntilExpiry <= defaultBeforeDays)
	))

	// Property 2: When days_until_expiry > default_before_days,
	// the certificate should NOT be identified as needing renewal.
	properties.Property("no renewal when days_until_expiry > default_before_days", prop.ForAll(
		func(defaultBeforeDays int, extraDays int) bool {
			// daysUntilExpiry = defaultBeforeDays + extraDays, where extraDays >= 1
			daysUntilExpiry := defaultBeforeDays + extraDays

			now := time.Now().UTC()
			expireAt := now.Add(time.Duration(daysUntilExpiry) * 24 * time.Hour)

			result := CertificateNeedsRenewal(true, expireAt, now, defaultBeforeDays)
			if result {
				t.Logf("Expected no renewal: auto_renew=true, daysUntilExpiry=%d, defaultBeforeDays=%d",
					daysUntilExpiry, defaultBeforeDays)
				return false
			}
			return true
		},
		gen.IntRange(7, 90),  // defaultBeforeDays
		gen.IntRange(1, 365), // extraDays (ensures daysUntilExpiry > defaultBeforeDays)
	))

	// Property 3: When auto_renew=false, the certificate should NEVER be identified
	// as needing renewal, regardless of how close to expiry it is.
	properties.Property("no renewal when auto_renew=false regardless of expiry", prop.ForAll(
		func(defaultBeforeDays int, daysUntilExpiry int) bool {
			now := time.Now().UTC()
			expireAt := now.Add(time.Duration(daysUntilExpiry) * 24 * time.Hour)

			result := CertificateNeedsRenewal(false, expireAt, now, defaultBeforeDays)
			if result {
				t.Logf("Expected no renewal: auto_renew=false, daysUntilExpiry=%d, defaultBeforeDays=%d",
					daysUntilExpiry, defaultBeforeDays)
				return false
			}
			return true
		},
		gen.IntRange(7, 90),    // defaultBeforeDays
		gen.IntRange(-30, 365), // daysUntilExpiry (any value, including already expired)
	))

	// Property 4: Boundary condition - when days_until_expiry == default_before_days exactly,
	// the certificate SHOULD be identified as needing renewal (requirement says "less than or equal to").
	properties.Property("renewal triggered at exact threshold boundary", prop.ForAll(
		func(defaultBeforeDays int) bool {
			now := time.Now().UTC()
			// Set expiry exactly at the threshold
			expireAt := now.Add(time.Duration(defaultBeforeDays) * 24 * time.Hour)

			result := CertificateNeedsRenewal(true, expireAt, now, defaultBeforeDays)
			if !result {
				t.Logf("Expected renewal at exact boundary: daysUntilExpiry=%d == defaultBeforeDays=%d",
					defaultBeforeDays, defaultBeforeDays)
				return false
			}
			return true
		},
		gen.IntRange(7, 90), // defaultBeforeDays
	))

	// Property 5: Already expired certificates (negative days) with auto_renew=true
	// should always be identified as needing renewal.
	properties.Property("already expired certs with auto_renew=true need renewal", prop.ForAll(
		func(defaultBeforeDays int, daysExpired int) bool {
			now := time.Now().UTC()
			// Certificate expired daysExpired days ago
			expireAt := now.Add(-time.Duration(daysExpired) * 24 * time.Hour)

			result := CertificateNeedsRenewal(true, expireAt, now, defaultBeforeDays)
			if !result {
				t.Logf("Expected renewal for expired cert: daysExpired=%d, defaultBeforeDays=%d",
					daysExpired, defaultBeforeDays)
				return false
			}
			return true
		},
		gen.IntRange(7, 90),  // defaultBeforeDays
		gen.IntRange(1, 365), // daysExpired (how many days ago it expired)
	))

	properties.TestingRun(t)
}
