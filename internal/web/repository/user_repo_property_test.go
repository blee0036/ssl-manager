package repository

import (
	"context"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"golang.org/x/crypto/bcrypt"
)

// TestPropertyPasswordBcryptHashStorage verifies that for any user password,
// it should be stored as a bcrypt hash, never in plaintext.
//
// **Validates: Requirements 16.7**
func TestPropertyPasswordBcryptHashStorage(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("password is stored as bcrypt hash, never plaintext", prop.ForAll(
		func(password string, username string) bool {
			db := setupUserTestDB(t)
			defer db.Close()

			repo := NewUserRepository(db)
			ctx := context.Background()

			user := &model.User{
				Username:     username,
				PasswordHash: password, // plain-text password passed in PasswordHash field
				Role:         "user",
			}

			err := repo.Create(ctx, user)
			if err != nil {
				t.Logf("Create failed: %v", err)
				return false
			}

			// Retrieve the user from the database
			got, err := repo.GetByUsername(ctx, username)
			if err != nil {
				t.Logf("GetByUsername failed: %v", err)
				return false
			}

			// Property 1: The stored hash must NOT equal the plaintext password
			if got.PasswordHash == password {
				t.Logf("password stored as plaintext for password=%q", password)
				return false
			}

			// Property 2: The stored hash must be a valid bcrypt hash
			// bcrypt hashes always start with "$2a$", "$2b$", or "$2y$"
			if len(got.PasswordHash) < 4 {
				t.Logf("stored hash too short: %q", got.PasswordHash)
				return false
			}
			prefix := got.PasswordHash[:4]
			if prefix != "$2a$" && prefix != "$2b$" && prefix != "$2y$" {
				t.Logf("stored hash does not have bcrypt prefix: %q", got.PasswordHash)
				return false
			}

			// Property 3: The stored bcrypt hash must verify against the original password
			err = bcrypt.CompareHashAndPassword([]byte(got.PasswordHash), []byte(password))
			if err != nil {
				t.Logf("bcrypt verification failed for password=%q, hash=%q: %v", password, got.PasswordHash, err)
				return false
			}

			return true
		},
		// Generate non-empty password strings (bcrypt requires non-empty input for meaningful test)
		gen.AlphaString().SuchThat(func(s string) bool {
			// bcrypt has a max length of 72 bytes; passwords must be non-empty
			return len(s) > 0 && len(s) <= 72
		}),
		// Generate unique usernames to avoid conflicts
		gen.AlphaString().SuchThat(func(s string) bool {
			return len(s) > 0 && len(s) <= 50
		}).Map(func(s string) string {
			return "user_" + s
		}),
	))

	properties.TestingRun(t)
}
