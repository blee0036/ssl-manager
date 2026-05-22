package service

import (
	"context"
	"strings"
	"testing"

	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

func setupAuditLogService(t *testing.T) *AuditLogService {
	t.Helper()
	db := setupTestDB(t)

	// Create audit_logs table
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

func TestAuditLogService_Log(t *testing.T) {
	svc := setupAuditLogService(t)
	ctx := context.Background()

	err := svc.Log(ctx, "user", "user-1", "POST /api/certificates", "certificate", "cert-1", "created certificate for example.com", "192.168.1.1")
	if err != nil {
		t.Fatalf("Log failed: %v", err)
	}

	logs, err := svc.List(ctx, repository.AuditLogFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].ActorType != "user" {
		t.Errorf("expected actor_type 'user', got '%s'", logs[0].ActorType)
	}
	if logs[0].Detail != "created certificate for example.com" {
		t.Errorf("expected detail 'created certificate for example.com', got '%s'", logs[0].Detail)
	}
}

func TestAuditLogService_Log_SanitizesDetail(t *testing.T) {
	svc := setupAuditLogService(t)
	ctx := context.Background()

	detail := `updated config with key: -----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQC7
-----END PRIVATE KEY-----`

	err := svc.Log(ctx, "user", "user-1", "PUT /api/system/config", "system", "", detail, "192.168.1.1")
	if err != nil {
		t.Fatalf("Log failed: %v", err)
	}

	logs, err := svc.List(ctx, repository.AuditLogFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	if strings.Contains(logs[0].Detail, "BEGIN PRIVATE KEY") {
		t.Error("detail should not contain private key")
	}
	if strings.Contains(logs[0].Detail, "MIIEvgIBADANBgkqhkiG9w0BAQ") {
		t.Error("detail should not contain private key content")
	}
}

func TestSanitizeDetail_EmptyString(t *testing.T) {
	result := SanitizeDetail("")
	if result != "" {
		t.Errorf("expected empty string, got '%s'", result)
	}
}

func TestSanitizeDetail_NoSensitiveData(t *testing.T) {
	detail := "created certificate for example.com with auto_renew=true"
	result := SanitizeDetail(detail)
	if result != detail {
		t.Errorf("expected unchanged detail, got '%s'", result)
	}
}

func TestSanitizeDetail_PrivateKey(t *testing.T) {
	tests := []struct {
		name   string
		detail string
	}{
		{
			name: "RSA private key",
			detail: `config updated: -----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn/ygWep4PAtGoRBh5IIx
-----END RSA PRIVATE KEY-----`,
		},
		{
			name: "generic private key",
			detail: `key: -----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQC7
-----END PRIVATE KEY-----`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeDetail(tt.detail)
			if strings.Contains(result, "BEGIN") && strings.Contains(result, "PRIVATE KEY") {
				t.Errorf("result should not contain private key markers: %s", result)
			}
			if strings.Contains(result, "MIIEp") || strings.Contains(result, "MIIEvg") {
				t.Errorf("result should not contain key content: %s", result)
			}
			if !strings.Contains(result, "[REDACTED]") {
				t.Errorf("result should contain [REDACTED]: %s", result)
			}
		})
	}
}

func TestSanitizeDetail_WebhookURL(t *testing.T) {
	tests := []struct {
		name   string
		detail string
	}{
		{
			name:   "lark webhook",
			detail: "updated alert config: url=https://open.feishu.cn/open-apis/bot/v2/hook/abc123def456",
		},
		{
			name:   "slack webhook",
			detail: "configured channel with https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX",
		},
		{
			name:   "generic webhook",
			detail: "set webhook to https://example.com/api/webhook/notifications",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeDetail(tt.detail)
			if strings.Contains(result, "feishu.cn") || strings.Contains(result, "hooks.slack.com") || strings.Contains(result, "example.com/api/webhook") {
				t.Errorf("result should not contain webhook URL: %s", result)
			}
		})
	}
}

func TestSanitizeDetail_TokenPatterns(t *testing.T) {
	tests := []struct {
		name   string
		detail string
	}{
		{
			name:   "agent token in key=value",
			detail: `machine created with agent_token="abcdef1234567890abcdef1234567890abcdef12"`,
		},
		{
			name:   "api token in JSON",
			detail: `config: {"api_token": "sk-1234567890abcdef1234567890abcdef1234567890"}`,
		},
		{
			name:   "bearer token",
			detail: `request with Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeDetail(tt.detail)
			if strings.Contains(result, "abcdef1234567890abcdef1234567890") {
				t.Errorf("result should not contain token value: %s", result)
			}
			if strings.Contains(result, "sk-1234567890abcdef") {
				t.Errorf("result should not contain api token value: %s", result)
			}
			if strings.Contains(result, "eyJhbGciOiJIUzI1NiI") {
				t.Errorf("result should not contain bearer token value: %s", result)
			}
		})
	}
}

func TestSanitizeDetail_TelegramBotToken(t *testing.T) {
	detail := `configured telegram: bot_token="123456789:ABCdefGHIjklMNOpqrsTUVwxyz0123456789"`
	result := SanitizeDetail(detail)
	if strings.Contains(result, "123456789:ABCdefGHI") {
		t.Errorf("result should not contain telegram bot token: %s", result)
	}
}

func TestSanitizeDetail_PasswordField(t *testing.T) {
	detail := `user updated: password="mysecretpassword123"`
	result := SanitizeDetail(detail)
	if strings.Contains(result, "mysecretpassword123") {
		t.Errorf("result should not contain password value: %s", result)
	}
}

func TestSanitizeDetail_MixedContent(t *testing.T) {
	detail := `updated machine config: name=web-server-1, agent_token="abcdef1234567890abcdef1234567890abcdef12", ip=192.168.1.100`
	result := SanitizeDetail(detail)

	if !strings.Contains(result, "web-server-1") {
		t.Errorf("result should contain non-sensitive name: %s", result)
	}
	if !strings.Contains(result, "192.168.1.100") {
		t.Errorf("result should contain non-sensitive IP: %s", result)
	}
	if strings.Contains(result, "abcdef1234567890abcdef1234567890") {
		t.Errorf("result should not contain token value: %s", result)
	}
}

func TestSanitizeDetail_PreservesNonSensitiveJSON(t *testing.T) {
	detail := `{"action": "create", "name": "my-cert", "domains": ["example.com"]}`
	result := SanitizeDetail(detail)
	if !strings.Contains(result, "my-cert") {
		t.Errorf("result should preserve non-sensitive JSON content: %s", result)
	}
	if !strings.Contains(result, "example.com") {
		t.Errorf("result should preserve domain names: %s", result)
	}
}

func TestAuditLogService_List_TimeDescOrder(t *testing.T) {
	svc := setupAuditLogService(t)
	ctx := context.Background()

	actions := []string{"first", "second", "third"}
	for _, action := range actions {
		err := svc.Log(ctx, "user", "user-1", action, "certificate", "cert-1", action+" detail", "1.1.1.1")
		if err != nil {
			t.Fatalf("Log failed: %v", err)
		}
	}

	logs, err := svc.List(ctx, repository.AuditLogFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(logs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(logs))
	}

	// Verify DESC order (newest first)
	for i := 0; i < len(logs)-1; i++ {
		if logs[i].CreatedAt.Before(logs[i+1].CreatedAt) {
			t.Errorf("logs not in DESC order: log[%d].CreatedAt=%v < log[%d].CreatedAt=%v",
				i, logs[i].CreatedAt, i+1, logs[i+1].CreatedAt)
		}
	}
}
