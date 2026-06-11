package tests

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	_ "github.com/glebarez/sqlite"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/database"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/handler"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
)

// --- Test Infrastructure ---

// testApp holds all the components needed for integration testing.
type testApp struct {
	router     *chi.Mux
	db         *sql.DB
	dataDir    string
	jwtSecret  []byte
	cfg        *config.Config
	authSvc    *service.AuthService
	userRepo   *repository.UserRepository
	machineRepo *repository.MachineRepository
}

// authServiceAdapter adapts service.AuthService to the middleware.AuthService interface.
type authServiceAdapter struct {
	jwtSecret []byte
}

func (a *authServiceAdapter) GetJWTSecret() []byte {
	return a.jwtSecret
}

func (a *authServiceAdapter) IsSessionValid(_ context.Context, sessionID string) bool {
	return true
}

func (a *authServiceAdapter) IsUserActive(_ context.Context, userID string) bool {
	return true
}

func (a *authServiceAdapter) GetCurrentRole(_ context.Context, userID string) (string, error) {
	return "", nil
}

func (a *authServiceAdapter) IsTokenValid(_ context.Context, _ string, _ time.Time) bool {
	return true
}

// mockAlertSender is a no-op alert sender for testing.
type mockAlertSender struct{}

func (m *mockAlertSender) SendAlert(_ context.Context, _, _, _, _, _, _ string) error {
	return nil
}

func (m *mockAlertSender) AutoResolve(_ context.Context, _, _, _ string) {}

func (m *mockAlertSender) SuppressActiveByTarget(_ context.Context, _, _ string) error { return nil }

// setupTestApp creates a full application stack with real database and router for integration testing.
func setupTestApp(t *testing.T) *testApp {
	t.Helper()

	// Create temp data directory
	dataDir := t.TempDir()

	// Initialize database using the real database package
	db, err := database.NewDB(dataDir)
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	sqlDB := db.DB

	// JWT secret for testing
	jwtSecret := []byte("integration-test-jwt-secret-key!!")

	// Config
	cfg := config.DefaultConfig()
	cfg.Server.ExternalURL = "https://ssl-test.example.com"
	cfg.Server.ListenAddr = ":8443"
	cfg.Agent.HeartbeatTimeoutSeconds = 120
	cfg.Agent.PollIntervalSeconds = 30

	// Save config to file so system handler can read it
	configPath := filepath.Join(dataDir, "config.json")
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Initialize repositories
	userRepo := repository.NewUserRepository(sqlDB)
	machineRepo := repository.NewMachineRepository(sqlDB)
	certRepo := repository.NewCertificateRepository(sqlDB, dataDir)
	mcRepo := repository.NewMachineCertificateRepository(sqlDB)
	deployLogRepo := repository.NewDeploymentLogRepository(sqlDB)
	domainRepo := repository.NewDomainRepository(sqlDB)
	alertRepo := repository.NewAlertRepository(sqlDB)
	channelRepo := repository.NewNotificationChannelRepository(sqlDB)
	auditLogRepo := repository.NewAuditLogRepository(sqlDB)
	dnsRepo := repository.NewThirdpartDNSRepository(sqlDB)

	// Initialize services
	runtimeCfg := config.NewRuntimeConfig(cfg)
	authService := service.NewAuthService(userRepo, runtimeCfg, jwtSecret)
	initService := service.NewInitService(db, userRepo, configPath, runtimeCfg)
	machineService := service.NewMachineService(machineRepo, runtimeCfg)
	certService := service.NewCertificateService(certRepo, sqlDB)
	mcService := service.NewMachineCertificateService(mcRepo)
	deployLogService := service.NewDeploymentLogService(deployLogRepo)
	alertService := service.NewAlertService(alertRepo, channelRepo)
	auditLogService := service.NewAuditLogService(auditLogRepo)
	dashboardService := service.NewDashboardService(sqlDB)
	domainMonitorService := service.NewDomainMonitorService(domainRepo, certRepo, alertService, runtimeCfg)
	dnsService := service.NewThirdpartDNSService(dnsRepo, domainRepo, nil, alertService, runtimeCfg)

	// Auth adapter for middleware
	authAdapter := &authServiceAdapter{jwtSecret: jwtSecret}

	// Initialize handlers
	initHandler := handler.NewInitHandler(initService)
	machineHandler := handler.NewMachineHandler(machineService)
	certHandler := handler.NewCertificateHandler(certService, nil, nil, nil, dataDir)
	mcHandler := handler.NewMachineCertificateHandler(mcService)
	deployLogHandler := handler.NewDeploymentLogHandler(deployLogService)
	domainHandler := handler.NewDomainHandler(domainMonitorService)
	alertHandler := handler.NewAlertHandler(alertService)
	auditLogHandler := handler.NewAuditLogHandler(auditLogService)
	dashboardHandler := handler.NewDashboardHandler(dashboardService)
	systemHandler := handler.NewSystemHandler(configPath, runtimeCfg)
	dnsHandler := handler.NewThirdpartDNSHandler(dnsService)
	agentHandler := handler.NewAgentHandler(machineService, mcService, deployLogService, certRepo, mcRepo, alertService)
	installHandler := handler.NewInstallHandler(runtimeCfg, filepath.Join(dataDir, "bin"))

	// Setup router
	r := chi.NewRouter()

	// Init middleware
	r.Use(middleware.InitMiddleware(initService))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	// Register all routes
	initHandler.RegisterRoutes(r)
	installHandler.RegisterRoutes(r)
	machineHandler.RegisterRoutes(r, authAdapter, auditLogRepo)
	certHandler.RegisterRoutes(r, authAdapter, auditLogRepo)
	mcHandler.RegisterRoutes(r, authAdapter, auditLogRepo)
	deployLogHandler.RegisterRoutes(r, authAdapter, auditLogRepo)
	domainHandler.RegisterRoutes(r, authAdapter, auditLogRepo)
	alertHandler.RegisterRoutes(r, authAdapter, auditLogRepo)
	auditLogHandler.RegisterRoutes(r, authAdapter, auditLogRepo)
	dashboardHandler.RegisterRoutes(r, authAdapter, auditLogRepo)
	systemHandler.RegisterRoutes(r, authAdapter, auditLogRepo)
	dnsHandler.RegisterRoutes(r, authAdapter, auditLogRepo)
	agentHandler.RegisterRoutes(r, machineRepo, &mockAlertSender{}, auditLogRepo)

	// Auth login routes
	r.Post("/api/auth/login", createLoginHandler(authService))

	return &testApp{
		router:      r,
		db:          sqlDB,
		dataDir:     dataDir,
		jwtSecret:   jwtSecret,
		cfg:         cfg,
		authSvc:     authService,
		userRepo:    userRepo,
		machineRepo: machineRepo,
	}
}

// createLoginHandler creates the login endpoint handler for integration tests.
func createLoginHandler(authService *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Username       string `json:"username"`
			Password       string `json:"password"`
			TurnstileToken string `json:"turnstile_token"`
		}

		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"code":    400,
				"message": "invalid request body",
			})
			return
		}

		remoteIP := service.GetBestEffortRemoteIP(r)
		token, err := authService.Login(r.Context(), input.Username, input.Password, input.TurnstileToken, remoteIP)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"code":    401,
				"message": "invalid credentials",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"code":    200,
			"message": "login successful",
			"data": map[string]interface{}{
				"token": token,
			},
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// createTestAdmin creates an admin user for testing and returns the username and password.
func createTestAdmin(t *testing.T, app *testApp) (username, password string) {
	t.Helper()
	username = "admin"
	password = "admin123456"

	user := &model.User{
		Username:     username,
		PasswordHash: password, // UserRepository.Create will hash this
		Role:         "admin",
	}
	if err := app.userRepo.Create(context.Background(), user); err != nil {
		t.Fatalf("failed to create test admin: %v", err)
	}
	return username, password
}

// loginAndGetToken performs a login request and returns the JWT token.
func loginAndGetToken(t *testing.T, app *testApp, username, password string) string {
	t.Helper()

	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("login failed: status %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal login response: %v", err)
	}

	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("login response missing data field")
	}

	token, ok := data["token"].(string)
	if !ok || token == "" {
		t.Fatalf("login response missing token")
	}

	return token
}

// generateTestCert generates a self-signed certificate and private key for testing.
func generateTestCert(t *testing.T, domains []string) (certPEM, keyPEM []byte) {
	t.Helper()

	// Generate ECDSA private key
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate private key: %v", err)
	}

	// Create certificate template
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: domains[0],
		},
		DNSNames:              domains,
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true, // self-signed
	}

	// Self-sign the certificate
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privKey.PublicKey, privKey)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	// Encode certificate to PEM
	certPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Encode private key to PEM
	keyDER, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		t.Fatalf("failed to marshal private key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	})

	return certPEM, keyPEM
}

// --- Integration Test 1: Full Authentication Flow ---
// Validates: Requirement 2.1 - Login → get token → use token for authenticated requests

func TestIntegration_AuthenticationFlow(t *testing.T) {
	app := setupTestApp(t)

	// Step 1: Create admin user
	username, password := createTestAdmin(t, app)

	// Step 2: Login with correct credentials → should get token
	token := loginAndGetToken(t, app, username, password)
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Step 3: Use token to access authenticated endpoint (GET /api/certificates)
	req := httptest.NewRequest(http.MethodGet, "/api/certificates/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for authenticated request, got %d; body: %s", w.Code, w.Body.String())
	}

	// Step 4: Access without token → should get 401
	req = httptest.NewRequest(http.MethodGet, "/api/certificates/", nil)
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated request, got %d", w.Code)
	}

	// Step 5: Access with invalid token → should get 401
	req = httptest.NewRequest(http.MethodGet, "/api/certificates/", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-xyz")
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid token, got %d", w.Code)
	}

	// Step 6: Login with wrong password → should get 401
	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": "wrong-password",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong password, got %d", w.Code)
	}

	// Verify error message doesn't leak info
	var errResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if msg, ok := errResp["message"].(string); ok {
		if msg != "invalid credentials" {
			t.Errorf("expected generic error message, got: %s", msg)
		}
	}

	// Step 7: Login with non-existent user → should get same 401 message
	body, _ = json.Marshal(map[string]string{
		"username": "nonexistent_user",
		"password": "somepassword",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for non-existent user, got %d", w.Code)
	}
}

// --- Integration Test 2: Certificate Upload and Deployment Config Flow ---
// Validates: Requirement 5.1 - Certificate upload with PEM validation → create deployment config

func TestIntegration_CertificateUploadAndDeployConfig(t *testing.T) {
	app := setupTestApp(t)

	// Setup: Create admin and login
	username, password := createTestAdmin(t, app)
	token := loginAndGetToken(t, app, username, password)

	// Step 1: Upload a valid certificate
	certPEM, keyPEM := generateTestCert(t, []string{"example.com", "www.example.com"})

	certInput := model.CreateCertInput{
		Name:      "Test Certificate",
		CertPEM:   certPEM,
		KeyPEM:    keyPEM,
		AutoRenew: false,
	}
	body, _ := json.Marshal(certInput)

	req := httptest.NewRequest(http.MethodPost, "/api/certificates/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for certificate upload, got %d; body: %s", w.Code, w.Body.String())
	}

	var certResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &certResp)
	certData := certResp["data"].(map[string]interface{})
	certID := certData["id"].(string)

	if certID == "" {
		t.Fatal("expected non-empty certificate ID")
	}

	// Verify certificate metadata was parsed correctly
	domains := certData["domains"].([]interface{})
	if len(domains) == 0 {
		t.Error("expected domains to be populated")
	}
	if certData["fingerprint_sha256"] == "" {
		t.Error("expected fingerprint to be populated")
	}
	if certData["source"] != "upload" {
		t.Errorf("expected source 'upload', got '%v'", certData["source"])
	}

	// Step 2: Verify certificate files were saved to disk
	certDir := filepath.Join(app.dataDir, "certificates", certID)
	if _, err := os.Stat(filepath.Join(certDir, "cert.pem")); os.IsNotExist(err) {
		t.Error("cert.pem file not found on disk")
	}
	if _, err := os.Stat(filepath.Join(certDir, "privkey.pem")); os.IsNotExist(err) {
		t.Error("privkey.pem file not found on disk")
	}

	// Step 3: Upload with mismatched key → should fail
	_, wrongKeyPEM := generateTestCert(t, []string{"other.com"})
	badInput := model.CreateCertInput{
		Name:    "Bad Certificate",
		CertPEM: certPEM,
		KeyPEM:  wrongKeyPEM,
	}
	body, _ = json.Marshal(badInput)

	req = httptest.NewRequest(http.MethodPost, "/api/certificates/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for mismatched key pair, got %d; body: %s", w.Code, w.Body.String())
	}

	// Step 4: Create a machine for deployment config
	machineInput := map[string]interface{}{
		"name": "Web Server 1",
		"ip":   "192.168.1.100",
	}
	body, _ = json.Marshal(machineInput)

	req = httptest.NewRequest(http.MethodPost, "/api/machines/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for machine creation, got %d; body: %s", w.Code, w.Body.String())
	}

	var machineResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &machineResp)
	machineData := machineResp["data"].(map[string]interface{})
	// Machine Create returns {"machine": {...}, "agent_token": "..."}
	machineObj := machineData["machine"].(map[string]interface{})
	machineID := machineObj["id"].(string)

	if machineID == "" {
		t.Fatal("expected non-empty machine ID")
	}

	// Step 5: Create deployment config for the machine
	deployInput := map[string]interface{}{
		"certificate_id":       certID,
		"cert_path":            "/etc/nginx/ssl/cert.pem",
		"private_key_path":     "/etc/nginx/ssl/key.pem",
		"post_deploy_commands": "nginx -s reload",
	}
	body, _ = json.Marshal(deployInput)

	req = httptest.NewRequest(http.MethodPost, "/api/machines/"+machineID+"/certificates/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for deployment config, got %d; body: %s", w.Code, w.Body.String())
	}

	var mcResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &mcResp)
	mcData := mcResp["data"].(map[string]interface{})
	mcID := mcData["id"].(string)

	if mcID == "" {
		t.Fatal("expected non-empty machine certificate ID")
	}
	if mcData["cert_path"] != "/etc/nginx/ssl/cert.pem" {
		t.Errorf("expected cert_path '/etc/nginx/ssl/cert.pem', got '%v'", mcData["cert_path"])
	}
	if int(mcData["config_revision"].(float64)) != 1 {
		t.Errorf("expected config_revision 1, got %v", mcData["config_revision"])
	}
	if mcData["last_deploy_status"] != "pending" {
		t.Errorf("expected last_deploy_status 'pending', got '%v'", mcData["last_deploy_status"])
	}

	// Step 6: Verify deployment config with empty path is rejected
	badDeployInput := map[string]interface{}{
		"certificate_id":   certID,
		"cert_path":        "",
		"private_key_path": "/etc/ssl/key.pem",
	}
	body, _ = json.Marshal(badDeployInput)

	req = httptest.NewRequest(http.MethodPost, "/api/machines/"+machineID+"/certificates/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty cert_path, got %d; body: %s", w.Code, w.Body.String())
	}

	// Step 7: List deployment configs for the machine
	req = httptest.NewRequest(http.MethodGet, "/api/machines/"+machineID+"/certificates/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for listing configs, got %d; body: %s", w.Code, w.Body.String())
	}

	var listResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &listResp)
	listData := listResp["data"].([]interface{})
	if len(listData) != 1 {
		t.Errorf("expected 1 deployment config, got %d", len(listData))
	}
}

// --- Integration Test 3: Agent Heartbeat and Certificate Sync Flow ---
// Validates: Requirement 8.1 - Agent heartbeat → certificate sync flow

func TestIntegration_AgentHeartbeatAndCertSync(t *testing.T) {
	app := setupTestApp(t)

	// Setup: Create admin, login, create machine, upload cert, create deploy config
	username, password := createTestAdmin(t, app)
	token := loginAndGetToken(t, app, username, password)

	// Create a machine (this generates an agent token)
	machineInput := map[string]interface{}{
		"name": "Agent Machine",
		"ip":   "10.0.0.50",
	}
	body, _ := json.Marshal(machineInput)

	req := httptest.NewRequest(http.MethodPost, "/api/machines/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for machine creation, got %d; body: %s", w.Code, w.Body.String())
	}

	var machineResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &machineResp)
	machineData := machineResp["data"].(map[string]interface{})
	// Machine Create returns {"machine": {...}, "agent_token": "..."}
	machineObj := machineData["machine"].(map[string]interface{})
	machineID := machineObj["id"].(string)
	agentToken := machineData["agent_token"].(string)

	if agentToken == "" {
		t.Fatal("expected non-empty agent token")
	}

	// Upload a certificate
	certPEM, keyPEM := generateTestCert(t, []string{"agent-test.example.com"})
	certInput := model.CreateCertInput{
		Name:    "Agent Test Cert",
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
	}
	body, _ = json.Marshal(certInput)

	req = httptest.NewRequest(http.MethodPost, "/api/certificates/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for cert upload, got %d; body: %s", w.Code, w.Body.String())
	}

	var certResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &certResp)
	certData := certResp["data"].(map[string]interface{})
	certID := certData["id"].(string)

	// Create deployment config
	deployInput := map[string]interface{}{
		"certificate_id":       certID,
		"cert_path":            "/etc/ssl/fullchain.pem",
		"private_key_path":     "/etc/ssl/privkey.pem",
		"post_deploy_commands": "systemctl reload nginx",
	}
	body, _ = json.Marshal(deployInput)

	req = httptest.NewRequest(http.MethodPost, "/api/machines/"+machineID+"/certificates/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for deploy config, got %d; body: %s", w.Code, w.Body.String())
	}

	var mcResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &mcResp)
	mcData := mcResp["data"].(map[string]interface{})
	mcID := mcData["id"].(string)

	// --- Agent Flow Starts Here ---

	// Step 1: Agent sends heartbeat
	heartbeatBody := map[string]interface{}{
		"agent_version": "1.2.0",
		"hostname":      "agent-machine-01",
		"ip":            "10.0.0.50",
		"os":            "linux",
		"arch":          "amd64",
	}
	body, _ = json.Marshal(heartbeatBody)

	req = httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+agentToken)
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for heartbeat, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify machine status was updated to online
	var status string
	err := app.db.QueryRow("SELECT status FROM machines WHERE id = ?", machineID).Scan(&status)
	if err != nil {
		t.Fatalf("failed to query machine status: %v", err)
	}
	if status != "online" {
		t.Errorf("expected machine status 'online', got '%s'", status)
	}

	// Step 2: Agent pulls certificate configs
	req = httptest.NewRequest(http.MethodGet, "/api/agent/machines/"+machineID+"/certificates", nil)
	req.Header.Set("Authorization", "Bearer "+agentToken)
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for cert config list, got %d; body: %s", w.Code, w.Body.String())
	}

	var configResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &configResp)
	configData := configResp["data"].([]interface{})

	if len(configData) != 1 {
		t.Fatalf("expected 1 certificate config, got %d", len(configData))
	}

	certConfig := configData[0].(map[string]interface{})
	if certConfig["machine_certificate_id"] != mcID {
		t.Errorf("expected mc_id '%s', got '%v'", mcID, certConfig["machine_certificate_id"])
	}
	if certConfig["certificate_id"] != certID {
		t.Errorf("expected cert_id '%s', got '%v'", certID, certConfig["certificate_id"])
	}
	if certConfig["last_deploy_status"] != "pending" {
		t.Errorf("expected last_deploy_status 'pending', got '%v'", certConfig["last_deploy_status"])
	}
	if certConfig["fingerprint_sha256"] == "" {
		t.Error("expected non-empty fingerprint_sha256")
	}

	// Step 3: Agent downloads certificate
	req = httptest.NewRequest(http.MethodGet, "/api/agent/machine-certificates/"+mcID+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+agentToken)
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for cert download, got %d; body: %s", w.Code, w.Body.String())
	}

	var dlResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &dlResp)
	dlData := dlResp["data"].(map[string]interface{})

	if dlData["certificate_id"] != certID {
		t.Errorf("expected certificate_id '%s', got '%v'", certID, dlData["certificate_id"])
	}
	if dlData["fullchain_pem"] == "" {
		t.Error("expected non-empty fullchain_pem")
	}
	if dlData["private_key_pem"] == "" {
		t.Error("expected non-empty private_key_pem")
	}
	if dlData["fingerprint_sha256"] == "" {
		t.Error("expected non-empty fingerprint_sha256")
	}

	// Step 4: Agent reports deployment log
	deployLog := map[string]interface{}{
		"machine_certificate_id":   mcID,
		"certificate_id":           certID,
		"status":                   "success",
		"cert_fingerprint_sha256":  certConfig["fingerprint_sha256"],
		"cert_path":                "/etc/ssl/fullchain.pem",
		"private_key_path":         "/etc/ssl/privkey.pem",
		"command_outputs": []map[string]interface{}{
			{
				"command":   "systemctl reload nginx",
				"exit_code": 0,
				"stdout":    "",
				"stderr":    "",
			},
		},
		"error_message": "",
		"started_at":    time.Now().UTC().Add(-5 * time.Second).Format(time.RFC3339),
		"finished_at":   time.Now().UTC().Format(time.RFC3339),
	}
	body, _ = json.Marshal(deployLog)

	req = httptest.NewRequest(http.MethodPost, "/api/agent/deployment-logs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+agentToken)
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for deployment log, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify deployment log was saved
	var logCount int
	err = app.db.QueryRow("SELECT COUNT(*) FROM deployment_logs WHERE machine_certificate_id = ?", mcID).Scan(&logCount)
	if err != nil {
		t.Fatalf("failed to count deployment logs: %v", err)
	}
	if logCount != 1 {
		t.Errorf("expected 1 deployment log, got %d", logCount)
	}

	// Verify machine_certificate deploy status was updated
	var deployStatus string
	err = app.db.QueryRow("SELECT last_deploy_status FROM machine_certificates WHERE id = ?", mcID).Scan(&deployStatus)
	if err != nil {
		t.Fatalf("failed to query deploy status: %v", err)
	}
	if deployStatus != "success" {
		t.Errorf("expected last_deploy_status 'success', got '%s'", deployStatus)
	}

	// Step 5: Verify agent with invalid token is rejected
	req = httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer invalid-agent-token")
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid agent token, got %d", w.Code)
	}

	// Step 6: Verify agent cannot access another machine's certificates
	req = httptest.NewRequest(http.MethodGet, "/api/agent/machines/other-machine-id/certificates", nil)
	req.Header.Set("Authorization", "Bearer "+agentToken)
	w = httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong machine_id, got %d; body: %s", w.Code, w.Body.String())
	}
}
