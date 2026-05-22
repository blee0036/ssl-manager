package cli

import (
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestPropertyURLValidationCorrectness verifies that ValidateURL returns true
// if and only if the string starts with "http://" or "https://".
// All other strings (empty, no protocol, ftp://, etc.) should be rejected.
//
// Feature: agent-cli-auto-update, Property 5: URL validation correctness
//
// **Validates: Requirements 8.5**
func TestPropertyURLValidationCorrectness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Property: For arbitrary strings, ValidateURL returns true iff the string
	// starts with "http://" or "https://"
	properties.Property("random strings: ValidateURL matches manual prefix check", prop.ForAll(
		func(s string) bool {
			expected := strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
			actual := ValidateURL(s)
			return actual == expected
		},
		gen.AnyString(),
	))

	// Property: Strings explicitly prefixed with "http://" should always be valid
	properties.Property("http:// prefixed strings are always valid", prop.ForAll(
		func(suffix string) bool {
			url := "http://" + suffix
			return ValidateURL(url) == true
		},
		gen.AnyString(),
	))

	// Property: Strings explicitly prefixed with "https://" should always be valid
	properties.Property("https:// prefixed strings are always valid", prop.ForAll(
		func(suffix string) bool {
			url := "https://" + suffix
			return ValidateURL(url) == true
		},
		gen.AnyString(),
	))

	// Property: Strings with other common protocols should be rejected
	properties.Property("ftp:// and other protocol prefixed strings are rejected", prop.ForAll(
		func(suffix string) bool {
			ftpURL := "ftp://" + suffix
			fileURL := "file://" + suffix
			sshURL := "ssh://" + suffix
			return ValidateURL(ftpURL) == false &&
				ValidateURL(fileURL) == false &&
				ValidateURL(sshURL) == false
		},
		gen.AnyString(),
	))

	// Property: Empty string is always rejected
	properties.Property("empty string is rejected", prop.ForAll(
		func(_ int) bool {
			return ValidateURL("") == false
		},
		gen.Int(),
	))

	properties.TestingRun(t)
}
