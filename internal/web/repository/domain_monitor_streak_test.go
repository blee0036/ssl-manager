package repository

import (
	"context"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

// insertStreakDomain creates the parent domain row for a monitor-result fixture.
// domain_monitor_results.domain_id has a foreign key to domains(id), so results cannot be
// inserted for an id that does not exist.
func insertStreakDomain(t *testing.T, repo *DomainRepository, domainID string) {
	t.Helper()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := repo.db.Exec(
		`INSERT OR IGNORE INTO domains (id, name, monitor_port, monitor_enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		domainID, domainID+".example.com", 443, 1, now, now,
	)
	if err != nil {
		t.Fatalf("failed to insert domain %s: %v", domainID, err)
	}
}

// saveResultAt stores a monitor result with an explicit checked_at and health flags.
//
// checked_at is persisted as second-precision RFC3339, so every row a test creates gets a
// distinct, explicitly spaced timestamp. Rows written within the same wall-clock second
// would tie under ORDER BY checked_at DESC and make the streak non-deterministic.
func saveResultAt(t *testing.T, repo *DomainRepository, domainID string, secondsAgo int, healthy bool) {
	t.Helper()

	insertStreakDomain(t, repo, domainID)

	result := &model.DomainMonitorResult{
		DomainID:    domainID,
		CheckedPort: 443,
		CheckedAt:   time.Now().UTC().Add(-time.Duration(secondsAgo) * time.Second),
	}
	if healthy {
		result.TLSSuccess = true
		result.DomainMatched = true
		result.ChainValid = true
	} else {
		result.ErrorMessage = "TLS handshake failed: context deadline exceeded"
	}

	if err := repo.SaveMonitorResult(context.Background(), result); err != nil {
		t.Fatalf("failed to save monitor result: %v", err)
	}
}

func TestCountConsecutiveMonitorFailures(t *testing.T) {
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		domainID string
		// history lists the health of each stored round, oldest first.
		history []bool
		limit   int
		want    int
	}{
		{
			name:     "no history yields no failures",
			domainID: "d-empty",
			history:  nil,
			limit:    2,
			want:     0,
		},
		{
			name:     "single failure counts as one",
			domainID: "d-one-fail",
			history:  []bool{false},
			limit:    2,
			want:     1,
		},
		{
			name:     "newest healthy result breaks the streak",
			domainID: "d-recovered",
			history:  []bool{false, false, true},
			limit:    3,
			want:     0,
		},
		{
			name:     "two consecutive failures count",
			domainID: "d-two-fail",
			history:  []bool{false, false},
			limit:    2,
			want:     2,
		},
		{
			name:     "healthy round in the middle resets the count",
			domainID: "d-reset",
			history:  []bool{false, true, false},
			limit:    3,
			want:     1,
		},
		{
			name:     "count is capped at the limit",
			domainID: "d-capped",
			history:  []bool{false, false, false, false},
			limit:    2,
			want:     2,
		},
		{
			name:     "zero limit short-circuits",
			domainID: "d-zero-limit",
			history:  []bool{false, false},
			limit:    0,
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Space rows 10s apart, oldest first.
			for i, healthy := range tt.history {
				secondsAgo := (len(tt.history) - i) * 10
				saveResultAt(t, repo, tt.domainID, secondsAgo, healthy)
			}

			got, err := repo.CountConsecutiveMonitorFailures(ctx, tt.domainID, tt.limit)
			if err != nil {
				t.Fatalf("CountConsecutiveMonitorFailures failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("expected streak %d, got %d", tt.want, got)
			}
		})
	}
}

func TestCountConsecutiveMonitorFailures_FingerprintMismatchCountsAsFailure(t *testing.T) {
	// A fingerprint mismatch keeps tls_success = 1 and domain_matched = 1 but carries an
	// error message. It must still count as a failure, otherwise the streak used to gate
	// alerts would disagree with the condition used to auto-resolve them.
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	insertStreakDomain(t, repo, "d-fp")

	result := &model.DomainMonitorResult{
		DomainID:      "d-fp",
		CheckedPort:   443,
		TLSSuccess:    true,
		DomainMatched: true,
		ChainValid:    true,
		ErrorMessage:  "fingerprint mismatch: online=aaa, system=bbb",
		CheckedAt:     time.Now().UTC(),
	}
	if err := repo.SaveMonitorResult(ctx, result); err != nil {
		t.Fatalf("failed to save monitor result: %v", err)
	}

	got, err := repo.CountConsecutiveMonitorFailures(ctx, "d-fp", 2)
	if err != nil {
		t.Fatalf("CountConsecutiveMonitorFailures failed: %v", err)
	}
	if got != 1 {
		t.Errorf("expected a fingerprint mismatch to count as 1 failure, got %d", got)
	}
}

func TestCountConsecutiveMonitorFailures_IsScopedPerDomain(t *testing.T) {
	// One domain's failures must not leak into another domain's streak.
	repo := setupDomainTestDB(t)
	ctx := context.Background()

	saveResultAt(t, repo, "d-noisy", 20, false)
	saveResultAt(t, repo, "d-noisy", 10, false)
	saveResultAt(t, repo, "d-quiet", 15, true)

	got, err := repo.CountConsecutiveMonitorFailures(ctx, "d-quiet", 2)
	if err != nil {
		t.Fatalf("CountConsecutiveMonitorFailures failed: %v", err)
	}
	if got != 0 {
		t.Errorf("expected the healthy domain's streak to be 0, got %d", got)
	}

	noisy, err := repo.CountConsecutiveMonitorFailures(ctx, "d-noisy", 2)
	if err != nil {
		t.Fatalf("CountConsecutiveMonitorFailures failed: %v", err)
	}
	if noisy != 2 {
		t.Errorf("expected the failing domain's streak to be 2, got %d", noisy)
	}
}
