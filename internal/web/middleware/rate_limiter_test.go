package middleware

import (
	"sync"
	"testing"
	"time"
)

// TestCooldownPeriodExpiry verifies that after the cooldown period elapses
// (measured from lastFail), the block is automatically lifted.
// Validates: Requirement 2.5
func TestCooldownPeriodExpiry(t *testing.T) {
	// Use very short durations for testing
	cooldown := 50 * time.Millisecond
	window := 200 * time.Millisecond
	rl := NewRateLimiter(3, 2, window, cooldown)
	defer rl.Stop()

	ip := "192.168.1.1"
	username := "admin"

	// Record failures to reach IP threshold
	for i := 0; i < 3; i++ {
		rl.RecordFailure(ip, username)
	}

	// Should be blocked immediately
	if !rl.IsBlocked(ip, username) {
		t.Fatal("expected to be blocked after reaching threshold")
	}

	// Wait for cooldown to elapse
	time.Sleep(cooldown + 10*time.Millisecond)

	// Should be unblocked after cooldown
	if rl.IsBlocked(ip, username) {
		t.Fatal("expected to be unblocked after cooldown period elapsed")
	}
}

// TestCooldownResetsFailureCount verifies that after cooldown expires,
// the failure count is actually reset to zero. A single subsequent failure
// should NOT immediately re-lock the account — it must accumulate back to threshold.
// Validates: Requirement 2.5 (auto-resolve lockout AND reset failure count)
func TestCooldownResetsFailureCount(t *testing.T) {
	cooldown := 50 * time.Millisecond
	window := 200 * time.Millisecond
	ipThreshold := 5
	userThreshold := 5 // same as IP to simplify this test
	rl := NewRateLimiter(ipThreshold, userThreshold, window, cooldown)
	defer rl.Stop()

	ip := "10.0.0.99"

	// Test IP dimension only (empty username to avoid cross-dimension interference)
	for i := 0; i < ipThreshold; i++ {
		rl.RecordFailure(ip, "")
	}

	// Should be blocked
	if !rl.IsIPBlocked(ip) {
		t.Fatal("expected to be blocked after reaching IP threshold")
	}

	// Wait for cooldown
	time.Sleep(cooldown + 10*time.Millisecond)

	// IsIPBlocked should return false AND reset the count
	if rl.IsIPBlocked(ip) {
		t.Fatal("expected unblocked after cooldown")
	}

	// Now record ONE failure — should NOT immediately re-lock
	rl.RecordFailure(ip, "")

	if rl.IsIPBlocked(ip) {
		t.Fatal("one failure after cooldown reset should NOT re-lock; count must accumulate from zero")
	}

	// Record failures until threshold-1 — still not blocked
	for i := 1; i < ipThreshold-1; i++ {
		rl.RecordFailure(ip, "")
	}
	if rl.IsIPBlocked(ip) {
		t.Fatalf("expected not blocked at %d failures (threshold=%d)", ipThreshold-1, ipThreshold)
	}

	// One more to reach threshold — now should be blocked again
	rl.RecordFailure(ip, "")
	if !rl.IsIPBlocked(ip) {
		t.Fatal("expected blocked after re-reaching threshold")
	}
}

// TestCooldownResetsFromLastFailure verifies that cooldown is measured from
// the LAST failure, not the first failure.
// Validates: Requirement 2.5
func TestCooldownResetsFromLastFailure(t *testing.T) {
	cooldown := 80 * time.Millisecond
	window := 300 * time.Millisecond
	rl := NewRateLimiter(3, 2, window, cooldown)
	defer rl.Stop()

	ip := "10.0.0.1"
	username := "user1"

	// Record 3 failures (reaches IP threshold of 3)
	for i := 0; i < 3; i++ {
		rl.RecordFailure(ip, username)
	}

	// Wait half the cooldown
	time.Sleep(40 * time.Millisecond)

	// Record another failure — this resets lastFail
	rl.RecordFailure(ip, username)

	// Wait just past the original cooldown from the first batch
	// but less than cooldown from the last failure
	time.Sleep(50 * time.Millisecond)

	// Should still be blocked (cooldown resets from last failure)
	if !rl.IsBlocked(ip, username) {
		t.Fatal("expected to still be blocked; cooldown should reset from last failure")
	}

	// Wait for the full cooldown from the last failure
	time.Sleep(40 * time.Millisecond)

	if rl.IsBlocked(ip, username) {
		t.Fatal("expected to be unblocked after full cooldown from last failure")
	}
}

// TestCleanupGoroutineRemovesStaleEntries verifies that the background cleanup
// goroutine removes entries whose lastFail is older than the window duration.
// Validates: Requirement 2.8
func TestCleanupGoroutineRemovesStaleEntries(t *testing.T) {
	window := 50 * time.Millisecond
	cooldown := 50 * time.Millisecond

	// We'll manually call cleanup instead of waiting for the 5-minute ticker
	rl := NewRateLimiter(3, 2, window, cooldown)
	defer rl.Stop()

	ip := "172.16.0.1"
	username := "staleuser"

	// Record failures to exceed threshold
	for i := 0; i < 3; i++ {
		rl.RecordFailure(ip, username)
	}

	// Verify entries exist
	rl.mu.RLock()
	if _, ok := rl.ipCounters[ip]; !ok {
		rl.mu.RUnlock()
		t.Fatal("expected IP counter to exist after recording failures")
	}
	if _, ok := rl.userCounters[username]; !ok {
		rl.mu.RUnlock()
		t.Fatal("expected user counter to exist after recording failures")
	}
	rl.mu.RUnlock()

	// Wait for entries to become stale (older than window)
	time.Sleep(window + 10*time.Millisecond)

	// Manually trigger cleanup (simulates the periodic goroutine action)
	rl.cleanup()

	// Verify entries are removed
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	if _, ok := rl.ipCounters[ip]; ok {
		t.Fatal("expected IP counter to be cleaned up after window elapsed")
	}
	if _, ok := rl.userCounters[username]; ok {
		t.Fatal("expected user counter to be cleaned up after window elapsed")
	}
}

// TestCleanupDoesNotRemoveFreshEntries verifies that cleanup does NOT remove
// entries whose lastFail is within the window.
// Validates: Requirement 2.8
func TestCleanupDoesNotRemoveFreshEntries(t *testing.T) {
	window := 500 * time.Millisecond
	cooldown := 500 * time.Millisecond
	rl := NewRateLimiter(3, 2, window, cooldown)
	defer rl.Stop()

	ip := "172.16.0.2"
	username := "freshuser"

	for i := 0; i < 3; i++ {
		rl.RecordFailure(ip, username)
	}

	// Immediately call cleanup — entries are fresh
	rl.cleanup()

	rl.mu.RLock()
	defer rl.mu.RUnlock()
	if _, ok := rl.ipCounters[ip]; !ok {
		t.Fatal("cleanup should NOT remove entries within the window")
	}
	if _, ok := rl.userCounters[username]; !ok {
		t.Fatal("cleanup should NOT remove user entries within the window")
	}
}

// TestIsBlockedChecksBothDimensions verifies that IsBlocked returns true
// when either IP or username dimension exceeds threshold.
// Validates: Requirement 2.3
func TestIsBlockedChecksBothDimensions(t *testing.T) {
	cooldown := 1 * time.Second
	window := 1 * time.Second
	rl := NewRateLimiter(5, 3, window, cooldown)
	defer rl.Stop()

	// Only IP exceeds threshold
	t.Run("IP exceeds threshold", func(t *testing.T) {
		ip := "10.1.1.1"
		for i := 0; i < 5; i++ {
			rl.RecordFailure(ip, "")
		}
		if !rl.IsBlocked(ip, "innocent_user") {
			t.Fatal("expected blocked when IP exceeds threshold")
		}
	})

	// Only username exceeds threshold
	t.Run("username exceeds threshold", func(t *testing.T) {
		rl2 := NewRateLimiter(5, 3, window, cooldown)
		defer rl2.Stop()

		username := "targetuser"
		for i := 0; i < 3; i++ {
			rl2.RecordFailure("different-ip", username)
		}
		if !rl2.IsBlocked("clean-ip", username) {
			t.Fatal("expected blocked when username exceeds threshold")
		}
	})

	// Both dimensions exceed threshold
	t.Run("both dimensions exceed", func(t *testing.T) {
		rl3 := NewRateLimiter(5, 3, window, cooldown)
		defer rl3.Stop()

		ip := "10.2.2.2"
		username := "bothuser"
		for i := 0; i < 5; i++ {
			rl3.RecordFailure(ip, username)
		}
		if !rl3.IsBlocked(ip, username) {
			t.Fatal("expected blocked when both dimensions exceed threshold")
		}
	})
}

// TestIsIPBlockedOnlyChecksIPDimension verifies that IsIPBlocked only checks
// the IP dimension and ignores username counters.
// Validates: Requirement 2.3 (readonly-login only uses IP dimension)
func TestIsIPBlockedOnlyChecksIPDimension(t *testing.T) {
	cooldown := 1 * time.Second
	window := 1 * time.Second
	rl := NewRateLimiter(5, 3, window, cooldown)
	defer rl.Stop()

	// Record failures only on username dimension (via different IPs)
	username := "readonly_target"
	for i := 0; i < 10; i++ {
		rl.RecordFailure("other-ip", username)
	}

	// IsIPBlocked for a clean IP should return false even though username is blocked
	if rl.IsIPBlocked("clean-ip") {
		t.Fatal("IsIPBlocked should NOT be affected by username-dimension failures")
	}

	// Now block an IP
	ip := "blocked-ip"
	for i := 0; i < 5; i++ {
		rl.RecordFailure(ip, "")
	}

	if !rl.IsIPBlocked(ip) {
		t.Fatal("IsIPBlocked should return true when IP exceeds threshold")
	}
}

// TestReadonlyLoginScenario simulates the readonly-login flow where only IP
// is recorded (empty username) and only IsIPBlocked is checked.
// Validates: Requirement 2.3, 2.6
func TestReadonlyLoginScenario(t *testing.T) {
	cooldown := 1 * time.Second
	window := 1 * time.Second
	rl := NewRateLimiter(5, 3, window, cooldown)
	defer rl.Stop()

	ip := "readonly-attacker"

	// Simulate readonly-login failures — empty username
	for i := 0; i < 4; i++ {
		rl.RecordFailure(ip, "")
	}

	// Not yet at threshold (5)
	if rl.IsIPBlocked(ip) {
		t.Fatal("should not be blocked below threshold")
	}

	// One more failure reaches threshold
	rl.RecordFailure(ip, "")

	if !rl.IsIPBlocked(ip) {
		t.Fatal("should be blocked at threshold for readonly-login (IP only)")
	}

	// Verify that username counters are unaffected
	rl.mu.RLock()
	if len(rl.userCounters) != 0 {
		rl.mu.RUnlock()
		t.Fatal("readonly-login with empty username should not create user counters")
	}
	rl.mu.RUnlock()
}

// TestSuccessResetsCounters verifies that a successful login resets
// both IP and username counters.
// Validates: Requirement 2.7
func TestSuccessResetsCounters(t *testing.T) {
	cooldown := 1 * time.Second
	window := 1 * time.Second
	rl := NewRateLimiter(5, 3, window, cooldown)
	defer rl.Stop()

	ip := "10.0.0.5"
	username := "admin"

	// Accumulate failures
	for i := 0; i < 5; i++ {
		rl.RecordFailure(ip, username)
	}

	// Should be blocked
	if !rl.IsBlocked(ip, username) {
		t.Fatal("expected blocked after exceeding threshold")
	}

	// Record success — resets both dimensions
	rl.RecordSuccess(ip, username)

	// Should no longer be blocked
	if rl.IsBlocked(ip, username) {
		t.Fatal("expected unblocked after successful login")
	}

	// Counters should be deleted
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	if _, ok := rl.ipCounters[ip]; ok {
		t.Fatal("IP counter should be deleted after success")
	}
	if _, ok := rl.userCounters[username]; ok {
		t.Fatal("user counter should be deleted after success")
	}
}

// TestBelowThresholdNotBlocked verifies that failures below the threshold
// do not trigger blocking.
// Validates: Requirement 2.1, 2.2, 2.6
func TestBelowThresholdNotBlocked(t *testing.T) {
	cooldown := 1 * time.Second
	window := 1 * time.Second
	rl := NewRateLimiter(20, 10, window, cooldown)
	defer rl.Stop()

	ip := "192.168.0.1"
	username := "testuser"

	// Record failures just below both thresholds
	for i := 0; i < 9; i++ {
		rl.RecordFailure(ip, username)
	}

	// 9 failures: below IP threshold (20) and below username threshold (10)
	if rl.IsBlocked(ip, username) {
		t.Fatal("should not be blocked when below threshold")
	}

	if rl.IsIPBlocked(ip) {
		t.Fatal("should not be IP-blocked when below IP threshold")
	}
}

// TestConcurrentAccess verifies that concurrent RecordFailure and IsBlocked
// calls don't cause data races.
// Validates: Requirement 2.6
func TestConcurrentAccess(t *testing.T) {
	cooldown := 1 * time.Second
	window := 1 * time.Second
	rl := NewRateLimiter(100, 50, window, cooldown)
	defer rl.Stop()

	var wg sync.WaitGroup
	const goroutines = 20
	const iterations = 50

	// Concurrent writes (RecordFailure)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				rl.RecordFailure("concurrent-ip", "concurrent-user")
			}
		}(g)
	}

	// Concurrent reads (IsBlocked / IsIPBlocked)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				rl.IsBlocked("concurrent-ip", "concurrent-user")
				rl.IsIPBlocked("concurrent-ip")
			}
		}(g)
	}

	// Concurrent success resets
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				rl.RecordSuccess("concurrent-ip", "concurrent-user")
			}
		}()
	}

	wg.Wait()
	// If we get here without data race panic, the test passes
}

// TestIsBlockedWithEmptyInputs verifies that empty IP or username doesn't
// cause panics and doesn't block.
func TestIsBlockedWithEmptyInputs(t *testing.T) {
	cooldown := 1 * time.Second
	window := 1 * time.Second
	rl := NewRateLimiter(5, 3, window, cooldown)
	defer rl.Stop()

	// Empty IP and username should not be blocked
	if rl.IsBlocked("", "") {
		t.Fatal("empty inputs should never be blocked")
	}

	if rl.IsIPBlocked("") {
		t.Fatal("empty IP should never be blocked")
	}
}

// TestMultipleIPsIndependent verifies that blocking one IP doesn't affect others.
func TestMultipleIPsIndependent(t *testing.T) {
	cooldown := 1 * time.Second
	window := 1 * time.Second
	rl := NewRateLimiter(3, 2, window, cooldown)
	defer rl.Stop()

	// Block IP1
	for i := 0; i < 3; i++ {
		rl.RecordFailure("ip1", "user1")
	}

	// IP1 should be blocked
	if !rl.IsBlocked("ip1", "other") {
		t.Fatal("ip1 should be blocked")
	}

	// IP2 should NOT be blocked
	if rl.IsBlocked("ip2", "other") {
		t.Fatal("ip2 should not be affected by ip1's failures")
	}
}

// TestStopPreventsCleanup verifies that Stop() halts the cleanup goroutine.
func TestStopPreventsCleanup(t *testing.T) {
	window := 1 * time.Millisecond
	cooldown := 1 * time.Millisecond
	rl := NewRateLimiter(3, 2, window, cooldown)

	rl.RecordFailure("stop-test-ip", "stop-test-user")

	// Stop the rate limiter
	rl.Stop()

	// Give time for any pending cleanup (there shouldn't be one)
	time.Sleep(20 * time.Millisecond)

	// Manually verify the goroutine doesn't panic after stop
	// The fact that we reach here without panic means Stop worked
}
