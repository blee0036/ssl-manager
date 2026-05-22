package service

import (
	"context"
	"errors"
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
	// Track invalidated sessions (user_id -> invalidated_at timestamp)
	invalidatedSessions sync.Map
}

// NewAuthService creates a new AuthService.
func NewAuthService(userRepo *repository.UserRepository, runtimeCfg *config.RuntimeConfig, jwtSecret []byte) *AuthService {
	return &AuthService{
		userRepo:   userRepo,
		runtimeCfg: runtimeCfg,
		jwtSecret:  jwtSecret,
	}
}

// Login validates credentials and returns a JWT token.
// Returns generic "invalid credentials" error for both wrong username and wrong password.
func (s *AuthService) Login(ctx context.Context, username, password string) (string, error) {
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
func (s *AuthService) LoginReadonly(ctx context.Context, password string) (string, error) {
	cfg := s.runtimeCfg.Get()
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

// generateToken creates a signed JWT token with the given claims.
func (s *AuthService) generateToken(userID, username, role string) (string, error) {
	now := time.Now()
	claims := &TokenClaims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}
