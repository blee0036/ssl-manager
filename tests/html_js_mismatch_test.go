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

// **Validates: Requirements 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9, 1.10, 1.13, 1.14**
//
// Bug Condition Exploration Tests
// These tests verify that the HTML templates and JS files are CONSISTENT.
// They are EXPECTED TO FAIL on the current (unfixed) code, confirming the bugs exist.
// After the fix is applied, these tests will PASS.

const htmlDir = "../web/templates/"
const jsStaticDir = "../web/static/js/"

func readHTMLFile(t *testing.T, filename string) string {
	t.Helper()
	content, err := os.ReadFile(htmlDir + filename)
	if err != nil {
		t.Fatalf("failed to read %s: %v", filename, err)
	}
	return string(content)
}

func readJSStaticFile(t *testing.T, filename string) string {
	t.Helper()
	content, err := os.ReadFile(jsStaticDir + filename)
	if err != nil {
		t.Fatalf("failed to read %s: %v", filename, err)
	}
	return string(content)
}

// extractGetElementByIdCalls extracts all ID strings from getElementById('...') or getElementById("...") calls
func extractGetElementByIdCalls(jsContent string) []string {
	re := regexp.MustCompile(`getElementById\(['"]([^'"]+)['"]\)`)
	matches := re.FindAllStringSubmatch(jsContent, -1)
	seen := make(map[string]bool)
	var ids []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			ids = append(ids, m[1])
		}
	}
	return ids
}

// htmlContainsId checks if the HTML content contains an element with the given id attribute
func htmlContainsId(htmlContent, id string) bool {
	// Match id="..." or id='...'
	pattern := `id=["']` + regexp.QuoteMeta(id) + `["']`
	re := regexp.MustCompile(pattern)
	return re.MatchString(htmlContent)
}

// --- Bug 1: init.html IDs don't match init.js expectations ---

func TestBugCondition_HTMLJSMismatch_InitPage(t *testing.T) {
	// Property: For every getElementById call in init.js, the corresponding id MUST exist in init.html
	// Bug: init.html has different IDs (step-admin, admin-form, username, password, step-config, config-form)
	//      and is missing cfg-* fields entirely
	initHTML := readHTMLFile(t, "init.html")
	initJS := readJSStaticFile(t, "init.js")

	jsIds := extractGetElementByIdCalls(initJS)

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = len(jsIds)

	properties := gopter.NewProperties(parameters)

	properties.Property("init.html contains all IDs referenced by init.js", prop.ForAll(
		func(idx int) bool {
			if idx >= len(jsIds) {
				return true
			}
			id := jsIds[idx]
			return htmlContainsId(initHTML, id)
		},
		gen.IntRange(0, len(jsIds)-1),
	))

	properties.TestingRun(t)
}

// --- Bug 1: login.html IDs don't match login.js expectations ---

func TestBugCondition_HTMLJSMismatch_LoginPage(t *testing.T) {
	// Property: For every getElementById call in login.js, the corresponding id MUST exist in login.html
	// Bug: login.html has login-username/login-password instead of username/password,
	//      readonly-form instead of readonly-login-form, readonly-pwd instead of readonly-password
	loginHTML := readHTMLFile(t, "login.html")
	loginJS := readJSStaticFile(t, "login.js")

	jsIds := extractGetElementByIdCalls(loginJS)

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = len(jsIds)

	properties := gopter.NewProperties(parameters)

	properties.Property("login.html contains all IDs referenced by login.js", prop.ForAll(
		func(idx int) bool {
			if idx >= len(jsIds) {
				return true
			}
			id := jsIds[idx]
			return htmlContainsId(loginHTML, id)
		},
		gen.IntRange(0, len(jsIds)-1),
	))

	properties.TestingRun(t)
}

// --- Bug 2: certificates.html IDs don't match certificates.js expectations ---

func TestBugCondition_HTMLJSMismatch_CertificatesPage(t *testing.T) {
	// Property: For every getElementById call in certificates.js, the corresponding id MUST exist in certificates.html
	// Bug: certificates.html has certs-body instead of certificates-tbody,
	//      and is missing upload-cert-form, issue-cloudflare-form, manual-dns-form
	certsHTML := readHTMLFile(t, "certificates.html")
	certsJS := readJSStaticFile(t, "certificates.js")

	// Extract only the IDs that should be in the initial page load (not dynamically created)
	// These are the IDs used in setupCertificateEvents() and renderCertificateList()
	expectedIds := []string{
		"certificates-tbody",
		"upload-cert-form",
		"issue-cloudflare-form",
		"manual-dns-form",
	}

	// Verify these IDs are actually referenced in the JS
	for _, id := range expectedIds {
		if !strings.Contains(certsJS, `"`+id+`"`) && !strings.Contains(certsJS, `'`+id+`'`) {
			t.Fatalf("expected ID %q not found in certificates.js", id)
		}
	}

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = len(expectedIds)

	properties := gopter.NewProperties(parameters)

	properties.Property("certificates.html contains all IDs referenced by certificates.js for page setup", prop.ForAll(
		func(idx int) bool {
			if idx >= len(expectedIds) {
				return true
			}
			id := expectedIds[idx]
			return htmlContainsId(certsHTML, id)
		},
		gen.IntRange(0, len(expectedIds)-1),
	))

	properties.TestingRun(t)
}

// --- Bug 2: machines.html IDs don't match machines.js expectations ---

func TestBugCondition_HTMLJSMismatch_MachinesPage(t *testing.T) {
	// Property: For every getElementById call in machines.js for page setup, the corresponding id MUST exist in machines.html
	// Bug: machines.html has machines-body instead of machines-tbody,
	//      and is missing create-machine-form, machine-filter-form
	machinesHTML := readHTMLFile(t, "machines.html")
	machinesJS := readJSStaticFile(t, "machines.js")

	// IDs used in setupMachineEvents() and renderMachineList()
	expectedIds := []string{
		"machines-tbody",
		"create-machine-form",
		"machine-filter-form",
	}

	// Verify these IDs are actually referenced in the JS
	for _, id := range expectedIds {
		if !strings.Contains(machinesJS, `"`+id+`"`) && !strings.Contains(machinesJS, `'`+id+`'`) {
			t.Fatalf("expected ID %q not found in machines.js", id)
		}
	}

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = len(expectedIds)

	properties := gopter.NewProperties(parameters)

	properties.Property("machines.html contains all IDs referenced by machines.js for page setup", prop.ForAll(
		func(idx int) bool {
			if idx >= len(expectedIds) {
				return true
			}
			id := expectedIds[idx]
			return htmlContainsId(machinesHTML, id)
		},
		gen.IntRange(0, len(expectedIds)-1),
	))

	properties.TestingRun(t)
}

// --- Bug 4: certificates.js reads wrong DNS challenge field names ---

func TestBugCondition_HTMLJSMismatch_DNSFieldNames(t *testing.T) {
	// Property: certificates.js MUST read txt_record_name and txt_record_value from DNS challenge responses
	// Bug: current code reads ch.record_name and ch.record_value (missing "txt_" prefix)
	certsJS := readJSStaticFile(t, "certificates.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("certificates.js reads txt_record_name and txt_record_value (not record_name/record_value)", prop.ForAll(
		func(_ int) bool {
			// Should read txt_record_name and txt_record_value
			hasTxtRecordName := strings.Contains(certsJS, "txt_record_name")
			hasTxtRecordValue := strings.Contains(certsJS, "txt_record_value")

			// Should NOT read just record_name or record_value (without txt_ prefix)
			// We check for the pattern ch.record_name or ch.record_value which is the bug
			hasWrongRecordName := strings.Contains(certsJS, "ch.record_name")
			hasWrongRecordValue := strings.Contains(certsJS, "ch.record_value")

			return hasTxtRecordName && hasTxtRecordValue && !hasWrongRecordName && !hasWrongRecordValue
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- Bug 5: thirdpart-dns.js has required on main_domains and rejects empty submission ---

func TestBugCondition_HTMLJSMismatch_MainDomainsRequired(t *testing.T) {
	// Property: thirdpart-dns.js MUST NOT have 'required' on main_domains input
	//           and MUST allow empty main_domains submission
	// Bug: current code has required attribute on dns-main-domains input
	//      and validates !mainDomainsStr before submitting
	dnsJS := readJSStaticFile(t, "thirdpart-dns.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("thirdpart-dns.js does NOT have required on main_domains and allows empty submission", prop.ForAll(
		func(_ int) bool {
			// The input for dns-main-domains should NOT have 'required' attribute
			// Look for the pattern: id="dns-main-domains" ... required
			mainDomainsInputPattern := regexp.MustCompile(`id="dns-main-domains"[^>]*required`)
			hasRequired := mainDomainsInputPattern.MatchString(dnsJS)

			// The validation should NOT reject empty mainDomainsStr
			// Bug pattern: if (!name || !mainDomainsStr)
			hasEmptyValidation := strings.Contains(dnsJS, "!mainDomainsStr")

			return !hasRequired && !hasEmptyValidation
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}

// --- Bug 3: machines.js does not assemble complete install command ---

func TestBugCondition_HTMLJSMismatch_InstallCommand(t *testing.T) {
	// Property: machines.js createMachine() MUST assemble a complete install command
	//           containing server URL, machine ID, and token (not just show raw token)
	// Bug: current code only shows the raw agent_token without assembling a command
	machinesJS := readJSStaticFile(t, "machines.js")

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1

	properties := gopter.NewProperties(parameters)

	properties.Property("machines.js createMachine displays complete install command with URL + machine ID + token", prop.ForAll(
		func(_ int) bool {
			// After successful machine creation, the code should assemble a complete command
			// that includes the server URL (window.location.origin or external_url),
			// the machine ID, and the token.
			//
			// The current buggy code just shows the raw token:
			//   ${App.escapeHtml(data.agent_token)}
			//
			// The fixed code should include something like:
			//   curl ... <server_url>/api/machines/<id>/install ... or
			//   a command that combines server URL + machine ID + token

			// Check that createMachine function references machine ID in the display
			// (data.machine.id or data.id or similar) along with the token
			hasMachineIdInDisplay := strings.Contains(machinesJS, "data.machine.id") ||
				strings.Contains(machinesJS, "data.id") ||
				strings.Contains(machinesJS, "machine_id")

			// Check that it references a server URL (window.location.origin or external_url)
			hasServerUrl := strings.Contains(machinesJS, "window.location.origin") ||
				strings.Contains(machinesJS, "external_url") ||
				strings.Contains(machinesJS, "location.origin")

			// The display should combine these into an install command, not just show raw token
			hasInstallCommandAssembly := hasMachineIdInDisplay && hasServerUrl

			return hasInstallCommandAssembly
		},
		gen.Const(1),
	))

	properties.TestingRun(t)
}
