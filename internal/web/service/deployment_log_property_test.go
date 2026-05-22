package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

// --- Generators ---

// genLogCount generates a random number of logs between 31 and 100 (exceeds the 30-log retention limit).
func genLogCount() gopter.Gen {
	return gen.IntRange(31, 100)
}

// genLogCountForOrder generates a random number of logs between 1 and 50 for ordering tests.
func genLogCountForOrder() gopter.Gen {
	return gen.IntRange(1, 50)
}

// genDeploymentStatus generates a valid deployment log status.
func genDeploymentStatus() gopter.Gen {
	return gen.OneConstOf("success", "failed", "skipped")
}

// --- Property Tests ---

// TestProperty17_DeploymentLogRetentionLimit verifies that for any machine certificate,
// the system should keep at most 30 deployment logs. When more than 30 are created,
// the oldest should be deleted.
//
// **Validates: Requirements 9.2**
func TestProperty17_DeploymentLogRetentionLimit(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Property: After creating N > 30 logs via the service, at most 30 remain
	properties.Property("at most 30 deployment logs remain after creating more than 30", prop.ForAll(
		func(logCount int, status string) bool {
			svc := setupDeploymentLogServiceTestDB(t)
			ctx := context.Background()

			baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

			// Create logCount logs for the same machine certificate via the service
			for i := 0; i < logCount; i++ {
				log := &model.DeploymentLog{
					MachineCertificateID:  "mc-prop17",
					MachineID:             "m-1",
					CertificateID:         "c-1",
					Status:                status,
					CertFingerprintSHA256: fmt.Sprintf("fingerprint-%d", i),
					CertPath:              "/etc/ssl/cert.pem",
					PrivateKeyPath:        "/etc/ssl/key.pem",
					CommandOutputs:        []model.CommandOutput{},
					ErrorMessage:          "",
					StartedAt:             baseTime.Add(time.Duration(i) * time.Second),
					FinishedAt:            baseTime.Add(time.Duration(i)*time.Second + 5*time.Second),
					CreatedAt:             baseTime.Add(time.Duration(i) * time.Second),
				}
				if err := svc.Create(ctx, log); err != nil {
					t.Logf("Failed to create log %d: %v", i, err)
					return false
				}
			}

			// Query all logs for this machine certificate
			logs, err := svc.GetByMachineCertificateID(ctx, "mc-prop17")
			if err != nil {
				t.Logf("Failed to query logs: %v", err)
				return false
			}

			// At most 30 logs should remain
			if len(logs) > 30 {
				t.Logf("Expected at most 30 logs, got %d (created %d)", len(logs), logCount)
				return false
			}

			return true
		},
		genLogCount(),
		genDeploymentStatus(),
	))

	// Property: The remaining logs after retention are the newest ones
	properties.Property("remaining logs after retention are the newest ones", prop.ForAll(
		func(logCount int) bool {
			svc := setupDeploymentLogServiceTestDB(t)
			ctx := context.Background()

			baseTime := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

			// Create logCount logs with strictly increasing timestamps
			for i := 0; i < logCount; i++ {
				log := &model.DeploymentLog{
					MachineCertificateID:  "mc-prop17-newest",
					MachineID:             "m-1",
					CertificateID:         "c-1",
					Status:                "success",
					CertFingerprintSHA256: fmt.Sprintf("fp-%d", i),
					CertPath:              "/etc/ssl/cert.pem",
					PrivateKeyPath:        "/etc/ssl/key.pem",
					CommandOutputs:        []model.CommandOutput{},
					ErrorMessage:          "",
					StartedAt:             baseTime.Add(time.Duration(i) * time.Minute),
					FinishedAt:            baseTime.Add(time.Duration(i)*time.Minute + 30*time.Second),
					CreatedAt:             baseTime.Add(time.Duration(i) * time.Minute),
				}
				if err := svc.Create(ctx, log); err != nil {
					t.Logf("Failed to create log %d: %v", i, err)
					return false
				}
			}

			// Query remaining logs
			logs, err := svc.GetByMachineCertificateID(ctx, "mc-prop17-newest")
			if err != nil {
				t.Logf("Failed to query logs: %v", err)
				return false
			}

			// The oldest remaining log should have been created at index (logCount - 30)
			// since we keep the newest 30
			expectedOldestIndex := logCount - 30
			expectedOldestTime := baseTime.Add(time.Duration(expectedOldestIndex) * time.Minute)

			// The last log in the result (oldest of the remaining) should be >= expectedOldestTime
			if len(logs) > 0 {
				oldestRemaining := logs[len(logs)-1].CreatedAt
				// Allow 1 second tolerance for time comparison
				if oldestRemaining.Before(expectedOldestTime.Add(-time.Second)) {
					t.Logf("Oldest remaining log time %v is before expected %v (created %d logs)",
						oldestRemaining, expectedOldestTime, logCount)
					return false
				}
			}

			return true
		},
		genLogCount(),
	))

	// Property: Retention is per machine certificate - other machine certificates are unaffected
	properties.Property("retention limit is per machine certificate", prop.ForAll(
		func(logCount int) bool {
			svc := setupDeploymentLogServiceTestDB(t)
			ctx := context.Background()

			baseTime := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

			// Create logCount logs for mc-A (exceeds limit)
			for i := 0; i < logCount; i++ {
				log := &model.DeploymentLog{
					MachineCertificateID:  "mc-A",
					MachineID:             "m-1",
					CertificateID:         "c-1",
					Status:                "success",
					CertFingerprintSHA256: fmt.Sprintf("fp-a-%d", i),
					CertPath:              "/etc/ssl/cert.pem",
					PrivateKeyPath:        "/etc/ssl/key.pem",
					CommandOutputs:        []model.CommandOutput{},
					ErrorMessage:          "",
					StartedAt:             baseTime.Add(time.Duration(i) * time.Second),
					FinishedAt:            baseTime.Add(time.Duration(i)*time.Second + 5*time.Second),
					CreatedAt:             baseTime.Add(time.Duration(i) * time.Second),
				}
				if err := svc.Create(ctx, log); err != nil {
					t.Logf("Failed to create log for mc-A: %v", err)
					return false
				}
			}

			// Create 10 logs for mc-B (under limit)
			for i := 0; i < 10; i++ {
				log := &model.DeploymentLog{
					MachineCertificateID:  "mc-B",
					MachineID:             "m-2",
					CertificateID:         "c-2",
					Status:                "success",
					CertFingerprintSHA256: fmt.Sprintf("fp-b-%d", i),
					CertPath:              "/etc/ssl/cert2.pem",
					PrivateKeyPath:        "/etc/ssl/key2.pem",
					CommandOutputs:        []model.CommandOutput{},
					ErrorMessage:          "",
					StartedAt:             baseTime.Add(time.Duration(i) * time.Second),
					FinishedAt:            baseTime.Add(time.Duration(i)*time.Second + 5*time.Second),
					CreatedAt:             baseTime.Add(time.Duration(i) * time.Second),
				}
				if err := svc.Create(ctx, log); err != nil {
					t.Logf("Failed to create log for mc-B: %v", err)
					return false
				}
			}

			// mc-A should have at most 30
			logsA, err := svc.GetByMachineCertificateID(ctx, "mc-A")
			if err != nil {
				t.Logf("Failed to query mc-A logs: %v", err)
				return false
			}
			if len(logsA) > 30 {
				t.Logf("mc-A: expected at most 30 logs, got %d", len(logsA))
				return false
			}

			// mc-B should still have all 10
			logsB, err := svc.GetByMachineCertificateID(ctx, "mc-B")
			if err != nil {
				t.Logf("Failed to query mc-B logs: %v", err)
				return false
			}
			if len(logsB) != 10 {
				t.Logf("mc-B: expected 10 logs, got %d", len(logsB))
				return false
			}

			return true
		},
		genLogCount(),
	))

	properties.TestingRun(t)
}

// TestProperty18_DeploymentLogTimeDescOrder verifies that deployment logs should
// always be returned in time descending order (newest first).
//
// **Validates: Requirements 9.3**
func TestProperty18_DeploymentLogTimeDescOrder(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Property: Logs are always returned in strict descending order by created_at
	properties.Property("deployment logs returned in strict time descending order", prop.ForAll(
		func(logCount int, baseOffset int) bool {
			svc := setupDeploymentLogServiceTestDB(t)
			ctx := context.Background()

			baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(baseOffset) * time.Hour)

			// Create logs with varying time gaps to simulate random timestamps
			for i := 0; i < logCount; i++ {
				// Use non-uniform time gaps to make the test more interesting
				offset := time.Duration(i*7+3) * time.Second
				log := &model.DeploymentLog{
					MachineCertificateID:  "mc-prop18",
					MachineID:             "m-1",
					CertificateID:         "c-1",
					Status:                "success",
					CertFingerprintSHA256: fmt.Sprintf("fp-%d", i),
					CertPath:              "/etc/ssl/cert.pem",
					PrivateKeyPath:        "/etc/ssl/key.pem",
					CommandOutputs:        []model.CommandOutput{},
					ErrorMessage:          "",
					StartedAt:             baseTime.Add(offset),
					FinishedAt:            baseTime.Add(offset + 5*time.Second),
					CreatedAt:             baseTime.Add(offset),
				}
				if err := svc.Create(ctx, log); err != nil {
					t.Logf("Failed to create log %d: %v", i, err)
					return false
				}
			}

			// Query logs
			logs, err := svc.GetByMachineCertificateID(ctx, "mc-prop18")
			if err != nil {
				t.Logf("Failed to query logs: %v", err)
				return false
			}

			// Verify strict descending order
			for i := 1; i < len(logs); i++ {
				if !logs[i-1].CreatedAt.After(logs[i].CreatedAt) {
					t.Logf("Logs not in strict DESC order at index %d: %v <= %v",
						i, logs[i-1].CreatedAt, logs[i].CreatedAt)
					return false
				}
			}

			return true
		},
		genLogCountForOrder(),
		gen.IntRange(0, 1000),
	))

	// Property: Logs inserted in random order are still returned in descending order
	properties.Property("logs inserted in random order still returned in descending order", prop.ForAll(
		func(logCount int) bool {
			svc := setupDeploymentLogServiceTestDB(t)
			ctx := context.Background()

			baseTime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

			// Create logs with timestamps that are NOT in sequential order
			// Use a pattern that creates out-of-order insertions
			for i := 0; i < logCount; i++ {
				// Alternate between past and future offsets to simulate out-of-order creation
				var offset time.Duration
				if i%2 == 0 {
					offset = time.Duration(i*10) * time.Second
				} else {
					offset = time.Duration((logCount-i)*10) * time.Second
				}

				log := &model.DeploymentLog{
					MachineCertificateID:  "mc-prop18-random",
					MachineID:             "m-1",
					CertificateID:         "c-1",
					Status:                "success",
					CertFingerprintSHA256: fmt.Sprintf("fp-rand-%d", i),
					CertPath:              "/etc/ssl/cert.pem",
					PrivateKeyPath:        "/etc/ssl/key.pem",
					CommandOutputs:        []model.CommandOutput{},
					ErrorMessage:          "",
					StartedAt:             baseTime.Add(offset),
					FinishedAt:            baseTime.Add(offset + 5*time.Second),
					CreatedAt:             baseTime.Add(offset),
				}
				if err := svc.Create(ctx, log); err != nil {
					t.Logf("Failed to create log %d: %v", i, err)
					return false
				}
			}

			// Query logs
			logs, err := svc.GetByMachineCertificateID(ctx, "mc-prop18-random")
			if err != nil {
				t.Logf("Failed to query logs: %v", err)
				return false
			}

			// Verify descending order (allow equal for same-second timestamps)
			for i := 1; i < len(logs); i++ {
				if logs[i].CreatedAt.After(logs[i-1].CreatedAt) {
					t.Logf("Logs not in DESC order at index %d: %v > %v",
						i, logs[i].CreatedAt, logs[i-1].CreatedAt)
					return false
				}
			}

			return true
		},
		genLogCountForOrder(),
	))

	// Property: Mixed statuses don't affect ordering
	properties.Property("mixed deployment statuses do not affect time ordering", prop.ForAll(
		func(logCount int, statuses []string) bool {
			svc := setupDeploymentLogServiceTestDB(t)
			ctx := context.Background()

			baseTime := time.Date(2024, 9, 1, 0, 0, 0, 0, time.UTC)

			// Create logs with different statuses
			for i := 0; i < logCount; i++ {
				status := statuses[i%len(statuses)]
				log := &model.DeploymentLog{
					MachineCertificateID:  "mc-prop18-mixed",
					MachineID:             "m-1",
					CertificateID:         "c-1",
					Status:                status,
					CertFingerprintSHA256: fmt.Sprintf("fp-mixed-%d", i),
					CertPath:              "/etc/ssl/cert.pem",
					PrivateKeyPath:        "/etc/ssl/key.pem",
					CommandOutputs:        []model.CommandOutput{},
					ErrorMessage:          "",
					StartedAt:             baseTime.Add(time.Duration(i) * time.Minute),
					FinishedAt:            baseTime.Add(time.Duration(i)*time.Minute + 30*time.Second),
					CreatedAt:             baseTime.Add(time.Duration(i) * time.Minute),
				}
				if err := svc.Create(ctx, log); err != nil {
					t.Logf("Failed to create log %d: %v", i, err)
					return false
				}
			}

			// Query logs
			logs, err := svc.GetByMachineCertificateID(ctx, "mc-prop18-mixed")
			if err != nil {
				t.Logf("Failed to query logs: %v", err)
				return false
			}

			// Verify descending order
			for i := 1; i < len(logs); i++ {
				if logs[i].CreatedAt.After(logs[i-1].CreatedAt) {
					t.Logf("Logs not in DESC order at index %d: %v > %v",
						i, logs[i].CreatedAt, logs[i-1].CreatedAt)
					return false
				}
			}

			return true
		},
		genLogCountForOrder(),
		gen.SliceOfN(3, genDeploymentStatus()),
	))

	properties.TestingRun(t)
}
