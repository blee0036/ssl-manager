package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/database"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// Errors for initialization flow.
var (
	ErrAlreadyInitialized = errors.New("system is already initialized")
	ErrInitNotComplete    = errors.New("system initialization is not complete")
	ErrUsernameRequired   = errors.New("username is required")
	ErrPasswordRequired   = errors.New("password is required")
	ErrPasswordTooShort   = errors.New("password must be at least 6 characters")
)

// InitService handles the system initialization flow.
// It checks whether the system needs initialization (no admin user exists),
// creates the first admin user, and saves system configuration.
// Initialization has two phases: admin_created and config_saved.
type InitService struct {
	db         *database.DB
	userRepo   *repository.UserRepository
	configPath string
	runtimeCfg *config.RuntimeConfig

	mu          sync.RWMutex
	initialized bool // true when admin exists
	configSaved bool // true when config.json has been saved via /init/config
}

// NewInitService creates a new InitService.
func NewInitService(db *database.DB, userRepo *repository.UserRepository, configPath string, runtimeCfg *config.RuntimeConfig) *InitService {
	svc := &InitService{
		db:         db,
		userRepo:   userRepo,
		configPath: configPath,
		runtimeCfg: runtimeCfg,
	}
	// If config.json already exists on disk, mark config as saved
	if _, err := os.Stat(configPath); err == nil {
		svc.configSaved = true
	}
	return svc
}

// CheckInitialized checks if the system has been initialized (admin user exists).
// It caches the result once initialization is confirmed.
func (s *InitService) CheckInitialized(ctx context.Context) (bool, error) {
	s.mu.RLock()
	if s.initialized {
		s.mu.RUnlock()
		return true, nil
	}
	s.mu.RUnlock()

	hasAdmin, err := s.db.HasAdminUser()
	if err != nil {
		return false, fmt.Errorf("failed to check initialization status: %w", err)
	}

	if hasAdmin {
		s.mu.Lock()
		s.initialized = true
		s.mu.Unlock()
	}

	return hasAdmin, nil
}

// NeedsInit returns true if the system needs initialization
// (either admin doesn't exist OR config hasn't been saved).
func (s *InitService) NeedsInit(ctx context.Context) (bool, error) {
	fullyInit, err := s.IsFullyInitialized(ctx)
	if err != nil {
		return false, err
	}
	return !fullyInit, nil
}

// CreateAdminInput holds the input for creating the first admin user.
type CreateAdminInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// CreateAdmin creates the first admin user during initialization.
// All state checks and writes happen within a single DB transaction
// (SQLite serialized writes guarantee mutual exclusion).
// Returns the created user, the plain init token, and any error.
// Returns ErrAlreadyInitialized if a completed init_state record exists.
// Returns ErrInitPendingNotExpired if an unexpired pending init_state exists.
func (s *InitService) CreateAdmin(ctx context.Context, input CreateAdminInput) (*model.User, string, error) {
	// Validate input before starting transaction
	if input.Username == "" {
		return nil, "", ErrUsernameRequired
	}
	if input.Password == "" {
		return nil, "", ErrPasswordRequired
	}
	if len(input.Password) < 6 {
		return nil, "", ErrPasswordTooShort
	}

	// Begin transaction — all checks and writes are within this tx
	tx, err := s.db.Begin()
	if err != nil {
		return nil, "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 1. Check if a completed record exists (pending_init=0)
	//    → if yes: system already initialized, return 403
	hasCompleted, err := s.db.HasCompletedInitState(tx)
	if err != nil {
		return nil, "", fmt.Errorf("check completed init_state: %w", err)
	}
	if hasCompleted {
		return nil, "", ErrAlreadyInitialized
	}

	// 2. Check if an active pending record exists (pending_init=1)
	pending, err := s.db.GetPendingInitState(tx)
	if err != nil {
		return nil, "", fmt.Errorf("get pending init_state: %w", err)
	}

	if pending != nil {
		if !IsInitStateExpired(pending) {
			// Unexpired pending admin exists → reject to prevent race condition
			return nil, "", ErrInitPendingNotExpired
		}
		// Pending is expired → delete old admin + old init_state
		if err := s.db.DeleteUserTx(tx, pending.AdminID); err != nil {
			return nil, "", fmt.Errorf("delete expired pending admin: %w", err)
		}
		if err := s.db.DeleteInitState(tx, pending.ID); err != nil {
			return nil, "", fmt.Errorf("delete expired init_state: %w", err)
		}
	}

	// 3. Create new admin user
	user := &model.User{
		Username:     input.Username,
		PasswordHash: input.Password, // CreateUserTx will hash this
		Role:         "admin",
	}
	if err := s.db.CreateUserTx(tx, user); err != nil {
		return nil, "", fmt.Errorf("failed to create admin user: %w", err)
	}

	// 4. Generate 32 bytes crypto/rand token → hex encode → sha256Hex
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, "", fmt.Errorf("generate init token: %w", err)
	}
	plainToken := hex.EncodeToString(tokenBytes)
	tokenHash := sha256Hex(plainToken)

	// 5. Insert init_state (pending_init=true, expires_at=now+30min)
	state := &database.InitState{
		ID:          uuid.New().String(),
		AdminID:     user.ID,
		TokenHash:   tokenHash,
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		PendingInit: true,
	}
	if err := s.db.InsertInitState(tx, state); err != nil {
		return nil, "", fmt.Errorf("insert init_state: %w", err)
	}

	// 6. Commit — atomic completion
	if err := tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("commit tx: %w", err)
	}

	// Mark as initialized in memory
	s.mu.Lock()
	s.initialized = true
	s.mu.Unlock()

	return user, plainToken, nil
}

// SaveConfigInput holds the input for saving system configuration.
type SaveConfigInput struct {
	Server        *config.ServerConfig        `json:"server,omitempty"`
	Auth          *config.AuthConfig          `json:"auth,omitempty"`
	Agent         *config.AgentConfig         `json:"agent,omitempty"`
	Alert         *config.AlertConfig         `json:"alert,omitempty"`
	Certbot       *config.CertbotConfig       `json:"certbot,omitempty"`
	Readonly      *config.ReadonlyConfig      `json:"readonly,omitempty"`
	DomainMonitor *config.DomainMonitorConfig `json:"domain_monitor,omitempty"`
	Turnstile     *config.TurnstileConfig     `json:"turnstile,omitempty"`
	ThirdpartDNS  *config.ThirdpartDNSConfig  `json:"thirdpart_dns,omitempty"`
	Cleanup       *config.CleanupConfig       `json:"cleanup,omitempty"`
}

// SaveConfig saves the system configuration to config.json during initialization.
// Requires a valid initToken (from CreateAdmin response) to authenticate the request.
// Flow: validate token → save config file → mark init_state completed.
// On file save failure: keeps pending token valid for retry.
// On DB mark failure after file save: returns 500, keeps pending token (EnsureInitState fixes on restart).
func (s *InitService) SaveConfig(ctx context.Context, initToken string, input SaveConfigInput) (*config.Config, error) {
	// 1. Validate token: SHA256(initToken) → compare with init_state.token_hash → check not expired
	if initToken == "" {
		return nil, ErrInvalidInitToken
	}

	hash := sha256Hex(initToken)

	state, err := s.db.GetPendingInitState(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query init_state: %w", err)
	}
	if state == nil || state.TokenHash != hash {
		return nil, ErrInvalidInitToken
	}
	if IsInitStateExpired(state) {
		return nil, ErrInitTokenExpired
	}

	// Build config from input, starting with defaults
	cfg := config.DefaultConfig()

	if input.Server != nil {
		if input.Server.ExternalURL != "" {
			cfg.Server.ExternalURL = input.Server.ExternalURL
		}
		if input.Server.ListenAddr != "" {
			cfg.Server.ListenAddr = input.Server.ListenAddr
		}
	}

	if input.Auth != nil {
		if input.Auth.SessionExpiryHours > 0 {
			cfg.Auth.SessionExpiryHours = input.Auth.SessionExpiryHours
		}
	}

	if input.Agent != nil {
		if input.Agent.HeartbeatTimeoutSeconds > 0 {
			cfg.Agent.HeartbeatTimeoutSeconds = input.Agent.HeartbeatTimeoutSeconds
		}
		if input.Agent.PollIntervalSeconds > 0 {
			cfg.Agent.PollIntervalSeconds = input.Agent.PollIntervalSeconds
		}
	}

	if input.Alert != nil {
		if input.Alert.DefaultBeforeDays > 0 {
			cfg.Alert.DefaultBeforeDays = input.Alert.DefaultBeforeDays
		}
	}

	if input.Certbot != nil {
		if input.Certbot.BinaryPath != "" {
			cfg.Certbot.BinaryPath = input.Certbot.BinaryPath
		}
		if input.Certbot.DataDir != "" {
			cfg.Certbot.DataDir = input.Certbot.DataDir
		}
		if input.Certbot.Email != "" {
			cfg.Certbot.Email = input.Certbot.Email
		}
	}

	if input.Readonly != nil {
		cfg.Readonly.Enabled = input.Readonly.Enabled
		cfg.Readonly.ViewPassword = input.Readonly.ViewPassword
	}

	if input.DomainMonitor != nil {
		if input.DomainMonitor.DefaultPort > 0 {
			cfg.DomainMonitor.DefaultPort = input.DomainMonitor.DefaultPort
		}
		if input.DomainMonitor.IntervalMinutes > 0 {
			cfg.DomainMonitor.IntervalMinutes = input.DomainMonitor.IntervalMinutes
		}
	}

	if input.Turnstile != nil {
		cfg.Turnstile.Enabled = input.Turnstile.Enabled
		cfg.Turnstile.SiteKey = input.Turnstile.SiteKey
		cfg.Turnstile.SecretKey = input.Turnstile.SecretKey
	}

	if input.ThirdpartDNS != nil {
		cfg.ThirdpartDNS.SyncIntervalMinutes = input.ThirdpartDNS.SyncIntervalMinutes
	}

	if input.Cleanup != nil {
		if input.Cleanup.RetentionDays >= 0 {
			cfg.Cleanup.RetentionDays = input.Cleanup.RetentionDays
		}
		if input.Cleanup.MinKeepCount > 0 {
			cfg.Cleanup.MinKeepCount = input.Cleanup.MinKeepCount
		}
	}

	// 2. Save config to file
	if err := config.SaveConfig(s.configPath, cfg); err != nil {
		// File save failure: init_state stays pending, token remains valid for retry
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	// 3. File saved successfully → atomically consume the init token
	//    Conditional UPDATE: only succeeds if pending_init=1 AND token_hash matches
	//    This prevents concurrent SaveConfig requests from both succeeding
	if err := s.db.ConsumeInitToken(nil, state.ID, hash); err != nil {
		if err == database.ErrInitTokenAlreadyConsumed {
			// Another concurrent request already consumed the token
			return nil, ErrInvalidInitToken
		}
		// ⚠️ File saved but DB update failed — return 500, keep pending token
		// EnsureInitState will fix this on next restart
		fmt.Printf("WARNING: config file saved but DB completion mark failed: %v\n", err)
		return nil, fmt.Errorf("config saved but failed to mark initialization complete: %w", err)
	}

	// 4. DB update success → update runtime config
	if s.runtimeCfg != nil {
		s.runtimeCfg.Update(cfg)
	}

	// Mark config as saved in memory
	s.mu.Lock()
	s.configSaved = true
	s.mu.Unlock()

	return cfg, nil
}

// IsInitialized returns the cached initialization state without querying the database.
func (s *InitService) IsInitialized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.initialized
}

// IsFullyInitialized returns true when init_state table has a completed record (pending_init=0).
// This is the single authority for "system is fully initialized" — used by InitMiddleware.
// It does NOT rely on admin existence or config file presence; the init_state table is authoritative.
func (s *InitService) IsFullyInitialized(ctx context.Context) (bool, error) {
	hasCompleted, err := s.db.HasCompletedInitState(nil)
	if err != nil {
		return false, fmt.Errorf("IsFullyInitialized: %w", err)
	}
	return hasCompleted, nil
}

// EnsureInitState checks and backfills the init_state table on startup.
// This handles three scenarios:
// 1. SaveConfig succeeded (config file exists) but DB completed mark failed — convert pending to completed.
// 2. Legacy system upgrade — admin + config exist but no init_state row — backfill a completed record.
// 3. Expired pending with no config — delete the stale pending admin and init_state so system resets to needs_admin.
// Non-fatal: caller should log warnings but not crash if this fails.
func (s *InitService) EnsureInitState(ctx context.Context) error {
	// 1. Check if completed record already exists → nothing to do
	hasCompleted, err := s.db.HasCompletedInitState(nil)
	if err != nil {
		return fmt.Errorf("EnsureInitState: failed to check completed init_state: %w", err)
	}
	if hasCompleted {
		return nil
	}

	// 2. Check if admin exists + config file exists + no completed record
	hasAdmin, err := s.db.HasAdminUser()
	if err != nil {
		return fmt.Errorf("EnsureInitState: failed to check admin user: %w", err)
	}
	_, configErr := os.Stat(s.configPath)
	configExists := configErr == nil

	if hasAdmin && configExists {
		// Check if a pending row exists — prefer converting it over creating new
		pending, err := s.db.GetPendingInitState(nil)
		if err != nil {
			return fmt.Errorf("EnsureInitState: failed to query pending state: %w", err)
		}
		if pending != nil {
			// Scenario: SaveConfig file saved but DB completed mark failed, then restart.
			// Convert the pending row to completed.
			log.Printf("[INFO] EnsureInitState: converting pending row %s to completed (config file exists)", pending.ID)
			return s.db.UpdateInitStateToCompleted(nil, pending.ID)
		}

		// Scenario: Legacy system upgrade — no pending row, admin + config already exist.
		// Backfill a completed record so GetPhase returns "completed".
		log.Printf("[INFO] EnsureInitState: backfilling completed record for legacy system")
		backfillState := &database.InitState{
			ID:          uuid.New().String(),
			AdminID:     "backfill-legacy",
			TokenHash:   "",
			ExpiresAt:   time.Now(),
			PendingInit: false,
			CompletedAt: timePtr(time.Now()),
		}
		return s.db.InsertInitState(nil, backfillState)
	}

	// 3. Check for expired pending with no config file → clean up stale admin
	if hasAdmin && !configExists {
		pending, err := s.db.GetPendingInitState(nil)
		if err != nil {
			return fmt.Errorf("EnsureInitState: failed to query pending state: %w", err)
		}
		if pending != nil && IsInitStateExpired(pending) {
			// Expired pending admin, never completed init → delete admin and init_state
			// so system resets to "needs_admin" phase
			log.Printf("[INFO] EnsureInitState: cleaning up expired pending admin %s (token expired, no config)", pending.AdminID)
			tx, err := s.db.Begin()
			if err != nil {
				return fmt.Errorf("EnsureInitState: begin tx: %w", err)
			}
			defer tx.Rollback()

			if err := s.db.DeleteUserTx(tx, pending.AdminID); err != nil {
				return fmt.Errorf("EnsureInitState: delete expired admin: %w", err)
			}
			if err := s.db.DeleteInitState(tx, pending.ID); err != nil {
				return fmt.Errorf("EnsureInitState: delete expired init_state: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("EnsureInitState: commit: %w", err)
			}

			// Reset memory state
			s.mu.Lock()
			s.initialized = false
			s.mu.Unlock()
		}
	}

	return nil
}

// GetPhase returns the frontend-facing initialization phase:
// "completed" — system is fully initialized
// "needs_config" — admin exists, waiting for config (active pending token)
// "needs_admin" — no admin yet, or pending token expired
func (s *InitService) GetPhase(ctx context.Context) (string, error) {
	hasCompleted, err := s.db.HasCompletedInitState(nil)
	if err != nil {
		return "", fmt.Errorf("GetPhase: failed to check completed: %w", err)
	}
	if hasCompleted {
		return "completed", nil
	}

	pending, err := s.db.GetPendingInitState(nil)
	if err != nil {
		return "", fmt.Errorf("GetPhase: failed to get pending: %w", err)
	}
	if pending != nil && !IsInitStateExpired(pending) {
		return "needs_config", nil
	}

	return "needs_admin", nil
}

// timePtr returns a pointer to the given time value.
func timePtr(t time.Time) *time.Time {
	return &t
}
