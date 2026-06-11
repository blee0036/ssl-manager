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
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"

	_ "github.com/glebarez/sqlite"
)

// Feature: ux-improvements-batch1, Property 13: Auto-Sync Triggered After Config Creation

// TestProperty_AutoSyncTriggeredAfterConfigCreation verifies that for any valid config name,
// after a successful HTTP POST to /api/thirdpart-dns, the response always contains either
// sync_result (indicating sync was triggered and succeeded) or sync_error (indicating sync
// was triggered but failed). This proves SyncRecords is always called after creation.
//
// **Validates: Requirements 10.1**
func TestProperty_AutoSyncTriggeredAfterConfigCreation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	properties.Property("Create always triggers sync (response contains sync_result or sync_error)", prop.ForAll(
		func(configName string) bool {
			// Setup fresh DB and handler for each test
			db := setupThirdpartDNSTestDB(t)
			dnsRepo := repository.NewThirdpartDNSRepository(db)
			domainRepo := repository.NewDomainRepository(db)
			cfClient := &mockCFClient{} // returns 1 record on ListDNSRecords
			dnsService := service.NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, nil, config.NewRuntimeConfig(config.DefaultConfig()))
			handler := NewThirdpartDNSHandler(dnsService)

			router := chi.NewRouter()
			router.Route("/api/thirdpart-dns", func(r chi.Router) {
				r.Post("/", handler.Create)
			})

			// Build request with random config name
			body := map[string]interface{}{
				"name":         configName,
				"type":         "cloudflare",
				"api_token":    "cf-api-token-test",
				"config_json":  "{}",
				"main_domains": []string{"example.com"},
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/api/thirdpart-dns", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Must return 201 Created
			if w.Code != http.StatusCreated {
				t.Logf("unexpected status %d for name=%q, body=%s", w.Code, configName, w.Body.String())
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
				t.Logf("response data is not a map")
				return false
			}

			// Property: response MUST contain sync_result (sync succeeded) or sync_error (sync was attempted but failed)
			// Either way, this proves SyncRecords was called.
			syncResult, hasSyncResult := data["sync_result"]
			syncError, hasSyncError := data["sync_error"]

			syncTriggered := (hasSyncResult && syncResult != nil) || (hasSyncError && syncError != "")
			if !syncTriggered {
				t.Logf("neither sync_result nor sync_error found for name=%q", configName)
				return false
			}

			// When mock client succeeds, sync_result should be present with records_count >= 0
			if hasSyncResult && syncResult != nil {
				sr, ok := syncResult.(map[string]interface{})
				if !ok {
					t.Logf("sync_result is not a map for name=%q", configName)
					return false
				}
				rc, ok := sr["records_count"].(float64)
				if !ok || rc < 0 {
					t.Logf("invalid records_count for name=%q: %v", configName, sr["records_count"])
					return false
				}
			}

			return true
		},
		// Generate config names that are valid: start with at least one alphanumeric char
		// (ensures name is non-empty after trimming whitespace)
		gen.RegexMatch(`[A-Za-z][A-Za-z0-9 _\-]{0,49}`),
	))

	properties.TestingRun(t)
}
