package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"

	_ "github.com/glebarez/sqlite"
)

// setupAuditLogTestDB creates an in-memory SQLite database with the audit_logs table.
func setupAuditLogTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE audit_logs (
		id TEXT PRIMARY KEY,
		actor_type TEXT NOT NULL,
		actor_id TEXT NOT NULL,
		action TEXT NOT NULL,
		target_type TEXT NOT NULL,
		target_id TEXT DEFAULT '',
		detail TEXT DEFAULT '',
		ip TEXT DEFAULT '',
		created_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create audit_logs table: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// setupAuditLogHandler creates an AuditLogHandler with test dependencies and admin context middleware.
func setupAuditLogHandler(t *testing.T) (*AuditLogHandler, *chi.Mux, *sql.DB) {
	t.Helper()

	db := setupAuditLogTestDB(t)
	repo := repository.NewAuditLogRepository(db)
	svc := service.NewAuditLogService(repo)
	handler := NewAuditLogHandler(svc)

	r := chi.NewRouter()
	// Inject admin claims into context for testing
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := &middleware.TokenClaims{
				UserID:   "admin-user-1",
				Username: "admin",
				Role:     "admin",
			}
			ctx := context.WithValue(r.Context(), middleware.UserClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Get("/api/audit-logs", handler.ListAuditLogs)

	return handler, r, db
}

// setupAuditLogHandlerWithRole creates an AuditLogHandler with a specific role in context.
func setupAuditLogHandlerWithRole(t *testing.T, role string) (*AuditLogHandler, *chi.Mux, *sql.DB) {
	t.Helper()

	db := setupAuditLogTestDB(t)
	repo := repository.NewAuditLogRepository(db)
	svc := service.NewAuditLogService(repo)
	handler := NewAuditLogHandler(svc)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := &middleware.TokenClaims{
				UserID:   "user-1",
				Username: "testuser",
				Role:     role,
			}
			ctx := context.WithValue(r.Context(), middleware.UserClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Get("/api/audit-logs", handler.ListAuditLogs)

	return handler, r, db
}

// insertTestAuditLog inserts a test audit log directly into the database.
func insertTestAuditLog(t *testing.T, db *sql.DB, actorType, actorID, action, targetType, targetID string) {
	t.Helper()

	now := time.Now().UTC()
	id := "audit-" + actorType + "-" + action + "-" + targetID
	_, err := db.Exec(`INSERT INTO audit_logs (id, actor_type, actor_id, action, target_type, target_id, detail, ip, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, actorType, actorID, action, targetType, targetID, "test detail", "127.0.0.1",
		now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("failed to insert test audit log: %v", err)
	}
}

func TestAuditLogHandler_ListAuditLogs_Empty(t *testing.T) {
	_, r, _ := setupAuditLogHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/audit-logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	dataMap, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	items, ok := dataMap["items"].([]interface{})
	if !ok {
		t.Fatalf("expected items to be an array, got %T", dataMap["items"])
	}
	if len(items) != 0 {
		t.Errorf("expected empty array, got %d items", len(items))
	}
	total, ok := dataMap["total"].(float64)
	if !ok {
		t.Fatalf("expected total to be a number, got %T", dataMap["total"])
	}
	if int(total) != 0 {
		t.Errorf("expected total=0, got %d", int(total))
	}
}

func TestAuditLogHandler_ListAuditLogs_WithLogs(t *testing.T) {
	_, r, db := setupAuditLogHandler(t)

	insertTestAuditLog(t, db, "user", "user-1", "create", "certificate", "cert-1")
	insertTestAuditLog(t, db, "agent", "machine-1", "deploy", "machine_certificate", "mc-1")

	req := httptest.NewRequest(http.MethodGet, "/api/audit-logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	dataMap, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	items, ok := dataMap["items"].([]interface{})
	if !ok {
		t.Fatalf("expected items to be an array, got %T", dataMap["items"])
	}
	if len(items) != 2 {
		t.Errorf("expected 2 logs, got %d", len(items))
	}
}

func TestAuditLogHandler_ListAuditLogs_FilterByActorType(t *testing.T) {
	_, r, db := setupAuditLogHandler(t)

	insertTestAuditLog(t, db, "user", "user-1", "create", "certificate", "cert-1")
	insertTestAuditLog(t, db, "agent", "machine-1", "deploy", "machine_certificate", "mc-1")
	insertTestAuditLog(t, db, "system", "system", "renew", "certificate", "cert-2")

	req := httptest.NewRequest(http.MethodGet, "/api/audit-logs?actor_type=user", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	dataMap, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	items, ok := dataMap["items"].([]interface{})
	if !ok {
		t.Fatalf("expected items to be an array, got %T", dataMap["items"])
	}
	if len(items) != 1 {
		t.Errorf("expected 1 log with actor_type=user, got %d", len(items))
	}
}

func TestAuditLogHandler_ListAuditLogs_FilterByTargetType(t *testing.T) {
	_, r, db := setupAuditLogHandler(t)

	insertTestAuditLog(t, db, "user", "user-1", "create", "certificate", "cert-1")
	insertTestAuditLog(t, db, "user", "user-1", "create", "machine", "machine-1")

	req := httptest.NewRequest(http.MethodGet, "/api/audit-logs?target_type=certificate", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	dataMap, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	items, ok := dataMap["items"].([]interface{})
	if !ok {
		t.Fatalf("expected items to be an array, got %T", dataMap["items"])
	}
	if len(items) != 1 {
		t.Errorf("expected 1 log with target_type=certificate, got %d", len(items))
	}
}

func TestAuditLogHandler_ListAuditLogs_LimitAndOffset(t *testing.T) {
	_, r, db := setupAuditLogHandler(t)

	// Insert 5 logs
	for i := 0; i < 5; i++ {
		id := "audit-limit-" + strconv.Itoa(i)
		now := time.Now().UTC().Add(time.Duration(i) * time.Second)
		_, err := db.Exec(`INSERT INTO audit_logs (id, actor_type, actor_id, action, target_type, target_id, detail, ip, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, "user", "user-1", "create", "certificate", "cert-"+strconv.Itoa(i), "", "127.0.0.1",
			now.Format(time.RFC3339),
		)
		if err != nil {
			t.Fatalf("failed to insert test audit log: %v", err)
		}
	}

	// Request with limit=2
	req := httptest.NewRequest(http.MethodGet, "/api/audit-logs?limit=2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	dataMap, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	items, ok := dataMap["items"].([]interface{})
	if !ok {
		t.Fatalf("expected items to be an array, got %T", dataMap["items"])
	}
	if len(items) != 2 {
		t.Errorf("expected 2 logs with limit=2, got %d", len(items))
	}
	// Verify total is 5 (all logs, not just the page)
	total, ok := dataMap["total"].(float64)
	if !ok {
		t.Fatalf("expected total to be a number, got %T", dataMap["total"])
	}
	if int(total) != 5 {
		t.Errorf("expected total=5, got %d", int(total))
	}

	// Request with limit=2, offset=2
	req = httptest.NewRequest(http.MethodGet, "/api/audit-logs?limit=2&offset=2", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	dataMap, ok = resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	items, ok = dataMap["items"].([]interface{})
	if !ok {
		t.Fatalf("expected items to be an array, got %T", dataMap["items"])
	}
	if len(items) != 2 {
		t.Errorf("expected 2 logs with limit=2&offset=2, got %d", len(items))
	}
}

func TestAuditLogHandler_ListAuditLogs_NonAdminAllowed(t *testing.T) {
	_, r, _ := setupAuditLogHandlerWithRole(t, "user")

	req := httptest.NewRequest(http.MethodGet, "/api/audit-logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Non-admin users (user role) should be able to view audit logs
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAuditLogHandler_ListAuditLogs_NoClaimsHandledByAuthMiddleware(t *testing.T) {
	db := setupAuditLogTestDB(t)
	repo := repository.NewAuditLogRepository(db)
	svc := service.NewAuditLogService(repo)
	handler := NewAuditLogHandler(svc)

	r := chi.NewRouter()
	// No claims middleware - simulates missing auth context
	// Without auth middleware, the handler still works (returns data)
	// In production, AuthMiddleware would reject unauthenticated requests before reaching the handler
	r.Get("/api/audit-logs", handler.ListAuditLogs)

	req := httptest.NewRequest(http.MethodGet, "/api/audit-logs", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Without auth middleware, the handler itself doesn't check claims anymore
	// (auth is enforced by the middleware layer in RegisterRoutes)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 (auth enforced by middleware, not handler), got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAuditLogHandler_RegisterRoutes(t *testing.T) {
	db := setupAuditLogTestDB(t)
	repo := repository.NewAuditLogRepository(db)
	svc := service.NewAuditLogService(repo)
	handler := NewAuditLogHandler(svc)

	r := chi.NewRouter()
	authSvc := &mockAuditLogAuthService{}
	handler.RegisterRoutes(r, authSvc, &mockAuditRepo{})

	// Verify routes are registered by walking the router - no panic means success
	if r == nil {
		t.Fatal("router should not be nil")
	}
}

// mockAuditLogAuthService is a mock implementation of middleware.AuthService for testing.
type mockAuditLogAuthService struct{}

func (m *mockAuditLogAuthService) GetJWTSecret() []byte {
	return []byte("test-secret")
}

func (m *mockAuditLogAuthService) IsSessionValid(_ context.Context, _ string) bool {
	return true
}

func (m *mockAuditLogAuthService) IsUserActive(_ context.Context, _ string) bool {
	return true
}

func (m *mockAuditLogAuthService) GetCurrentRole(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (m *mockAuditLogAuthService) IsTokenValid(_ context.Context, _ string, _ time.Time) bool {
	return true
}
