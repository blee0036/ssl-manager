package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// newTestSchedulerForHeartbeat creates a SchedulerService configured for heartbeat testing.
// Only cfg and db are needed for CheckHeartbeatTimeouts.
func newTestSchedulerForHeartbeat(db *sql.DB, cfg *config.Config) *SchedulerService {
	certRepo := repository.NewCertificateRepository(db, "./data")
	machineRepo := repository.NewMachineRepository(db)
	certService := NewCertificateService(certRepo, db)
	return NewSchedulerService(config.NewRuntimeConfig(cfg), certRepo, machineRepo, certService, nil, nil, db)
}

// TestProperty8_HeartbeatTimeoutStateTransition verifies that for any machine,
// when its last heartbeat time exceeds heartbeat_timeout_seconds from the current time,
// that machine's status should be offline.
//
// **Validates: Requirements 4.2**
func TestProperty8_HeartbeatTimeoutStateTransition(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Property: When (now - last_heartbeat_at) > heartbeat_timeout_seconds, machine is marked offline
	properties.Property("machine marked offline when heartbeat exceeds timeout", prop.ForAll(
		func(timeoutSeconds int, extraSeconds int) bool {
			db := setupTestDB(t)
			repo := repository.NewMachineRepository(db)
			cfg := config.DefaultConfig()
			cfg.Agent.HeartbeatTimeoutSeconds = timeoutSeconds
			cfg.Server.ExternalURL = "https://ssl.example.com"

			svc := NewMachineService(repo, config.NewRuntimeConfig(cfg))
			schedulerSvc := newTestSchedulerForHeartbeat(db, cfg)
			ctx := context.Background()

			// Create a machine
			machine, _, err := svc.Create(ctx, model.CreateMachineInput{
				Name: "timeout-test",
				IP:   "192.168.1.1",
			})
			if err != nil {
				t.Logf("Failed to create machine: %v", err)
				return false
			}

			// Simulate heartbeat that happened (timeout + extraSeconds) ago
			// This means the heartbeat is expired
			heartbeatTime := time.Now().UTC().Add(-time.Duration(timeoutSeconds+extraSeconds) * time.Second)
			_, err = db.ExecContext(ctx,
				`UPDATE machines SET status = 'online', last_heartbeat_at = ?, updated_at = ? WHERE id = ?`,
				heartbeatTime.Format(time.RFC3339),
				time.Now().UTC().Format(time.RFC3339),
				machine.ID,
			)
			if err != nil {
				t.Logf("Failed to update heartbeat time: %v", err)
				return false
			}

			// Run heartbeat timeout check
			err = schedulerSvc.CheckHeartbeatTimeouts(ctx)
			if err != nil {
				t.Logf("CheckHeartbeatTimeouts failed: %v", err)
				return false
			}

			// Verify machine is now offline
			updated, err := repo.GetByID(ctx, machine.ID)
			if err != nil {
				t.Logf("Failed to get machine: %v", err)
				return false
			}

			if updated.Status != "offline" {
				t.Logf("Expected status 'offline', got %q (timeout=%ds, extra=%ds)",
					updated.Status, timeoutSeconds, extraSeconds)
				return false
			}

			return true
		},
		gen.IntRange(30, 600),  // heartbeat_timeout_seconds: 30-600
		gen.IntRange(1, 3600),  // extra seconds beyond timeout: 1-3600
	))

	// Property: When (now - last_heartbeat_at) <= heartbeat_timeout_seconds, machine remains online
	// We use a percentage-based approach: generate timeout and a fraction (0-90%) to compute withinSeconds
	properties.Property("machine remains online when heartbeat within timeout", prop.ForAll(
		func(timeoutSeconds int, pct int) bool {
			// Compute withinSeconds as a percentage of timeout, ensuring it's strictly less
			// pct is 0-90, so withinSeconds = timeout * pct / 100, which is always < timeout
			withinSeconds := timeoutSeconds * pct / 100

			db := setupTestDB(t)
			repo := repository.NewMachineRepository(db)
			cfg := config.DefaultConfig()
			cfg.Agent.HeartbeatTimeoutSeconds = timeoutSeconds
			cfg.Server.ExternalURL = "https://ssl.example.com"

			svc := NewMachineService(repo, config.NewRuntimeConfig(cfg))
			schedulerSvc := newTestSchedulerForHeartbeat(db, cfg)
			ctx := context.Background()

			// Create a machine
			machine, _, err := svc.Create(ctx, model.CreateMachineInput{
				Name: "online-test",
				IP:   "192.168.1.2",
			})
			if err != nil {
				t.Logf("Failed to create machine: %v", err)
				return false
			}

			// Simulate heartbeat that happened withinSeconds ago (within timeout)
			heartbeatTime := time.Now().UTC().Add(-time.Duration(withinSeconds) * time.Second)
			_, err = db.ExecContext(ctx,
				`UPDATE machines SET status = 'online', last_heartbeat_at = ?, updated_at = ? WHERE id = ?`,
				heartbeatTime.Format(time.RFC3339),
				time.Now().UTC().Format(time.RFC3339),
				machine.ID,
			)
			if err != nil {
				t.Logf("Failed to update heartbeat time: %v", err)
				return false
			}

			// Run heartbeat timeout check
			err = schedulerSvc.CheckHeartbeatTimeouts(ctx)
			if err != nil {
				t.Logf("CheckHeartbeatTimeouts failed: %v", err)
				return false
			}

			// Verify machine remains online
			updated, err := repo.GetByID(ctx, machine.ID)
			if err != nil {
				t.Logf("Failed to get machine: %v", err)
				return false
			}

			if updated.Status != "online" {
				t.Logf("Expected status 'online', got %q (timeout=%ds, within=%ds)",
					updated.Status, timeoutSeconds, withinSeconds)
				return false
			}

			return true
		},
		gen.IntRange(30, 600), // heartbeat_timeout_seconds: 30-600
		gen.IntRange(0, 90),   // percentage of timeout (0-90%), ensures withinSeconds < timeout
	))

	// Property: Machines with no heartbeat (pending status) remain pending
	properties.Property("pending machines without heartbeat remain pending", prop.ForAll(
		func(timeoutSeconds int) bool {
			db := setupTestDB(t)
			repo := repository.NewMachineRepository(db)
			cfg := config.DefaultConfig()
			cfg.Agent.HeartbeatTimeoutSeconds = timeoutSeconds
			cfg.Server.ExternalURL = "https://ssl.example.com"

			svc := NewMachineService(repo, config.NewRuntimeConfig(cfg))
			schedulerSvc := newTestSchedulerForHeartbeat(db, cfg)
			ctx := context.Background()

			// Create a machine (starts as pending, no heartbeat)
			machine, _, err := svc.Create(ctx, model.CreateMachineInput{
				Name: "pending-test",
				IP:   "192.168.1.3",
			})
			if err != nil {
				t.Logf("Failed to create machine: %v", err)
				return false
			}

			// Verify initial status is pending
			created, err := repo.GetByID(ctx, machine.ID)
			if err != nil {
				t.Logf("Failed to get machine: %v", err)
				return false
			}
			if created.Status != "pending" {
				t.Logf("Expected initial status 'pending', got %q", created.Status)
				return false
			}

			// Run heartbeat timeout check
			err = schedulerSvc.CheckHeartbeatTimeouts(ctx)
			if err != nil {
				t.Logf("CheckHeartbeatTimeouts failed: %v", err)
				return false
			}

			// Verify machine remains pending (not changed to offline)
			updated, err := repo.GetByID(ctx, machine.ID)
			if err != nil {
				t.Logf("Failed to get machine: %v", err)
				return false
			}

			if updated.Status != "pending" {
				t.Logf("Expected status 'pending' after timeout check, got %q (timeout=%ds)",
					updated.Status, timeoutSeconds)
				return false
			}

			return true
		},
		gen.IntRange(30, 600), // heartbeat_timeout_seconds: 30-600
	))

	properties.TestingRun(t)
}
