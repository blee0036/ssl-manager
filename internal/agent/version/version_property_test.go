package version

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestPropertySemVerComparisonCorrectness verifies that Compare(a, b) returns
// the correct result by comparing major → minor → patch sequentially.
//
// Feature: agent-cli-auto-update, Property 1: SemVer comparison correctness
//
// **Validates: Requirements 6.3**
func TestPropertySemVerComparisonCorrectness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	versionGen := gen.Struct(
		reflect.TypeOf(Version{}),
		map[string]gopter.Gen{
			"Major": gen.IntRange(0, 1000),
			"Minor": gen.IntRange(0, 1000),
			"Patch": gen.IntRange(0, 1000),
		},
	)

	properties.Property("Compare returns correct result based on major → minor → patch", prop.ForAll(
		func(a, b Version) bool {
			result := Compare(a, b)

			// Compute expected result by comparing major → minor → patch
			var expected int
			if a.Major > b.Major {
				expected = 1
			} else if a.Major < b.Major {
				expected = -1
			} else if a.Minor > b.Minor {
				expected = 1
			} else if a.Minor < b.Minor {
				expected = -1
			} else if a.Patch > b.Patch {
				expected = 1
			} else if a.Patch < b.Patch {
				expected = -1
			} else {
				expected = 0
			}

			return result == expected
		},
		versionGen,
		versionGen,
	))

	properties.Property("Parse roundtrip: Parse(fmt.Sprintf) returns correct Version", prop.ForAll(
		func(major, minor, patch int) bool {
			s := fmt.Sprintf("%d.%d.%d", major, minor, patch)
			v, err := Parse(s)
			if err != nil {
				return false
			}
			return v.Major == major && v.Minor == minor && v.Patch == patch
		},
		gen.IntRange(0, 9999),
		gen.IntRange(0, 9999),
		gen.IntRange(0, 9999),
	))

	properties.TestingRun(t)
}
