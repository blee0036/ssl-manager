package cli

import (
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestPropertyTokenMaskingCorrectness verifies that MaskToken correctly masks
// tokens: for tokens with length >= 8, the last 8 chars match the original,
// preceding chars are all '*', and total length equals original. For tokens
// with length < 8, all chars are '*' and length equals original.
//
// Feature: agent-cli-auto-update, Property 4: Token masking correctness
//
// **Validates: Requirements 8.1**
func TestPropertyTokenMaskingCorrectness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Generator for random byte slices of length 1-200, used as token bytes
	tokenBytesGen := gen.SliceOfN(200, gen.UInt8()).
		SuchThat(func(v interface{}) bool {
			bs, ok := v.([]uint8)
			return ok && len(bs) >= 1
		})

	properties.Property("tokens >= 8 chars: last 8 match original, preceding are '*', length preserved", prop.ForAll(
		func(data []uint8) bool {
			// Use the byte slice as a token string
			token := string(data)
			if len(token) < 8 {
				return true // skip, handled by other property
			}

			masked := MaskToken(token)

			// Total length must equal original
			if len(masked) != len(token) {
				return false
			}

			// Last 8 characters must match original
			if masked[len(masked)-8:] != token[len(token)-8:] {
				return false
			}

			// All preceding characters must be '*'
			for i := 0; i < len(masked)-8; i++ {
				if masked[i] != '*' {
					return false
				}
			}

			return true
		},
		tokenBytesGen,
	))

	properties.Property("tokens < 8 chars: all chars are '*' and length preserved", prop.ForAll(
		func(data []uint8) bool {
			// Use the byte slice as a token string
			token := string(data)
			if len(token) >= 8 {
				return true // skip, handled by other property
			}

			masked := MaskToken(token)

			// Total length must equal original
			if len(masked) != len(token) {
				return false
			}

			// All characters must be '*'
			for i := 0; i < len(masked); i++ {
				if masked[i] != '*' {
					return false
				}
			}

			return true
		},
		tokenBytesGen,
	))

	properties.TestingRun(t)
}
