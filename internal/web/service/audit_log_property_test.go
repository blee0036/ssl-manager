package service

import (
	"context"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// --- Generators ---

// genActorType generates valid actor_type values.
func genActorType() gopter.Gen {
	return gen.OneConstOf("user", "agent", "system")
}

// genAuditAction generates random action strings representing write operations.
func genAuditAction() gopter.Gen {
	return gen.OneConstOf(
		"POST /api/certificates",
		"PUT /api/certificates/{id}",
		"DELETE /api/certificates/{id}",
		"POST /api/machines",
		"PUT /api/machines/{id}",
		"DELETE /api/machines/{id}",
		"POST /api/machines/{id}/certificates",
		"PUT /api/machine-certificates/{id}",
		"DELETE /api/machine-certificates/{id}",
		"POST /api/users",
		"PUT /api/users/{id}",
		"DELETE /api/users/{id}",
		"POST /api/agent/deployment-logs",
	)
}

// genAuditTargetType generates valid target_type values.
func genAuditTargetType() gopter.Gen {
	return gen.OneConstOf(
		"certificate",
		"machine",
		"machine_certificate",
		"user",
		"domain",
		"thirdpart_dns",
		"system",
	)
}

// genAuditTargetID generates non-empty target ID strings.
func genAuditTargetID() gopter.Gen {
	return gen.OneConstOf(
		"cert-001",
		"machine-abc",
		"user-123",
		"mc-456",
		"domain-789",
		"dns-xyz",
		"sys-config",
		"a1b2c3d4",
	)
}

// genActorID generates non-empty actor ID strings.
func genActorID() gopter.Gen {
	return gen.OneConstOf(
		"user-1",
		"user-admin",
		"agent-m1",
		"system",
		"user-abc123",
		"agent-web01",
		"user-operator",
		"system-scheduler",
	)
}

// genIP generates valid IP address strings.
func genIP() gopter.Gen {
	return gen.OneConstOf(
		"192.168.1.1",
		"10.0.0.1",
		"172.16.0.100",
		"127.0.0.1",
		"203.0.113.42",
		"2001:db8::1",
	)
}

// genSafeDetail generates detail strings without sensitive information.
func genSafeDetail() gopter.Gen {
	return gen.OneConstOf(
		"created certificate for example.com",
		"updated machine name to web-server-1",
		"deleted deployment config",
		"added user admin2 with role admin",
		"deployed certificate to /etc/ssl/cert.pem",
		"triggered manual deploy",
		"revoked agent token for machine m-123",
	)
}

// genSensitiveDetail generates detail strings that contain sensitive information.
func genSensitiveDetail() gopter.Gen {
	return gen.OneConstOf(
		`updated config with key: -----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQC7
-----END PRIVATE KEY-----`,
		`machine created with agent_token="abcdef1234567890abcdef1234567890abcdef12"`,
		`configured webhook: https://open.feishu.cn/open-apis/bot/v2/hook/abc123def456`,
		`set api_token="sk-1234567890abcdef1234567890abcdef1234567890"`,
		`request with Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0`,
		`configured telegram: bot_token="123456789:ABCdefGHIjklMNOpqrsTUVwxyz0123456789"`,
		`updated password="mysecretpassword123" for user admin`,
		`set webhook_url="https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"`,
	)
}

// --- Helper ---

// setupAuditLogPropertyTestDB creates an in-memory SQLite database with the audit_logs table.
func setupAuditLogPropertyTestDB(t *testing.T) *AuditLogService {
	t.Helper()
	db := setupTestDB(t)

	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS audit_logs (
		id TEXT PRIMARY KEY,
		actor_type TEXT NOT NULL CHECK(actor_type IN ('user', 'agent', 'system')),
		actor_id TEXT NOT NULL,
		action TEXT NOT NULL,
		target_type TEXT NOT NULL,
		target_id TEXT DEFAULT '',
		detail TEXT DEFAULT '',
		ip TEXT DEFAULT '',
		created_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create audit_logs table: %v", err)
	}

	repo := repository.NewAuditLogRepository(db)
	return NewAuditLogService(repo)
}

// --- Property Tests ---

// TestProperty21_WriteOperationAuditLogCompleteness verifies that for any write operation
// (create, update, delete), an audit log entry is recorded with all required fields:
// actor_type, actor_id, action, target_type, target_id, detail, and ip.
//
// **Validates: Requirements 13.1**
func TestProperty21_WriteOperationAuditLogCompleteness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Property 1: For any combination of actor_type, action, target_type, and target_id,
	// calling Log() creates a complete audit record with all required fields populated.
	properties.Property("Log creates complete audit record with all required fields", prop.ForAll(
		func(actorType, actorID, action, targetType, targetID, detail, ip string) bool {
			svc := setupAuditLogPropertyTestDB(t)
			ctx := context.Background()

			err := svc.Log(ctx, actorType, actorID, action, targetType, targetID, detail, ip)
			if err != nil {
				t.Logf("Log failed: %v", err)
				return false
			}

			// Retrieve the saved record
			logs, err := svc.List(ctx, repository.AuditLogFilter{})
			if err != nil {
				t.Logf("List failed: %v", err)
				return false
			}

			if len(logs) != 1 {
				t.Logf("Expected 1 log record, got %d", len(logs))
				return false
			}

			log := logs[0]

			// Verify all required fields are populated
			if log.ID == "" {
				t.Logf("ID should not be empty")
				return false
			}
			if log.ActorType != actorType {
				t.Logf("ActorType mismatch: expected %q, got %q", actorType, log.ActorType)
				return false
			}
			if log.ActorID != actorID {
				t.Logf("ActorID mismatch: expected %q, got %q", actorID, log.ActorID)
				return false
			}
			if log.Action != action {
				t.Logf("Action mismatch: expected %q, got %q", action, log.Action)
				return false
			}
			if log.TargetType != targetType {
				t.Logf("TargetType mismatch: expected %q, got %q", targetType, log.TargetType)
				return false
			}
			if log.TargetID != targetID {
				t.Logf("TargetID mismatch: expected %q, got %q", targetID, log.TargetID)
				return false
			}
			if log.IP != ip {
				t.Logf("IP mismatch: expected %q, got %q", ip, log.IP)
				return false
			}
			if log.CreatedAt.IsZero() {
				t.Logf("CreatedAt should not be zero")
				return false
			}

			return true
		},
		genActorType(),
		genActorID(),
		genAuditAction(),
		genAuditTargetType(),
		genAuditTargetID(),
		genSafeDetail(),
		genIP(),
	))

	// Property 2: All required fields are populated in the saved record -
	// no field should be empty when non-empty values are provided.
	properties.Property("all required fields are non-empty in saved record", prop.ForAll(
		func(actorType, actorID, action, targetType, targetID, ip string) bool {
			svc := setupAuditLogPropertyTestDB(t)
			ctx := context.Background()

			detail := "operation performed successfully"
			err := svc.Log(ctx, actorType, actorID, action, targetType, targetID, detail, ip)
			if err != nil {
				t.Logf("Log failed: %v", err)
				return false
			}

			logs, err := svc.List(ctx, repository.AuditLogFilter{})
			if err != nil {
				t.Logf("List failed: %v", err)
				return false
			}

			if len(logs) != 1 {
				t.Logf("Expected 1 log, got %d", len(logs))
				return false
			}

			log := logs[0]

			// All required fields must be non-empty
			requiredFields := map[string]string{
				"id":          log.ID,
				"actor_type":  log.ActorType,
				"actor_id":    log.ActorID,
				"action":      log.Action,
				"target_type": log.TargetType,
				"target_id":   log.TargetID,
				"ip":          log.IP,
			}

			for field, value := range requiredFields {
				if value == "" {
					t.Logf("Required field %q is empty", field)
					return false
				}
			}

			// Detail should also be non-empty since we provided a non-empty detail
			if log.Detail == "" {
				t.Logf("Detail should not be empty when non-empty detail is provided")
				return false
			}

			return true
		},
		genActorType(),
		genActorID(),
		genAuditAction(),
		genAuditTargetType(),
		genAuditTargetID(),
		genIP(),
	))

	// Property 3: The detail field is sanitized - no sensitive info (private keys,
	// tokens, webhook URLs) should appear in the saved record.
	properties.Property("detail field is sanitized of sensitive information", prop.ForAll(
		func(actorType, actorID, action, targetType, targetID, sensitiveDetail, ip string) bool {
			svc := setupAuditLogPropertyTestDB(t)
			ctx := context.Background()

			err := svc.Log(ctx, actorType, actorID, action, targetType, targetID, sensitiveDetail, ip)
			if err != nil {
				t.Logf("Log failed: %v", err)
				return false
			}

			logs, err := svc.List(ctx, repository.AuditLogFilter{})
			if err != nil {
				t.Logf("List failed: %v", err)
				return false
			}

			if len(logs) != 1 {
				t.Logf("Expected 1 log, got %d", len(logs))
				return false
			}

			savedDetail := logs[0].Detail

			// The saved detail must NOT contain any sensitive patterns
			sensitiveIndicators := []string{
				"BEGIN PRIVATE KEY",
				"BEGIN RSA PRIVATE KEY",
				"abcdef1234567890abcdef1234567890",
				"feishu.cn/open-apis/bot",
				"hooks.slack.com/services",
				"eyJhbGciOiJIUzI1NiI",
				"ABCdefGHIjklMNOpqrsTUVwxyz",
				"mysecretpassword123",
				"sk-1234567890abcdef",
			}

			for _, indicator := range sensitiveIndicators {
				if strings.Contains(savedDetail, indicator) {
					t.Logf("Saved detail contains sensitive info %q: %s", indicator, savedDetail)
					return false
				}
			}

			// The saved detail should contain [REDACTED] if the original had sensitive data
			if savedDetail != sensitiveDetail && !strings.Contains(savedDetail, "[REDACTED]") {
				t.Logf("Detail was modified but does not contain [REDACTED]: %s", savedDetail)
				return false
			}

			return true
		},
		genActorType(),
		genActorID(),
		genAuditAction(),
		genAuditTargetType(),
		genAuditTargetID(),
		genSensitiveDetail(),
		genIP(),
	))

	properties.TestingRun(t)
}
