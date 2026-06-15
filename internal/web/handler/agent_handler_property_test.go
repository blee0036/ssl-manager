package handler

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// sensitivePatterns are the regexes used to detect sensitive data in output.
// Mirrors the patterns in service.NewSanitizer().
var sensitiveCheckPatterns = []*regexp.Regexp{
	regexp.MustCompile(`Bearer\s+[A-Za-z0-9._\-]+`),
	regexp.MustCompile(`-----BEGIN\s+.*?PRIVATE KEY-----[\s\S]*?-----END\s+.*?PRIVATE KEY-----`),
	regexp.MustCompile(`-----BEGIN\s+.*?PRIVATE KEY-----[\s\S]*$`),
	regexp.MustCompile(`(?i)(?:KEY|SECRET|TOKEN|PASSWORD)\s*[=:]\s*\S+`),
}

// containsSensitivePattern checks if a string matches any known sensitive pattern.
func containsSensitivePattern(s string) bool {
	for _, re := range sensitiveCheckPatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// --- Generators for error messages containing sensitive data ---

// genErrorWithBearer generates error messages containing Bearer tokens.
func genErrorWithBearer() gopter.Gen {
	return gen.RegexMatch(`[A-Za-z0-9._\-]{10,80}`).Map(func(token string) string {
		return fmt.Sprintf("deployment failed: Authorization: Bearer %s connection refused", token)
	})
}

// genErrorWithPEM generates error messages containing PEM private key blocks.
func genErrorWithPEM() gopter.Gen {
	return gen.IntRange(50, 500).Map(func(bodyLen int) string {
		body := strings.Repeat("A", bodyLen)
		return fmt.Sprintf("error loading key: -----BEGIN RSA PRIVATE KEY-----\n%s\n-----END RSA PRIVATE KEY-----\nfailed to deploy", body)
	})
}

// genErrorWithIncompletePEM generates error messages with incomplete PEM blocks (no END marker).
func genErrorWithIncompletePEM() gopter.Gen {
	return gen.IntRange(50, 300).Map(func(bodyLen int) string {
		body := strings.Repeat("B", bodyLen)
		return fmt.Sprintf("key parse error: -----BEGIN EC PRIVATE KEY-----\n%s", body)
	})
}

// genErrorWithEnvSecret generates error messages with env-style secrets.
func genErrorWithEnvSecret() gopter.Gen {
	return gen.IntRange(0, 11).Map(func(idx int) string {
		keywords := []string{"SECRET_KEY", "API_TOKEN", "DB_PASSWORD", "ACCESS_KEY"}
		keyword := keywords[idx%4]
		secretChars := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
		secretLen := 12 + (idx % 20)
		secret := ""
		for i := 0; i < secretLen; i++ {
			secret += string(secretChars[(idx+i*7)%len(secretChars)])
		}
		return fmt.Sprintf("config load failed: %s=%s unable to connect", keyword, secret)
	})
}

// genLongErrorWithSensitiveData generates long error messages (>8KB) containing sensitive data.
func genLongErrorWithSensitiveData() gopter.Gen {
	return gen.IntRange(9000, 20000).Map(func(totalLen int) string {
		// Embed a Bearer token in a long string
		token := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.payload.signature"
		padding := strings.Repeat("x", totalLen-len(token)-50)
		return fmt.Sprintf("error: Bearer %s %s end", token, padding)
	})
}

// TestProperty11_AlertAndLastDeployMessageUseSanitizedValues verifies that:
// 1. For any error_message containing sensitive data: after sanitize + truncate to 8KB,
//    the result contains no sensitive patterns
// 2. For any error_message: TruncateField(sanitized, 256) produces output ≤ 256 + len(TruncateMarker) chars
// 3. Alert summary (max 256 chars) after sanitize+truncate does not contain any raw sensitive data
//
// This tests the same service-layer logic that the handler's CreateDeploymentLog uses:
// - deployLogService.Create() calls sanitizer.SanitizeDeploymentLog() which sanitizes+truncates error_message
// - Alert summary uses: service.TruncateField(log.ErrorMessage, service.MaxAlertSummaryLen)
// - last_deploy_message uses: log.ErrorMessage (already sanitized+truncated by the service)
//
// **Validates: Requirements 3.10, 3.11**
func TestProperty11_AlertAndLastDeployMessageUseSanitizedValues(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 500
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	sanitizer, err := service.NewSanitizer()
	if err != nil {
		t.Fatalf("NewSanitizer() failed: %v", err)
	}

	// Property 1: After sanitize + truncate to MaxErrorMessageLen (8KB), no sensitive patterns remain
	properties.Property("Bearer token: sanitized error_message contains no sensitive data", prop.ForAll(
		func(errorMsg string) bool {
			sanitized := sanitizer.Sanitize(errorMsg)
			truncated := service.TruncateField(sanitized, service.MaxErrorMessageLen)
			// Second sanitize (as done in SanitizeDeploymentLog)
			final := sanitizer.Sanitize(truncated)
			return !containsSensitivePattern(final)
		},
		genErrorWithBearer(),
	))

	properties.Property("PEM key: sanitized error_message contains no sensitive data", prop.ForAll(
		func(errorMsg string) bool {
			sanitized := sanitizer.Sanitize(errorMsg)
			truncated := service.TruncateField(sanitized, service.MaxErrorMessageLen)
			final := sanitizer.Sanitize(truncated)
			return !containsSensitivePattern(final)
		},
		genErrorWithPEM(),
	))

	properties.Property("incomplete PEM: sanitized error_message contains no sensitive data", prop.ForAll(
		func(errorMsg string) bool {
			sanitized := sanitizer.Sanitize(errorMsg)
			truncated := service.TruncateField(sanitized, service.MaxErrorMessageLen)
			final := sanitizer.Sanitize(truncated)
			return !containsSensitivePattern(final)
		},
		genErrorWithIncompletePEM(),
	))

	properties.Property("env secret: sanitized error_message contains no sensitive data", prop.ForAll(
		func(errorMsg string) bool {
			sanitized := sanitizer.Sanitize(errorMsg)
			truncated := service.TruncateField(sanitized, service.MaxErrorMessageLen)
			final := sanitizer.Sanitize(truncated)
			return !containsSensitivePattern(final)
		},
		genErrorWithEnvSecret(),
	))

	// Property 2: Alert summary (TruncateField to 256) always produces output ≤ 256 + len(TruncateMarker)
	properties.Property("alert summary length ≤ MaxAlertSummaryLen + marker length", prop.ForAll(
		func(errorMsg string) bool {
			// Simulate the handler flow: sanitize → truncate to 8KB → second sanitize → truncate to 256 for alert
			sanitized := sanitizer.Sanitize(errorMsg)
			truncatedMsg := service.TruncateField(sanitized, service.MaxErrorMessageLen)
			finalMsg := sanitizer.Sanitize(truncatedMsg)
			alertSummary := service.TruncateField(finalMsg, service.MaxAlertSummaryLen)

			maxAllowed := service.MaxAlertSummaryLen + len(service.TruncateMarker)
			return len(alertSummary) <= maxAllowed
		},
		genLongErrorWithSensitiveData(),
	))

	// Also test with short messages
	properties.Property("short messages: alert summary length ≤ MaxAlertSummaryLen + marker length", prop.ForAll(
		func(errorMsg string) bool {
			sanitized := sanitizer.Sanitize(errorMsg)
			truncatedMsg := service.TruncateField(sanitized, service.MaxErrorMessageLen)
			finalMsg := sanitizer.Sanitize(truncatedMsg)
			alertSummary := service.TruncateField(finalMsg, service.MaxAlertSummaryLen)

			maxAllowed := service.MaxAlertSummaryLen + len(service.TruncateMarker)
			return len(alertSummary) <= maxAllowed
		},
		genErrorWithBearer(),
	))

	// Property 3: Alert summary after sanitize+truncate contains no sensitive data
	properties.Property("alert summary with Bearer token contains no sensitive data", prop.ForAll(
		func(errorMsg string) bool {
			sanitized := sanitizer.Sanitize(errorMsg)
			truncatedMsg := service.TruncateField(sanitized, service.MaxErrorMessageLen)
			finalMsg := sanitizer.Sanitize(truncatedMsg)
			alertSummary := service.TruncateField(finalMsg, service.MaxAlertSummaryLen)
			return !containsSensitivePattern(alertSummary)
		},
		genErrorWithBearer(),
	))

	properties.Property("alert summary with PEM key contains no sensitive data", prop.ForAll(
		func(errorMsg string) bool {
			sanitized := sanitizer.Sanitize(errorMsg)
			truncatedMsg := service.TruncateField(sanitized, service.MaxErrorMessageLen)
			finalMsg := sanitizer.Sanitize(truncatedMsg)
			alertSummary := service.TruncateField(finalMsg, service.MaxAlertSummaryLen)
			return !containsSensitivePattern(alertSummary)
		},
		genErrorWithPEM(),
	))

	properties.Property("alert summary with env secret contains no sensitive data", prop.ForAll(
		func(errorMsg string) bool {
			sanitized := sanitizer.Sanitize(errorMsg)
			truncatedMsg := service.TruncateField(sanitized, service.MaxErrorMessageLen)
			finalMsg := sanitizer.Sanitize(truncatedMsg)
			alertSummary := service.TruncateField(finalMsg, service.MaxAlertSummaryLen)
			return !containsSensitivePattern(alertSummary)
		},
		genErrorWithEnvSecret(),
	))

	// Property 3 continued: Long error messages with sensitive data also produce clean alert summaries
	properties.Property("long error with sensitive data: alert summary is clean", prop.ForAll(
		func(errorMsg string) bool {
			sanitized := sanitizer.Sanitize(errorMsg)
			truncatedMsg := service.TruncateField(sanitized, service.MaxErrorMessageLen)
			finalMsg := sanitizer.Sanitize(truncatedMsg)
			alertSummary := service.TruncateField(finalMsg, service.MaxAlertSummaryLen)
			return !containsSensitivePattern(alertSummary)
		},
		genLongErrorWithSensitiveData(),
	))

	properties.TestingRun(t)
}
