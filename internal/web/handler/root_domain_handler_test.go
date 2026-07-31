package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"

	_ "github.com/glebarez/sqlite"
)

// This file contains the task 7.3 handler UNIT tests (standard testing + httptest):
//   1. success responses conform to the { code, message, data } envelope,
//   2. an unknown id yields 404 (GET / PUT / DELETE / refresh),
//   3. readonly-session write operations are intercepted with 403,
//   4. creating a duplicate registrable domain yields 409.
//
// _Requirements: 3.4, 8.4, 9.1_
//
// Every top-level identifier is prefixed with rdunit* so it never collides with
// the sibling root_domain_handler_property_test.go (task 7.2, rdprop* prefix).
// The shared mockAuthService / mockAuditRepo test doubles (defined in
// certificate_handler_test.go, secret "test-secret") are reused as-is.

// rdunitNoopWhois is a service.WhoisClient test double that never performs a
// network request. It always returns an error, so any best-effort refresh (e.g.
// the one Create makes) is folded into the record as a failed check — keeping
// these handler tests hermetic and deterministic (no real WHOIS traffic).
type rdunitNoopWhois struct{}

func (rdunitNoopWhois) LookupExpiry(_ context.Context, _ string) (time.Time, error) {
	return time.Time{}, errors.New("whois disabled in handler unit test")
}

// Compile-time assertion that the stub satisfies the WHOIS client interface.
var _ service.WhoisClient = rdunitNoopWhois{}

// rdunitSetup builds an in-memory SQLite DB with the root_domains schema (mirroring
// internal/database/migrate.go, including the UNIQUE index on registrable_domain),
// wires a real DomainExpiryService (nil zoneScanner + nil alerter — unused by the
// paths under test) over it with a hermetic WHOIS stub, and registers the handler
// routes through the REAL RootDomainHandler.RegisterRoutes so the full
// Auth + Audit + Readonly middleware chain is exercised exactly as in production.
func rdunitSetup(t *testing.T) (*repository.RootDomainRepository, chi.Router) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`CREATE TABLE root_domains (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT 'manual' CHECK(source IN ('manual', 'cloudflare')),
		registrable_domain TEXT NOT NULL,
		expiry_date TEXT,
		last_checked_at TEXT,
		last_status TEXT NOT NULL DEFAULT '',
		last_error TEXT NOT NULL DEFAULT '',
		monitor_enabled INTEGER NOT NULL DEFAULT 1,
		alert_ignored INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("failed to create root_domains table: %v", err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX idx_root_domains_registrable ON root_domains(registrable_domain)`); err != nil {
		t.Fatalf("failed to create unique index: %v", err)
	}

	repo := repository.NewRootDomainRepository(db)
	svc := service.NewDomainExpiryService(repo, nil, nil, config.NewRuntimeConfig(config.DefaultConfig()))
	svc.SetWhoisClient(rdunitNoopWhois{}) // no real WHOIS traffic
	// nil DNSConfigResolver: the config_id import path is not exercised here.
	h := NewRootDomainHandler(svc, nil)

	r := chi.NewRouter()
	h.RegisterRoutes(r, &mockAuthService{}, &mockAuditRepo{})
	return repo, r
}

// rdunitSeed inserts a root domain directly via the repository (no WHOIS) and
// returns the persisted record (with its generated id populated).
func rdunitSeed(t *testing.T, repo *repository.RootDomainRepository, name, registrable, source string) *model.RootDomain {
	t.Helper()
	rd := &model.RootDomain{
		Name:              name,
		Source:            source,
		RegistrableDomain: registrable,
		MonitorEnabled:    true,
	}
	if err := repo.Create(context.Background(), rd); err != nil {
		t.Fatalf("failed to seed root domain %q: %v", registrable, err)
	}
	return rd
}

// rdunitToken mints a JWT for the given role signed with the mockAuthService
// secret ("test-secret"). mockAuthService.GetCurrentRole returns ("", nil), so the
// role encoded here is preserved through AuthMiddleware (never overridden).
func rdunitToken(t *testing.T, role string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"user_id":    role + "-user",
		"username":   role,
		"role":       role,
		"session_id": "sess-" + role,
		"iat":        time.Now().Unix(),
		"exp":        time.Now().Add(time.Hour).Unix(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}
	return signed
}

// rdunitDo issues an authenticated request against the router and returns the
// recorder. An empty body sends no request body; a non-empty body is sent as JSON.
func rdunitDo(t *testing.T, r chi.Router, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// --- 1. Success responses conform to the { code, message, data } envelope ---

// TestRootDomainHandler_List_SuccessEnvelope verifies GET /api/root-domains returns
// the standard { code, message, data } envelope with data = { items, total, page,
// per_page } (requirement 9.1).
func TestRootDomainHandler_List_SuccessEnvelope(t *testing.T) {
	repo, r := rdunitSetup(t)
	rdunitSeed(t, repo, "example.com", "example.com", "manual")
	rdunitSeed(t, repo, "test.org", "test.org", "cloudflare")

	w := rdunitDo(t, r, http.MethodGet, "/api/root-domains?per_page=100", "", rdunitToken(t, "admin"))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v; body: %s", err, w.Body.String())
	}
	if resp.Code != http.StatusOK {
		t.Errorf("expected envelope code 200, got %d", resp.Code)
	}
	if resp.Message != "success" {
		t.Errorf("expected envelope message 'success', got %q", resp.Message)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be an object, got %T", resp.Data)
	}
	items, ok := data["items"].([]interface{})
	if !ok {
		t.Fatalf("expected data.items to be an array, got %T", data["items"])
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
	if total, ok := data["total"].(float64); !ok || int(total) != 2 {
		t.Errorf("expected data.total 2, got %v", data["total"])
	}
	if _, ok := data["page"]; !ok {
		t.Error("expected data.page to be present")
	}
	if _, ok := data["per_page"]; !ok {
		t.Error("expected data.per_page to be present")
	}
}

// TestRootDomainHandler_GetByID_SuccessEnvelope verifies GET /api/root-domains/{id}
// returns the standard envelope wrapping the RootDomain payload (requirement 9.1).
func TestRootDomainHandler_GetByID_SuccessEnvelope(t *testing.T) {
	repo, r := rdunitSetup(t)
	seeded := rdunitSeed(t, repo, "example.com", "example.com", "manual")

	w := rdunitDo(t, r, http.MethodGet, "/api/root-domains/"+seeded.ID, "", rdunitToken(t, "admin"))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v; body: %s", err, w.Body.String())
	}
	if resp.Code != http.StatusOK {
		t.Errorf("expected envelope code 200, got %d", resp.Code)
	}
	if resp.Message != "success" {
		t.Errorf("expected envelope message 'success', got %q", resp.Message)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be an object, got %T", resp.Data)
	}
	if data["id"] != seeded.ID {
		t.Errorf("expected data.id %q, got %v", seeded.ID, data["id"])
	}
	if data["registrable_domain"] != "example.com" {
		t.Errorf("expected data.registrable_domain 'example.com', got %v", data["registrable_domain"])
	}
}

// TestRootDomainHandler_Create_SuccessEnvelope verifies POST /api/root-domains
// creates a manual root domain and returns 201 with the standard envelope
// (requirements 3.1, 9.1). The hermetic WHOIS stub makes the best-effort refresh a
// no-op, so no real network request is made.
func TestRootDomainHandler_Create_SuccessEnvelope(t *testing.T) {
	_, r := rdunitSetup(t)

	w := rdunitDo(t, r, http.MethodPost, "/api/root-domains", `{"name":"fresh-example.com"}`, rdunitToken(t, "admin"))

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v; body: %s", err, w.Body.String())
	}
	if resp.Code != http.StatusCreated {
		t.Errorf("expected envelope code 201, got %d", resp.Code)
	}
	if resp.Message != "root domain created" {
		t.Errorf("expected envelope message 'root domain created', got %q", resp.Message)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be an object, got %T", resp.Data)
	}
	if data["registrable_domain"] != "fresh-example.com" {
		t.Errorf("expected data.registrable_domain 'fresh-example.com', got %v", data["registrable_domain"])
	}
	if data["source"] != "manual" {
		t.Errorf("expected data.source 'manual', got %v", data["source"])
	}
}

// --- 2. Unknown id yields 404 (GET / PUT / DELETE / refresh) ---

// TestRootDomainHandler_UnknownID_NotFound verifies that read/update/delete/refresh
// on a non-existent id return HTTP 404 with the standard error envelope
// (requirement 8.4 delete round-trip; the missing-id mapping backs requirement 8).
func TestRootDomainHandler_UnknownID_NotFound(t *testing.T) {
	_, r := rdunitSetup(t)
	token := rdunitToken(t, "admin")

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"get", http.MethodGet, "/api/root-domains/does-not-exist", ""},
		{"update", http.MethodPut, "/api/root-domains/does-not-exist", `{"monitor_enabled":false}`},
		{"delete", http.MethodDelete, "/api/root-domains/does-not-exist", ""},
		{"refresh", http.MethodPost, "/api/root-domains/does-not-exist/refresh", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := rdunitDo(t, r, tc.method, tc.path, tc.body, token)

			if w.Code != http.StatusNotFound {
				t.Fatalf("%s %s: expected status 404, got %d; body: %s", tc.method, tc.path, w.Code, w.Body.String())
			}

			var resp model.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal error response: %v; body: %s", err, w.Body.String())
			}
			if resp.Code != http.StatusNotFound {
				t.Errorf("expected error envelope code 404, got %d", resp.Code)
			}
			if resp.Message == "" {
				t.Error("expected a non-empty error message")
			}
		})
	}
}

// --- 3. Readonly-session write operations are intercepted with 403 ---

// TestRootDomainHandler_Readonly_ForbidsWrites verifies that with the real
// middleware chain active (Auth + Audit + Readonly, as wired by RegisterRoutes), a
// readonly session is blocked from every write operation with HTTP 403. Readonly is
// determined by role == "readonly" in the JWT claims (there is no separate runtime
// flag); write endpoints are not on the readonly whitelist, so they are rejected
// before ever reaching the handler (requirement 9.1 / design error-handling table).
func TestRootDomainHandler_Readonly_ForbidsWrites(t *testing.T) {
	repo, r := rdunitSetup(t)
	seeded := rdunitSeed(t, repo, "example.com", "example.com", "manual")
	token := rdunitToken(t, "readonly")

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create", http.MethodPost, "/api/root-domains", `{"name":"new-domain.com"}`},
		{"import", http.MethodPost, "/api/root-domains/import", `{"api_token":"tok"}`},
		{"update", http.MethodPut, "/api/root-domains/" + seeded.ID, `{"monitor_enabled":false}`},
		{"delete", http.MethodDelete, "/api/root-domains/" + seeded.ID, ""},
		{"refresh", http.MethodPost, "/api/root-domains/" + seeded.ID + "/refresh", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := rdunitDo(t, r, tc.method, tc.path, tc.body, token)

			if w.Code != http.StatusForbidden {
				t.Fatalf("%s %s: expected status 403 for readonly session, got %d; body: %s",
					tc.method, tc.path, w.Code, w.Body.String())
			}

			var resp model.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal error response: %v; body: %s", err, w.Body.String())
			}
			if resp.Code != http.StatusForbidden {
				t.Errorf("expected error envelope code 403, got %d", resp.Code)
			}
		})
	}

	// Guard: the seeded record must NOT have been deleted/updated by the blocked
	// write ops (the middleware rejects them before the handler runs).
	if _, err := repo.GetByID(context.Background(), seeded.ID); err != nil {
		t.Errorf("expected seeded root domain to still exist after blocked writes, got error: %v", err)
	}
}

// --- 4. Creating a duplicate registrable domain yields 409 ---

// TestRootDomainHandler_Create_Duplicate_Conflict verifies POST /api/root-domains
// with a name whose registrable domain (eTLD+1) already exists returns HTTP 409 and
// keeps the existing record (requirement 3.4). The service returns service.ErrDuplicate,
// which the handler maps to 409.
func TestRootDomainHandler_Create_Duplicate_Conflict(t *testing.T) {
	repo, r := rdunitSetup(t)
	rdunitSeed(t, repo, "example.com", "example.com", "manual")

	w := rdunitDo(t, r, http.MethodPost, "/api/root-domains", `{"name":"example.com"}`, rdunitToken(t, "admin"))

	if w.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal error response: %v; body: %s", err, w.Body.String())
	}
	if resp.Code != http.StatusConflict {
		t.Errorf("expected error envelope code 409, got %d", resp.Code)
	}

	// The existing record is preserved and no duplicate is created.
	items, total, err := repo.ListWithSort(context.Background(), model.RootDomainListParams{}, 14)
	if err != nil {
		t.Fatalf("failed to list root domains: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Errorf("expected exactly 1 root domain after duplicate create, got total=%d len=%d", total, len(items))
	}
}
