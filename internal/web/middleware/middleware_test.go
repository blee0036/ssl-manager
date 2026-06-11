package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ssl-manager/ssl-manager/internal/model"
)

// --- Mock implementations ---

type mockAuthService struct {
	secret          []byte
	validSessions   map[string]bool
	activeUsers     map[string]bool
	userRoles       map[string]string
}

func (m *mockAuthService) GetJWTSecret() []byte {
	return m.secret
}

func (m *mockAuthService) IsSessionValid(_ context.Context, sessionID string) bool {
	if m.validSessions == nil {
		return true
	}
	valid, exists := m.validSessions[sessionID]
	return exists && valid
}

func (m *mockAuthService) IsUserActive(_ context.Context, userID string) bool {
	if m.activeUsers == nil {
		return true
	}
	active, exists := m.activeUsers[userID]
	return exists && active
}

func (m *mockAuthService) GetCurrentRole(_ context.Context, userID string) (string, error) {
	if m.userRoles == nil {
		return "", fmt.Errorf("no roles configured")
	}
	role, exists := m.userRoles[userID]
	if !exists {
		return "", fmt.Errorf("user not found")
	}
	return role, nil
}

func (m *mockAuthService) IsTokenValid(_ context.Context, _ string, _ time.Time) bool {
	return true
}

type mockMachineRepo struct {
	machines        map[string]*model.Machine // keyed by token hash (non-revoked)
	allMachines     map[string]*model.Machine // keyed by token hash (including revoked)
}

func (m *mockMachineRepo) GetByTokenHash(_ context.Context, tokenHash string) (*model.Machine, error) {
	machine, ok := m.machines[tokenHash]
	if !ok {
		return nil, nil
	}
	return machine, nil
}

func (m *mockMachineRepo) GetByTokenHashIncludingRevoked(_ context.Context, tokenHash string) (*model.Machine, error) {
	if m.allMachines != nil {
		machine, ok := m.allMachines[tokenHash]
		if !ok {
			return nil, nil
		}
		return machine, nil
	}
	// Fallback to non-revoked machines if allMachines not set
	machine, ok := m.machines[tokenHash]
	if !ok {
		return nil, nil
	}
	return machine, nil
}

type mockAgentAlertSender struct {
	alerts []struct {
		Level      string
		AlertType  string
		Title      string
		Content    string
		TargetType string
		TargetID   string
	}
}

func (m *mockAgentAlertSender) SendAlert(_ context.Context, level, alertType, title, content, targetType, targetID string) error {
	m.alerts = append(m.alerts, struct {
		Level      string
		AlertType  string
		Title      string
		Content    string
		TargetType string
		TargetID   string
	}{level, alertType, title, content, targetType, targetID})
	return nil
}

func (m *mockAgentAlertSender) AutoResolve(_ context.Context, _, _, _ string) {}

func (m *mockAgentAlertSender) SuppressActiveByTarget(_ context.Context, _, _ string) error {
	return nil
}

type mockAuditRepo struct {
	logs []*model.AuditLog
}

func (m *mockAuditRepo) CreateAuditLog(_ context.Context, log *model.AuditLog) error {
	m.logs = append(m.logs, log)
	return nil
}

// --- Helper functions ---

func createTestToken(secret []byte, claims map[string]interface{}) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(claims))
	tokenStr, _ := token.SignedString(secret)
	return tokenStr
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

// --- AuthMiddleware Tests ---

func TestAuthMiddleware_NoHeader(t *testing.T) {
	svc := &mockAuthService{secret: []byte("test-secret")}
	handler := AuthMiddleware(svc)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_InvalidFormat(t *testing.T) {
	svc := &mockAuthService{secret: []byte("test-secret")}
	handler := AuthMiddleware(svc)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Basic abc123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	svc := &mockAuthService{secret: []byte("test-secret")}
	handler := AuthMiddleware(svc)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	secret := []byte("test-secret")
	svc := &mockAuthService{secret: secret}

	tokenStr := createTestToken(secret, map[string]interface{}{
		"user_id":    "user-1",
		"username":   "admin",
		"role":       "admin",
		"session_id": "sess-1",
		"iat":        float64(time.Now().Unix()),
		"exp":        float64(time.Now().Add(time.Hour).Unix()),
	})

	var capturedClaims *TokenClaims
	handler := AuthMiddleware(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedClaims = GetUserClaims(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if capturedClaims == nil {
		t.Fatal("expected claims in context")
	}
	if capturedClaims.UserID != "user-1" {
		t.Errorf("expected user_id 'user-1', got '%s'", capturedClaims.UserID)
	}
	if capturedClaims.Role != "admin" {
		t.Errorf("expected role 'admin', got '%s'", capturedClaims.Role)
	}
}

func TestAuthMiddleware_InvalidatedSession(t *testing.T) {
	secret := []byte("test-secret")
	svc := &mockAuthService{
		secret:        secret,
		validSessions: map[string]bool{"sess-1": false},
	}

	tokenStr := createTestToken(secret, map[string]interface{}{
		"user_id":    "user-1",
		"username":   "admin",
		"role":       "admin",
		"session_id": "sess-1",
		"iat":        float64(time.Now().Unix()),
		"exp":        float64(time.Now().Add(time.Hour).Unix()),
	})

	handler := AuthMiddleware(svc)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// --- RoleMiddleware Tests ---

func TestRoleMiddleware_AllowedRole(t *testing.T) {
	handler := RoleMiddleware("admin")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{Role: "admin"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRoleMiddleware_DeniedRole(t *testing.T) {
	handler := RoleMiddleware("admin")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{Role: "user"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestRoleMiddleware_NoClaims(t *testing.T) {
	handler := RoleMiddleware("admin")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestRoleMiddleware_MultipleRoles(t *testing.T) {
	handler := RoleMiddleware("admin", "user")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{Role: "user"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// --- ReadonlyMiddleware Tests ---

func TestReadonlyMiddleware_NonReadonlyUser(t *testing.T) {
	handler := ReadonlyMiddleware()(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/certificates", nil)
	ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{Role: "admin"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestReadonlyMiddleware_AllowedGET(t *testing.T) {
	handler := ReadonlyMiddleware()(okHandler())

	tests := []struct {
		path string
	}{
		{"/api/certificates"},
		{"/api/certificates/abc-123"},
		{"/api/machines"},
		{"/api/machines/xyz"},
		{"/api/domains"},
		{"/api/alerts"},
		{"/api/audit-logs"},
		{"/api/dashboard"},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{Role: "readonly"})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: expected 200, got %d", tt.path, rec.Code)
		}
	}
}

func TestReadonlyMiddleware_AllowedAuthEndpoints(t *testing.T) {
	handler := ReadonlyMiddleware()(okHandler())

	tests := []struct {
		method string
		path   string
	}{
		{"POST", "/api/auth/login"},
		{"POST", "/api/auth/logout"},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{Role: "readonly"})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("%s %s: expected 200, got %d", tt.method, tt.path, rec.Code)
		}
	}
}

func TestReadonlyMiddleware_BlockedWriteOps(t *testing.T) {
	handler := ReadonlyMiddleware()(okHandler())

	tests := []struct {
		method string
		path   string
	}{
		{"POST", "/api/certificates"},
		{"PUT", "/api/certificates/abc-123"},
		{"DELETE", "/api/machines/xyz"},
		{"POST", "/api/machines/xyz/token"},
		{"POST", "/api/certificates/abc/renew"},
		{"POST", "/api/domains/abc/probe"},
		{"POST", "/api/thirdpart-dns/abc/sync"},
		{"POST", "/api/alerts/test"},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{Role: "readonly"})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: expected 403, got %d", tt.method, tt.path, rec.Code)
		}
	}
}

func TestReadonlyMiddleware_BlockedDownload(t *testing.T) {
	handler := ReadonlyMiddleware()(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/agent/machine-certificates/abc-123/download", nil)
	ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{Role: "readonly"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestReadonlyMiddleware_BlockedSystemEndpoints(t *testing.T) {
	handler := ReadonlyMiddleware()(okHandler())

	// /api/system/* should be blocked for readonly users (both GET and write methods)
	tests := []struct {
		method string
		path   string
	}{
		{"GET", "/api/system/config"},
		{"PUT", "/api/system/config"},
		{"GET", "/api/system"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{Role: "readonly"})
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Errorf("%s %s: expected 403, got %d", tt.method, tt.path, rec.Code)
			}
		})
	}
}

// --- AuditMiddleware Tests ---

func TestAuditMiddleware_SkipsGET(t *testing.T) {
	repo := &mockAuditRepo{}
	handler := AuditMiddleware(repo)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/certificates", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	// Give async goroutine time to complete
	time.Sleep(50 * time.Millisecond)
	if len(repo.logs) != 0 {
		t.Errorf("expected no audit logs for GET, got %d", len(repo.logs))
	}
}

func TestAuditMiddleware_LogsPOST(t *testing.T) {
	repo := &mockAuditRepo{}
	handler := AuditMiddleware(repo)(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/certificates", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{
		UserID: "user-1",
		Role:   "admin",
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Give async goroutine time to complete
	time.Sleep(50 * time.Millisecond)

	if len(repo.logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(repo.logs))
	}
	log := repo.logs[0]
	if log.ActorType != "user" {
		t.Errorf("expected actor_type 'user', got '%s'", log.ActorType)
	}
	if log.ActorID != "user-1" {
		t.Errorf("expected actor_id 'user-1', got '%s'", log.ActorID)
	}
	if log.Action != "POST /api/certificates" {
		t.Errorf("expected action 'POST /api/certificates', got '%s'", log.Action)
	}
	if log.IP != "192.168.1.1" {
		t.Errorf("expected IP '192.168.1.1', got '%s'", log.IP)
	}
	if !strings.Contains(log.Detail, `"status":200`) {
		t.Errorf("expected detail to contain status:200, got '%s'", log.Detail)
	}
	if !strings.Contains(log.Detail, `"operation":"create_certificate"`) {
		t.Errorf("expected detail to contain operation:create_certificate, got '%s'", log.Detail)
	}
}

func TestAuditMiddleware_SkipsAuthEndpoints(t *testing.T) {
	repo := &mockAuditRepo{}
	handler := AuditMiddleware(repo)(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	time.Sleep(50 * time.Millisecond)
	if len(repo.logs) != 0 {
		t.Errorf("expected no audit logs for auth endpoints, got %d", len(repo.logs))
	}
}

func TestAuditMiddleware_HandlerOverridesAuditInfo(t *testing.T) {
	repo := &mockAuditRepo{}

	// Handler that sets explicit audit info (simulating a create handler)
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetAuditInfo(r, AuditInfo{
			TargetType: "machine_certificate",
			TargetID:   "mc-new-123",
			Operation:  "create_deployment_config",
		})
		w.WriteHeader(http.StatusCreated)
	})

	handler := AuditMiddleware(repo)(innerHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/machines/m-1/certificates", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	ctx := context.WithValue(req.Context(), UserClaimsKey, &TokenClaims{
		UserID:   "user-2",
		Username: "admin",
		Role:     "admin",
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Give async goroutine time to complete
	time.Sleep(50 * time.Millisecond)

	if len(repo.logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(repo.logs))
	}
	log := repo.logs[0]
	if log.TargetType != "machine_certificate" {
		t.Errorf("expected target_type 'machine_certificate', got '%s'", log.TargetType)
	}
	if log.TargetID != "mc-new-123" {
		t.Errorf("expected target_id 'mc-new-123', got '%s'", log.TargetID)
	}
	if !strings.Contains(log.Detail, `"operation":"create_deployment_config"`) {
		t.Errorf("expected detail to contain operation:create_deployment_config, got '%s'", log.Detail)
	}
}

func TestAuditMiddleware_AgentDeploymentLog(t *testing.T) {
	repo := &mockAuditRepo{}

	// Handler that sets audit info for agent deployment log
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetAuditInfo(r, AuditInfo{
			TargetType: "deployment_log",
			TargetID:   "mc-456",
			Operation:  "report_deployment",
		})
		w.WriteHeader(http.StatusOK)
	})

	handler := AuditMiddleware(repo)(innerHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/deployment-logs", nil)
	req.RemoteAddr = "172.16.0.5:8080"
	ctx := context.WithValue(req.Context(), MachineKey, &model.Machine{ID: "machine-abc", Name: "web-server-1"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	time.Sleep(50 * time.Millisecond)

	if len(repo.logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(repo.logs))
	}
	log := repo.logs[0]
	if log.ActorType != "agent" {
		t.Errorf("expected actor_type 'agent', got '%s'", log.ActorType)
	}
	if log.ActorID != "machine-abc" {
		t.Errorf("expected actor_id 'machine-abc', got '%s'", log.ActorID)
	}
	if log.TargetType != "deployment_log" {
		t.Errorf("expected target_type 'deployment_log', got '%s'", log.TargetType)
	}
	if log.TargetID != "mc-456" {
		t.Errorf("expected target_id 'mc-456', got '%s'", log.TargetID)
	}
	if !strings.Contains(log.Detail, `"operation":"report_deployment"`) {
		t.Errorf("expected detail to contain operation:report_deployment, got '%s'", log.Detail)
	}
}

// --- AgentAuthMiddleware Tests ---

func TestAgentAuthMiddleware_NoHeader(t *testing.T) {
	repo := &mockMachineRepo{machines: map[string]*model.Machine{}}
	handler := AgentAuthMiddleware(repo, nil)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/agent/heartbeat", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAgentAuthMiddleware_InvalidToken(t *testing.T) {
	repo := &mockMachineRepo{machines: map[string]*model.Machine{}}
	handler := AgentAuthMiddleware(repo, nil)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/agent/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAgentAuthMiddleware_ValidToken(t *testing.T) {
	token := "my-agent-token-123"
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	machine := &model.Machine{
		ID:   "machine-1",
		Name: "test-machine",
	}

	repo := &mockMachineRepo{
		machines: map[string]*model.Machine{tokenHash: machine},
	}

	var capturedMachine *model.Machine
	handler := AgentAuthMiddleware(repo, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMachine = GetMachine(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/agent/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if capturedMachine == nil {
		t.Fatal("expected machine in context")
	}
	if capturedMachine.ID != "machine-1" {
		t.Errorf("expected machine ID 'machine-1', got '%s'", capturedMachine.ID)
	}
}

func TestAgentAuthMiddleware_MachineIDMismatch(t *testing.T) {
	token := "my-agent-token-123"
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	machine := &model.Machine{
		ID:   "machine-1",
		Name: "test-machine",
	}

	repo := &mockMachineRepo{
		machines: map[string]*model.Machine{tokenHash: machine},
	}

	handler := AgentAuthMiddleware(repo, nil)(okHandler())

	// Request with a different machine_id in the URL
	req := httptest.NewRequest(http.MethodGet, "/api/agent/machines/machine-2/certificates", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAgentAuthMiddleware_MachineIDMatch(t *testing.T) {
	token := "my-agent-token-123"
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	machine := &model.Machine{
		ID:   "machine-1",
		Name: "test-machine",
	}

	repo := &mockMachineRepo{
		machines: map[string]*model.Machine{tokenHash: machine},
	}

	handler := AgentAuthMiddleware(repo, nil)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/agent/machines/machine-1/certificates", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// --- Context Helper Tests ---

func TestGetUserClaims_NilContext(t *testing.T) {
	ctx := context.Background()
	claims := GetUserClaims(ctx)
	if claims != nil {
		t.Error("expected nil claims from empty context")
	}
}

func TestGetMachine_NilContext(t *testing.T) {
	ctx := context.Background()
	machine := GetMachine(ctx)
	if machine != nil {
		t.Error("expected nil machine from empty context")
	}
}

// --- HashToken Tests ---

func TestHashToken(t *testing.T) {
	token := "test-token-123"
	expected := sha256.Sum256([]byte(token))
	expectedHex := hex.EncodeToString(expected[:])

	result := HashToken(token)
	if result != expectedHex {
		t.Errorf("expected %s, got %s", expectedHex, result)
	}
}

// --- extractTargetFromPath Tests ---

func TestExtractTargetFromPath(t *testing.T) {
	tests := []struct {
		path       string
		wantType   string
		wantID     string
	}{
		{"/api/certificates/abc-123", "certificate", "abc-123"},
		{"/api/machines/xyz", "machine", "xyz"},
		{"/api/users/user-1", "user", "user-1"},
		{"/api/certificates", "certificate", ""},
		{"/api/thirdpart-dns/dns-1", "thirdpart_dns", "dns-1"},
		// Nested resource: POST /api/machines/{id}/certificates (create deployment config)
		{"/api/machines/m-1/certificates", "machine_certificate", ""},
		// Nested resource: PUT/DELETE /api/machines/{id}/certificates/{mc_id}
		{"/api/machines/m-1/certificates/mc-1", "machine_certificate", "mc-1"},
		// Nested resource: POST /api/machines/{id}/certificates/{mc_id}/deploy
		{"/api/machines/m-1/certificates/mc-1/deploy", "machine_certificate", "mc-1"},
		// Agent deployment logs
		{"/api/agent/deployment-logs", "deployment_log", ""},
	}

	for _, tt := range tests {
		gotType, gotID := extractTargetFromPath(tt.path)
		if gotType != tt.wantType {
			t.Errorf("extractTargetFromPath(%s): type = %s, want %s", tt.path, gotType, tt.wantType)
		}
		if gotID != tt.wantID {
			t.Errorf("extractTargetFromPath(%s): id = %s, want %s", tt.path, gotID, tt.wantID)
		}
	}
}

// --- extractMachineIDFromPath Tests ---

func TestExtractMachineIDFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/api/agent/machines/machine-1/certificates", "machine-1"},
		{"/api/agent/machines/abc-xyz/certificates", "abc-xyz"},
		{"/api/agent/heartbeat", ""},
		{"/api/agent/machine-certificates/mc-1/download", ""},
		{"/api/agent/deployment-logs", ""},
	}

	for _, tt := range tests {
		got := extractMachineIDFromPath(tt.path)
		if got != tt.want {
			t.Errorf("extractMachineIDFromPath(%s) = %s, want %s", tt.path, got, tt.want)
		}
	}
}

// --- writeJSON Tests ---

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]interface{}{"code": 200, "message": "ok"})

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", ct)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["message"] != "ok" {
		t.Errorf("expected message 'ok', got '%v'", resp["message"])
	}
}
