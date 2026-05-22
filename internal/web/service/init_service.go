package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/database"
	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
)

// Errors for initialization flow.
var (
	ErrAlreadyInitialized = errors.New("system is already initialized")
	ErrInitNotComplete    = errors.New("system initialization is not complete")
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
// Returns ErrAlreadyInitialized if an admin user already exists.
func (s *InitService) CreateAdmin(ctx context.Context, input CreateAdminInput) (*model.User, error) {
	// Check if already initialized
	initialized, err := s.CheckInitialized(ctx)
	if err != nil {
		return nil, err
	}
	if initialized {
		return nil, ErrAlreadyInitialized
	}

	// Validate input
	if input.Username == "" {
		return nil, errors.New("username is required")
	}
	if input.Password == "" {
		return nil, errors.New("password is required")
	}
	if len(input.Password) < 6 {
		return nil, errors.New("password must be at least 6 characters")
	}

	// Create admin user
	user := &model.User{
		Username:     input.Username,
		PasswordHash: input.Password, // UserRepository.Create will hash this
		Role:         "admin",
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to create admin user: %w", err)
	}

	// Mark as initialized
	s.mu.Lock()
	s.initialized = true
	s.mu.Unlock()

	return user, nil
}

// SaveConfigInput holds the input for saving system configuration.
type SaveConfigInput struct {
	Server        *config.ServerConfig        `json:"server,omitempty"`
	Agent         *config.AgentConfig         `json:"agent,omitempty"`
	Alert         *config.AlertConfig         `json:"alert,omitempty"`
	Certbot       *config.CertbotConfig       `json:"certbot,omitempty"`
	Readonly      *config.ReadonlyConfig      `json:"readonly,omitempty"`
	DomainMonitor *config.DomainMonitorConfig `json:"domain_monitor,omitempty"`
}

// SaveConfig saves the system configuration to config.json during initialization.
// Only allowed when admin has been created but config has not yet been saved.
// Returns ErrAlreadyInitialized if the config was already saved.
func (s *InitService) SaveConfig(ctx context.Context, input SaveConfigInput) (*config.Config, error) {
	// Only allow config save if admin has been created (system is in init phase)
	initialized, err := s.CheckInitialized(ctx)
	if err != nil {
		return nil, err
	}
	if !initialized {
		return nil, ErrInitNotComplete
	}

	// Check if config was already saved — if so, reject (use /api/system/config instead)
	s.mu.RLock()
	alreadySaved := s.configSaved
	s.mu.RUnlock()
	if alreadySaved {
		return nil, ErrAlreadyInitialized
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

	// Save config to file
	if err := config.SaveConfig(s.configPath, cfg); err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	// Update the in-memory runtime config so all services see the new values
	if s.runtimeCfg != nil {
		s.runtimeCfg.Update(cfg)
	}

	// Mark config as saved — subsequent calls to /init/config will be rejected
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

// IsFullyInitialized returns true when both admin is created and config is saved.
// Used by InitMiddleware to determine if /init/* should return 403.
func (s *InitService) IsFullyInitialized(ctx context.Context) (bool, error) {
	initialized, err := s.CheckInitialized(ctx)
	if err != nil {
		return false, err
	}
	if !initialized {
		return false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.configSaved, nil
}
