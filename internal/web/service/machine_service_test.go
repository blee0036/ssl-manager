package service

import (
	"context"
	"strings"
	"testing"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

func newTestMachineService(t *testing.T) *MachineService {
	t.Helper()
	db := setupTestDB(t)
	repo := repository.NewMachineRepository(db)
	cfg := config.DefaultConfig()
	cfg.Server.ExternalURL = "https://ssl.example.com"
	svc := NewMachineService(repo, config.NewRuntimeConfig(cfg))
	return svc
}

func TestCreate_Success(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	input := model.CreateMachineInput{
		Name:   "web-server-1",
		IP:     "192.168.1.100",
		Tags:   []string{"production", "web"},
		Remark: "Main web server",
	}

	machine, token, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if machine == nil {
		t.Fatal("expected machine to be non-nil")
	}
	if machine.ID == "" {
		t.Error("expected machine ID to be set")
	}
	if machine.Name != "web-server-1" {
		t.Errorf("expected name 'web-server-1', got '%s'", machine.Name)
	}
	if machine.IP != "192.168.1.100" {
		t.Errorf("expected IP '192.168.1.100', got '%s'", machine.IP)
	}
	if machine.Status != "pending" {
		t.Errorf("expected status 'pending', got '%s'", machine.Status)
	}

	// Token should be 64 hex chars (32 bytes)
	if len(token) != 64 {
		t.Errorf("expected token length 64, got %d", len(token))
	}

	// Token hash should be stored, not the plaintext
	if machine.AgentTokenHash == "" {
		t.Error("expected agent token hash to be set")
	}
	if machine.AgentTokenHash == token {
		t.Error("token hash should not equal plaintext token")
	}

	// Verify hash matches
	expectedHash := HashToken(token)
	if machine.AgentTokenHash != expectedHash {
		t.Error("stored hash does not match hash of returned token")
	}
}

func TestCreate_EmptyName(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	input := model.CreateMachineInput{
		Name: "",
		IP:   "192.168.1.100",
	}

	_, _, err := svc.Create(ctx, input)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCreate_EmptyIP(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	input := model.CreateMachineInput{
		Name: "test-machine",
		IP:   "",
	}

	_, _, err := svc.Create(ctx, input)
	if err == nil {
		t.Fatal("expected error for empty IP")
	}
	if !strings.Contains(err.Error(), "IP is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCreate_UniqueTokens(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	_, token1, err := svc.Create(ctx, model.CreateMachineInput{Name: "m1", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, token2, err := svc.Create(ctx, model.CreateMachineInput{Name: "m2", IP: "10.0.0.2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token1 == token2 {
		t.Error("expected unique tokens for different machines")
	}
}

func TestGetByID_Success(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	created, _, err := svc.Create(ctx, model.CreateMachineInput{Name: "test", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	machine, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if machine.Name != "test" {
		t.Errorf("expected name 'test', got '%s'", machine.Name)
	}
}

func TestGetByID_NotFound(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	_, err := svc.GetByID(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent machine")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestMachineList(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	// Create some machines
	svc.Create(ctx, model.CreateMachineInput{Name: "web-1", IP: "10.0.0.1"})
	svc.Create(ctx, model.CreateMachineInput{Name: "web-2", IP: "10.0.0.2"})
	svc.Create(ctx, model.CreateMachineInput{Name: "db-1", IP: "10.0.0.3"})

	// List all
	machines, err := svc.List(ctx, model.MachineFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(machines) != 3 {
		t.Errorf("expected 3 machines, got %d", len(machines))
	}

	// List with search filter
	machines, err = svc.List(ctx, model.MachineFilter{Search: "web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(machines) != 2 {
		t.Errorf("expected 2 machines matching 'web', got %d", len(machines))
	}
}

func TestUpdate_Success(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	created, _, err := svc.Create(ctx, model.CreateMachineInput{Name: "old-name", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newName := "new-name"
	newIP := "10.0.0.2"
	updated, err := svc.Update(ctx, created.ID, model.UpdateMachineInput{
		Name: &newName,
		IP:   &newIP,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "new-name" {
		t.Errorf("expected name 'new-name', got '%s'", updated.Name)
	}
	if updated.IP != "10.0.0.2" {
		t.Errorf("expected IP '10.0.0.2', got '%s'", updated.IP)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	name := "test"
	_, err := svc.Update(ctx, "nonexistent", model.UpdateMachineInput{Name: &name})
	if err == nil {
		t.Fatal("expected error for nonexistent machine")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDelete_Success(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	created, _, err := svc.Create(ctx, model.CreateMachineInput{Name: "to-delete", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = svc.Delete(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify deleted
	_, err = svc.GetByID(ctx, created.ID)
	if err == nil {
		t.Fatal("expected error after deletion")
	}
}

func TestDelete_NotFound(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	err := svc.Delete(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent machine")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerateToken_Success(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	created, originalToken, err := svc.Create(ctx, model.CreateMachineInput{Name: "test", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Generate new token
	newToken, err := svc.GenerateToken(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// New token should be different from original
	if newToken == originalToken {
		t.Error("new token should differ from original")
	}

	// New token should be 64 hex chars
	if len(newToken) != 64 {
		t.Errorf("expected token length 64, got %d", len(newToken))
	}

	// Verify the machine's token hash is updated
	machine, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedHash := HashToken(newToken)
	if machine.AgentTokenHash != expectedHash {
		t.Error("machine token hash should match new token hash")
	}

	// Revoked_at should be cleared
	if machine.AgentTokenRevokedAt != nil {
		t.Error("agent_token_revoked_at should be nil after token regeneration")
	}
}

func TestGenerateToken_NotFound(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	_, err := svc.GenerateToken(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent machine")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRevokeToken_Success(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	created, _, err := svc.Create(ctx, model.CreateMachineInput{Name: "test", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = svc.RevokeToken(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify token is revoked
	machine, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if machine.AgentTokenRevokedAt == nil {
		t.Error("expected agent_token_revoked_at to be set")
	}
	if machine.Status != "revoked" {
		t.Errorf("expected status 'revoked', got '%s'", machine.Status)
	}
}

func TestRevokeToken_NotFound(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	err := svc.RevokeToken(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent machine")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGetInstallCommand_Success(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	created, token, err := svc.Create(ctx, model.CreateMachineInput{Name: "test", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd, err := svc.GetInstallCommand(ctx, created.ID, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify command contains required components
	if !strings.Contains(cmd, "https://ssl.example.com") {
		t.Error("install command should contain external URL")
	}
	if !strings.Contains(cmd, created.ID) {
		t.Error("install command should contain machine ID")
	}
	if !strings.Contains(cmd, token) {
		t.Error("install command should contain agent token")
	}
	if !strings.Contains(cmd, "curl -sSL") {
		t.Error("install command should start with curl")
	}
	if !strings.Contains(cmd, "/api/agent/install.sh") {
		t.Error("install command should reference install.sh endpoint")
	}
	if !strings.Contains(cmd, "--server-url") {
		t.Error("install command should contain --server-url flag")
	}
	if !strings.Contains(cmd, "--machine-id") {
		t.Error("install command should contain --machine-id flag")
	}
	if !strings.Contains(cmd, "--agent-token") {
		t.Error("install command should contain --agent-token flag")
	}
}

func TestGetInstallCommand_NotFound(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	_, err := svc.GetInstallCommand(ctx, "nonexistent", "some-token")
	if err == nil {
		t.Fatal("expected error for nonexistent machine")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestUpdateHeartbeat_Success(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	created, _, err := svc.Create(ctx, model.CreateMachineInput{Name: "test", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := model.HeartbeatInfo{
		MachineID:    created.ID,
		AgentVersion: "1.0.0",
		Hostname:     "web-server-1",
		IP:           "192.168.1.100",
		OS:           "linux",
		Arch:         "amd64",
	}

	err = svc.UpdateHeartbeat(ctx, created.ID, info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify heartbeat updated
	machine, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if machine.Status != "online" {
		t.Errorf("expected status 'online', got '%s'", machine.Status)
	}
	if machine.AgentVersion != "1.0.0" {
		t.Errorf("expected agent version '1.0.0', got '%s'", machine.AgentVersion)
	}
	if machine.Hostname != "web-server-1" {
		t.Errorf("expected hostname 'web-server-1', got '%s'", machine.Hostname)
	}
	if machine.OS != "linux" {
		t.Errorf("expected OS 'linux', got '%s'", machine.OS)
	}
	if machine.Arch != "amd64" {
		t.Errorf("expected arch 'amd64', got '%s'", machine.Arch)
	}
	if machine.LastHeartbeatAt == nil {
		t.Error("expected last_heartbeat_at to be set")
	}
}

func TestUpdateHeartbeat_NotFound(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	info := model.HeartbeatInfo{
		MachineID:    "nonexistent",
		AgentVersion: "1.0.0",
		Hostname:     "test",
		IP:           "10.0.0.1",
		OS:           "linux",
		Arch:         "amd64",
	}

	err := svc.UpdateHeartbeat(ctx, "nonexistent", info)
	if err == nil {
		t.Fatal("expected error for nonexistent machine")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerateToken_ThenRevoke_ThenRegenerate(t *testing.T) {
	svc := newTestMachineService(t)
	ctx := context.Background()

	created, _, err := svc.Create(ctx, model.CreateMachineInput{Name: "test", IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Revoke token
	err = svc.RevokeToken(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error revoking: %v", err)
	}

	// Verify revoked
	machine, _ := svc.GetByID(ctx, created.ID)
	if machine.AgentTokenRevokedAt == nil {
		t.Fatal("expected token to be revoked")
	}

	// Regenerate token
	newToken, err := svc.GenerateToken(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error regenerating: %v", err)
	}
	if len(newToken) != 64 {
		t.Errorf("expected token length 64, got %d", len(newToken))
	}

	// Verify revoked_at is cleared
	machine, _ = svc.GetByID(ctx, created.ID)
	if machine.AgentTokenRevokedAt != nil {
		t.Error("expected agent_token_revoked_at to be nil after regeneration")
	}

	// Verify new hash matches
	expectedHash := HashToken(newToken)
	if machine.AgentTokenHash != expectedHash {
		t.Error("machine token hash should match new token hash after regeneration")
	}
}

func TestHashToken(t *testing.T) {
	// Verify HashToken produces consistent results
	token := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	hash1 := HashToken(token)
	hash2 := HashToken(token)

	if hash1 != hash2 {
		t.Error("HashToken should produce consistent results")
	}

	// Different tokens should produce different hashes
	hash3 := HashToken("different-token")
	if hash1 == hash3 {
		t.Error("different tokens should produce different hashes")
	}

	// Hash should be 64 hex chars (SHA256 = 32 bytes = 64 hex chars)
	if len(hash1) != 64 {
		t.Errorf("expected hash length 64, got %d", len(hash1))
	}
}
