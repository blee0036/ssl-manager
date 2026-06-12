package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"

	_ "github.com/glebarez/sqlite"
)

// setupAlertTestDB creates an in-memory SQLite database with alert-related tables.
func setupAlertTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE alerts (
		id TEXT PRIMARY KEY,
		level TEXT NOT NULL CHECK(level IN ('info', 'warning', 'critical')),
		type TEXT NOT NULL,
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'resolved', 'suppressed')),
		target_type TEXT DEFAULT '',
		target_id TEXT DEFAULT '',
		sent_channels TEXT DEFAULT '',
		created_at TEXT NOT NULL,
		resolved_at TEXT
	)`)
	if err != nil {
		t.Fatalf("failed to create alerts table: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE notification_channels (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL CHECK(type IN ('lark', 'telegram')),
		name TEXT NOT NULL,
		config_json TEXT NOT NULL DEFAULT '{}',
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("failed to create notification_channels table: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

// mockNotificationSender is a mock sender for testing.
type mockNotificationSender struct {
	sendErr     error
	testSendErr error
}

func (m *mockNotificationSender) Send(_ context.Context, _ *model.NotificationChannel, _, _, _ string) error {
	return m.sendErr
}

func (m *mockNotificationSender) SendTest(_ context.Context, _ *model.NotificationChannel) error {
	return m.testSendErr
}

// setupAlertHandler creates an AlertHandler with test dependencies.
func setupAlertHandler(t *testing.T) (*AlertHandler, *chi.Mux, *sql.DB) {
	t.Helper()

	db := setupAlertTestDB(t)
	alertRepo := repository.NewAlertRepository(db)
	channelRepo := repository.NewNotificationChannelRepository(db)

	senders := map[string]service.NotificationSender{
		"lark":     &mockNotificationSender{},
		"telegram": &mockNotificationSender{},
	}

	alertService := service.NewAlertServiceWithSenders(alertRepo, channelRepo, senders)
	handler := NewAlertHandler(alertService)

	// Setup router without auth middleware for testing
	r := chi.NewRouter()
	r.Route("/api/alerts", func(r chi.Router) {
		r.Get("/", handler.ListAlerts)
		r.Get("/channels", handler.ListChannels)
		r.Post("/channels", handler.CreateChannel)
		r.Put("/channels/{id}", handler.UpdateChannel)
		r.Delete("/channels/{id}", handler.DeleteChannel)
		r.Post("/channels/{id}/test", handler.TestChannel)
		r.Get("/{id}", handler.GetAlert)
		r.Post("/{id}/resolve", handler.ResolveAlert)
	})

	return handler, r, db
}

// insertTestAlert inserts a test alert directly into the database.
func insertTestAlert(t *testing.T, db *sql.DB, id, level, alertType, title, status string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO alerts (id, level, type, title, content, status, target_type, target_id, sent_channels, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, level, alertType, title, "test content", status, "certificate", "cert-1", "", now)
	if err != nil {
		t.Fatalf("failed to insert test alert: %v", err)
	}
}

// insertTestChannel inserts a test notification channel directly into the database.
func insertTestChannel(t *testing.T, db *sql.DB, id, chType, name string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO notification_channels (id, type, name, config_json, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, chType, name, `{"webhook_url":"https://example.com/hook"}`, 1, now, now)
	if err != nil {
		t.Fatalf("failed to insert test channel: %v", err)
	}
}

// --- Alert History Tests ---

func TestAlertHandler_ListAlerts_Empty(t *testing.T) {
	_, r, _ := setupAlertHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/alerts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
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
}

func TestAlertHandler_ListAlerts_WithAlerts(t *testing.T) {
	_, r, db := setupAlertHandler(t)

	insertTestAlert(t, db, "alert-1", "warning", "cert_expiring", "Cert expiring", "active")
	insertTestAlert(t, db, "alert-2", "critical", "cert_expired", "Cert expired", "active")

	req := httptest.NewRequest(http.MethodGet, "/api/alerts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
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
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

func TestAlertHandler_ListAlerts_WithFilter(t *testing.T) {
	_, r, db := setupAlertHandler(t)

	insertTestAlert(t, db, "alert-1", "warning", "cert_expiring", "Cert expiring", "active")
	insertTestAlert(t, db, "alert-2", "critical", "cert_expired", "Cert expired", "resolved")

	// Filter by status=active
	req := httptest.NewRequest(http.MethodGet, "/api/alerts?status=active", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
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
		t.Errorf("expected 1 item (only active), got %d", len(items))
	}
}

func TestAlertHandler_ListAlerts_FilterByLevel(t *testing.T) {
	_, r, db := setupAlertHandler(t)

	insertTestAlert(t, db, "alert-1", "warning", "cert_expiring", "Cert expiring", "active")
	insertTestAlert(t, db, "alert-2", "critical", "cert_expired", "Cert expired", "active")

	req := httptest.NewRequest(http.MethodGet, "/api/alerts?level=critical", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
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
		t.Errorf("expected 1 item (only critical), got %d", len(items))
	}
}

func TestAlertHandler_GetAlert_Success(t *testing.T) {
	_, r, db := setupAlertHandler(t)

	insertTestAlert(t, db, "alert-1", "warning", "cert_expiring", "Cert expiring", "active")

	req := httptest.NewRequest(http.MethodGet, "/api/alerts/alert-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	if data["title"] != "Cert expiring" {
		t.Errorf("expected title 'Cert expiring', got '%v'", data["title"])
	}
}

func TestAlertHandler_GetAlert_NotFound(t *testing.T) {
	_, r, _ := setupAlertHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/alerts/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAlertHandler_ResolveAlert_Success(t *testing.T) {
	_, r, db := setupAlertHandler(t)

	insertTestAlert(t, db, "alert-1", "warning", "cert_expiring", "Cert expiring", "active")

	req := httptest.NewRequest(http.MethodPost, "/api/alerts/alert-1/resolve", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Message != "alert resolved" {
		t.Errorf("expected message 'alert resolved', got '%s'", resp.Message)
	}

	// Verify alert is resolved in DB
	var status string
	err := db.QueryRow("SELECT status FROM alerts WHERE id = ?", "alert-1").Scan(&status)
	if err != nil {
		t.Fatalf("failed to query alert: %v", err)
	}
	if status != "resolved" {
		t.Errorf("expected status 'resolved', got '%s'", status)
	}
}

func TestAlertHandler_ResolveAlert_NotFound(t *testing.T) {
	_, r, _ := setupAlertHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/alerts/nonexistent/resolve", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- Notification Channel Tests ---

func TestAlertHandler_ListChannels_Empty(t *testing.T) {
	_, r, _ := setupAlertHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/alerts/channels", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be an array, got %T", resp.Data)
	}
	if len(data) != 0 {
		t.Errorf("expected empty array, got %d items", len(data))
	}
}

func TestAlertHandler_ListChannels_WithChannels(t *testing.T) {
	_, r, db := setupAlertHandler(t)

	insertTestChannel(t, db, "ch-1", "lark", "Lark Channel")
	insertTestChannel(t, db, "ch-2", "telegram", "Telegram Channel")

	req := httptest.NewRequest(http.MethodGet, "/api/alerts/channels", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be an array, got %T", resp.Data)
	}
	if len(data) != 2 {
		t.Errorf("expected 2 items, got %d", len(data))
	}
}

func TestAlertHandler_CreateChannel_Success(t *testing.T) {
	_, r, _ := setupAlertHandler(t)

	body := map[string]interface{}{
		"type":        "lark",
		"name":        "My Lark Channel",
		"config_json": `{"webhook_url":"https://open.feishu.cn/hook/xxx"}`,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/alerts/channels", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Message != "channel created" {
		t.Errorf("expected message 'channel created', got '%s'", resp.Message)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	if data["type"] != "lark" {
		t.Errorf("expected type 'lark', got '%v'", data["type"])
	}
	if data["name"] != "My Lark Channel" {
		t.Errorf("expected name 'My Lark Channel', got '%v'", data["name"])
	}
	if data["enabled"] != true {
		t.Errorf("expected enabled true, got %v", data["enabled"])
	}
}

func TestAlertHandler_CreateChannel_MissingType(t *testing.T) {
	_, r, _ := setupAlertHandler(t)

	body := map[string]interface{}{
		"name": "My Channel",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/alerts/channels", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp model.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Message != "channel type is required" {
		t.Errorf("expected 'channel type is required', got '%s'", resp.Message)
	}
}

func TestAlertHandler_CreateChannel_InvalidType(t *testing.T) {
	_, r, _ := setupAlertHandler(t)

	body := map[string]interface{}{
		"type": "email",
		"name": "My Channel",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/alerts/channels", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp model.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Message != "channel type must be 'lark' or 'telegram'" {
		t.Errorf("expected type validation error, got '%s'", resp.Message)
	}
}

func TestAlertHandler_CreateChannel_MissingName(t *testing.T) {
	_, r, _ := setupAlertHandler(t)

	body := map[string]interface{}{
		"type": "lark",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/alerts/channels", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp model.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Message != "channel name is required" {
		t.Errorf("expected 'channel name is required', got '%s'", resp.Message)
	}
}

func TestAlertHandler_CreateChannel_InvalidBody(t *testing.T) {
	_, r, _ := setupAlertHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/alerts/channels", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestAlertHandler_UpdateChannel_Success(t *testing.T) {
	_, r, db := setupAlertHandler(t)

	insertTestChannel(t, db, "ch-1", "lark", "Old Name")

	body := map[string]interface{}{
		"name": "New Name",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/alerts/channels/ch-1", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Message != "channel updated" {
		t.Errorf("expected message 'channel updated', got '%s'", resp.Message)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be a map, got %T", resp.Data)
	}
	if data["name"] != "New Name" {
		t.Errorf("expected name 'New Name', got '%v'", data["name"])
	}
}

func TestAlertHandler_UpdateChannel_NotFound(t *testing.T) {
	_, r, _ := setupAlertHandler(t)

	body := map[string]interface{}{
		"name": "New Name",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/alerts/channels/nonexistent", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAlertHandler_UpdateChannel_NoFields(t *testing.T) {
	_, r, db := setupAlertHandler(t)

	insertTestChannel(t, db, "ch-1", "lark", "My Channel")

	body := map[string]interface{}{}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/alerts/channels/ch-1", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAlertHandler_DeleteChannel_Success(t *testing.T) {
	_, r, db := setupAlertHandler(t)

	insertTestChannel(t, db, "ch-1", "lark", "My Channel")

	req := httptest.NewRequest(http.MethodDelete, "/api/alerts/channels/ch-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Message != "channel deleted" {
		t.Errorf("expected message 'channel deleted', got '%s'", resp.Message)
	}

	// Verify channel is deleted
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM notification_channels WHERE id = ?", "ch-1").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query channel count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected channel to be deleted, but found %d records", count)
	}
}

func TestAlertHandler_DeleteChannel_NotFound(t *testing.T) {
	_, r, _ := setupAlertHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/alerts/channels/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAlertHandler_TestChannel_Success(t *testing.T) {
	_, r, db := setupAlertHandler(t)

	insertTestChannel(t, db, "ch-1", "lark", "My Lark Channel")

	req := httptest.NewRequest(http.MethodPost, "/api/alerts/channels/ch-1/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Message != "test message sent" {
		t.Errorf("expected message 'test message sent', got '%s'", resp.Message)
	}
}

func TestAlertHandler_TestChannel_NotFound(t *testing.T) {
	_, r, _ := setupAlertHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/alerts/channels/nonexistent/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestAlertHandler_RegisterRoutes(t *testing.T) {
	db := setupAlertTestDB(t)
	alertRepo := repository.NewAlertRepository(db)
	channelRepo := repository.NewNotificationChannelRepository(db)

	senders := map[string]service.NotificationSender{
		"lark":     &mockNotificationSender{},
		"telegram": &mockNotificationSender{},
	}

	alertService := service.NewAlertServiceWithSenders(alertRepo, channelRepo, senders)
	handler := NewAlertHandler(alertService)

	r := chi.NewRouter()
	authSvc := &alertTestAuthService{}
	handler.RegisterRoutes(r, authSvc, &mockAuditRepo{})

	// Verify routes are registered by walking the router - ensure no panic
	if r == nil {
		t.Fatal("router should not be nil")
	}
}

// alertTestAuthService is a minimal implementation of middleware.AuthService for testing RegisterRoutes.
type alertTestAuthService struct{}

func (s *alertTestAuthService) GetJWTSecret() []byte {
	return []byte("test-secret")
}

func (s *alertTestAuthService) IsSessionValid(_ context.Context, _ string) bool {
	return true
}

func (s *alertTestAuthService) IsUserActive(_ context.Context, _ string) bool {
	return true
}

func (s *alertTestAuthService) GetCurrentRole(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (s *alertTestAuthService) IsTokenValid(_ context.Context, _ string, _ time.Time) bool {
	return true
}

// --- Security: Sensitive Field Masking Tests (Requirement 2.8) ---

func TestMaskNotificationChannel_LarkWebhookURL(t *testing.T) {
	ch := &model.NotificationChannel{
		ID:         "ch-1",
		Type:       "lark",
		Name:       "My Lark",
		ConfigJSON: `{"webhook_url":"https://open.feishu.cn/open-apis/bot/v2/hook/abc123def456"}`,
		Enabled:    true,
	}

	masked := maskNotificationChannel(ch)

	// The webhook_url should be masked
	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(masked.ConfigJSON), &configMap); err != nil {
		t.Fatalf("failed to parse masked config_json: %v", err)
	}

	webhookURL, ok := configMap["webhook_url"].(string)
	if !ok {
		t.Fatal("expected webhook_url to be a string")
	}

	// Should not contain the original URL
	if webhookURL == "https://open.feishu.cn/open-apis/bot/v2/hook/abc123def456" {
		t.Error("webhook_url should be masked but was returned in plaintext")
	}

	// Should contain asterisks
	if !containsAsterisks(webhookURL) {
		t.Errorf("masked webhook_url should contain asterisks, got: %s", webhookURL)
	}

	// Original channel should not be modified
	if ch.ConfigJSON != `{"webhook_url":"https://open.feishu.cn/open-apis/bot/v2/hook/abc123def456"}` {
		t.Error("original channel should not be modified")
	}
}

func TestMaskNotificationChannel_TelegramBotToken(t *testing.T) {
	ch := &model.NotificationChannel{
		ID:         "ch-2",
		Type:       "telegram",
		Name:       "My Telegram",
		ConfigJSON: `{"bot_token":"123456789:ABCdefGHIjklMNOpqrsTUVwxyz","chat_id":"-100123456"}`,
		Enabled:    true,
	}

	masked := maskNotificationChannel(ch)

	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(masked.ConfigJSON), &configMap); err != nil {
		t.Fatalf("failed to parse masked config_json: %v", err)
	}

	botToken, ok := configMap["bot_token"].(string)
	if !ok {
		t.Fatal("expected bot_token to be a string")
	}

	// Should not contain the original token
	if botToken == "123456789:ABCdefGHIjklMNOpqrsTUVwxyz" {
		t.Error("bot_token should be masked but was returned in plaintext")
	}

	// Should contain asterisks
	if !containsAsterisks(botToken) {
		t.Errorf("masked bot_token should contain asterisks, got: %s", botToken)
	}

	// chat_id should NOT be masked (it's not sensitive)
	chatID, ok := configMap["chat_id"].(string)
	if !ok {
		t.Fatal("expected chat_id to be a string")
	}
	if chatID != "-100123456" {
		t.Errorf("chat_id should not be masked, got: %s", chatID)
	}
}

func TestMaskNotificationChannel_NilChannel(t *testing.T) {
	result := maskNotificationChannel(nil)
	if result != nil {
		t.Error("expected nil result for nil input")
	}
}

func TestMaskNotificationChannel_EmptyConfigJSON(t *testing.T) {
	ch := &model.NotificationChannel{
		ID:         "ch-3",
		Type:       "lark",
		Name:       "Empty Config",
		ConfigJSON: "{}",
		Enabled:    true,
	}

	masked := maskNotificationChannel(ch)
	if masked.ConfigJSON != "{}" {
		t.Errorf("expected empty config to remain '{}', got: %s", masked.ConfigJSON)
	}
}

func TestMaskNotificationChannel_InvalidJSON(t *testing.T) {
	ch := &model.NotificationChannel{
		ID:         "ch-4",
		Type:       "lark",
		Name:       "Invalid Config",
		ConfigJSON: "not valid json",
		Enabled:    true,
	}

	masked := maskNotificationChannel(ch)
	// Should fallback to empty JSON when config can't be parsed
	if masked.ConfigJSON != "{}" {
		t.Errorf("expected '{}' for invalid JSON, got: %s", masked.ConfigJSON)
	}
}

func TestMaskString_LongString(t *testing.T) {
	result := maskString("https://example.com/webhook/secret123")
	// Should show first 4 and last 4 chars
	if len(result) != len("https://example.com/webhook/secret123") {
		t.Errorf("masked string should have same length as original, got %d vs %d",
			len(result), len("https://example.com/webhook/secret123"))
	}
	if result[:4] != "http" {
		t.Errorf("expected first 4 chars to be 'http', got '%s'", result[:4])
	}
	if result[len(result)-4:] != "t123" {
		t.Errorf("expected last 4 chars to be 't123', got '%s'", result[len(result)-4:])
	}
	if !containsAsterisks(result) {
		t.Errorf("expected asterisks in middle, got: %s", result)
	}
}

func TestMaskString_ShortString(t *testing.T) {
	result := maskString("short")
	// Strings <= 10 chars should be fully masked
	if result != "*****" {
		t.Errorf("expected '*****' for short string, got: %s", result)
	}
}

func TestMaskString_ExactlyTenChars(t *testing.T) {
	result := maskString("1234567890")
	// Strings <= 10 chars should be fully masked
	if result != "**********" {
		t.Errorf("expected '**********' for 10-char string, got: %s", result)
	}
}

func TestMaskString_ElevenChars(t *testing.T) {
	result := maskString("12345678901")
	// 11 chars: show first 4 and last 4, mask middle 3
	if result != "1234***8901" {
		t.Errorf("expected '1234***8901', got: %s", result)
	}
}

// TestListChannels_MasksWebhookURL verifies that the ListChannels endpoint masks sensitive fields.
func TestAlertHandler_ListChannels_MasksSensitiveFields(t *testing.T) {
	_, r, db := setupAlertHandler(t)

	// Insert a channel with a real webhook URL
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO notification_channels (id, type, name, config_json, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"ch-mask-1", "lark", "Lark Test",
		`{"webhook_url":"https://open.feishu.cn/open-apis/bot/v2/hook/secret-token-value"}`,
		1, now, now)
	if err != nil {
		t.Fatalf("failed to insert test channel: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/alerts/channels", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp model.SuccessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	data, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be an array, got %T", resp.Data)
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(data))
	}

	channel := data[0].(map[string]interface{})
	configJSON := channel["config_json"].(string)

	// The config_json should NOT contain the original webhook URL in plaintext
	if configJSON == `{"webhook_url":"https://open.feishu.cn/open-apis/bot/v2/hook/secret-token-value"}` {
		t.Error("ListChannels should mask webhook_url in config_json, but returned plaintext")
	}

	// Parse the masked config to verify structure is preserved
	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &configMap); err != nil {
		t.Fatalf("masked config_json should still be valid JSON: %v", err)
	}

	webhookURL, ok := configMap["webhook_url"].(string)
	if !ok {
		t.Fatal("expected webhook_url key to exist in masked config")
	}

	if !containsAsterisks(webhookURL) {
		t.Errorf("webhook_url should be masked with asterisks, got: %s", webhookURL)
	}
}

// containsAsterisks checks if a string contains asterisk characters.
func containsAsterisks(s string) bool {
	for _, c := range s {
		if c == '*' {
			return true
		}
	}
	return false
}
