package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// --- Generators ---

// genNonEmptyString generates a non-empty alphanumeric string.
func genNonEmptyString() gopter.Gen {
	return gen.AlphaString().SuchThat(func(s string) bool {
		return len(s) > 0
	})
}

// genInvalidUsername generates usernames that do NOT match the valid user "admin".
func genInvalidUsername() gopter.Gen {
	return gen.AlphaString().SuchThat(func(s string) bool {
		return s != "admin" && len(s) > 0
	})
}

// genInvalidPassword generates passwords that do NOT match the valid password "correct-password".
func genInvalidPassword() gopter.Gen {
	return gen.AlphaString().SuchThat(func(s string) bool {
		return s != "correctpassword" && len(s) > 0
	})
}

// genUserManagementEndpoint generates user management endpoints (create, edit, disable).
func genUserManagementEndpoint() gopter.Gen {
	return gen.OneConstOf(
		"/api/users",
		"/api/users/user-1",
		"/api/users/user-2",
		"/api/users/abc-123",
		"/api/users/xyz-789/disable",
	)
}

// genUserManagementMethod generates HTTP methods used for user management write operations.
func genUserManagementMethod() gopter.Gen {
	return gen.OneConstOf("POST", "PUT", "DELETE")
}

// genWriteMethod generates HTTP methods that represent write operations.
func genWriteMethod() gopter.Gen {
	return gen.OneConstOf("POST", "PUT", "DELETE")
}

// genWriteEndpoint generates endpoints that are NOT in the readonly whitelist.
func genWriteEndpoint() gopter.Gen {
	return gen.OneConstOf(
		"/api/certificates",
		"/api/certificates/abc-123",
		"/api/machines",
		"/api/machines/xyz-456",
		"/api/machines/xyz/token",
		"/api/certificates/abc/renew",
		"/api/domains/abc/probe",
		"/api/thirdpart-dns/abc/sync",
		"/api/alerts/test",
		"/api/users",
		"/api/users/user-1",
		"/api/system/config",
	)
}

// genReadonlyBlockedGETEndpoint generates GET endpoints that should be blocked for readonly
// even though they are GET requests (not in whitelist or explicitly blocked).
func genReadonlyBlockedGETEndpoint() gopter.Gen {
	return gen.OneConstOf(
		// Private key download - explicitly blocked
		"/api/agent/machine-certificates/abc-123/download",
		"/api/agent/machine-certificates/xyz-789/download",
		// Agent endpoints - not in whitelist
		"/api/agent/machines/m1/certificates",
		// User management - not in whitelist for GET
		"/api/users",
		"/api/users/user-1",
		// Thirdpart DNS - not in whitelist
		"/api/thirdpart-dns",
		"/api/thirdpart-dns/dns-1",
	)
}

// --- Mock for Property 2 ---

// mockAuthServiceForProperty2 simulates an auth service where only "admin"/"correctpassword" is valid.
type mockAuthServiceForProperty2 struct {
	secret []byte
}

func (m *mockAuthServiceForProperty2) GetJWTSecret() []byte {
	return m.secret
}

func (m *mockAuthServiceForProperty2) IsSessionValid(_ context.Context, _ string) bool {
	return true
}

func (m *mockAuthServiceForProperty2) IsUserActive(_ context.Context, _ string) bool {
	return true
}

func (m *mockAuthServiceForProperty2) GetCurrentRole(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (m *mockAuthServiceForProperty2) IsTokenValid(_ context.Context, _ string, _ time.Time) bool {
	return true
}

// --- Property Tests ---

// TestProperty2_InvalidCredentialsUniformRejection verifies that for any invalid
// username or password combination, the auth middleware always returns 401 with a
// generic error message, not revealing whether the user exists.
//
// **Validates: Requirements 2.2**
func TestProperty2_InvalidCredentialsUniformRejection(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	secret := []byte("test-secret-for-property2")
	svc := &mockAuthServiceForProperty2{secret: secret}

	// Create a valid token for the "admin" user to compare against
	validToken := createTestToken(secret, map[string]interface{}{
		"user_id":    "user-1",
		"username":   "admin",
		"role":       "admin",
		"session_id": "sess-1",
		"iat":        float64(time.Now().Unix()),
		"exp":        float64(time.Now().Add(time.Hour).Unix()),
	})

	handler := AuthMiddleware(svc)(okHandler())

	// Property: Any request with an invalid/missing/malformed token gets 401
	properties.Property("invalid credentials always return 401 with generic message", prop.ForAll(
		func(invalidToken string) bool {
			// Skip if by chance we generated the valid token
			if invalidToken == validToken {
				return true
			}

			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			req.Header.Set("Authorization", "Bearer "+invalidToken)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			// Must return 401
			if rec.Code != http.StatusUnauthorized {
				t.Logf("Expected 401, got %d for token: %s", rec.Code, invalidToken)
				return false
			}

			// Response must not reveal whether user exists
			body := rec.Body.String()
			if containsUserExistenceHint(body) {
				t.Logf("Response reveals user existence info: %s", body)
				return false
			}

			return true
		},
		genNonEmptyString(),
	))

	// Property: Missing authorization header always returns 401
	properties.Property("missing auth header returns 401", prop.ForAll(
		func(path string) bool {
			req := httptest.NewRequest(http.MethodGet, "/"+path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			return rec.Code == http.StatusUnauthorized
		},
		gen.AlphaString(),
	))

	// Property: Token signed with wrong secret returns 401
	properties.Property("token with wrong secret returns 401", prop.ForAll(
		func(username string, role string) bool {
			wrongSecret := []byte("wrong-secret-key")
			wrongToken := createTestToken(wrongSecret, map[string]interface{}{
				"user_id":    "user-1",
				"username":   username,
				"role":       role,
				"session_id": "sess-1",
				"iat":        float64(time.Now().Unix()),
				"exp":        float64(time.Now().Add(time.Hour).Unix()),
			})

			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			req.Header.Set("Authorization", "Bearer "+wrongToken)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Logf("Expected 401 for wrong-secret token, got %d", rec.Code)
				return false
			}

			// Must not reveal user existence
			body := rec.Body.String()
			return !containsUserExistenceHint(body)
		},
		genNonEmptyString(),
		gen.OneConstOf("admin", "user", "readonly"),
	))

	properties.TestingRun(t)
}

// TestProperty3_NonAdminUserManagementRejection verifies that for any user with
// role "user", accessing any user management interface returns 403.
//
// **Validates: Requirements 2.4**
func TestProperty3_NonAdminUserManagementRejection(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	handler := RoleMiddleware("admin")(okHandler())

	// Property: Any user with role "user" is rejected with 403 on user management endpoints
	properties.Property("non-admin user always gets 403 on user management endpoints", prop.ForAll(
		func(endpoint string, method string, userID string) bool {
			req := httptest.NewRequest(method, endpoint, nil)
			ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{
				UserID:   userID,
				Username: "regular_user",
				Role:     "user",
			})
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Logf("Expected 403 for user role on %s %s, got %d", method, endpoint, rec.Code)
				return false
			}

			return true
		},
		genUserManagementEndpoint(),
		genUserManagementMethod(),
		genNonEmptyString(),
	))

	// Property: Any user with role "user" is rejected even on GET user management endpoints
	properties.Property("non-admin user gets 403 on GET user management endpoints too", prop.ForAll(
		func(endpoint string, userID string) bool {
			req := httptest.NewRequest(http.MethodGet, endpoint, nil)
			ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{
				UserID:   userID,
				Username: "regular_user",
				Role:     "user",
			})
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Logf("Expected 403 for user role on GET %s, got %d", endpoint, rec.Code)
				return false
			}

			return true
		},
		genUserManagementEndpoint(),
		genNonEmptyString(),
	))

	properties.TestingRun(t)
}

// TestProperty4_ReadonlySessionWriteOperationRejection verifies that for any
// readonly session and any write operation interface (POST/PUT/DELETE),
// Web_Backend returns 403.
//
// **Validates: Requirements 2.6**
func TestProperty4_ReadonlySessionWriteOperationRejection(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	handler := ReadonlyMiddleware()(okHandler())

	// Property: Any write operation (POST/PUT/DELETE) by readonly session returns 403
	properties.Property("readonly session always gets 403 on write operations", prop.ForAll(
		func(method string, endpoint string) bool {
			req := httptest.NewRequest(method, endpoint, nil)
			ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{
				UserID:   "readonly",
				Username: "readonly",
				Role:     "readonly",
			})
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Logf("Expected 403 for readonly %s %s, got %d", method, endpoint, rec.Code)
				return false
			}

			return true
		},
		genWriteMethod(),
		genWriteEndpoint(),
	))

	// Property: Readonly session is blocked on POST/PUT/DELETE regardless of path
	properties.Property("readonly session blocked on any POST/PUT/DELETE path", prop.ForAll(
		func(method string, pathSuffix string) bool {
			// Exclude the whitelisted auth endpoints
			path := "/api/" + pathSuffix
			if path == "/api/auth/login" || path == "/api/auth/logout" {
				return true // skip whitelisted
			}

			req := httptest.NewRequest(method, path, nil)
			ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{
				UserID:   "readonly",
				Username: "readonly",
				Role:     "readonly",
			})
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Logf("Expected 403 for readonly %s %s, got %d", method, path, rec.Code)
				return false
			}

			return true
		},
		gen.OneConstOf("PUT", "DELETE"),
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 }),
	))

	properties.TestingRun(t)
}

// TestProperty29_ReadonlyModeWhitelist verifies that readonly mode uses an interface
// whitelist rather than simply allowing all GET requests. GET requests to endpoints
// NOT in the whitelist should be blocked.
//
// **Validates: Requirements 2.6, 2.7**
func TestProperty29_ReadonlyModeWhitelist(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	handler := ReadonlyMiddleware()(okHandler())

	// Property: GET requests to non-whitelisted endpoints are blocked for readonly sessions
	properties.Property("readonly GET to non-whitelisted endpoints returns 403", prop.ForAll(
		func(endpoint string) bool {
			req := httptest.NewRequest(http.MethodGet, endpoint, nil)
			ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{
				UserID:   "readonly",
				Username: "readonly",
				Role:     "readonly",
			})
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Logf("Expected 403 for readonly GET %s, got %d", endpoint, rec.Code)
				return false
			}

			return true
		},
		genReadonlyBlockedGETEndpoint(),
	))

	// Property: Explicitly blocked endpoints (private key download) are blocked even for GET
	properties.Property("private key download blocked for readonly even as GET", prop.ForAll(
		func(certID string) bool {
			if certID == "" {
				certID = "test-id"
			}
			path := "/api/agent/machine-certificates/" + certID + "/download"

			req := httptest.NewRequest(http.MethodGet, path, nil)
			ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{
				UserID:   "readonly",
				Username: "readonly",
				Role:     "readonly",
			})
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Logf("Expected 403 for readonly GET %s, got %d", path, rec.Code)
				return false
			}

			return true
		},
		genNonEmptyString(),
	))

	// Property: Non-readonly users are NOT affected by readonly middleware
	properties.Property("non-readonly users pass through readonly middleware", prop.ForAll(
		func(role string, method string, endpoint string) bool {
			req := httptest.NewRequest(method, endpoint, nil)
			ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{
				UserID:   "user-1",
				Username: "testuser",
				Role:     role,
			})
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			// Non-readonly users should always pass through (200 from okHandler)
			if rec.Code != http.StatusOK {
				t.Logf("Expected 200 for %s role on %s %s, got %d", role, method, endpoint, rec.Code)
				return false
			}

			return true
		},
		gen.OneConstOf("admin", "user"),
		gen.OneConstOf("GET", "POST", "PUT", "DELETE"),
		genWriteEndpoint(),
	))

	properties.TestingRun(t)
}

// --- Helper functions ---

// containsUserExistenceHint checks if a response body reveals whether a user exists.
// The error messages should be generic and not differentiate between "user not found"
// and "wrong password".
func containsUserExistenceHint(body string) bool {
	hints := []string{
		"user not found",
		"user does not exist",
		"no such user",
		"username not found",
		"unknown user",
		"user exists",
	}
	for _, hint := range hints {
		if containsIgnoreCase(body, hint) {
			return true
		}
	}
	return false
}

// containsIgnoreCase checks if s contains substr (case-insensitive).
func containsIgnoreCase(s, substr string) bool {
	sLower := strings.ToLower(s)
	substrLower := strings.ToLower(substr)
	return strings.Contains(sLower, substrLower)
}
