package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ssl-manager/ssl-manager/internal/config"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"golang.org/x/crypto/bcrypt"
)

// ErrInvalidCredentials is a generic error for authentication failures.
// It intentionally does not reveal whether the username or password was wrong.
var ErrInvalidCredentials = errors.New("invalid credentials")

// ErrReadonlyDisabled is returned when readonly login is attempted but not enabled.
var ErrReadonlyDisabled = errors.New("readonly access is not enabled")

// ErrAccountDisabled is returned when a disabled user attempts to login.
var ErrAccountDisabled = errors.New("invalid credentials")

// ErrInvalidToken is returned when token validation fails.
var ErrInvalidToken = errors.New("invalid token")

// ErrSessionInvalidated is returned when a session has been invalidated.
var ErrSessionInvalidated = errors.New("session invalidated")

// ErrTurnstileRequired is returned when Turnstile is enabled but no token was provided.
var ErrTurnstileRequired = errors.New("人机验证失败，请重试")

// ErrTurnstileFailed is returned when Turnstile verification fails.
var ErrTurnstileFailed = errors.New("人机验证失败，请重试")

// TurnstileSiteverifyURL is the Cloudflare Turnstile siteverify endpoint.
const TurnstileSiteverifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// TurnstileSiteverifyResponse represents the response from Cloudflare Turnstile siteverify.
type TurnstileSiteverifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes,omitempty"`
}

// TokenClaims represents the JWT claims for authenticated users.
type TokenClaims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"` // "admin", "user", or "readonly"
	jwt.RegisteredClaims
}

// AuthService handles authentication logic including login, token management,
// and session invalidation.
type AuthService struct {
	userRepo   *repository.UserRepository
	runtimeCfg *config.RuntimeConfig
	jwtSecret  []byte
	httpClient *http.Client
	// Track invalidated sessions (user_id -> invalidated_at timestamp)
	invalidatedSessions sync.Map
}

// NewAuthService creates a new AuthService.
func NewAuthService(userRepo *repository.UserRepository, runtimeCfg *config.RuntimeConfig, jwtSecret []byte) *AuthService {
	return &AuthService{
		userRepo:   userRepo,
		runtimeCfg: runtimeCfg,
		jwtSecret:  jwtSecret,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Login validates credentials and returns a JWT token.
// Returns generic "invalid credentials" error for both wrong username and wrong password.
// turnstileToken and remoteIP are used for Turnstile verification when enabled.
func (s *AuthService) Login(ctx context.Context, username, password, turnstileToken, remoteIP string) (string, error) {
	// Read config on every request (not cached)
	cfg := s.runtimeCfg.Get()

	// Turnstile verification BEFORE password check
	if cfg.Turnstile.Enabled {
		if turnstileToken == "" {
			return "", ErrTurnstileRequired
		}
		if err := s.verifyTurnstile(ctx, cfg.Turnstile.SecretKey, turnstileToken, remoteIP); err != nil {
			// Log failure but NEVER log secret/token/password
			log.Printf("[WARN] Turnstile verification failed for user=%s ip=%s", username, remoteIP)
			return "", ErrTurnstileFailed
		}
	}

	user, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		// User not found - return generic error
		return "", ErrInvalidCredentials
	}

	// Check if user is enabled
	if !user.Enabled {
		return "", ErrInvalidCredentials
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return "", ErrInvalidCredentials
	}

	// Generate JWT token
	token, err := s.generateToken(user.ID, user.Username, user.Role)
	if err != nil {
		return "", err
	}

	return token, nil
}

// LoginReadonly validates the readonly password and returns a JWT token with readonly role.
// turnstileToken and remoteIP are used for Turnstile verification when enabled.
func (s *AuthService) LoginReadonly(ctx context.Context, password, turnstileToken, remoteIP string) (string, error) {
	cfg := s.runtimeCfg.Get()

	// Turnstile verification BEFORE password check
	if cfg.Turnstile.Enabled {
		if turnstileToken == "" {
			return "", ErrTurnstileRequired
		}
		if err := s.verifyTurnstile(ctx, cfg.Turnstile.SecretKey, turnstileToken, remoteIP); err != nil {
			// Log failure but NEVER log secret/token/password
			log.Printf("[WARN] Turnstile verification failed for readonly login ip=%s", remoteIP)
			return "", ErrTurnstileFailed
		}
	}

	if !cfg.Readonly.Enabled {
		return "", ErrReadonlyDisabled
	}

	if password != cfg.Readonly.ViewPassword {
		return "", ErrInvalidCredentials
	}

	// Generate JWT token with readonly role
	token, err := s.generateToken("readonly", "readonly", "readonly")
	if err != nil {
		return "", err
	}

	return token, nil
}

// ValidateToken validates a JWT token and returns the claims.
func (s *AuthService) ValidateToken(tokenString string) (*TokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &TokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*TokenClaims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// InvalidateUserSessions marks all sessions for a user as invalid.
// Any token issued before this timestamp will be considered invalid.
func (s *AuthService) InvalidateUserSessions(userID string) {
	s.invalidatedSessions.Store(userID, time.Now())
}

// IsSessionValid checks if a token's session is still valid (not invalidated).
func (s *AuthService) IsSessionValid(userID string, issuedAt time.Time) bool {
	val, ok := s.invalidatedSessions.Load(userID)
	if !ok {
		// No invalidation record, session is valid
		return true
	}

	invalidatedAt, ok := val.(time.Time)
	if !ok {
		return true
	}

	// Token is valid only if it was issued after the invalidation time
	return issuedAt.After(invalidatedAt)
}

// sessionExpiryDuration returns the effective session (JWT) validity duration
// from the current runtime config, falling back to 24 hours when runtimeCfg is
// nil or the configured value is non-positive (defense-in-depth; ValidateConfig
// already rejects non-positive/out-of-range values before a config is saved or
// loaded, so this fallback should only ever be exercised by a nil runtimeCfg,
// e.g. in tests that construct AuthService directly).
func (s *AuthService) sessionExpiryDuration() time.Duration {
	if s.runtimeCfg != nil {
		if hours := s.runtimeCfg.Get().Auth.SessionExpiryHours; hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return 24 * time.Hour
}

// generateToken creates a signed JWT token with the given claims. The token's
// expiry is driven by the current auth.session_expiry_hours config (default 24
// hours), read fresh on every call so a config change takes effect for the next
// login without restarting the process.
func (s *AuthService) generateToken(userID, username, role string) (string, error) {
	now := time.Now()
	claims := &TokenClaims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.sessionExpiryDuration())),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

// verifyTurnstile calls Cloudflare Turnstile siteverify to validate the token.
// It sends secret and response (token); remoteip is best-effort and omitted if empty.
// Returns nil on success, error on failure.
func (s *AuthService) verifyTurnstile(ctx context.Context, secretKey, token, remoteIP string) error {
	formData := url.Values{}
	formData.Set("secret", secretKey)
	formData.Set("response", token)
	if remoteIP != "" {
		formData.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TurnstileSiteverifyURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create turnstile request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("turnstile request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("turnstile returned status %d", resp.StatusCode)
	}

	var result TurnstileSiteverifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode turnstile response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("turnstile verification failed: %v", result.ErrorCodes)
	}

	return nil
}

// GetBestEffortRemoteIP extracts the client IP from the request (best-effort).
// Order: CF-Connecting-IP → X-Forwarded-For first IP → X-Real-IP → RemoteAddr.
// Returns empty string if no reliable value can be determined.
func GetBestEffortRemoteIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	// RemoteAddr format is "ip:port"
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return ""
}
