package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// --- Generators ---

// genEmptyOrWhitespacePath generates strings that are empty or contain only whitespace.
func genEmptyOrWhitespacePath() gopter.Gen {
	return gen.OneGenOf(
		gen.Const(""),
		gen.Const(" "),
		gen.Const("  "),
		gen.Const("\t"),
		gen.Const("\n"),
		gen.Const(" \t\n "),
		gen.IntRange(1, 10).Map(func(n int) string {
			return strings.Repeat(" ", n)
		}),
	)
}

// genValidPath generates valid non-empty file paths.
func genValidPath() gopter.Gen {
	return gen.Identifier().Map(func(s string) string {
		return "/etc/ssl/" + s + ".pem"
	})
}

// genUpdateCount generates the number of sequential updates to perform (1-5).
func genUpdateCount() gopter.Gen {
	return gen.IntRange(1, 5)
}

// --- Property Tests ---

// TestProperty13_DeployPathNonEmptyValidation verifies that for any CreateMachineCertInput
// or UpdateMachineCertInput where cert_path or private_key_path is empty/whitespace,
// the service should reject with an error.
//
// **Validates: Requirements 7.2**
func TestProperty13_DeployPathNonEmptyValidation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Property: Create with empty/whitespace cert_path is always rejected
	properties.Property("Create rejects empty/whitespace cert_path", prop.ForAll(
		func(emptyPath string, validKeyPath string) bool {
			db := setupMachineCertServiceTestDB(t)
			repo := repository.NewMachineCertificateRepository(db)
			svc := NewMachineCertificateService(repo)
			ctx := context.Background()

			input := model.CreateMachineCertInput{
				MachineID:      "machine-1",
				CertificateID:  "cert-1",
				CertPath:       emptyPath,
				PrivateKeyPath: validKeyPath,
			}

			_, err := svc.Create(ctx, input)
			if err == nil {
				t.Logf("Expected error for empty/whitespace cert_path %q, got nil", emptyPath)
				return false
			}
			return true
		},
		genEmptyOrWhitespacePath(),
		genValidPath(),
	))

	// Property: Create with empty/whitespace private_key_path is always rejected
	properties.Property("Create rejects empty/whitespace private_key_path", prop.ForAll(
		func(validCertPath string, emptyPath string) bool {
			db := setupMachineCertServiceTestDB(t)
			repo := repository.NewMachineCertificateRepository(db)
			svc := NewMachineCertificateService(repo)
			ctx := context.Background()

			input := model.CreateMachineCertInput{
				MachineID:      "machine-1",
				CertificateID:  "cert-1",
				CertPath:       validCertPath,
				PrivateKeyPath: emptyPath,
			}

			_, err := svc.Create(ctx, input)
			if err == nil {
				t.Logf("Expected error for empty/whitespace private_key_path %q, got nil", emptyPath)
				return false
			}
			return true
		},
		genValidPath(),
		genEmptyOrWhitespacePath(),
	))

	// Property: Update with empty/whitespace cert_path is always rejected
	properties.Property("Update rejects empty/whitespace cert_path", prop.ForAll(
		func(emptyPath string) bool {
			db := setupMachineCertServiceTestDB(t)
			repo := repository.NewMachineCertificateRepository(db)
			svc := NewMachineCertificateService(repo)
			ctx := context.Background()

			// Create a valid machine certificate first
			createInput := model.CreateMachineCertInput{
				MachineID:      "machine-1",
				CertificateID:  "cert-1",
				CertPath:       "/etc/ssl/cert.pem",
				PrivateKeyPath: "/etc/ssl/key.pem",
			}
			mc, err := svc.Create(ctx, createInput)
			if err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}

			// Try to update with empty/whitespace cert_path
			updateInput := model.UpdateMachineCertInput{
				CertPath: &emptyPath,
			}

			_, err = svc.Update(ctx, mc.ID, updateInput)
			if err == nil {
				t.Logf("Expected error for empty/whitespace cert_path %q in update, got nil", emptyPath)
				return false
			}
			return true
		},
		genEmptyOrWhitespacePath(),
	))

	// Property: Update with empty/whitespace private_key_path is always rejected
	properties.Property("Update rejects empty/whitespace private_key_path", prop.ForAll(
		func(emptyPath string) bool {
			db := setupMachineCertServiceTestDB(t)
			repo := repository.NewMachineCertificateRepository(db)
			svc := NewMachineCertificateService(repo)
			ctx := context.Background()

			// Create a valid machine certificate first
			createInput := model.CreateMachineCertInput{
				MachineID:      "machine-1",
				CertificateID:  "cert-1",
				CertPath:       "/etc/ssl/cert.pem",
				PrivateKeyPath: "/etc/ssl/key.pem",
			}
			mc, err := svc.Create(ctx, createInput)
			if err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}

			// Try to update with empty/whitespace private_key_path
			updateInput := model.UpdateMachineCertInput{
				PrivateKeyPath: &emptyPath,
			}

			_, err = svc.Update(ctx, mc.ID, updateInput)
			if err == nil {
				t.Logf("Expected error for empty/whitespace private_key_path %q in update, got nil", emptyPath)
				return false
			}
			return true
		},
		genEmptyOrWhitespacePath(),
	))

	// Property: Create with both paths empty is always rejected
	properties.Property("Create rejects both paths empty", prop.ForAll(
		func(emptyPath1 string, emptyPath2 string) bool {
			db := setupMachineCertServiceTestDB(t)
			repo := repository.NewMachineCertificateRepository(db)
			svc := NewMachineCertificateService(repo)
			ctx := context.Background()

			input := model.CreateMachineCertInput{
				MachineID:      "machine-1",
				CertificateID:  "cert-1",
				CertPath:       emptyPath1,
				PrivateKeyPath: emptyPath2,
			}

			_, err := svc.Create(ctx, input)
			if err == nil {
				t.Logf("Expected error for both paths empty (%q, %q), got nil", emptyPath1, emptyPath2)
				return false
			}
			return true
		},
		genEmptyOrWhitespacePath(),
		genEmptyOrWhitespacePath(),
	))

	properties.TestingRun(t)
}

// TestProperty27_ConfigRevisionIncrementTriggersDeploy verifies that when a machine
// certificate is created, updated, or manually triggered for deploy, the config_revision
// should increment, signaling the Agent to deploy.
//
// **Validates: Requirements 7.4, 7.5, 8.1**
func TestProperty27_ConfigRevisionIncrementTriggersDeploy(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 50
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Property: Create always sets config_revision to 1 and status to pending
	properties.Property("Create sets initial config_revision=1 and status=pending", prop.ForAll(
		func(certPath string, keyPath string) bool {
			db := setupMachineCertServiceTestDB(t)
			repo := repository.NewMachineCertificateRepository(db)
			svc := NewMachineCertificateService(repo)
			ctx := context.Background()

			input := model.CreateMachineCertInput{
				MachineID:      "machine-1",
				CertificateID:  "cert-1",
				CertPath:       certPath,
				PrivateKeyPath: keyPath,
			}

			mc, err := svc.Create(ctx, input)
			if err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}

			if mc.ConfigRevision != 1 {
				t.Logf("Expected config_revision=1, got %d", mc.ConfigRevision)
				return false
			}
			if mc.LastDeployStatus != "pending" {
				t.Logf("Expected last_deploy_status=pending, got %q", mc.LastDeployStatus)
				return false
			}
			return true
		},
		genValidPath(),
		genValidPath(),
	))

	// Property: Each Update increments config_revision by exactly 1
	properties.Property("Update increments config_revision by 1 each time", prop.ForAll(
		func(updateCount int) bool {
			db := setupMachineCertServiceTestDB(t)
			repo := repository.NewMachineCertificateRepository(db)
			svc := NewMachineCertificateService(repo)
			ctx := context.Background()

			// Create initial machine certificate
			input := model.CreateMachineCertInput{
				MachineID:      "machine-1",
				CertificateID:  "cert-1",
				CertPath:       "/etc/ssl/cert.pem",
				PrivateKeyPath: "/etc/ssl/key.pem",
			}
			mc, err := svc.Create(ctx, input)
			if err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}

			// Perform N sequential updates
			for i := 0; i < updateCount; i++ {
				newPath := fmt.Sprintf("/etc/ssl/cert-%d.pem", i)
				updateInput := model.UpdateMachineCertInput{
					CertPath: &newPath,
				}

				updated, err := svc.Update(ctx, mc.ID, updateInput)
				if err != nil {
					t.Logf("Update %d failed: %v", i, err)
					return false
				}

				expectedRevision := 1 + (i + 1) // initial 1 + number of updates
				if updated.ConfigRevision != expectedRevision {
					t.Logf("After update %d: expected config_revision=%d, got %d", i, expectedRevision, updated.ConfigRevision)
					return false
				}

				// Status should be pending after each update
				if updated.LastDeployStatus != "pending" {
					t.Logf("After update %d: expected status=pending, got %q", i, updated.LastDeployStatus)
					return false
				}
			}

			return true
		},
		genUpdateCount(),
	))

	// Property: TriggerManualDeploy increments config_revision and sets status to pending
	properties.Property("TriggerManualDeploy increments config_revision", prop.ForAll(
		func(certPath string, keyPath string) bool {
			db := setupMachineCertServiceTestDB(t)
			repo := repository.NewMachineCertificateRepository(db)
			svc := NewMachineCertificateService(repo)
			ctx := context.Background()

			input := model.CreateMachineCertInput{
				MachineID:      "machine-1",
				CertificateID:  "cert-1",
				CertPath:       certPath,
				PrivateKeyPath: keyPath,
			}

			mc, err := svc.Create(ctx, input)
			if err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}

			initialRevision := mc.ConfigRevision

			// Trigger manual deploy
			err = svc.TriggerManualDeploy(ctx, mc.ID)
			if err != nil {
				t.Logf("TriggerManualDeploy failed: %v", err)
				return false
			}

			// Retrieve and verify
			results, err := svc.GetByMachineID(ctx, "machine-1")
			if err != nil {
				t.Logf("GetByMachineID failed: %v", err)
				return false
			}

			var found *model.MachineCertificate
			for _, r := range results {
				if r.ID == mc.ID {
					found = r
					break
				}
			}

			if found == nil {
				t.Logf("Could not find machine certificate after TriggerManualDeploy")
				return false
			}

			if found.ConfigRevision != initialRevision+1 {
				t.Logf("Expected config_revision=%d after TriggerManualDeploy, got %d", initialRevision+1, found.ConfigRevision)
				return false
			}

			if found.LastDeployStatus != "pending" {
				t.Logf("Expected status=pending after TriggerManualDeploy, got %q", found.LastDeployStatus)
				return false
			}

			return true
		},
		genValidPath(),
		genValidPath(),
	))

	// Property: config_revision is strictly monotonically increasing across mixed operations
	properties.Property("config_revision strictly increases across mixed operations", prop.ForAll(
		func(updateCount int) bool {
			db := setupMachineCertServiceTestDB(t)
			repo := repository.NewMachineCertificateRepository(db)
			svc := NewMachineCertificateService(repo)
			ctx := context.Background()

			input := model.CreateMachineCertInput{
				MachineID:      "machine-1",
				CertificateID:  "cert-1",
				CertPath:       "/etc/ssl/cert.pem",
				PrivateKeyPath: "/etc/ssl/key.pem",
			}

			mc, err := svc.Create(ctx, input)
			if err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}

			lastRevision := mc.ConfigRevision

			// Alternate between Update and TriggerManualDeploy
			for i := 0; i < updateCount; i++ {
				if i%2 == 0 {
					// Update operation
					newPath := fmt.Sprintf("/etc/ssl/cert-%d.pem", i)
					updateInput := model.UpdateMachineCertInput{
						CertPath: &newPath,
					}
					updated, err := svc.Update(ctx, mc.ID, updateInput)
					if err != nil {
						t.Logf("Update %d failed: %v", i, err)
						return false
					}
					if updated.ConfigRevision <= lastRevision {
						t.Logf("Revision did not increase after update %d: was %d, now %d", i, lastRevision, updated.ConfigRevision)
						return false
					}
					lastRevision = updated.ConfigRevision
				} else {
					// TriggerManualDeploy operation
					err := svc.TriggerManualDeploy(ctx, mc.ID)
					if err != nil {
						t.Logf("TriggerManualDeploy %d failed: %v", i, err)
						return false
					}

					results, err := svc.GetByMachineID(ctx, "machine-1")
					if err != nil {
						t.Logf("GetByMachineID failed: %v", err)
						return false
					}

					var found *model.MachineCertificate
					for _, r := range results {
						if r.ID == mc.ID {
							found = r
							break
						}
					}
					if found == nil {
						t.Logf("Could not find machine certificate")
						return false
					}

					if found.ConfigRevision <= lastRevision {
						t.Logf("Revision did not increase after TriggerManualDeploy %d: was %d, now %d", i, lastRevision, found.ConfigRevision)
						return false
					}
					lastRevision = found.ConfigRevision
				}
			}

			return true
		},
		genUpdateCount(),
	))

	properties.TestingRun(t)
}
