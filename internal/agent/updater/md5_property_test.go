package updater

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestPropertyMD5VerificationCorrectness verifies that VerifyMD5 returns nil
// when the file content matches the expected MD5, and returns an error when
// the MD5 does not match.
//
// Feature: agent-cli-auto-update, Property 2: MD5 verification correctness
//
// **Validates: Requirements 1.2**
func TestPropertyMD5VerificationCorrectness(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Generate random byte slices between 1 and 10000 bytes
	byteSliceGen := gen.SliceOfN(10000, gen.UInt8()).
		SuchThat(func(v interface{}) bool {
			bs, ok := v.([]uint8)
			return ok && len(bs) >= 1
		}).
		Map(func(v interface{}) interface{} {
			return v
		})

	properties.Property("correct MD5 passes verification (VerifyMD5 returns nil)", prop.ForAll(
		func(data []uint8) bool {
			// Write data to a temp file
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "testfile")
			if err := os.WriteFile(tmpFile, data, 0644); err != nil {
				t.Logf("failed to write temp file: %v", err)
				return false
			}

			// Compute correct MD5
			hash := md5.Sum(data)
			correctMD5 := hex.EncodeToString(hash[:])

			// VerifyMD5 with correct MD5 should return nil
			err := VerifyMD5(tmpFile, correctMD5)
			return err == nil
		},
		byteSliceGen,
	))

	properties.Property("incorrect MD5 is rejected (VerifyMD5 returns error)", prop.ForAll(
		func(data []uint8) bool {
			// Write data to a temp file
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "testfile")
			if err := os.WriteFile(tmpFile, data, 0644); err != nil {
				t.Logf("failed to write temp file: %v", err)
				return false
			}

			// Compute correct MD5 then flip a character to make it wrong
			hash := md5.Sum(data)
			correctMD5 := hex.EncodeToString(hash[:])

			// Create a wrong MD5 by flipping the first hex character
			wrongMD5Bytes := []byte(correctMD5)
			if wrongMD5Bytes[0] == 'a' {
				wrongMD5Bytes[0] = 'b'
			} else {
				wrongMD5Bytes[0] = 'a'
			}
			wrongMD5 := string(wrongMD5Bytes)

			// VerifyMD5 with wrong MD5 should return an error
			err := VerifyMD5(tmpFile, wrongMD5)
			return err != nil
		},
		byteSliceGen,
	))

	properties.TestingRun(t)
}
