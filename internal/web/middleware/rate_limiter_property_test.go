package middleware

import (
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestProperty5_RateLimiterResponseConsistency verifies that regardless of which
// dimension (IP or username) triggers the block, the IsBlocked function returns
// the same type of response (a single bool). This structural property ensures that
// the handler layer cannot distinguish which dimension caused the block, preventing
// information leakage in the 429 response.
//
// **Validates: Requirements 2.4**
func TestProperty5_RateLimiterResponseConsistency(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 200
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Property: When IP exceeds threshold but username does not, IsBlocked returns true (same bool type)
	properties.Property("IP-triggered block returns same bool as username-triggered block", prop.ForAll(
		func(ip string, username string, ipFailures int, userFailures int) bool {
			// Ensure we have valid inputs
			if ip == "" || username == "" {
				return true
			}

			ipThreshold := 5
			userThreshold := 3
			window := 15 * time.Minute
			cooldown := 15 * time.Minute

			// --- Scenario 1: Only IP exceeds threshold ---
			rl1 := NewRateLimiter(ipThreshold, userThreshold, window, cooldown)
			defer rl1.Stop()

			// Record enough IP failures to exceed threshold
			for i := 0; i < ipThreshold; i++ {
				rl1.RecordFailure(ip, "")
			}
			ipBlocked := rl1.IsBlocked(ip, username)

			// --- Scenario 2: Only username exceeds threshold ---
			rl2 := NewRateLimiter(ipThreshold, userThreshold, window, cooldown)
			defer rl2.Stop()

			// Record enough username failures to exceed threshold
			for i := 0; i < userThreshold; i++ {
				rl2.RecordFailure("other-ip", username)
			}
			userBlocked := rl2.IsBlocked(ip, username)

			// --- Scenario 3: Both exceed threshold ---
			rl3 := NewRateLimiter(ipThreshold, userThreshold, window, cooldown)
			defer rl3.Stop()

			for i := 0; i < ipThreshold; i++ {
				rl3.RecordFailure(ip, "")
			}
			for i := 0; i < userThreshold; i++ {
				rl3.RecordFailure("another-ip", username)
			}
			bothBlocked := rl3.IsBlocked(ip, username)

			// ALL scenarios must return true — the same bool value
			// There's no way to distinguish which dimension triggered it
			if !ipBlocked {
				t.Logf("IP-triggered block returned false for ip=%q (expected true)", ip)
				return false
			}
			if !userBlocked {
				t.Logf("Username-triggered block returned false for username=%q (expected true)", username)
				return false
			}
			if !bothBlocked {
				t.Logf("Both-triggered block returned false (expected true)")
				return false
			}

			// The key property: all three return the exact same value (true)
			// A handler receiving this bool cannot tell WHICH dimension triggered it
			return ipBlocked == userBlocked && userBlocked == bothBlocked
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(5, 30),
		gen.IntRange(3, 20),
	))

	// Property: IsBlocked return type is indistinguishable regardless of trigger source
	// Even with different failure counts above threshold, the response is the same bool
	properties.Property("response is uniform bool regardless of how far above threshold", prop.ForAll(
		func(ip string, username string, extraIPFails int, extraUserFails int) bool {
			if ip == "" || username == "" {
				return true
			}

			ipThreshold := 5
			userThreshold := 3
			window := 15 * time.Minute
			cooldown := 15 * time.Minute

			// IP far above threshold
			rl1 := NewRateLimiter(ipThreshold, userThreshold, window, cooldown)
			defer rl1.Stop()
			for i := 0; i < ipThreshold+extraIPFails; i++ {
				rl1.RecordFailure(ip, "")
			}
			result1 := rl1.IsBlocked(ip, username)

			// Username far above threshold
			rl2 := NewRateLimiter(ipThreshold, userThreshold, window, cooldown)
			defer rl2.Stop()
			for i := 0; i < userThreshold+extraUserFails; i++ {
				rl2.RecordFailure("different-ip", username)
			}
			result2 := rl2.IsBlocked(ip, username)

			// Both must be true and indistinguishable
			if !result1 {
				t.Logf("IP blocked (count=%d, threshold=%d) returned false", ipThreshold+extraIPFails, ipThreshold)
				return false
			}
			if !result2 {
				t.Logf("Username blocked (count=%d, threshold=%d) returned false", userThreshold+extraUserFails, userThreshold)
				return false
			}

			// The return value is the same regardless of trigger dimension
			return result1 == result2
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(0, 50),
		gen.IntRange(0, 50),
	))

	// Property: When not blocked, the return is also uniform (false) regardless of which dimension has failures
	properties.Property("below-threshold returns uniform false regardless of dimension with failures", prop.ForAll(
		func(ip string, username string, ipFails int, userFails int) bool {
			if ip == "" || username == "" {
				return true
			}

			ipThreshold := 20
			userThreshold := 10
			window := 15 * time.Minute
			cooldown := 15 * time.Minute

			// IP has some failures but below threshold
			rl1 := NewRateLimiter(ipThreshold, userThreshold, window, cooldown)
			defer rl1.Stop()
			for i := 0; i < ipFails; i++ {
				rl1.RecordFailure(ip, "")
			}
			result1 := rl1.IsBlocked(ip, username)

			// Username has some failures but below threshold
			rl2 := NewRateLimiter(ipThreshold, userThreshold, window, cooldown)
			defer rl2.Stop()
			for i := 0; i < userFails; i++ {
				rl2.RecordFailure("other-ip", username)
			}
			result2 := rl2.IsBlocked(ip, username)

			// Both should be false — same response regardless of which dimension has failures
			if result1 {
				t.Logf("IP below threshold (%d/%d) returned true", ipFails, ipThreshold)
				return false
			}
			if result2 {
				t.Logf("Username below threshold (%d/%d) returned true", userFails, userThreshold)
				return false
			}

			return result1 == result2
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
		gen.IntRange(0, 19),  // below IP threshold of 20
		gen.IntRange(0, 9),   // below username threshold of 10
	))

	properties.TestingRun(t)
}
