package middleware

import (
	"sync"
	"time"
)

// rateBucket holds the failure count and timing for a single IP or username.
type rateBucket struct {
	count    int
	lastFail time.Time
	lockedAt *time.Time // time when count first reached threshold
}

// RateLimiter implements in-memory login rate limiting with dual dimensions (IP + username).
type RateLimiter struct {
	mu            sync.RWMutex
	ipCounters    map[string]*rateBucket
	userCounters  map[string]*rateBucket
	ipThreshold   int
	userThreshold int
	window        time.Duration
	cooldown      time.Duration
	cleanupTicker *time.Ticker
	stopCh        chan struct{}
}

// NewRateLimiter creates a new rate limiter and starts a background cleanup goroutine.
// Default recommended values: ipThreshold=20, userThreshold=10, window=15min, cooldown=15min.
func NewRateLimiter(ipThreshold, userThreshold int, window, cooldown time.Duration) *RateLimiter {
	rl := &RateLimiter{
		ipCounters:    make(map[string]*rateBucket),
		userCounters:  make(map[string]*rateBucket),
		ipThreshold:   ipThreshold,
		userThreshold: userThreshold,
		window:        window,
		cooldown:      cooldown,
		cleanupTicker: time.NewTicker(5 * time.Minute),
		stopCh:        make(chan struct{}),
	}

	go rl.cleanupLoop()

	return rl
}

// RecordFailure records a login failure for both the IP and username dimensions.
// If username is empty (e.g., readonly-login), only IP is recorded.
// If a bucket's lastFail is older than the window, the count is reset before recording
// the new failure (ensures only failures within the window are accumulated).
func (rl *RateLimiter) RecordFailure(ip, username string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Record IP failure
	if ip != "" {
		bucket := rl.ipCounters[ip]
		if bucket == nil {
			bucket = &rateBucket{}
			rl.ipCounters[ip] = bucket
		}
		// Reset if last failure is outside the window
		if !bucket.lastFail.IsZero() && now.Sub(bucket.lastFail) > rl.window {
			bucket.count = 0
			bucket.lockedAt = nil
		}
		bucket.count++
		bucket.lastFail = now
		if bucket.lockedAt == nil && bucket.count >= rl.ipThreshold {
			bucket.lockedAt = &now
		}
	}

	// Record username failure
	if username != "" {
		bucket := rl.userCounters[username]
		if bucket == nil {
			bucket = &rateBucket{}
			rl.userCounters[username] = bucket
		}
		// Reset if last failure is outside the window
		if !bucket.lastFail.IsZero() && now.Sub(bucket.lastFail) > rl.window {
			bucket.count = 0
			bucket.lockedAt = nil
		}
		bucket.count++
		bucket.lastFail = now
		if bucket.lockedAt == nil && bucket.count >= rl.userThreshold {
			bucket.lockedAt = &now
		}
	}
}

// RecordSuccess resets both the IP and username counters on successful login.
func (rl *RateLimiter) RecordSuccess(ip, username string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if ip != "" {
		delete(rl.ipCounters, ip)
	}
	if username != "" {
		delete(rl.userCounters, username)
	}
}

// IsBlocked checks if a login attempt should be blocked based on either IP or username dimension.
// Returns true if either dimension has exceeded its threshold and the cooldown hasn't elapsed.
func (rl *RateLimiter) IsBlocked(ip, username string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if ip != "" && rl.isBucketBlocked(rl.ipCounters[ip], rl.ipThreshold) {
		return true
	}
	if username != "" && rl.isBucketBlocked(rl.userCounters[username], rl.userThreshold) {
		return true
	}
	return false
}

// IsIPBlocked checks only the IP dimension (used for readonly-login).
func (rl *RateLimiter) IsIPBlocked(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	return rl.isBucketBlocked(rl.ipCounters[ip], rl.ipThreshold)
}

// Stop stops the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	rl.cleanupTicker.Stop()
	close(rl.stopCh)
}

// isBucketBlocked checks if a single bucket is in blocked state.
// A bucket is blocked when count >= threshold AND cooldown period hasn't elapsed since lastFail.
// When cooldown expires, the bucket is reset (count zeroed) so the next failures
// must accumulate from scratch before triggering another lockout.
func (rl *RateLimiter) isBucketBlocked(bucket *rateBucket, threshold int) bool {
	if bucket == nil {
		return false
	}
	if bucket.count < threshold {
		return false
	}
	// Cooldown is measured from lastFail time
	if time.Since(bucket.lastFail) >= rl.cooldown {
		// Cooldown expired — reset the bucket so failures accumulate from zero
		bucket.count = 0
		bucket.lockedAt = nil
		return false
	}
	return true
}

// cleanupLoop runs every 5 minutes and removes stale buckets.
func (rl *RateLimiter) cleanupLoop() {
	for {
		select {
		case <-rl.cleanupTicker.C:
			rl.cleanup()
		case <-rl.stopCh:
			return
		}
	}
}

// cleanup removes buckets where lastFail is older than the window duration.
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	for key, bucket := range rl.ipCounters {
		if now.Sub(bucket.lastFail) > rl.window {
			delete(rl.ipCounters, key)
		}
	}

	for key, bucket := range rl.userCounters {
		if now.Sub(bucket.lastFail) > rl.window {
			delete(rl.userCounters, key)
		}
	}
}
