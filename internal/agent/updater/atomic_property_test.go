package updater

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// TestPropertyAtomicFileReplaceIntegrity verifies that after a successful
// AtomicReplace, the target file content equals the new content, and if
// AtomicReplace fails, the original file content is unchanged.
//
// Feature: agent-cli-auto-update, Property 3: Atomic file replace integrity
//
// **Validates: Requirements 1.2**
func TestPropertyAtomicFileReplaceIntegrity(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Generator for non-empty byte slices (1 to 4096 bytes)
	contentGen := gen.SliceOfN(4096, gen.UInt8()).
		SuchThat(func(v interface{}) bool {
			bs, ok := v.([]uint8)
			return ok && len(bs) >= 1
		})

	properties.Property("successful AtomicReplace results in target containing new content", prop.ForAll(
		func(originalContent, newContent []uint8) bool {
			tmpDir := t.TempDir()

			// Write original content to target file
			targetPath := filepath.Join(tmpDir, "target-binary")
			if err := os.WriteFile(targetPath, originalContent, 0755); err != nil {
				t.Logf("failed to write target file: %v", err)
				return false
			}

			// Write new content to a new file in the same directory
			newFilePath := filepath.Join(tmpDir, "new-binary.tmp")
			if err := os.WriteFile(newFilePath, newContent, 0644); err != nil {
				t.Logf("failed to write new file: %v", err)
				return false
			}

			// Call AtomicReplace
			err := AtomicReplace(targetPath, newFilePath)
			if err != nil {
				t.Logf("AtomicReplace failed unexpectedly: %v", err)
				return false
			}

			// Verify target file now contains new content
			resultContent, err := os.ReadFile(targetPath)
			if err != nil {
				t.Logf("failed to read target file after replace: %v", err)
				return false
			}

			return bytes.Equal(resultContent, newContent)
		},
		contentGen,
		contentGen,
	))

	properties.Property("failed AtomicReplace preserves original file content", prop.ForAll(
		func(originalContent []uint8) bool {
			tmpDir := t.TempDir()

			// Write original content to target file
			targetPath := filepath.Join(tmpDir, "target-binary")
			if err := os.WriteFile(targetPath, originalContent, 0755); err != nil {
				t.Logf("failed to write target file: %v", err)
				return false
			}

			// Use a non-existent new file path to force AtomicReplace to fail
			nonExistentPath := filepath.Join(tmpDir, "nonexistent", "subdir", "file.tmp")

			// Call AtomicReplace - this should fail because the new file doesn't exist
			err := AtomicReplace(targetPath, nonExistentPath)
			if err == nil {
				t.Logf("AtomicReplace should have failed with non-existent new file")
				return false
			}

			// Verify original file content is unchanged
			resultContent, err := os.ReadFile(targetPath)
			if err != nil {
				t.Logf("failed to read target file after failed replace: %v", err)
				return false
			}

			return bytes.Equal(resultContent, originalContent)
		},
		contentGen,
	))

	properties.TestingRun(t)
}
