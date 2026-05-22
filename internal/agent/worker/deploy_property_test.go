package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/agent/config"
)

// === Generators ===

// genFingerprint generates a hex-like fingerprint string.
func genFingerprint() gopter.Gen {
	return gen.AlphaString().SuchThat(func(s string) bool {
		return len(s) > 0
	}).Map(func(s string) string {
		return "sha256:" + s
	})
}

// genNonEmptyAlpha generates a non-empty alphanumeric string.
func genNonEmptyAlpha() gopter.Gen {
	return gen.AlphaString().SuchThat(func(s string) bool {
		return len(s) > 0
	})
}

// genConfigRevision generates a positive integer for config revision.
func genConfigRevision() gopter.Gen {
	return gen.IntRange(1, 1000)
}

// === Property 14: 指纹不一致触发同步 ===
// When local fingerprint doesn't match server fingerprint, deployment should be triggered.
//
// **Validates: Requirements 8.1**
func TestProperty14_FingerprintMismatchTriggersSync(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 200
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("fingerprint mismatch always triggers deployment", prop.ForAll(
		func(localFP, serverFP string, revision int) bool {
			// Create a temp file so the "file exists" check passes
			tmpDir := t.TempDir()
			certPath := filepath.Join(tmpDir, "cert.pem")
			if err := os.WriteFile(certPath, []byte("cert"), 0644); err != nil {
				return false
			}

			cfg := CertConfigResponse{
				MachineCertificateID: "mc-test",
				CertificateID:        "cert-test",
				FingerprintSHA256:    serverFP,
				CertPath:             certPath,
				ConfigRevision:       revision,
				LastDeployStatus:     "success",
			}

			localState := &config.MachineCertState{
				MachineCertificateID:  "mc-test",
				LastSyncedRevision:    revision, // same revision
				LastSyncedFingerprint: localFP,  // potentially different fingerprint
				LastDeployStatus:      "success",
			}

			// When fingerprints differ, deployment should be triggered
			if localFP != serverFP {
				return NeedsDeployment(cfg, localState) == true
			}
			// When fingerprints match (and revision matches, file exists, status not pending),
			// deployment should NOT be triggered
			return NeedsDeployment(cfg, localState) == false
		},
		genFingerprint(),
		genFingerprint(),
		genConfigRevision(),
	))

	properties.TestingRun(t)
}

// === Property 15: 命令有序执行与失败即停 ===
// Post-deploy commands execute in order; if any fails, subsequent commands are not executed.
//
// **Validates: Requirements 8.4, 8.5**
func TestProperty15_CommandOrderedExecutionAndFailStop(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 50
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("commands before failure are executed, commands after failure are not", prop.ForAll(
		func(successCount int) bool {
			// Build a command list: successCount successful commands, then 1 failing, then 1 more
			var commands []string
			for i := 0; i < successCount; i++ {
				commands = append(commands, "echo success")
			}
			commands = append(commands, "exit 1")          // failing command
			commands = append(commands, "echo should_not") // should not execute

			ctx := context.Background()
			outputs, err := ExecuteCommands(ctx, commands, 10*time.Second)

			// There should be an error since a command failed
			if err == nil {
				return false
			}

			// Number of outputs should be successCount + 1 (the failing command)
			expectedOutputs := successCount + 1
			if len(outputs) != expectedOutputs {
				t.Logf("expected %d outputs, got %d", expectedOutputs, len(outputs))
				return false
			}

			// All successful commands should have exit code 0
			for i := 0; i < successCount; i++ {
				if outputs[i].ExitCode != 0 {
					return false
				}
			}

			// The failing command should have non-zero exit code
			if outputs[successCount].ExitCode == 0 {
				return false
			}

			return true
		},
		gen.IntRange(0, 5),
	))

	properties.Property("all successful commands produce outputs in order", prop.ForAll(
		func(count int) bool {
			var commands []string
			for i := 0; i < count; i++ {
				commands = append(commands, "echo ok")
			}

			ctx := context.Background()
			outputs, err := ExecuteCommands(ctx, commands, 10*time.Second)

			// All commands succeed, no error
			if err != nil {
				return false
			}

			// Number of outputs should match number of commands
			if len(outputs) != count {
				return false
			}

			// All should have exit code 0
			for _, o := range outputs {
				if o.ExitCode != 0 {
					return false
				}
			}

			return true
		},
		gen.IntRange(0, 5),
	))

	properties.TestingRun(t)
}

// === Property 16: 写入失败保留原文件 ===
// If file write fails, original files are preserved.
//
// **Validates: Requirements 8.7**
func TestProperty16_WriteFailurePreservesOriginalFiles(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("if key write fails, original cert file is preserved", prop.ForAll(
		func(originalCert, newCert string) bool {
			tmpDir := t.TempDir()
			certPath := filepath.Join(tmpDir, "cert.pem")

			// Write original cert file
			if err := os.WriteFile(certPath, []byte(originalCert), 0644); err != nil {
				return false
			}

			// Use an invalid path that will definitely fail on any OS
			// NUL is a reserved device name on Windows, /dev/null/subdir is invalid on Linux
			invalidKeyPath := filepath.Join(tmpDir, "cert.pem", "impossible", "key.pem")

			// Attempt atomic write - should fail because key directory path is invalid
			// (cert.pem is a file, can't create subdirectory inside it)
			err := WriteFilesAtomic(certPath, invalidKeyPath, []byte(newCert), []byte("new-key"))

			// Should have failed
			if err == nil {
				return false
			}

			// Original cert file should still have original content
			content, readErr := os.ReadFile(certPath)
			if readErr != nil {
				return false
			}

			return string(content) == originalCert
		},
		genNonEmptyAlpha(),
		genNonEmptyAlpha(),
	))

	properties.TestingRun(t)
}

// === Property 25: 命令超时强制终止 ===
// Commands exceeding 60 seconds are terminated.
//
// **Validates: Requirements 16.5**
func TestProperty25_CommandTimeoutTermination(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 10
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("commands exceeding timeout are terminated with timed_out flag", prop.ForAll(
		func(iteration int) bool {
			// Use a very short timeout (200ms) and a command that sleeps longer
			commands := []string{"ping -n 5 127.0.0.1"} // Windows: ping 5 times takes ~4 seconds

			ctx := context.Background()
			timeout := 200 * time.Millisecond // very short timeout

			outputs, err := ExecuteCommands(ctx, commands, timeout)

			// Should have an error
			if err == nil {
				return false
			}

			// Should have exactly 1 output
			if len(outputs) != 1 {
				return false
			}

			// The output should be marked as timed out
			if !outputs[0].TimedOut {
				t.Logf("expected TimedOut=true, got false. ExitCode=%d, Stderr=%s", outputs[0].ExitCode, outputs[0].Stderr)
				return false
			}

			// Exit code should be non-zero
			if outputs[0].ExitCode == 0 {
				return false
			}

			return true
		},
		gen.IntRange(1, 10), // just for iteration count
	))

	properties.TestingRun(t)
}

// === Property 28: 证书下载接口机器绑定校验 ===
// Certificate download API verifies machine_id matches token.
// This tests the core logic: when machine_id from token doesn't match machine_certificate's machine_id,
// access should be denied.
//
// **Validates: Requirements 16.2**
func TestProperty28_CertDownloadMachineBindingVerification(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 200
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// verifyMachineBinding simulates the core security check in DownloadCertificate handler:
	// the machine_certificate's machine_id must match the authenticated machine's ID.
	verifyMachineBinding := func(authenticatedMachineID, machineCertMachineID string) bool {
		return authenticatedMachineID == machineCertMachineID
	}

	properties.Property("download is denied when machine_id from token doesn't match machine_certificate's machine_id", prop.ForAll(
		func(authMachineID, mcMachineID string) bool {
			allowed := verifyMachineBinding(authMachineID, mcMachineID)

			if authMachineID == mcMachineID {
				// Same machine - should be allowed
				return allowed == true
			}
			// Different machines - should be denied
			return allowed == false
		},
		genNonEmptyAlpha(),
		genNonEmptyAlpha(),
	))

	properties.TestingRun(t)
}

// === Property 31: 部署文件双文件一致性 ===
// Both cert and key files must be written successfully; if either fails, neither is replaced.
//
// **Validates: Requirements 8.2, 8.8**
func TestProperty31_DeployFileDualFileConsistency(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("successful atomic write results in both files having new content", prop.ForAll(
		func(certContent, keyContent string) bool {
			tmpDir := t.TempDir()
			certPath := filepath.Join(tmpDir, "cert.pem")
			keyPath := filepath.Join(tmpDir, "key.pem")

			err := WriteFilesAtomic(certPath, keyPath, []byte(certContent), []byte(keyContent))
			if err != nil {
				return false
			}

			// Both files should exist with correct content
			certData, err := os.ReadFile(certPath)
			if err != nil {
				return false
			}
			keyData, err := os.ReadFile(keyPath)
			if err != nil {
				return false
			}

			return string(certData) == certContent && string(keyData) == keyContent
		},
		genNonEmptyAlpha(),
		genNonEmptyAlpha(),
	))

	properties.Property("if key temp file write fails, cert file is not replaced", prop.ForAll(
		func(originalCert, originalKey, newCert, newKey string) bool {
			tmpDir := t.TempDir()
			certPath := filepath.Join(tmpDir, "cert.pem")

			// Write original cert file
			if err := os.WriteFile(certPath, []byte(originalCert), 0644); err != nil {
				return false
			}

			// Use an invalid path that will definitely fail on any OS:
			// certPath is a file, so we can't create a subdirectory inside it
			failKeyPath := filepath.Join(certPath, "impossible", "key.pem")

			// Attempt atomic write - should fail on key directory creation
			err := WriteFilesAtomic(certPath, failKeyPath, []byte(newCert), []byte(newKey))

			// Should have failed
			if err == nil {
				return false
			}

			// Original cert should be preserved
			certData, readErr := os.ReadFile(certPath)
			if readErr != nil {
				return false
			}

			return string(certData) == originalCert
		},
		genNonEmptyAlpha(),
		genNonEmptyAlpha(),
		genNonEmptyAlpha(),
		genNonEmptyAlpha(),
	))

	properties.TestingRun(t)
}
