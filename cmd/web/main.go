package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	_ "github.com/glebarez/sqlite"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/ssl-manager/ssl-manager/internal/certbot"
	"github.com/ssl-manager/ssl-manager/internal/cloudflare"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/database"
	"github.com/ssl-manager/ssl-manager/internal/web/handler"
	"github.com/ssl-manager/ssl-manager/internal/web/middleware"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"github.com/ssl-manager/ssl-manager/internal/web/service"
	"github.com/ssl-manager/ssl-manager/webui"
)

const dataDir = "./data"

func main() {
	log.Println("[INFO] SSL Manager Web Backend starting...")
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// authServiceAdapter adapts service.AuthService to the middleware.AuthService interface.
type authServiceAdapter struct {
	authService *service.AuthService
	userRepo    *repository.UserRepository
	jwtSecret   []byte
}

func (a *authServiceAdapter) GetJWTSecret() []byte {
	return a.jwtSecret
}

func (a *authServiceAdapter) IsSessionValid(_ context.Context, sessionID string) bool {
	// The middleware passes sessionID; we always return true here since
	// session invalidation is tracked by user_id + issued_at in the service layer.
	// The actual session check is done via the token's issued_at vs invalidation time.
	return true
}

func (a *authServiceAdapter) IsUserActive(ctx context.Context, userID string) bool {
	user, err := a.userRepo.GetByID(ctx, userID)
	if err != nil {
		return false
	}
	return user.Enabled
}

func (a *authServiceAdapter) GetCurrentRole(ctx context.Context, userID string) (string, error) {
	user, err := a.userRepo.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	return user.Role, nil
}

func (a *authServiceAdapter) IsTokenValid(_ context.Context, userID string, issuedAt time.Time) bool {
	return a.authService.IsSessionValid(userID, issuedAt)
}

func run() error {
	// Determine config path
	configPath := filepath.Join(dataDir, "config.json")

	// Load or create default config
	var cfg *config.Config
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Println("[INFO] Config file not found, using defaults")
		cfg = config.DefaultConfig()
	} else {
		// Check file permissions (security warning if too open)
		if err := config.CheckFilePermissions(configPath); err != nil {
			log.Printf("[WARN] %v", err)
		}
		loadedCfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		cfg = loadedCfg
	}

	// Create shared runtime config holder
	runtimeCfg := config.NewRuntimeConfig(cfg)

	// Initialize database
	db, err := database.NewDB(dataDir)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer db.Close()

	sqlDB := db.DB

	// Generate or load JWT secret
	jwtSecret := generateJWTSecret()

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
	rootDomainRepo := repository.NewRootDomainRepository(sqlDB)

	// Initialize sanitizer (fail-closed: service must not start if regex compilation fails)
	sanitizer, err := service.NewSanitizer()
	if err != nil {
		log.Fatalf("FATAL: sanitizer initialization failed: %v", err)
	}

	// Initialize services
	authService := service.NewAuthService(userRepo, runtimeCfg, jwtSecret)
	initService := service.NewInitService(db, userRepo, configPath, runtimeCfg)

	// Ensure init_state table is consistent on startup (non-fatal)
	if err := initService.EnsureInitState(context.Background()); err != nil {
		log.Printf("[WARN] EnsureInitState failed: %v", err)
	}

	machineService := service.NewMachineService(machineRepo, runtimeCfg)
	certService := service.NewCertificateService(certRepo, sqlDB)
	mcService := service.NewMachineCertificateService(mcRepo)
	deployLogService := service.NewDeploymentLogService(deployLogRepo, sanitizer)
	alertService := service.NewAlertService(alertRepo, channelRepo)
	auditLogService := service.NewAuditLogService(auditLogRepo)
	dashboardService := service.NewDashboardService(sqlDB)
	userService := service.NewUserService(userRepo, authService)

	// Domain monitor service
	domainMonitorService := service.NewDomainMonitorService(domainRepo, certRepo, alertService, runtimeCfg)

	// ThirdpartDNS service with real Cloudflare client
	cfClient := cloudflare.NewClient()
	dnsService := service.NewThirdpartDNSService(dnsRepo, domainRepo, cfClient, alertService, runtimeCfg)

	// Domain expiry (WHOIS registration expiry) monitor service — independent of
	// domainMonitorService (TLS certificate monitoring). Reuses dnsService as its
	// Cloudflare zone scanner (ZoneScanner) and alertService as its alert sender.
	domainExpiryService := service.NewDomainExpiryService(rootDomainRepo, dnsService, alertService, runtimeCfg)

	// Certbot wrapper
	certbotExecutor := certbot.NewDefaultExecutor()
	certbotWrapper := certbot.NewCertbotWrapper(runtimeCfg, certbotExecutor)

	// Scheduler service
	schedulerService := service.NewSchedulerService(
		runtimeCfg, certRepo, machineRepo, certService, certbotWrapper, alertService, sqlDB,
	)
	schedulerService.SetDomainMonitorService(domainMonitorService)
	schedulerService.SetThirdpartDNSService(dnsService, dnsRepo)
	// Must be set before schedulerService.Start so the periodic expiry refresh
	// ticker and the DNS-sync reconcile step have the service available.
	schedulerService.SetDomainExpiryService(domainExpiryService)

	// Create auth service adapter for middleware
	authAdapter := &authServiceAdapter{
		authService: authService,
		userRepo:    userRepo,
		jwtSecret:   jwtSecret,
	}
	_ = authService // used indirectly via adapter

	// Create rate limiter for login endpoints
	rateLimiter := middleware.NewRateLimiter(20, 10, 15*time.Minute, 15*time.Minute)
	defer rateLimiter.Stop()

	// Initialize handlers
	initHandler := handler.NewInitHandler(initService)
	machineHandler := handler.NewMachineHandler(machineService)
	certHandler := handler.NewCertificateHandler(certService, certbotWrapper, dnsRepo, mcRepo, dataDir)
	mcHandler := handler.NewMachineCertificateHandler(mcService)
	deployLogHandler := handler.NewDeploymentLogHandler(deployLogService)
	domainHandler := handler.NewDomainHandler(domainMonitorService)
	alertHandler := handler.NewAlertHandler(alertService)
	auditLogHandler := handler.NewAuditLogHandler(auditLogService)
	dashboardHandler := handler.NewDashboardHandler(dashboardService)
	systemHandler := handler.NewSystemHandler(configPath, runtimeCfg)
	dnsHandler := handler.NewThirdpartDNSHandler(dnsService)
	// dnsService doubles as the DNSConfigResolver, resolving an api_token from a
	// stored DNS config_id on the import path (GetConfig).
	rootDomainHandler := handler.NewRootDomainHandler(domainExpiryService, dnsService)
	// Initialize VersionCache for agent binary version management
	versionCache := service.NewVersionCache("./bin", 5*time.Minute)

	agentHandler := handler.NewAgentHandler(machineService, mcService, deployLogService, certRepo, mcRepo, alertService, versionCache)
	installHandler := handler.NewInstallHandler(runtimeCfg, "./bin", versionCache)
	userHandler := handler.NewUserHandler(userService)

	// Setup router
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RealIP)

	// Init middleware: redirects to /init when system is uninitialized
	r.Use(middleware.InitMiddleware(initService))

	// NOTE: AuditMiddleware is NOT global. It is added per-route-group AFTER AuthMiddleware
	// so that it can read user claims from the request context.

	// Health check (always available)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	// Register all routes (audit middleware is added inside each handler's RegisterRoutes)
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
	rootDomainHandler.RegisterRoutes(r, authAdapter, auditLogRepo)
	userHandler.RegisterRoutes(r, authAdapter, auditLogRepo)
	agentHandler.RegisterRoutes(r, machineRepo, alertService, auditLogRepo)

	// Register auth login routes (no auth middleware needed)
	r.Post("/api/auth/login", createLoginHandler(authService, rateLimiter))
	r.Post("/api/auth/readonly-login", createReadonlyLoginHandler(authService, rateLimiter))

	// Register Turnstile config route (no auth needed — frontend calls before login)
	turnstileHandler := handler.NewTurnstileHandler(runtimeCfg)
	turnstileHandler.RegisterRoutes(r)

	// Register SPA handler (serves frontend static files with SPA fallback)
	// This MUST be registered LAST so that /api/*, /health, /init/* take priority.
	distFS, err := fs.Sub(webui.DistFS, "dist")
	if err != nil {
		return fmt.Errorf("failed to access webui dist filesystem: %w", err)
	}
	spaHandler := handler.NewWebUIHandler(distFS)
	spaHandler.RegisterRoutes(r)

	// Start scheduler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := schedulerService.Start(ctx); err != nil {
		log.Printf("[WARN] Failed to start scheduler: %v", err)
	} else {
		log.Println("[INFO] Scheduler started (renewal checks, heartbeat timeout, domain monitoring)")
	}

	// Create HTTP server
	listenAddr := cfg.Server.ListenAddr
	srv := &http.Server{
		Addr:    listenAddr,
		Handler: r,
	}

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("[INFO] Shutdown signal received, stopping...")

		// Stop scheduler
		if err := schedulerService.Stop(); err != nil {
			log.Printf("[WARN] Failed to stop scheduler: %v", err)
		}

		// Stop version cache
		versionCache.Stop()

		// Shutdown HTTP server with timeout
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("[ERROR] HTTP server shutdown error: %v", err)
		}

		cancel()
	}()

	log.Printf("[INFO] Listening on %s", listenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server error: %w", err)
	}

	log.Println("[INFO] Web Backend stopped")
	return nil
}

// generateJWTSecret loads or generates a 32-byte JWT signing secret.
// The secret is persisted to ./data/jwt_secret so it survives restarts.
func generateJWTSecret() []byte {
	secretPath := filepath.Join(dataDir, "jwt_secret")

	// Try to load existing secret
	if data, err := os.ReadFile(secretPath); err == nil && len(data) >= 32 {
		return data
	}

	// Generate new secret
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		log.Println("[WARN] Failed to generate random JWT secret, using fallback")
		return []byte("ssl-manager-default-jwt-secret!!")
	}
	encoded := []byte(hex.EncodeToString(secret))

	// Persist to file (create data dir if needed)
	if err := os.MkdirAll(dataDir, 0700); err == nil {
		if err := os.WriteFile(secretPath, encoded, 0600); err != nil {
			log.Printf("[WARN] Failed to persist JWT secret: %v", err)
		}
	}

	return encoded
}

// createLoginHandler creates the login endpoint handler.
func createLoginHandler(authService *service.AuthService, rateLimiter *middleware.RateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Username       string `json:"username"`
			Password       string `json:"password"`
			TurnstileToken string `json:"turnstile_token"`
		}

		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"code":    400,
				"message": "invalid request body",
			})
			return
		}

		remoteIP := service.GetBestEffortRemoteIP(r)

		// Check rate limit before attempting login
		if rateLimiter.IsBlocked(remoteIP, input.Username) {
			writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
				"code":    429,
				"message": "请求过于频繁，请稍后重试",
			})
			return
		}

		token, err := authService.Login(r.Context(), input.Username, input.Password, input.TurnstileToken, remoteIP)
		if err != nil {
			// Record failure for rate limiting
			rateLimiter.RecordFailure(remoteIP, input.Username)

			// Turnstile errors return specific message
			if errors.Is(err, service.ErrTurnstileRequired) || errors.Is(err, service.ErrTurnstileFailed) {
				writeJSON(w, http.StatusForbidden, map[string]interface{}{
					"code":    403,
					"message": err.Error(),
				})
				return
			}
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"code":    401,
				"message": "invalid credentials",
			})
			return
		}

		// Record success — reset rate limit counters
		rateLimiter.RecordSuccess(remoteIP, input.Username)

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"code":    200,
			"message": "login successful",
			"data": map[string]interface{}{
				"token": token,
			},
		})
	}
}

// createReadonlyLoginHandler creates the readonly login endpoint handler.
func createReadonlyLoginHandler(authService *service.AuthService, rateLimiter *middleware.RateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Password       string `json:"password"`
			TurnstileToken string `json:"turnstile_token"`
		}

		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"code":    400,
				"message": "invalid request body",
			})
			return
		}

		remoteIP := service.GetBestEffortRemoteIP(r)

		// Check rate limit — readonly-login only uses IP dimension
		if rateLimiter.IsIPBlocked(remoteIP) {
			writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
				"code":    429,
				"message": "请求过于频繁，请稍后重试",
			})
			return
		}

		token, err := authService.LoginReadonly(r.Context(), input.Password, input.TurnstileToken, remoteIP)
		if err != nil {
			// Record failure — only IP dimension (no username for readonly-login)
			rateLimiter.RecordFailure(remoteIP, "")

			// Turnstile errors return specific message
			if errors.Is(err, service.ErrTurnstileRequired) || errors.Is(err, service.ErrTurnstileFailed) {
				writeJSON(w, http.StatusForbidden, map[string]interface{}{
					"code":    403,
					"message": err.Error(),
				})
				return
			}
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"code":    401,
				"message": "invalid credentials",
			})
			return
		}

		// Record success — reset IP counter
		rateLimiter.RecordSuccess(remoteIP, "")

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
	enc := json.NewEncoder(w)
	enc.Encode(data)
}

func decodeJSON(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}
