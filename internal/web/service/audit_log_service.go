package service

import (
	"context"
	"regexp"
	"strings"

	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// Sensitive patterns to sanitize from audit log detail fields.
var sensitivePatterns = []*regexp.Regexp{
	// Private key blocks (PEM format)
	regexp.MustCompile(`(?s)-----BEGIN\s+(RSA\s+)?PRIVATE\s+KEY-----.*?-----END\s+(RSA\s+)?PRIVATE\s+KEY-----`),
	// Generic token patterns (hex or base64 strings that look like tokens, 32+ chars)
	regexp.MustCompile(`(?i)(token|agent_token|api_token|secret)["\s:=]+["\s]*([A-Za-z0-9+/=_\-]{32,})`),
	// Webhook URLs (Lark, Slack, Discord, generic webhook - matches "webhook", "hook", "hooks" in URL)
	regexp.MustCompile(`(?i)https?://[^\s"',}]*(?:webhook|hooks?\.slack\.com|hook/)[^\s"',}]*`),
	// Lark/Feishu bot webhook URLs
	regexp.MustCompile(`(?i)https?://[^\s"',}]*(?:feishu\.cn|larksuite\.com)[^\s"',}]*/hook[^\s"',}]*`),
	// Bearer tokens in headers
	regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9+/=_\-\.]{10,}`),
	// Telegram bot tokens (numeric:alphanumeric pattern)
	regexp.MustCompile(`(?i)(bot_token|telegram[_\s]*token)["\s:=]+["\s]*\d+:[A-Za-z0-9_\-]{30,}`),
}

// Replacement text for sanitized content.
const sanitizedReplacement = "[REDACTED]"

// AuditLogService handles audit log business logic.
type AuditLogService struct {
	repo *repository.AuditLogRepository
}

// NewAuditLogService creates a new AuditLogService.
func NewAuditLogService(repo *repository.AuditLogRepository) *AuditLogService {
	return &AuditLogService{repo: repo}
}

// Log creates an audit log entry with sanitized detail.
func (s *AuditLogService) Log(ctx context.Context, actorType, actorID, action, targetType, targetID, detail, ip string) error {
	sanitizedDetail := SanitizeDetail(detail)

	log := &model.AuditLog{
		ActorType:  actorType,
		ActorID:    actorID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Detail:     sanitizedDetail,
		IP:         ip,
	}

	return s.repo.Create(ctx, log)
}

// List returns audit logs matching the filter, ordered by time DESC.
func (s *AuditLogService) List(ctx context.Context, filter repository.AuditLogFilter) ([]*model.AuditLog, error) {
	return s.repo.List(ctx, filter)
}

// Count returns the total number of audit logs matching the filter.
func (s *AuditLogService) Count(ctx context.Context, filter repository.AuditLogFilter) (int, error) {
	return s.repo.Count(ctx, filter)
}

// SanitizeDetail removes sensitive information from the detail string.
// It redacts: private keys, tokens, webhook URLs, bearer tokens, and telegram bot tokens.
func SanitizeDetail(detail string) string {
	if detail == "" {
		return detail
	}

	result := detail
	for _, pattern := range sensitivePatterns {
		result = pattern.ReplaceAllString(result, sanitizedReplacement)
	}

	// Additional keyword-based sanitization for key=value patterns
	result = sanitizeKeyValuePairs(result)

	return result
}

// sanitizeKeyValuePairs redacts values for known sensitive keys in key=value or key:value patterns.
func sanitizeKeyValuePairs(s string) string {
	sensitiveKeys := []string{
		"private_key", "privkey", "secret_key",
		"webhook_url", "webhook",
		"agent_token", "api_token", "access_token",
		"password", "passwd",
	}

	result := s
	for _, key := range sensitiveKeys {
		// Match patterns like key="value" or key=value or "key": "value"
		patterns := []*regexp.Regexp{
			regexp.MustCompile(`(?i)` + regexp.QuoteMeta(key) + `\s*[=:]\s*"[^"]*"`),
			regexp.MustCompile(`(?i)"` + regexp.QuoteMeta(key) + `"\s*:\s*"[^"]*"`),
		}
		for _, p := range patterns {
			result = p.ReplaceAllStringFunc(result, func(match string) string {
				// Keep the key part, redact the value
				eqIdx := strings.IndexAny(match, "=:")
				if eqIdx == -1 {
					return sanitizedReplacement
				}
				return match[:eqIdx+1] + " " + sanitizedReplacement
			})
		}
	}

	return result
}
