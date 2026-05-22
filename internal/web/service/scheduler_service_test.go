package service

import (
	"context"
	"testing"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

func newTestSchedulerService(t *testing.T) (*SchedulerService, *MachineService) {
	t.Helper()
	db := setupTestDB(t)
	machineRepo := repository.NewMachineRepository(db)
	certRepo := repository.NewCertificateRepository(db, t.TempDir())
	certService := NewCertificateService(certRepo, db)
	cfg := &config.Config{
		Server: config.ServerConfig{
			ExternalURL: "https://ssl.example.com",
			ListenAddr:  ":8080",
		},
		Agent: config.AgentConfig{
			HeartbeatTimeoutSeconds: 120, // 2 minutes timeout
			PollIntervalSeconds:     60,
		},
		Alert: config.AlertConfig{
			DefaultBeforeDays: 15,
		},
		Readonly: config.ReadonlyConfig{
			Enabled:      false,
			ViewPassword: "",
		},
		DomainMonitor: config.DomainMonitorConfig{
			DefaultPort:     443,
			IntervalMinutes: 60,
		},
	}

	schedulerSvc := NewSchedulerService(config.NewRuntimeConfig(cfg), certRepo, machineRepo, certService, nil, nil, db)
	machineSvc := NewMachineService(machineRepo, config.NewRuntimeConfig(cfg))
	return schedulerSvc, machineSvc
}

// createMachineWithHeartbeat creates a machine and sends a heartbeat, then returns the machine.
func createMachineWithHeartbeat(t *testing.T, machineSvc *MachineService, name, ip string) *model.Machine {
	t.Helper()
	ctx := context.Background()

	machine, _, err := machineSvc.Create(ctx, model.CreateMachineInput{Name: name, IP: ip})
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}

	// Send heartbeat to set status to online
	err = machineSvc.UpdateHeartbeat(ctx, machine.ID, model.HeartbeatInfo{
		MachineID:    machine.ID,
		AgentVersion: "1.0.0",
		Hostname:     name,
		IP:           ip,
		OS:           "linux",
		Arch:         "amd64",
	})
	if err != nil {
		t.Fatalf("failed to update heartbeat: %v", err)
	}

	// Retrieve updated machine
	machine, err = machineSvc.GetByID(ctx, machine.ID)
	if err != nil {
		t.Fatalf("failed to get machine: %v", err)
	}
	return machine
}

func TestCheckHeartbeatTimeouts_OnlineMachineTimesOut(t *testing.T) {
	schedulerSvc, machineSvc := newTestSchedulerService(t)
	ctx := context.Background()

	// Create a machine and send heartbeat (makes it online)
	machine := createMachineWithHeartbeat(t, machineSvc, "web-1", "10.0.0.1")

	// Verify it's online
	if machine.Status != "online" {
		t.Fatalf("expected status 'online', got '%s'", machine.Status)
	}

	// Set a very short timeout (1 second) so the heartbeat we just sent will expire
	schedulerSvc.runtimeCfg.Get().Agent.HeartbeatTimeoutSeconds = 1

	// Wait briefly to ensure the heartbeat is older than 1 second
	time.Sleep(2 * time.Second)

	// Run the heartbeat timeout check
	err := schedulerSvc.CheckHeartbeatTimeouts(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify machine is now offline
	updated, err := machineSvc.GetByID(ctx, machine.ID)
	if err != nil {
		t.Fatalf("failed to get machine: %v", err)
	}
	if updated.Status != "offline" {
		t.Errorf("expected status 'offline', got '%s'", updated.Status)
	}
}

func TestCheckHeartbeatTimeouts_PendingMachineUnchanged(t *testing.T) {
	schedulerSvc, machineSvc := newTestSchedulerService(t)
	ctx := context.Background()

	// Create a machine but DON'T send heartbeat (stays pending)
	machine, _, err := machineSvc.Create(ctx, model.CreateMachineInput{Name: "pending-machine", IP: "10.0.0.2"})
	if err != nil {
		t.Fatalf("failed to create machine: %v", err)
	}

	// Verify it's pending
	if machine.Status != "pending" {
		t.Fatalf("expected status 'pending', got '%s'", machine.Status)
	}

	// Set a very short timeout
	schedulerSvc.runtimeCfg.Get().Agent.HeartbeatTimeoutSeconds = 1
	time.Sleep(2 * time.Second)

	// Run the heartbeat timeout check
	err = schedulerSvc.CheckHeartbeatTimeouts(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify machine is still pending (not changed to offline)
	updated, err := machineSvc.GetByID(ctx, machine.ID)
	if err != nil {
		t.Fatalf("failed to get machine: %v", err)
	}
	if updated.Status != "pending" {
		t.Errorf("expected status 'pending' (unchanged), got '%s'", updated.Status)
	}
}

func TestCheckHeartbeatTimeouts_RecentHeartbeatNotAffected(t *testing.T) {
	schedulerSvc, machineSvc := newTestSchedulerService(t)
	ctx := context.Background()

	// Create a machine and send heartbeat (makes it online)
	machine := createMachineWithHeartbeat(t, machineSvc, "active-machine", "10.0.0.3")

	// Keep the default timeout of 120 seconds - the heartbeat was just sent
	// so it should NOT be marked as offline

	// Run the heartbeat timeout check
	err := schedulerSvc.CheckHeartbeatTimeouts(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify machine is still online
	updated, err := machineSvc.GetByID(ctx, machine.ID)
	if err != nil {
		t.Fatalf("failed to get machine: %v", err)
	}
	if updated.Status != "online" {
		t.Errorf("expected status 'online', got '%s'", updated.Status)
	}
}

func TestCheckHeartbeatTimeouts_OfflineMachineRecoveryViaHeartbeat(t *testing.T) {
	schedulerSvc, machineSvc := newTestSchedulerService(t)
	ctx := context.Background()

	// Create a machine and send heartbeat (makes it online)
	machine := createMachineWithHeartbeat(t, machineSvc, "recovery-machine", "10.0.0.4")

	// Set very short timeout and wait for it to expire
	schedulerSvc.runtimeCfg.Get().Agent.HeartbeatTimeoutSeconds = 1
	time.Sleep(2 * time.Second)

	// Run timeout check - should mark as offline
	err := schedulerSvc.CheckHeartbeatTimeouts(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify machine is offline
	updated, err := machineSvc.GetByID(ctx, machine.ID)
	if err != nil {
		t.Fatalf("failed to get machine: %v", err)
	}
	if updated.Status != "offline" {
		t.Fatalf("expected status 'offline', got '%s'", updated.Status)
	}

	// Simulate the machine sending a new heartbeat (recovery)
	err = machineSvc.UpdateHeartbeat(ctx, machine.ID, model.HeartbeatInfo{
		MachineID:    machine.ID,
		AgentVersion: "1.0.1",
		Hostname:     "recovery-machine",
		IP:           "10.0.0.4",
		OS:           "linux",
		Arch:         "amd64",
	})
	if err != nil {
		t.Fatalf("failed to update heartbeat: %v", err)
	}

	// Verify machine is back online
	recovered, err := machineSvc.GetByID(ctx, machine.ID)
	if err != nil {
		t.Fatalf("failed to get machine: %v", err)
	}
	if recovered.Status != "online" {
		t.Errorf("expected status 'online' after recovery heartbeat, got '%s'", recovered.Status)
	}
}

func TestCheckHeartbeatTimeouts_MultipleOnlineMachines(t *testing.T) {
	schedulerSvc, machineSvc := newTestSchedulerService(t)
	ctx := context.Background()

	// Create multiple machines with heartbeats
	machine1 := createMachineWithHeartbeat(t, machineSvc, "machine-1", "10.0.0.1")
	machine2 := createMachineWithHeartbeat(t, machineSvc, "machine-2", "10.0.0.2")
	machine3 := createMachineWithHeartbeat(t, machineSvc, "machine-3", "10.0.0.3")

	// Set very short timeout and wait
	schedulerSvc.runtimeCfg.Get().Agent.HeartbeatTimeoutSeconds = 1
	time.Sleep(2 * time.Second)

	// Send a fresh heartbeat for machine2 only (so it stays online)
	err := machineSvc.UpdateHeartbeat(ctx, machine2.ID, model.HeartbeatInfo{
		MachineID:    machine2.ID,
		AgentVersion: "1.0.0",
		Hostname:     "machine-2",
		IP:           "10.0.0.2",
		OS:           "linux",
		Arch:         "amd64",
	})
	if err != nil {
		t.Fatalf("failed to update heartbeat: %v", err)
	}

	// Run timeout check
	err = schedulerSvc.CheckHeartbeatTimeouts(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// machine1 and machine3 should be offline, machine2 should still be online
	m1, _ := machineSvc.GetByID(ctx, machine1.ID)
	m2, _ := machineSvc.GetByID(ctx, machine2.ID)
	m3, _ := machineSvc.GetByID(ctx, machine3.ID)

	if m1.Status != "offline" {
		t.Errorf("machine1: expected 'offline', got '%s'", m1.Status)
	}
	if m2.Status != "online" {
		t.Errorf("machine2: expected 'online', got '%s'", m2.Status)
	}
	if m3.Status != "offline" {
		t.Errorf("machine3: expected 'offline', got '%s'", m3.Status)
	}
}

func TestCheckHeartbeatTimeouts_RevokedMachineNotAffected(t *testing.T) {
	schedulerSvc, machineSvc := newTestSchedulerService(t)
	ctx := context.Background()

	// Create a machine, send heartbeat, then revoke token
	machine := createMachineWithHeartbeat(t, machineSvc, "revoked-machine", "10.0.0.5")

	// Revoke the token (changes status to "revoked")
	err := machineSvc.RevokeToken(ctx, machine.ID)
	if err != nil {
		t.Fatalf("failed to revoke token: %v", err)
	}

	// Set very short timeout and wait
	schedulerSvc.runtimeCfg.Get().Agent.HeartbeatTimeoutSeconds = 1
	time.Sleep(2 * time.Second)

	// Run timeout check
	err = schedulerSvc.CheckHeartbeatTimeouts(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify machine is still "revoked" (not changed to offline)
	updated, err := machineSvc.GetByID(ctx, machine.ID)
	if err != nil {
		t.Fatalf("failed to get machine: %v", err)
	}
	if updated.Status != "revoked" {
		t.Errorf("expected status 'revoked' (unchanged), got '%s'", updated.Status)
	}
}

func TestCheckHeartbeatTimeouts_NoMachines(t *testing.T) {
	schedulerSvc, _ := newTestSchedulerService(t)
	ctx := context.Background()

	// Run timeout check with no machines - should not error
	err := schedulerSvc.CheckHeartbeatTimeouts(ctx)
	if err != nil {
		t.Fatalf("unexpected error with no machines: %v", err)
	}
}

func TestSchedulerStartStop(t *testing.T) {
	schedulerSvc, _ := newTestSchedulerService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the scheduler
	err := schedulerSvc.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start scheduler: %v", err)
	}

	// Verify it's running
	if !schedulerSvc.IsRunning() {
		t.Error("expected scheduler to be running after Start")
	}

	// Stop the scheduler
	err = schedulerSvc.Stop()
	if err != nil {
		t.Fatalf("failed to stop scheduler: %v", err)
	}

	// Verify it's stopped
	if schedulerSvc.IsRunning() {
		t.Error("expected scheduler to not be running after Stop")
	}
}
