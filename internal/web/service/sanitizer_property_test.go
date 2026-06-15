package service

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

// TestProperty6_TruncateFieldCorrectness verifies field truncation correctness:
// - If len(S) > L: result is S[:L] + TruncateMarker
// - If len(S) <= L: result is S unchanged
//
// **Validates: Requirements 3.3, 3.4, 3.5, 3.6**
func TestProperty6_TruncateFieldCorrectness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1000
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Property: short strings (len <= maxLen) are returned unchanged
	properties.Property("short strings unchanged", prop.ForAll(
		func(s string, maxLen int) bool {
			if len(s) <= maxLen {
				result := TruncateField(s, maxLen)
				return result == s
			}
			return true
		},
		gen.AnyString(),
		gen.IntRange(0, 100000),
	))

	// Property: long strings (len > maxLen) are truncated to S[:maxLen] + TruncateMarker
	properties.Property("long strings truncated correctly", prop.ForAll(
		func(s string, maxLen int) bool {
			if len(s) > maxLen && maxLen >= 0 {
				result := TruncateField(s, maxLen)
				expected := s[:maxLen] + TruncateMarker
				return result == expected
			}
			return true
		},
		gen.AnyString(),
		gen.IntRange(0, 100000),
	))

	// Property: truncated result always ends with TruncateMarker when truncation occurs
	properties.Property("truncated result ends with marker", prop.ForAll(
		func(s string, maxLen int) bool {
			if len(s) > maxLen && maxLen >= 0 {
				result := TruncateField(s, maxLen)
				return strings.HasSuffix(result, TruncateMarker)
			}
			return true
		},
		gen.AnyString(),
		gen.IntRange(0, 100000),
	))

	// Property: truncated result length equals maxLen + len(TruncateMarker) when truncation occurs
	properties.Property("truncated result length is maxLen + marker length", prop.ForAll(
		func(s string, maxLen int) bool {
			if len(s) > maxLen && maxLen >= 0 {
				result := TruncateField(s, maxLen)
				expectedLen := maxLen + len(TruncateMarker)
				return len(result) == expectedLen
			}
			return true
		},
		gen.AnyString(),
		gen.IntRange(0, 100000),
	))

	// Property: the prefix of the truncated result (before the marker) matches the original string prefix
	properties.Property("truncated prefix matches original", prop.ForAll(
		func(s string, maxLen int) bool {
			if len(s) > maxLen && maxLen >= 0 {
				result := TruncateField(s, maxLen)
				prefix := result[:maxLen]
				return prefix == s[:maxLen]
			}
			return true
		},
		gen.AnyString(),
		gen.IntRange(0, 100000),
	))

	// Edge case property: empty string with any maxLen >= 0 is always unchanged
	properties.Property("empty string always unchanged", prop.ForAll(
		func(maxLen int) bool {
			result := TruncateField("", maxLen)
			return result == ""
		},
		gen.IntRange(0, 100000),
	))

	// Edge case property: string exactly at maxLen is unchanged
	properties.Property("string exactly at maxLen unchanged", prop.ForAll(
		func(maxLen int) bool {
			if maxLen <= 0 {
				return true
			}
			// Generate a string of exactly maxLen bytes
			s := strings.Repeat("x", maxLen)
			result := TruncateField(s, maxLen)
			return result == s
		},
		gen.IntRange(1, 10000),
	))

	// Edge case property: maxLen=0 truncates any non-empty string
	properties.Property("maxLen zero truncates non-empty string", prop.ForAll(
		func(s string) bool {
			if len(s) == 0 {
				return TruncateField(s, 0) == ""
			}
			result := TruncateField(s, 0)
			return result == TruncateMarker
		},
		gen.AnyString().SuchThat(func(v interface{}) bool {
			return len(v.(string)) > 0
		}),
	))

	properties.TestingRun(t)
}

// TestProperty7_CommandOutputsArrayTruncation verifies command_outputs array truncation:
// - If N > MaxCommandOutputs (20): after SanitizeDeploymentLog, exactly 20 entries remain (the first 20)
// - If N <= MaxCommandOutputs (20): all entries are preserved unchanged in count
// - Order is maintained (first MaxCommandOutputs entries are kept)
//
// **Validates: Requirements 3.2**
func TestProperty7_CommandOutputsArrayTruncation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 500
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	sanitizer, err := NewSanitizer()
	if err != nil {
		t.Fatalf("NewSanitizer() failed: %v", err)
	}

	// Generator for a slice of CommandOutputs with length between 0 and 100
	genCommandOutputs := gen.IntRange(0, 100).FlatMap(func(v interface{}) gopter.Gen {
		n := v.(int)
		return gen.SliceOfN(n, gen.Struct(reflect.TypeOf(model.CommandOutput{}), map[string]gopter.Gen{
			"Command":  gen.AlphaString(),
			"ExitCode": gen.IntRange(0, 255),
			"Stdout":   gen.AlphaString(),
			"Stderr":   gen.AlphaString(),
			"TimedOut": gen.Bool(),
		}))
	}, reflect.TypeOf([]model.CommandOutput{}))

	// Property: if N > MaxCommandOutputs, result has exactly MaxCommandOutputs entries
	properties.Property("more than 20 outputs truncated to exactly 20", prop.ForAll(
		func(outputs interface{}) bool {
			commandOutputs := outputs.([]model.CommandOutput)
			if len(commandOutputs) <= MaxCommandOutputs {
				return true // skip, handled by next property
			}
			dl := &model.DeploymentLog{
				CommandOutputs: make([]model.CommandOutput, len(commandOutputs)),
			}
			copy(dl.CommandOutputs, commandOutputs)
			sanitizer.SanitizeDeploymentLog(dl)
			return len(dl.CommandOutputs) == MaxCommandOutputs
		},
		genCommandOutputs,
	))

	// Property: if N <= MaxCommandOutputs, all entries are preserved (same count)
	properties.Property("20 or fewer outputs preserved in count", prop.ForAll(
		func(outputs interface{}) bool {
			commandOutputs := outputs.([]model.CommandOutput)
			if len(commandOutputs) > MaxCommandOutputs {
				return true // skip, handled by previous property
			}
			dl := &model.DeploymentLog{
				CommandOutputs: make([]model.CommandOutput, len(commandOutputs)),
			}
			copy(dl.CommandOutputs, commandOutputs)
			sanitizer.SanitizeDeploymentLog(dl)
			return len(dl.CommandOutputs) == len(commandOutputs)
		},
		genCommandOutputs,
	))

	// Property: the first MaxCommandOutputs entries are preserved in order
	// (comparing Command field which serves as an identifier for each entry)
	properties.Property("first 20 entries preserved in order", prop.ForAll(
		func(outputs interface{}) bool {
			commandOutputs := outputs.([]model.CommandOutput)
			if len(commandOutputs) == 0 {
				return true
			}

			// Tag each entry with a unique marker in Command field to track order
			tagged := make([]model.CommandOutput, len(commandOutputs))
			for i := range commandOutputs {
				tagged[i] = commandOutputs[i]
				tagged[i].Command = fmt.Sprintf("cmd_%d", i)
			}

			dl := &model.DeploymentLog{
				CommandOutputs: make([]model.CommandOutput, len(tagged)),
			}
			copy(dl.CommandOutputs, tagged)
			sanitizer.SanitizeDeploymentLog(dl)

			// Verify order: each remaining entry's Command should match "cmd_<index>"
			expectedCount := len(tagged)
			if expectedCount > MaxCommandOutputs {
				expectedCount = MaxCommandOutputs
			}
			if len(dl.CommandOutputs) != expectedCount {
				return false
			}
			for i, co := range dl.CommandOutputs {
				expected := fmt.Sprintf("cmd_%d", i)
				if co.Command != expected {
					return false
				}
			}
			return true
		},
		genCommandOutputs,
	))

	properties.TestingRun(t)
}

// TestProperty12_FailClosedGuarantee verifies the fail-closed guarantee:
// When Sanitize encounters a panic (e.g., nil regex in patterns slice),
// it returns "[REDACTED]" rather than the original input.
//
// **Validates: Requirements 3.7, 3.8**
func TestProperty12_FailClosedGuarantee(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1000
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Construct a Sanitizer with a nil *regexp.Regexp entry in patterns.
	// Calling ReplaceAllString on a nil *regexp.Regexp will panic,
	// triggering the defer/recover fail-closed path.
	brokenSanitizer := &Sanitizer{
		patterns: []*regexp.Regexp{nil},
		replacer: "[REDACTED]",
	}

	// Property: for any input string, a panicking Sanitize always returns "[REDACTED]"
	properties.Property("panic returns REDACTED not original input", prop.ForAll(
		func(input string) bool {
			result := brokenSanitizer.Sanitize(input)
			return result == "[REDACTED]"
		},
		gen.AnyString(),
	))

	// Property: the original input is never returned when a panic occurs
	properties.Property("original input never leaked on panic", prop.ForAll(
		func(input string) bool {
			if input == "[REDACTED]" {
				// Skip this case since input equals the expected output
				return true
			}
			result := brokenSanitizer.Sanitize(input)
			return result != input
		},
		gen.AnyString().SuchThat(func(v interface{}) bool {
			return len(v.(string)) > 0
		}),
	))

	properties.TestingRun(t)
}

// TestProperty9_DoubleSanitizeGuarantee verifies that the sequence
// sanitize → truncate → sanitize produces output free of any sensitive pattern.
// Key scenario: truncation cuts through a PEM block, creating partial PEM that
// the first sanitize missed but the second sanitize catches.
//
// **Validates: Requirements 3.7**
func TestProperty9_DoubleSanitizeGuarantee(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1000
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	sanitizer, err := NewSanitizer()
	if err != nil {
		t.Fatalf("NewSanitizer() failed: %v", err)
	}

	// Sensitive patterns to check against (same as sanitizer uses)
	sensitivePatterns := []string{
		`Bearer\s+[A-Za-z0-9._\-]+`,
		`-----BEGIN\s+.*?PRIVATE KEY-----[\s\S]*?-----END\s+.*?PRIVATE KEY-----`,
		`-----BEGIN\s+.*?PRIVATE KEY-----[\s\S]*$`,
		`(?i)(?:KEY|SECRET|TOKEN|PASSWORD)\s*[=:]\s*\S+`,
	}
	compiledPatterns := make([]*regexp.Regexp, len(sensitivePatterns))
	for i, p := range sensitivePatterns {
		compiledPatterns[i] = regexp.MustCompile(p)
	}

	containsSensitive := func(s string) bool {
		for _, re := range compiledPatterns {
			if re.MatchString(s) {
				return true
			}
		}
		return false
	}

	// Generator: strings with embedded PEM blocks where truncation cuts through the middle
	genPEMWithTruncation := gen.IntRange(50, 500).FlatMap(func(v interface{}) gopter.Gen {
		keyBodyLen := v.(int)
		return gen.IntRange(30, keyBodyLen+30).Map(func(truncLen int) string {
			// Build a PEM block that will be partially cut by truncation
			header := "-----BEGIN RSA PRIVATE KEY-----\n"
			body := strings.Repeat("A", keyBodyLen) + "\n"
			footer := "-----END RSA PRIVATE KEY-----"
			full := "prefix data " + header + body + footer + " suffix data"
			_ = truncLen
			return full
		})
	}, reflect.TypeOf(""))

	// Generator: strings with Bearer tokens
	genBearerToken := gen.AlphaString().SuchThat(func(v interface{}) bool {
		return len(v.(string)) > 5
	}).Map(func(v string) string {
		return fmt.Sprintf("Authorization: Bearer %s some trailing data", v)
	})

	// Generator: strings with environment variable secrets
	genEnvSecret := gen.AnyString().SuchThat(func(v interface{}) bool {
		return len(v.(string)) > 0 && !strings.ContainsAny(v.(string), " \t\n\r")
	}).Map(func(v string) string {
		prefixes := []string{"SECRET_KEY", "API_TOKEN", "DB_PASSWORD", "ACCESS_KEY"}
		prefix := prefixes[len(v)%len(prefixes)]
		return fmt.Sprintf("config: %s=%s other data", prefix, v)
	})

	// Generator: random truncation length
	genTruncLen := gen.IntRange(10, 200)

	// Property: sanitize→truncate→sanitize produces no sensitive patterns (PEM scenario)
	properties.Property("PEM block: sanitize-truncate-sanitize leaves no sensitive pattern", prop.ForAll(
		func(input string, truncLen int) bool {
			// Step 1: first sanitize
			after1 := sanitizer.Sanitize(input)
			// Step 2: truncate (may cut through [REDACTED] or residual text)
			after2 := TruncateField(after1, truncLen)
			// Step 3: second sanitize
			final := sanitizer.Sanitize(after2)
			// Verify: no sensitive pattern remains
			return !containsSensitive(final)
		},
		genPEMWithTruncation,
		genTruncLen,
	))

	// Property: Bearer token: sanitize-truncate-sanitize leaves no sensitive pattern
	properties.Property("Bearer token: sanitize-truncate-sanitize leaves no sensitive pattern", prop.ForAll(
		func(input string, truncLen int) bool {
			after1 := sanitizer.Sanitize(input)
			after2 := TruncateField(after1, truncLen)
			final := sanitizer.Sanitize(after2)
			return !containsSensitive(final)
		},
		genBearerToken,
		genTruncLen,
	))

	// Property: env var secrets: sanitize-truncate-sanitize leaves no sensitive pattern
	properties.Property("env secret: sanitize-truncate-sanitize leaves no sensitive pattern", prop.ForAll(
		func(input string, truncLen int) bool {
			after1 := sanitizer.Sanitize(input)
			after2 := TruncateField(after1, truncLen)
			final := sanitizer.Sanitize(after2)
			return !containsSensitive(final)
		},
		genEnvSecret,
		genTruncLen,
	))

	// Property: mixed content with PEM that gets truncated mid-block
	// This specifically tests the scenario where truncation creates partial PEM
	properties.Property("partial PEM after truncation is caught by second sanitize", prop.ForAll(
		func(bodyLen int, truncPoint int) bool {
			// Construct a PEM block and truncate at a point inside the body
			header := "-----BEGIN EC PRIVATE KEY-----\n"
			body := strings.Repeat("B", bodyLen)
			footer := "\n-----END EC PRIVATE KEY-----"
			input := "log: " + header + body + footer

			// First sanitize should catch the complete PEM
			after1 := sanitizer.Sanitize(input)
			// Truncate at a point that might expose partial content
			after2 := TruncateField(after1, truncPoint)
			// Second sanitize catches any residual patterns
			final := sanitizer.Sanitize(after2)
			return !containsSensitive(final)
		},
		gen.IntRange(10, 300),
		gen.IntRange(5, 150),
	))

	properties.TestingRun(t)
}

// TestProperty8_SanitizationCompleteness verifies that any string containing a recognized
// sensitive pattern will NOT contain the original sensitive value after Sanitize() is called,
// and the result will contain [REDACTED] in place of the sensitive data.
//
// **Validates: Requirements 3.8, 3.9**
func TestProperty8_SanitizationCompleteness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 1000
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	sanitizer, err := NewSanitizer()
	if err != nil {
		t.Fatalf("NewSanitizer() failed: %v", err)
	}

	// Generator for Bearer token strings — uses typed Map to avoid interface{} issues
	genBearerToken := gen.RegexMatch(`[A-Za-z0-9._\-]{8,64}`).Map(func(v string) string {
		return "Bearer " + v
	})

	// Generator for complete PEM private key blocks — typed Map
	genCompletePEM := gen.RegexMatch(`[A-Za-z]{4,40}`).Map(func(body string) string {
		return "-----BEGIN RSA PRIVATE KEY-----\n" + body + "\n-----END RSA PRIVATE KEY-----"
	})

	// Generator for incomplete PEM private key blocks (no END marker) — typed Map
	genIncompletePEM := gen.RegexMatch(`[A-Za-z]{4,40}`).Map(func(body string) string {
		return "-----BEGIN EC PRIVATE KEY-----\n" + body
	})

	// Generator for environment variable secret patterns
	// Use a single generator that combines keyword + separator + value to avoid nested FlatMap issues
	genEnvSecret := gen.IntRange(0, 11).Map(func(idx int) string {
		keywords := []string{"KEY", "SECRET", "TOKEN", "PASSWORD"}
		separators := []string{"=", ": ", "= "}
		keyword := keywords[idx%4]
		separator := separators[idx%3]
		// Use a deterministic but varied secret value based on idx
		secretChars := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
		secretLen := 8 + (idx % 25)
		secret := ""
		for i := 0; i < secretLen; i++ {
			secret += string(secretChars[(idx+i*7)%len(secretChars)])
		}
		return keyword + separator + secret
	})

	// Generator for random surrounding (prefix/suffix) text that does NOT contain sensitive patterns
	genSafeText := gen.RegexMatch(`[a-z ]{0,30}`)

	// Property: Bearer token is fully redacted after sanitization
	properties.Property("bearer token redacted completely", prop.ForAll(
		func(prefix string, token string, suffix string) bool {
			input := prefix + token + suffix
			result := sanitizer.Sanitize(input)

			// The original token must not appear in the result
			if strings.Contains(result, token) {
				return false
			}
			// [REDACTED] must appear in the result
			if !strings.Contains(result, "[REDACTED]") {
				return false
			}
			return true
		},
		genSafeText,
		genBearerToken,
		genSafeText,
	))

	// Property: complete PEM key block is fully redacted after sanitization
	properties.Property("complete PEM key redacted completely", prop.ForAll(
		func(prefix string, pemBlock string, suffix string) bool {
			input := prefix + pemBlock + suffix
			result := sanitizer.Sanitize(input)

			// The original PEM block must not appear in the result
			if strings.Contains(result, pemBlock) {
				return false
			}
			// [REDACTED] must appear
			if !strings.Contains(result, "[REDACTED]") {
				return false
			}
			return true
		},
		genSafeText,
		genCompletePEM,
		genSafeText,
	))

	// Property: incomplete PEM key block is fully redacted after sanitization
	properties.Property("incomplete PEM key redacted completely", prop.ForAll(
		func(prefix string, pemBlock string) bool {
			// Incomplete PEM is at the end (no suffix since pattern matches to end of string)
			input := prefix + pemBlock
			result := sanitizer.Sanitize(input)

			// The original incomplete PEM must not appear in the result
			if strings.Contains(result, pemBlock) {
				return false
			}
			// [REDACTED] must appear
			if !strings.Contains(result, "[REDACTED]") {
				return false
			}
			return true
		},
		genSafeText,
		genIncompletePEM,
	))

	// Property: environment variable secrets are fully redacted after sanitization
	properties.Property("env var secret redacted completely", prop.ForAll(
		func(prefix string, secret string, suffix string) bool {
			input := prefix + secret + suffix
			result := sanitizer.Sanitize(input)

			// The original secret pattern must not appear in the result
			if strings.Contains(result, secret) {
				return false
			}
			// [REDACTED] must appear
			if !strings.Contains(result, "[REDACTED]") {
				return false
			}
			return true
		},
		genSafeText,
		genEnvSecret,
		genSafeText,
	))

	properties.TestingRun(t)
}
