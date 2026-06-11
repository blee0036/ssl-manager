package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"

	_ "github.com/glebarez/sqlite"
)

// Feature: ux-improvements-batch1, Property 16: Auto-Probe After Manual Domain Creation

// TestProperty_AutoProbeAfterManualDomainCreation verifies that for any valid domain name,
// after a successful HTTP POST to /api/domains (creating a manual domain), the response
// always contains either probe_result (probe succeeded) or probe_error (probe was attempted
// but failed). Additionally, the domain must always persist in the database regardless of
// probe outcome.
//
// **Validates: Requirements 11.1, 11.3, 11.5**
func TestProperty_AutoProbeAfterManualDomainCreation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("Create manual domain always triggers probe (response contains probe_result or probe_error) and domain persists", prop.ForAll(
		func(domainName string) bool {
			// Setup fresh DB and handler for each test iteration
			db := setupDomainTestDB(t)
			domainRepo := repository.NewDomainRepository(db)
			certRepo := repository.NewCertificateRepository(db, t.TempDir())
			domainService := service.NewDomainMonitorService(domainRepo, certRepo, nil, nil)
			// Note: No custom DNS resolver or TLS dialer set, so Probe will fail
			// for non-resolvable random domains. This is intentional — we verify that
			// probe failure does not prevent domain persistence.
			handler := NewDomainHandler(domainService)

			router := chi.NewRouter()
			router.Route("/api/domains", func(r chi.Router) {
				r.Post("/", handler.Create)
			})

			// Build request with random domain name
			body := map[string]interface{}{
				"name":         domainName,
				"monitor_port": 443,
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/api/domains", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Must return 201 Created
			if w.Code != http.StatusCreated {
				t.Logf("unexpected status %d for name=%q, body=%s", w.Code, domainName, w.Body.String())
				return false
			}

			// Parse response
			var resp model.SuccessResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Logf("failed to unmarshal response: %v", err)
				return false
			}

			data, ok := resp.Data.(map[string]interface{})
			if !ok {
				t.Logf("response data is not a map for name=%q", domainName)
				return false
			}

			// Property 1: response MUST contain probe_result or probe_error.
			// Either way, this proves Probe() was called after creation.
			probeResult, hasProbeResult := data["probe_result"]
			probeError, hasProbeError := data["probe_error"]

			probeTriggered := (hasProbeResult && probeResult != nil) || (hasProbeError && probeError != "")
			if !probeTriggered {
				t.Logf("neither probe_result nor probe_error found for name=%q", domainName)
				return false
			}

			// Property 2: domain must exist in the database regardless of probe outcome.
			// Extract the domain ID from the response.
			domainID, ok := data["id"].(string)
			if !ok || domainID == "" {
				t.Logf("missing or invalid domain id in response for name=%q", domainName)
				return false
			}

			// Verify domain persisted in DB
			var count int
			err := db.QueryRow("SELECT COUNT(*) FROM domains WHERE id = ?", domainID).Scan(&count)
			if err != nil {
				t.Logf("failed to query domain from DB: %v", err)
				return false
			}
			if count != 1 {
				t.Logf("domain not persisted in DB (count=%d) for name=%q, id=%s", count, domainName, domainID)
				return false
			}

			// Property 3: when probe fails (which it will for random names that can't resolve),
			// probe_error should be non-empty string.
			if hasProbeError && probeError != "" {
				// This is the expected case for random domain names — probe failed but domain exists.
				return true
			}

			// If probe_result is present (very unlikely with random names), validate it's a valid map.
			if hasProbeResult && probeResult != nil {
				pr, ok := probeResult.(map[string]interface{})
				if !ok {
					t.Logf("probe_result is not a map for name=%q", domainName)
					return false
				}
				// Should have domain_id field
				if _, hasDomainID := pr["domain_id"]; !hasDomainID {
					t.Logf("probe_result missing domain_id for name=%q", domainName)
					return false
				}
			}

			return true
		},
		// Generate random domain-like names (1-30 chars, alphanumeric + dots + hyphens)
		gen.RegexMatch(`[a-z][a-z0-9\-]{0,15}\.[a-z]{2,6}`),
	))

	properties.TestingRun(t)
}
