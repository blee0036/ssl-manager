package tests

import (
	"os"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9**
//
// Preservation Property Tests
// These tests verify that currently-correct frontend behaviors are preserved.
// They MUST PASS on the current (unfixed) code - they verify what's already correct.
// After the fix is applied, these tests must CONTINUE to pass (no regression).

func readPreservationJSFile(t *testing.T, filename string) string {
	t.Helper()
	content, err := os.ReadFile(jsDir + filename)
	if err != nil {
		t.Fatalf("failed to read %s: %v", filename, err)
	}
	return string(content)
}

// --- Requirement 3.1: certificates.js API calls are correct ---

func TestPreservation_CertificatesJS_APICallsCorrect(t *testing.T) {
	content := readPreservationJSFile(t, "certificates.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("certificates.js contains correct API calls for list, detail, upload, issue, delete", prop.ForAll(
		func(_ int) bool {
			// List endpoint
			hasList := strings.Contains(content, "/api/certificates/")

			// Detail endpoint (GET /api/certificates/ + id)
			hasDetail := strings.Contains(content, "'/api/certificates/' + id") || strings.Contains(content, `"/api/certificates/" + id`)

			// Upload endpoint (POST /api/certificates/)
			hasUpload := strings.Contains(content, "App.post('/api/certificates/'")

			// Issue via Cloudflare
			hasCloudflare := strings.Contains(content, "/api/certificates/issue/cloudflare")

			// Issue via manual DNS start
			hasManualDNSStart := strings.Contains(content, "/api/certificates/issue/manual-dns/start")

			// Issue via manual DNS complete
			hasManualDNSComplete := strings.Contains(content, "/api/certificates/issue/manual-dns/complete")

			// Delete endpoint
			hasDelete := strings.Contains(content, "App.delete('/api/certificates/' + id")

			return hasList && hasDetail && hasUpload && hasCloudflare &&
				hasManualDNSStart && hasManualDNSComplete && hasDelete
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- Requirement 3.7: login.js is not modified ---

func TestPreservation_LoginJS_Unchanged(t *testing.T) {
	content := readPreservationJSFile(t, "login.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("login.js contains correct login flow with /api/auth/login and token storage", prop.ForAll(
		func(_ int) bool {
			// Login endpoint
			hasLoginEndpoint := strings.Contains(content, "/api/auth/login")

			// Sends username and password
			hasUsername := strings.Contains(content, "username")
			hasPassword := strings.Contains(content, "password")

			// Stores token
			hasTokenStorage := strings.Contains(content, "localStorage.setItem('token'")

			// Uses resp.data.token
			hasRespDataToken := strings.Contains(content, "resp.data.token")

			// Readonly login endpoint
			hasReadonlyLogin := strings.Contains(content, "/api/auth/readonly-login")

			return hasLoginEndpoint && hasUsername && hasPassword &&
				hasTokenStorage && hasRespDataToken && hasReadonlyLogin
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- Requirement 3.6: init.js uses nested config structure ---

func TestPreservation_InitJS_NestedConfigStructure(t *testing.T) {
	content := readPreservationJSFile(t, "init.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("init.js uses nested config structure with server, agent, alert, certbot, readonly, domain_monitor", prop.ForAll(
		func(_ int) bool {
			// Nested config structure keys
			hasServer := strings.Contains(content, "server:")
			hasAgent := strings.Contains(content, "agent:")
			hasAlert := strings.Contains(content, "alert:")
			hasCertbot := strings.Contains(content, "certbot:")
			hasReadonly := strings.Contains(content, "readonly:")
			hasDomainMonitor := strings.Contains(content, "domain_monitor:")

			// Posts to /init/config
			hasInitConfig := strings.Contains(content, "/init/config")

			// Posts to /init/admin
			hasInitAdmin := strings.Contains(content, "/init/admin")

			// Gets /init/status
			hasInitStatus := strings.Contains(content, "/init/status")

			return hasServer && hasAgent && hasAlert && hasCertbot &&
				hasReadonly && hasDomainMonitor &&
				hasInitConfig && hasInitAdmin && hasInitStatus
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- Requirement 3.8: app.js utility functions exist ---

func TestPreservation_AppJS_UtilityFunctions(t *testing.T) {
	content := readPreservationJSFile(t, "app.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("app.js provides App.get, App.post, App.put, App.delete, App.escapeHtml, App.formatDate, App.toast", prop.ForAll(
		func(_ int) bool {
			// HTTP methods
			hasGet := strings.Contains(content, "async get(")
			hasPost := strings.Contains(content, "async post(")
			hasPut := strings.Contains(content, "async put(")
			hasDelete := strings.Contains(content, "async delete(")

			// Utility functions
			hasEscapeHtml := strings.Contains(content, "escapeHtml(")
			hasFormatDate := strings.Contains(content, "formatDate(")
			hasToast := strings.Contains(content, "toast(")

			// Auth helper
			hasRequireAuth := strings.Contains(content, "requireAuth(")

			// Uses unified { code, message, data } response pattern
			hasJsonParse := strings.Contains(content, "resp.json()")

			return hasGet && hasPost && hasPut && hasDelete &&
				hasEscapeHtml && hasFormatDate && hasToast &&
				hasRequireAuth && hasJsonParse
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- Requirement 3.2: dashboard.js stats rendering ---

func TestPreservation_DashboardJS_StatsRendering(t *testing.T) {
	content := readPreservationJSFile(t, "dashboard.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("dashboard.js renders certificates_total, machines_online, certificates_expiring_15d and other stats", prop.ForAll(
		func(_ int) bool {
			// Stats fields from backend DashboardStats model
			hasCertsTotal := strings.Contains(content, "certificates_total")
			hasCertsExpiring := strings.Contains(content, "certificates_expiring_15d")
			hasCertsExpired := strings.Contains(content, "certificates_expired")
			hasMachinesOnline := strings.Contains(content, "machines_online")
			hasMachinesOffline := strings.Contains(content, "machines_offline")
			hasDeployFailures := strings.Contains(content, "deploy_failures_24h")
			hasRenewFailures := strings.Contains(content, "renew_failures_24h")
			hasDomainAnomalies := strings.Contains(content, "domain_anomalies")
			hasHasAnomalies := strings.Contains(content, "has_anomalies")

			return hasCertsTotal && hasCertsExpiring && hasCertsExpired &&
				hasMachinesOnline && hasMachinesOffline &&
				hasDeployFailures && hasRenewFailures &&
				hasDomainAnomalies && hasHasAnomalies
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- Requirement 3.3: machines.js create body and response ---

func TestPreservation_MachinesJS_CreateBody(t *testing.T) {
	content := readPreservationJSFile(t, "machines.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("machines.js create sends { name, ip, tags, remark } and handles { machine, agent_token } response", prop.ForAll(
		func(_ int) bool {
			// Create request body fields
			hasName := strings.Contains(content, "name: name")
			hasIp := strings.Contains(content, "ip: ip")
			hasTags := strings.Contains(content, "tags: tags")
			hasRemark := strings.Contains(content, "remark: remark")

			// Response handling - agent_token display
			hasAgentToken := strings.Contains(content, "data.agent_token")

			// Posts to /api/machines
			hasCreateEndpoint := strings.Contains(content, "App.post('/api/machines',") || strings.Contains(content, "App.post('/api/machines'")

			return hasName && hasIp && hasTags && hasRemark &&
				hasAgentToken && hasCreateEndpoint
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- Requirement 3.4: machines.js token management ---

func TestPreservation_MachinesJS_TokenManagement(t *testing.T) {
	content := readPreservationJSFile(t, "machines.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("machines.js has regenerate-token, revoke-token, and install-command endpoints", prop.ForAll(
		func(_ int) bool {
			// Regenerate token endpoint
			hasRegenerateToken := strings.Contains(content, "/regenerate-token")

			// Revoke token endpoint
			hasRevokeToken := strings.Contains(content, "/revoke-token")

			// Install command functionality (either via GET /install-command or via regenerate-token flow)
			hasInstallCommandFunc := strings.Contains(content, "getInstallCommand") || strings.Contains(content, "安装命令")

			// All use App.post or App.get
			hasRegeneratePost := strings.Contains(content, "App.post('/api/machines/' + id + '/regenerate-token'")
			hasRevokePost := strings.Contains(content, "App.post('/api/machines/' + id + '/revoke-token'")

			return hasRegenerateToken && hasRevokeToken && hasInstallCommandFunc &&
				hasRegeneratePost && hasRevokePost
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- Requirement 3.5: machines.js certificate deploy config CRUD ---

func TestPreservation_MachinesJS_CertDeployConfig(t *testing.T) {
	content := readPreservationJSFile(t, "machines.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("machines.js has certificate deploy config list, create, deploy trigger, and delete", prop.ForAll(
		func(_ int) bool {
			// List certificates endpoint
			hasCertList := strings.Contains(content, "/certificates/")

			// Create certificate config - sends certificate_id, cert_path, private_key_path
			hasCertId := strings.Contains(content, "certificate_id:")
			hasCertPath := strings.Contains(content, "cert_path:")
			hasPrivateKeyPath := strings.Contains(content, "private_key_path:")

			// Deploy trigger endpoint
			hasDeploy := strings.Contains(content, "/deploy")

			// Delete certificate config
			hasDeleteCert := strings.Contains(content, "deleteMachineCert")

			// Post deploy commands field
			hasPostDeployCommands := strings.Contains(content, "post_deploy_commands:")

			return hasCertList && hasCertId && hasCertPath && hasPrivateKeyPath &&
				hasDeploy && hasDeleteCert && hasPostDeployCommands
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- Requirement 3.9: All API responses use unified { code, message, data } wrapper ---

func TestPreservation_AllJS_UnifiedResponseWrapper(t *testing.T) {
	// Check that the app.js _request method returns the full JSON response
	// and all page JS files use resp.data to extract business data
	appContent := readPreservationJSFile(t, "app.js")
	certsContent := readPreservationJSFile(t, "certificates.js")
	dashboardContent := readPreservationJSFile(t, "dashboard.js")
	machinesContent := readPreservationJSFile(t, "machines.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("all JS files use resp.data extraction pattern for unified { code, message, data } wrapper", prop.ForAll(
		func(_ int) bool {
			// app.js returns json directly (which contains { code, message, data })
			appReturnsJson := strings.Contains(appContent, "return json")

			// app.js checks json.message for errors
			appChecksMessage := strings.Contains(appContent, "json.message")

			// certificates.js uses resp.data
			certsUsesRespData := strings.Contains(certsContent, "resp.data")

			// dashboard.js uses resp.data
			dashboardUsesRespData := strings.Contains(dashboardContent, "resp.data")

			// machines.js uses resp.data
			machinesUsesRespData := strings.Contains(machinesContent, "resp.data")

			return appReturnsJson && appChecksMessage &&
				certsUsesRespData && dashboardUsesRespData && machinesUsesRespData
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}
