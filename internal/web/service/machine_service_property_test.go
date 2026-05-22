package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// --- Generators ---

// genMachineName generates valid non-empty machine names.
func genMachineName() gopter.Gen {
	return gen.Identifier().Map(func(s string) string {
		if len(s) > 64 {
			return s[:64]
		}
		return s
	})
}

// genIPAddress generates valid-looking IP address strings.
func genIPAddress() gopter.Gen {
	return gen.SliceOfN(4, gen.IntRange(1, 254)).Map(func(parts []int) string {
		return itoa(parts[0]) + "." + itoa(parts[1]) + "." + itoa(parts[2]) + "." + itoa(parts[3])
	})
}

// itoa converts int to string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	result := ""
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	return result
}

// genExternalURL generates valid external URLs.
func genExternalURL() gopter.Gen {
	return gen.Identifier().Map(func(s string) string {
		if len(s) > 32 {
			s = s[:32]
		}
		return "https://" + s + ".example.com"
	})
}

// --- Property 5: 机器创建生成唯一 Token ---

// TestProperty5_MachineCreationGeneratesUniqueToken verifies that for any valid
// machine name and IP address, creating a machine generates a unique Agent_Token
// that differs from all other tokens in the system.
//
// **Validates: Requirements 3.1**
func TestProperty5_MachineCreationGeneratesUniqueToken(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("creating machines always generates unique tokens", prop.ForAll(
		func(name1, name2, ip1, ip2 string) bool {
			db := setupTestDB(t)
			repo := repository.NewMachineRepository(db)
			cfg := config.DefaultConfig()
			cfg.Server.ExternalURL = "https://ssl.example.com"
			svc := NewMachineService(repo, config.NewRuntimeConfig(cfg))
			ctx := context.Background()

			// Create first machine
			_, token1, err := svc.Create(ctx, model.CreateMachineInput{
				Name: name1,
				IP:   ip1,
			})
			if err != nil {
				t.Logf("Failed to create machine 1: %v", err)
				return false
			}

			// Create second machine
			_, token2, err := svc.Create(ctx, model.CreateMachineInput{
				Name: name2,
				IP:   ip2,
			})
			if err != nil {
				t.Logf("Failed to create machine 2: %v", err)
				return false
			}

			// Tokens must be different
			if token1 == token2 {
				t.Logf("Tokens are identical: %s", token1)
				return false
			}

			// Each token should be 64 hex chars (32 bytes)
			if len(token1) != 64 || len(token2) != 64 {
				t.Logf("Token lengths: %d, %d (expected 64)", len(token1), len(token2))
				return false
			}

			return true
		},
		genMachineName(),
		genMachineName(),
		genIPAddress(),
		genIPAddress(),
	))

	// Property: Regenerated tokens are also unique from original
	properties.Property("regenerated tokens differ from original", prop.ForAll(
		func(name, ip string) bool {
			db := setupTestDB(t)
			repo := repository.NewMachineRepository(db)
			cfg := config.DefaultConfig()
			cfg.Server.ExternalURL = "https://ssl.example.com"
			svc := NewMachineService(repo, config.NewRuntimeConfig(cfg))
			ctx := context.Background()

			machine, originalToken, err := svc.Create(ctx, model.CreateMachineInput{
				Name: name,
				IP:   ip,
			})
			if err != nil {
				t.Logf("Failed to create machine: %v", err)
				return false
			}

			newToken, err := svc.GenerateToken(ctx, machine.ID)
			if err != nil {
				t.Logf("Failed to generate new token: %v", err)
				return false
			}

			if originalToken == newToken {
				t.Logf("Regenerated token is same as original")
				return false
			}

			if len(newToken) != 64 {
				t.Logf("New token length: %d (expected 64)", len(newToken))
				return false
			}

			return true
		},
		genMachineName(),
		genIPAddress(),
	))

	properties.TestingRun(t)
}

// --- Property 6: 安装命令包含必要组件 ---

// TestProperty6_InstallCommandContainsRequiredComponents verifies that for any
// created machine, the generated install command contains the Web external URL,
// machine_id, and agent_token.
//
// **Validates: Requirements 3.2**
func TestProperty6_InstallCommandContainsRequiredComponents(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("install command contains external URL, machine_id, and agent_token", prop.ForAll(
		func(name, ip, externalURL string) bool {
			db := setupTestDB(t)
			repo := repository.NewMachineRepository(db)
			cfg := config.DefaultConfig()
			cfg.Server.ExternalURL = externalURL
			svc := NewMachineService(repo, config.NewRuntimeConfig(cfg))
			ctx := context.Background()

			machine, token, err := svc.Create(ctx, model.CreateMachineInput{
				Name: name,
				IP:   ip,
			})
			if err != nil {
				t.Logf("Failed to create machine: %v", err)
				return false
			}

			cmd, err := svc.GetInstallCommand(ctx, machine.ID, token)
			if err != nil {
				t.Logf("Failed to get install command: %v", err)
				return false
			}

			// Must contain external URL
			if !strings.Contains(cmd, externalURL) {
				t.Logf("Install command missing external URL '%s': %s", externalURL, cmd)
				return false
			}

			// Must contain machine_id
			if !strings.Contains(cmd, machine.ID) {
				t.Logf("Install command missing machine_id '%s': %s", machine.ID, cmd)
				return false
			}

			// Must contain agent_token
			if !strings.Contains(cmd, token) {
				t.Logf("Install command missing agent_token: %s", cmd)
				return false
			}

			return true
		},
		genMachineName(),
		genIPAddress(),
		genExternalURL(),
	))

	properties.TestingRun(t)
}

// --- Property 7: 已吊销 Token 全面拒绝 ---

// mockMachineRepoForProperty7 implements the MachineRepository interface for agent auth middleware.
type mockMachineRepoForProperty7 struct {
	machines map[string]*model.Machine // keyed by token hash
}

func (m *mockMachineRepoForProperty7) GetByTokenHash(_ context.Context, tokenHash string) (*model.Machine, error) {
	machine, ok := m.machines[tokenHash]
	if !ok {
		return nil, nil
	}
	return machine, nil
}

func (m *mockMachineRepoForProperty7) GetByTokenHashIncludingRevoked(_ context.Context, tokenHash string) (*model.Machine, error) {
	return nil, nil
}

// TestProperty7_RevokedTokenUniversalRejection verifies that for any revoked
// Agent_Token and any Agent API endpoint (heartbeat, config pull, cert download),
// Web_Backend returns 401.
//
// **Validates: Requirements 3.4**
func TestProperty7_RevokedTokenUniversalRejection(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// genAgentEndpoint generates Agent API endpoints.
	genAgentEndpoint := func() gopter.Gen {
		return gen.OneConstOf(
			"/api/agent/heartbeat",
			"/api/agent/machines/machine-1/certificates",
			"/api/agent/machine-certificates/mc-1/download",
			"/api/agent/deployment-logs",
			"/api/agent/machines/machine-2/certificates",
			"/api/agent/machine-certificates/mc-2/download",
		)
	}

	// genHTTPMethod generates HTTP methods used by Agent API.
	genHTTPMethod := func() gopter.Gen {
		return gen.OneConstOf("GET", "POST")
	}

	// Property: Revoked token (not in repo lookup) always returns 401
	properties.Property("revoked token always returns 401 on any agent endpoint", prop.ForAll(
		func(endpoint, method, token string) bool {
			// The repo returns nil for revoked tokens (GetByTokenHash with
			// agent_token_revoked_at IS NULL filter means revoked tokens won't match)
			repo := &mockMachineRepoForProperty7{
				machines: map[string]*model.Machine{}, // empty = no valid tokens
			}

			handler := middleware.AgentAuthMiddleware(repo, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("ok"))
			}))

			req := httptest.NewRequest(method, endpoint, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Logf("Expected 401 for revoked token on %s %s, got %d", method, endpoint, rec.Code)
				return false
			}

			return true
		},
		genAgentEndpoint(),
		genHTTPMethod(),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
	))

	// Property: A valid token that gets revoked (simulated by removing from repo) returns 401
	properties.Property("previously valid token returns 401 after revocation", prop.ForAll(
		func(endpoint, method string) bool {
			// Simulate a token that was valid but is now revoked
			token := "valid-token-that-was-revoked"
			hash := sha256.Sum256([]byte(token))
			tokenHash := hex.EncodeToString(hash[:])

			// Repo does NOT return this machine (simulating revoked state)
			// The real repo query has "agent_token_revoked_at IS NULL" filter
			repo := &mockMachineRepoForProperty7{
				machines: map[string]*model.Machine{
					// Token hash exists but won't be returned because in real impl
					// the query filters by agent_token_revoked_at IS NULL
				},
			}
			_ = tokenHash // The repo is empty, simulating revocation

			handler := middleware.AgentAuthMiddleware(repo, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("ok"))
			}))

			req := httptest.NewRequest(method, endpoint, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Logf("Expected 401 for revoked token on %s %s, got %d", method, endpoint, rec.Code)
				return false
			}

			return true
		},
		genAgentEndpoint(),
		genHTTPMethod(),
	))

	// Property: Integration test - create machine, revoke token, verify rejection
	properties.Property("full lifecycle: create, revoke, then all endpoints reject", prop.ForAll(
		func(name, ip string) bool {
			db := setupTestDB(t)
			repo := repository.NewMachineRepository(db)
			cfg := config.DefaultConfig()
			cfg.Server.ExternalURL = "https://ssl.example.com"
			svc := NewMachineService(repo, config.NewRuntimeConfig(cfg))
			ctx := context.Background()

			// Create machine
			machine, token, err := svc.Create(ctx, model.CreateMachineInput{
				Name: name,
				IP:   ip,
			})
			if err != nil {
				t.Logf("Failed to create machine: %v", err)
				return false
			}

			// Verify token works before revocation
			hash := sha256.Sum256([]byte(token))
			tokenHash := hex.EncodeToString(hash[:])
			foundMachine, err := repo.GetByTokenHash(ctx, tokenHash)
			if err != nil || foundMachine == nil {
				t.Logf("Token should be valid before revocation")
				return false
			}
			if foundMachine.ID != machine.ID {
				t.Logf("Found machine ID mismatch")
				return false
			}

			// Revoke token
			err = svc.RevokeToken(ctx, machine.ID)
			if err != nil {
				t.Logf("Failed to revoke token: %v", err)
				return false
			}

			// Verify token no longer works (GetByTokenHash filters by revoked_at IS NULL)
			foundMachine, err = repo.GetByTokenHash(ctx, tokenHash)
			if err == nil && foundMachine != nil {
				t.Logf("Token should be rejected after revocation, but found machine: %s", foundMachine.ID)
				return false
			}

			return true
		},
		genMachineName(),
		genIPAddress(),
	))

	properties.TestingRun(t)
}

// --- Property 24: Token 哈希存储 ---

// TestProperty24_TokenHashStorage verifies that Agent tokens are only stored as
// hashes, never in plaintext. The stored hash should not equal the plaintext token,
// and should be a valid SHA256 hex string.
//
// **Validates: Requirements 16.1**
func TestProperty24_TokenHashStorage(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Property: Created machine stores hash, not plaintext token
	properties.Property("machine stores token hash, never plaintext", prop.ForAll(
		func(name, ip string) bool {
			db := setupTestDB(t)
			repo := repository.NewMachineRepository(db)
			cfg := config.DefaultConfig()
			cfg.Server.ExternalURL = "https://ssl.example.com"
			svc := NewMachineService(repo, config.NewRuntimeConfig(cfg))
			ctx := context.Background()

			machine, token, err := svc.Create(ctx, model.CreateMachineInput{
				Name: name,
				IP:   ip,
			})
			if err != nil {
				t.Logf("Failed to create machine: %v", err)
				return false
			}

			// Token hash must NOT equal the plaintext token
			if machine.AgentTokenHash == token {
				t.Logf("Token hash equals plaintext token - stored in plaintext!")
				return false
			}

			// Token hash must be a valid SHA256 hex string (64 chars)
			if len(machine.AgentTokenHash) != 64 {
				t.Logf("Token hash length %d, expected 64", len(machine.AgentTokenHash))
				return false
			}

			// Verify it's valid hex
			_, err = hex.DecodeString(machine.AgentTokenHash)
			if err != nil {
				t.Logf("Token hash is not valid hex: %v", err)
				return false
			}

			// Verify hash matches SHA256 of the token
			expectedHash := HashToken(token)
			if machine.AgentTokenHash != expectedHash {
				t.Logf("Stored hash doesn't match SHA256(token)")
				return false
			}

			// Verify by reading from DB directly
			retrieved, err := repo.GetByID(ctx, machine.ID)
			if err != nil {
				t.Logf("Failed to retrieve machine: %v", err)
				return false
			}

			// DB stored value must also be hash, not plaintext
			if retrieved.AgentTokenHash == token {
				t.Logf("DB stores plaintext token!")
				return false
			}
			if retrieved.AgentTokenHash != expectedHash {
				t.Logf("DB stored hash doesn't match expected hash")
				return false
			}

			return true
		},
		genMachineName(),
		genIPAddress(),
	))

	// Property: Regenerated token is also stored as hash
	properties.Property("regenerated token is stored as hash, not plaintext", prop.ForAll(
		func(name, ip string) bool {
			db := setupTestDB(t)
			repo := repository.NewMachineRepository(db)
			cfg := config.DefaultConfig()
			cfg.Server.ExternalURL = "https://ssl.example.com"
			svc := NewMachineService(repo, config.NewRuntimeConfig(cfg))
			ctx := context.Background()

			machine, _, err := svc.Create(ctx, model.CreateMachineInput{
				Name: name,
				IP:   ip,
			})
			if err != nil {
				t.Logf("Failed to create machine: %v", err)
				return false
			}

			// Regenerate token
			newToken, err := svc.GenerateToken(ctx, machine.ID)
			if err != nil {
				t.Logf("Failed to regenerate token: %v", err)
				return false
			}

			// Retrieve machine and verify hash storage
			retrieved, err := repo.GetByID(ctx, machine.ID)
			if err != nil {
				t.Logf("Failed to retrieve machine: %v", err)
				return false
			}

			// Must not store plaintext
			if retrieved.AgentTokenHash == newToken {
				t.Logf("Regenerated token stored in plaintext!")
				return false
			}

			// Must match SHA256 hash
			expectedHash := HashToken(newToken)
			if retrieved.AgentTokenHash != expectedHash {
				t.Logf("Stored hash doesn't match SHA256 of new token")
				return false
			}

			return true
		},
		genMachineName(),
		genIPAddress(),
	))

	properties.TestingRun(t)
}
