package tests

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// **Validates: Requirements 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9, 1.10, 1.11, 1.12, 1.13, 1.14, 1.15, 1.16, 1.17, 1.18, 1.19, 1.20, 1.21, 1.22, 1.23, 1.24**
//
// Bug Condition Exploration Tests
// These tests verify that the frontend JS files use the CORRECT API contracts.
// They are EXPECTED TO FAIL on the current (unfixed) code, confirming the bugs exist.
// After the fix is applied, these tests will PASS.

const jsDir = "../web/static/js/"

func readJSFile(t *testing.T, filename string) string {
	t.Helper()
	content, err := os.ReadFile(jsDir + filename)
	if err != nil {
		t.Fatalf("failed to read %s: %v", filename, err)
	}
	return string(content)
}

// --- domains.js tests ---

func TestBugCondition_DomainsJS_CreateBody(t *testing.T) {
	// Property: domains.js create request body MUST use { name, monitor_port, linked_certificate_id }
	// Bug: current code uses { domain, port, certificate_id }
	content := readJSFile(t, "domains.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("domains.js create body uses 'name' field (not 'domain')", prop.ForAll(
		func(_ int) bool {
			// The create/submit function should build a body with field name "name" (not "domain")
			// Look for the request body construction
			// Correct: { name: ..., monitor_port: ... }
			// Bug: { domain: ..., port: ... }

			// Check that the code does NOT use `{ domain` or `domain,` as a request body field
			// and DOES use `name` as the domain field in the request body
			hasCorrectNameField := strings.Contains(content, "name:") || strings.Contains(content, `"name"`)
			hasBuggyDomainField := strings.Contains(content, "{ domain") || strings.Contains(content, "{domain")

			// The body construction should use monitor_port, not just port
			hasMonitorPort := strings.Contains(content, "monitor_port")

			// Should use linked_certificate_id, not certificate_id
			hasLinkedCertId := strings.Contains(content, "linked_certificate_id")

			return hasCorrectNameField && !hasBuggyDomainField && hasMonitorPort && hasLinkedCertId
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

func TestBugCondition_DomainsJS_NoProbeAll(t *testing.T) {
	// Property: domains.js MUST NOT call /api/domains/probe-all (endpoint doesn't exist)
	content := readJSFile(t, "domains.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("domains.js does not call probe-all endpoint", prop.ForAll(
		func(_ int) bool {
			return !strings.Contains(content, "probe-all")
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

func TestBugCondition_DomainsJS_NoPagination(t *testing.T) {
	// Property: domains.js MUST NOT use page/page_size pagination params
	content := readJSFile(t, "domains.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("domains.js does not use page/page_size params", prop.ForAll(
		func(_ int) bool {
			hasPage := strings.Contains(content, "page=") || strings.Contains(content, "page_size")
			return !hasPage
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- thirdpart-dns.js tests ---

func TestBugCondition_ThirdpartDNSJS_CreateBody(t *testing.T) {
	// Property: thirdpart-dns.js create body MUST use { name, type, api_token, config_json, main_domains }
	// Bug: current code uses { name, provider, domain, api_token, zone_id, enabled }
	content := readJSFile(t, "thirdpart-dns.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("thirdpart-dns.js uses correct create body fields", prop.ForAll(
		func(_ int) bool {
			// Should have config_json and main_domains
			hasConfigJson := strings.Contains(content, "config_json")
			hasMainDomains := strings.Contains(content, "main_domains")

			// Should NOT use provider as a request body field (it should use "type")
			// Check that the body construction uses "type" not "provider"
			// The body variable assignment should not include "provider"
			bodyPattern := regexp.MustCompile(`body\s*=\s*\{[^}]*provider`)
			hasBuggyProvider := bodyPattern.MatchString(content)

			return hasConfigJson && hasMainDomains && !hasBuggyProvider
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

func TestBugCondition_ThirdpartDNSJS_NoSyncAll(t *testing.T) {
	// Property: thirdpart-dns.js MUST NOT call /api/thirdpart-dns/sync-all
	content := readJSFile(t, "thirdpart-dns.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("thirdpart-dns.js does not call sync-all endpoint", prop.ForAll(
		func(_ int) bool {
			return !strings.Contains(content, "sync-all")
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- alerts.js tests ---

func TestBugCondition_AlertsJS_ChannelCreateBody(t *testing.T) {
	// Property: alerts.js channel create body MUST use { name, type, config_json, enabled }
	//           with type only ['lark','telegram']
	// Bug: current code uses { name, type, webhook_url, enabled } with types webhook/email/dingtalk/feishu/wechat
	content := readJSFile(t, "alerts.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("alerts.js channel create uses config_json and only lark/telegram types", prop.ForAll(
		func(_ int) bool {
			// Should use config_json in request body
			hasConfigJson := strings.Contains(content, "config_json")

			// Should NOT use webhook_url as a request body field
			hasWebhookUrl := strings.Contains(content, "webhook_url")

			// Should have lark and telegram as type options
			hasLark := strings.Contains(content, "lark")
			hasTelegram := strings.Contains(content, "telegram")

			// Should NOT have webhook/email/dingtalk/feishu/wechat as type options
			hasWebhookType := strings.Contains(content, `"webhook"`) || strings.Contains(content, `'webhook'`)
			hasEmailType := strings.Contains(content, `"email"`) || strings.Contains(content, `'email'`)
			hasDingtalkType := strings.Contains(content, `"dingtalk"`) || strings.Contains(content, `'dingtalk'`)
			hasFeishuType := strings.Contains(content, `"feishu"`) || strings.Contains(content, `'feishu'`)
			hasWechatType := strings.Contains(content, `"wechat"`) || strings.Contains(content, `'wechat'`)

			return hasConfigJson && !hasWebhookUrl &&
				hasLark && hasTelegram &&
				!hasWebhookType && !hasEmailType && !hasDingtalkType && !hasFeishuType && !hasWechatType
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

func TestBugCondition_AlertsJS_NoPaginationAndCorrectFilters(t *testing.T) {
	// Property: alerts.js MUST NOT use page/page_size, MUST use level/type/status filters
	content := readJSFile(t, "alerts.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("alerts.js uses correct filter params without pagination", prop.ForAll(
		func(_ int) bool {
			// Should NOT use page/page_size in alert history loading
			// Look for the alert history URL construction
			alertHistoryPattern := regexp.MustCompile(`/api/alerts\?.*page`)
			hasPagination := alertHistoryPattern.MatchString(content)

			// Should use level, type, status as filter params
			hasLevelFilter := strings.Contains(content, "level")
			hasStatusFilter := strings.Contains(content, "status")

			return !hasPagination && hasLevelFilter && hasStatusFilter
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

func TestBugCondition_AlertsJS_RenderFields(t *testing.T) {
	// Property: alerts.js renders using a.level, a.type, a.title, a.content, a.created_at
	// Bug: current code uses a.severity, a.alert_type, a.message, a.timestamp
	content := readJSFile(t, "alerts.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("alerts.js renders with correct field names", prop.ForAll(
		func(_ int) bool {
			// Should use a.title and a.content for alert message display
			hasTitle := strings.Contains(content, "a.title")
			hasContent := strings.Contains(content, "a.content")

			// Should NOT use a.message as the primary display field
			// (note: the current code has `a.message || '-'` which is the bug)
			hasMessage := strings.Contains(content, "a.message")

			// Should NOT use a.severity (should use a.level)
			hasSeverity := strings.Contains(content, "a.severity")

			// Should NOT use a.alert_type (should use a.type)
			hasAlertType := strings.Contains(content, "a.alert_type") || strings.Contains(content, "alert_type")

			// Should NOT use a.timestamp (should use a.created_at)
			// Note: a.created_at is already used with fallback, but a.timestamp should not be present
			hasTimestamp := strings.Contains(content, "a.timestamp")

			return hasTitle && hasContent && !hasMessage && !hasSeverity && !hasAlertType && !hasTimestamp
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- audit-logs.js tests ---

func TestBugCondition_AuditLogsJS_PaginationParams(t *testing.T) {
	// Property: audit-logs.js MUST use limit/offset/actor_type/target_type params
	// Bug: current code uses page/page_size/action/from/to
	content := readJSFile(t, "audit-logs.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("audit-logs.js uses limit/offset and actor_type/target_type params", prop.ForAll(
		func(_ int) bool {
			// Should use limit and offset
			hasLimit := strings.Contains(content, "limit")
			hasOffset := strings.Contains(content, "offset")

			// Should use actor_type and target_type
			hasActorType := strings.Contains(content, "actor_type")
			hasTargetType := strings.Contains(content, "target_type")

			// Should NOT use page/page_size
			hasPage := regexp.MustCompile(`page=\$\{|page_size`).MatchString(content)

			// Should NOT use action/from/to as filter params in URL
			hasActionFilter := regexp.MustCompile(`&action=`).MatchString(content)
			hasFromFilter := regexp.MustCompile(`&from=`).MatchString(content)
			hasToFilter := regexp.MustCompile(`&to=`).MatchString(content)

			return hasLimit && hasOffset && hasActorType && hasTargetType &&
				!hasPage && !hasActionFilter && !hasFromFilter && !hasToFilter
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

func TestBugCondition_AuditLogsJS_RenderFields(t *testing.T) {
	// Property: audit-logs.js renders using log.actor_type, log.actor_id, log.target_type,
	//           log.target_id, log.detail, log.ip, log.created_at
	// Bug: current code uses log.username, log.user_id, log.resource_type, log.resource_id,
	//      log.details, log.ip_address, log.timestamp
	content := readJSFile(t, "audit-logs.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("audit-logs.js renders with correct field names", prop.ForAll(
		func(_ int) bool {
			// Should use actor_type and actor_id
			hasActorType := strings.Contains(content, "log.actor_type")
			hasActorId := strings.Contains(content, "log.actor_id")

			// Should use target_type and target_id
			hasTargetType := strings.Contains(content, "log.target_type")
			hasTargetId := strings.Contains(content, "log.target_id")

			// Should use detail (singular) and ip (short)
			hasDetail := strings.Contains(content, "log.detail")
			hasIp := strings.Contains(content, "log.ip")

			// Should NOT use username, resource_type, resource_id, ip_address
			hasUsername := strings.Contains(content, "log.username")
			hasResourceType := strings.Contains(content, "log.resource_type")
			hasResourceId := strings.Contains(content, "log.resource_id")
			hasIpAddress := strings.Contains(content, "log.ip_address")

			return hasActorType && hasActorId && hasTargetType && hasTargetId &&
				hasDetail && hasIp &&
				!hasUsername && !hasResourceType && !hasResourceId && !hasIpAddress
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- users.js tests ---

func TestBugCondition_UsersJS_ResetPassword(t *testing.T) {
	// Property: users.js reset password calls POST /api/users/{id}/reset-password + { new_password }
	// Bug: current code calls PUT /api/users/{id}/password + { password }
	content := readJSFile(t, "users.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("users.js reset password uses correct endpoint and field", prop.ForAll(
		func(_ int) bool {
			// Should call POST to reset-password endpoint
			hasResetPasswordEndpoint := strings.Contains(content, "reset-password")

			// Should use new_password field
			hasNewPasswordField := strings.Contains(content, "new_password")

			// Should NOT call PUT .../password
			hasPutPassword := regexp.MustCompile(`App\.put\([^)]*password`).MatchString(content)

			return hasResetPasswordEndpoint && hasNewPasswordField && !hasPutPassword
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

func TestBugCondition_UsersJS_DisableUser(t *testing.T) {
	// Property: users.js disable user calls POST /api/users/{id}/disable
	// Bug: current code calls PUT /api/users/{id} + { enabled: false }
	content := readJSFile(t, "users.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("users.js disable user uses POST /disable endpoint", prop.ForAll(
		func(_ int) bool {
			// Should have /disable endpoint call
			hasDisableEndpoint := strings.Contains(content, "/disable")

			// Should NOT use { enabled: enable } or { enabled: false } pattern for toggling
			hasEnabledToggle := regexp.MustCompile(`\{\s*enabled\s*:`).MatchString(content)

			return hasDisableEndpoint && !hasEnabledToggle
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

func TestBugCondition_UsersJS_NoDeleteOrGetSingle(t *testing.T) {
	// Property: users.js MUST NOT call DELETE /api/users/{id} or GET /api/users/{id}
	content := readJSFile(t, "users.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("users.js does not call DELETE or GET single user", prop.ForAll(
		func(_ int) bool {
			// Should NOT have App.delete for users
			hasDelete := regexp.MustCompile(`App\.delete\([^)]*users`).MatchString(content)

			// Should NOT have App.get for single user (GET /api/users/{id})
			// Note: GET /api/users (list) is fine, but GET /api/users/${id} is not
			hasSingleGet := regexp.MustCompile(`App\.get\([^)]*users/\$\{`).MatchString(content)

			return !hasDelete && !hasSingleGet
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

func TestBugCondition_UsersJS_RoleOptions(t *testing.T) {
	// Property: users.js role options are only 'admin'/'user' (no 'readonly')
	content := readJSFile(t, "users.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("users.js role options are admin/user only", prop.ForAll(
		func(_ int) bool {
			// Should have 'user' as a role option
			hasUserRole := strings.Contains(content, `"user"`) || strings.Contains(content, `'user'`) ||
				strings.Contains(content, `value="user"`)

			// Should NOT have 'readonly' as a role option
			hasReadonlyRole := strings.Contains(content, `"readonly"`) || strings.Contains(content, `'readonly'`) ||
				strings.Contains(content, `value="readonly"`)

			return hasUserRole && !hasReadonlyRole
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- system.js tests ---

func TestBugCondition_SystemJS_SaveNestedConfig(t *testing.T) {
	// Property: system.js saves config as nested structure
	//           { server: {...}, agent: {...}, alert: {...}, certbot: {...}, readonly: {...}, domain_monitor: {...} }
	// Bug: current code sends flat structure { listen_addr, certbot_email, ... }
	content := readJSFile(t, "system.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("system.js saves config as nested structure", prop.ForAll(
		func(_ int) bool {
			// Should construct nested structure with these top-level keys
			hasServer := strings.Contains(content, "server:")  || strings.Contains(content, `"server"`) || strings.Contains(content, "server :")
			hasAgent := strings.Contains(content, "agent:") || strings.Contains(content, `"agent"`) || strings.Contains(content, "agent :")
			hasCertbot := strings.Contains(content, "certbot:") || strings.Contains(content, `"certbot"`) || strings.Contains(content, "certbot :")
			hasReadonly := strings.Contains(content, "readonly:") || strings.Contains(content, `"readonly"`) || strings.Contains(content, "readonly :")
			hasDomainMonitor := strings.Contains(content, "domain_monitor:") || strings.Contains(content, `"domain_monitor"`) || strings.Contains(content, "domain_monitor :")

			// Should NOT send flat fields like listen_addr, certbot_email directly in body
			hasFlatListenAddr := regexp.MustCompile(`body\.listen_addr|body\["listen_addr"\]|listen_addr:`).MatchString(content)
			hasFlatCertbotEmail := regexp.MustCompile(`body\.certbot_email|body\["certbot_email"\]|certbot_email:`).MatchString(content)

			return hasServer && hasAgent && hasCertbot && hasReadonly && hasDomainMonitor &&
				!hasFlatListenAddr && !hasFlatCertbotEmail
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

func TestBugCondition_SystemJS_LoadNestedConfig(t *testing.T) {
	// Property: system.js loads config from nested paths (cfg.server.listen_addr, cfg.certbot.email, etc.)
	// Bug: current code reads cfg.listen_addr, cfg.certbot_email (flat)
	content := readJSFile(t, "system.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("system.js loads config from nested paths", prop.ForAll(
		func(_ int) bool {
			// Should read from nested paths
			hasServerListenAddr := strings.Contains(content, "cfg.server.listen_addr") || strings.Contains(content, "server?.listen_addr") || strings.Contains(content, "server.listen_addr")
			hasCertbotEmail := strings.Contains(content, "cfg.certbot.email") || strings.Contains(content, "certbot?.email") || strings.Contains(content, "certbot.email")

			// Should NOT read flat fields as primary (without nested fallback being primary)
			// The bug is that cfg.listen_addr is the primary read, with cfg.server?.listen_addr as fallback
			// After fix, cfg.server.listen_addr should be primary
			hasFlatPrimary := regexp.MustCompile(`cfg\.listen_addr\s*\|\|`).MatchString(content)
			hasFlatCertbotPrimary := regexp.MustCompile(`cfg\.certbot_email\s*\|\|`).MatchString(content)

			return hasServerListenAddr && hasCertbotEmail && !hasFlatPrimary && !hasFlatCertbotPrimary
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- dashboard.js tests ---

func TestBugCondition_DashboardJS_NoTrailingSlash(t *testing.T) {
	// Property: dashboard.js uses /api/dashboard (no trailing slash)
	content := readJSFile(t, "dashboard.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("dashboard.js uses /api/dashboard without trailing slash", prop.ForAll(
		func(_ int) bool {
			// Should have /api/dashboard without trailing slash
			hasCorrectUrl := strings.Contains(content, "'/api/dashboard'") || strings.Contains(content, `"/api/dashboard"`) ||
				strings.Contains(content, "'/api/dashboard)") || strings.Contains(content, `'/api/dashboard')`)

			// Should NOT have /api/dashboard/ with trailing slash
			hasTrailingSlash := strings.Contains(content, "/api/dashboard/'") || strings.Contains(content, `/api/dashboard/"`) ||
				strings.Contains(content, "/api/dashboard/')") || strings.Contains(content, `/api/dashboard/")`)

			return hasCorrectUrl && !hasTrailingSlash
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- machines.js tests ---

func TestBugCondition_MachinesJS_NoTrailingSlashes(t *testing.T) {
	// Property: machines.js uses URLs without trailing slashes
	content := readJSFile(t, "machines.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("machines.js uses URLs without trailing slashes", prop.ForAll(
		func(_ int) bool {
			// Should NOT have /api/machines/ as a standalone list URL (with trailing slash and no path continuation)
			// Pattern: App.get('/api/machines/') or url = '/api/machines/'
			hasListTrailingSlash := regexp.MustCompile(`(get|post)\('/api/machines/'\)`).MatchString(content) ||
				strings.Contains(content, `url = '/api/machines/'`)

			// Should NOT have trailing slashes after id in detail/delete URLs
			// Pattern: + id + '/' (trailing slash after id with nothing following)
			hasDetailTrailingSlash := regexp.MustCompile(`\+\s*id\s*\+\s*'/'\s*\)`).MatchString(content)

			// Should NOT have trailing slashes in machine certificates URLs that end with just /
			// Pattern: /certificates/' + mcId + '/' at end, or mcId + '/' at end
			// Note: App.get('/api/certificates/') is a DIFFERENT endpoint (cert list) and correctly has trailing slash
			hasCertTrailingSlash := regexp.MustCompile(`\+\s*mcId\s*\+\s*'/'\s*\)`).MatchString(content)

			return !hasListTrailingSlash && !hasDetailTrailingSlash && !hasCertTrailingSlash
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}
