package tests

import (
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 3.10, 3.11, 3.12**
//
// Preservation Property Tests for HTML/JS Mismatch Bugfix
// These tests verify EXISTING correct behavior that must not regress.
// They MUST PASS on the current (unfixed) code.
// After the fix is applied, these tests must CONTINUE to pass (no regression).

// --- init.js API endpoint preservation ---

func TestPreservation_HTMLJSMismatch_InitJS_APIEndpoints(t *testing.T) {
	// Property: init.js calls POST /init/admin with {username, password}
	//           and POST /init/config with nested config structure
	content := readJSStaticFile(t, "init.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("init.js calls POST /init/admin with username and password fields", prop.ForAll(
		func(_ int) bool {
			// Must call App.post('/init/admin', ...)
			hasInitAdminEndpoint := strings.Contains(content, "App.post('/init/admin'")

			// Body must contain username and password fields
			hasUsernameField := strings.Contains(content, "username: username")
			hasPasswordField := strings.Contains(content, "password: password")

			return hasInitAdminEndpoint && hasUsernameField && hasPasswordField
		},
		gen.Const(1),
	))

	properties.Property("init.js calls POST /init/config with nested config structure", prop.ForAll(
		func(_ int) bool {
			// Must call App.post('/init/config', ...)
			hasInitConfigEndpoint := strings.Contains(content, "App.post('/init/config'")

			// Config body must have nested structure keys
			hasServerKey := strings.Contains(content, "server:")
			hasAgentKey := strings.Contains(content, "agent:")
			hasAlertKey := strings.Contains(content, "alert:")
			hasCertbotKey := strings.Contains(content, "certbot:")
			hasReadonlyKey := strings.Contains(content, "readonly:")
			hasDomainMonitorKey := strings.Contains(content, "domain_monitor:")

			// Nested fields within server
			hasExternalUrl := strings.Contains(content, "external_url:")
			hasListenAddr := strings.Contains(content, "listen_addr:")

			// Nested fields within agent
			hasHeartbeatTimeout := strings.Contains(content, "heartbeat_timeout_seconds:")
			hasPollInterval := strings.Contains(content, "poll_interval_seconds:")

			// Nested fields within certbot
			hasBinaryPath := strings.Contains(content, "binary_path:")
			hasDataDir := strings.Contains(content, "data_dir:")
			hasEmail := strings.Contains(content, "email:")

			return hasInitConfigEndpoint &&
				hasServerKey && hasAgentKey && hasAlertKey &&
				hasCertbotKey && hasReadonlyKey && hasDomainMonitorKey &&
				hasExternalUrl && hasListenAddr &&
				hasHeartbeatTimeout && hasPollInterval &&
				hasBinaryPath && hasDataDir && hasEmail
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- login.js API endpoint preservation ---

func TestPreservation_HTMLJSMismatch_LoginJS_APIEndpoints(t *testing.T) {
	// Property: login.js calls POST /api/auth/login with {username, password}
	//           and POST /api/auth/readonly-login with {password}
	content := readJSStaticFile(t, "login.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("login.js calls POST /api/auth/login with username and password", prop.ForAll(
		func(_ int) bool {
			// Must call App.post('/api/auth/login', ...)
			hasLoginEndpoint := strings.Contains(content, "App.post('/api/auth/login'")

			// Body must contain username and password
			hasUsernameField := strings.Contains(content, "username: username")
			hasPasswordField := strings.Contains(content, "password: password")

			return hasLoginEndpoint && hasUsernameField && hasPasswordField
		},
		gen.Const(1),
	))

	properties.Property("login.js calls POST /api/auth/readonly-login with password", prop.ForAll(
		func(_ int) bool {
			// Must call App.post('/api/auth/readonly-login', ...)
			hasReadonlyEndpoint := strings.Contains(content, "App.post('/api/auth/readonly-login'")

			// Body must contain password field
			hasPasswordField := strings.Contains(content, "password: password")

			return hasReadonlyEndpoint && hasPasswordField
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- certificates.js API endpoint preservation ---

func TestPreservation_HTMLJSMismatch_CertificatesJS_APIEndpoints(t *testing.T) {
	// Property: certificates.js calls correct API endpoints for CRUD operations
	content := readJSStaticFile(t, "certificates.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("certificates.js calls GET /api/certificates/ for listing", prop.ForAll(
		func(_ int) bool {
			return strings.Contains(content, "App.get('/api/certificates/'")
		},
		gen.Const(1),
	))

	properties.Property("certificates.js calls POST /api/certificates/ for upload with correct body fields", prop.ForAll(
		func(_ int) bool {
			// Must call App.post('/api/certificates/', body)
			hasUploadEndpoint := strings.Contains(content, "App.post('/api/certificates/'")

			// Body must contain name, cert_pem, key_pem, auto_renew
			hasNameField := strings.Contains(content, "name:")
			hasCertPem := strings.Contains(content, "cert_pem:")
			hasKeyPem := strings.Contains(content, "key_pem:")
			hasAutoRenew := strings.Contains(content, "auto_renew:")

			return hasUploadEndpoint && hasNameField && hasCertPem && hasKeyPem && hasAutoRenew
		},
		gen.Const(1),
	))

	properties.Property("certificates.js calls POST /api/certificates/issue/cloudflare with correct body", prop.ForAll(
		func(_ int) bool {
			// Must call App.post('/api/certificates/issue/cloudflare', ...)
			hasCloudflareEndpoint := strings.Contains(content, "App.post('/api/certificates/issue/cloudflare'")

			// Body must contain name, domains, thirdpart_dns_id, auto_renew
			hasName := strings.Contains(content, "name:")
			hasDomains := strings.Contains(content, "domains:")
			hasThirdpartDnsId := strings.Contains(content, "thirdpart_dns_id:")
			hasAutoRenew := strings.Contains(content, "auto_renew:")

			return hasCloudflareEndpoint && hasName && hasDomains && hasThirdpartDnsId && hasAutoRenew
		},
		gen.Const(1),
	))

	properties.Property("certificates.js calls POST /api/certificates/issue/manual-dns/start with correct body", prop.ForAll(
		func(_ int) bool {
			// Must call App.post('/api/certificates/issue/manual-dns/start', ...)
			hasStartEndpoint := strings.Contains(content, "App.post('/api/certificates/issue/manual-dns/start'")

			// Body must contain name, domains, email
			hasName := strings.Contains(content, "name:")
			hasDomains := strings.Contains(content, "domains:")
			hasEmail := strings.Contains(content, "email:")

			return hasStartEndpoint && hasName && hasDomains && hasEmail
		},
		gen.Const(1),
	))

	properties.Property("certificates.js calls POST /api/certificates/issue/manual-dns/complete with correct body", prop.ForAll(
		func(_ int) bool {
			// Must call App.post('/api/certificates/issue/manual-dns/complete', ...)
			hasCompleteEndpoint := strings.Contains(content, "App.post('/api/certificates/issue/manual-dns/complete'")

			// Body must contain session_id and auto_renew
			hasSessionId := strings.Contains(content, "session_id:")
			hasAutoRenew := strings.Contains(content, "auto_renew:")

			return hasCompleteEndpoint && hasSessionId && hasAutoRenew
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- machines.js API endpoint preservation ---

func TestPreservation_HTMLJSMismatch_MachinesJS_APIEndpoints(t *testing.T) {
	// Property: machines.js calls correct API endpoints for CRUD operations
	content := readJSStaticFile(t, "machines.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("machines.js calls GET /api/machines for listing", prop.ForAll(
		func(_ int) bool {
			return strings.Contains(content, "App.get(url)") &&
				strings.Contains(content, "let url = '/api/machines'")
		},
		gen.Const(1),
	))

	properties.Property("machines.js calls POST /api/machines with {name, ip, tags, remark}", prop.ForAll(
		func(_ int) bool {
			// Must call App.post('/api/machines', ...)
			hasCreateEndpoint := strings.Contains(content, "App.post('/api/machines',")

			// Body must contain name, ip, tags, remark
			hasName := strings.Contains(content, "name: name")
			hasIp := strings.Contains(content, "ip: ip")
			hasTags := strings.Contains(content, "tags: tags")
			hasRemark := strings.Contains(content, "remark: remark")

			return hasCreateEndpoint && hasName && hasIp && hasTags && hasRemark
		},
		gen.Const(1),
	))

	properties.Property("machines.js calls DELETE /api/machines/{id}", prop.ForAll(
		func(_ int) bool {
			return strings.Contains(content, "App.delete('/api/machines/' + id)")
		},
		gen.Const(1),
	))

	properties.Property("machines.js calls POST /api/machines/{id}/regenerate-token", prop.ForAll(
		func(_ int) bool {
			return strings.Contains(content, "App.post('/api/machines/' + id + '/regenerate-token'")
		},
		gen.Const(1),
	))

	properties.Property("machines.js calls POST /api/machines/{id}/certificates/{mcId}/deploy", prop.ForAll(
		func(_ int) bool {
			return strings.Contains(content, "/certificates/") &&
				strings.Contains(content, "/deploy")
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- thirdpart-dns.js API endpoint preservation ---

func TestPreservation_HTMLJSMismatch_ThirdpartDNSJS_APIEndpoints(t *testing.T) {
	// Property: thirdpart-dns.js calls correct API endpoints for CRUD operations
	content := readJSStaticFile(t, "thirdpart-dns.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("thirdpart-dns.js calls GET /api/thirdpart-dns for listing", prop.ForAll(
		func(_ int) bool {
			return strings.Contains(content, "App.get('/api/thirdpart-dns'")
		},
		gen.Const(1),
	))

	properties.Property("thirdpart-dns.js calls POST /api/thirdpart-dns with {name, type, config_json, main_domains}", prop.ForAll(
		func(_ int) bool {
			// Must call App.post('/api/thirdpart-dns', body)
			hasCreateEndpoint := strings.Contains(content, "App.post('/api/thirdpart-dns'")

			// Body must contain name, type, config_json, main_domains
			hasBody := strings.Contains(content, "{ name, type, config_json, main_domains }")

			return hasCreateEndpoint && hasBody
		},
		gen.Const(1),
	))

	properties.Property("thirdpart-dns.js calls PUT /api/thirdpart-dns/{id} for update", prop.ForAll(
		func(_ int) bool {
			return strings.Contains(content, "App.put(`/api/thirdpart-dns/${id}`")
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}
