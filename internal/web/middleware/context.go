package middleware

import (
	"context"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

// contextKey is a private type for context keys in this package.
type contextKey string

const (
	// UserClaimsKey is the context key for user claims.
	UserClaimsKey contextKey = "user_claims"
	// MachineKey is the context key for machine info.
	MachineKey contextKey = "machine"
)

// TokenClaims represents the claims extracted from a JWT token.
type TokenClaims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	// SessionID is used to invalidate sessions when user is disabled.
	SessionID string    `json:"session_id"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// GetUserClaims extracts user claims from the request context.
func GetUserClaims(ctx context.Context) *TokenClaims {
	claims, _ := ctx.Value(UserClaimsKey).(*TokenClaims)
	return claims
}

// GetMachine extracts machine info from the request context.
func GetMachine(ctx context.Context) *model.Machine {
	machine, _ := ctx.Value(MachineKey).(*model.Machine)
	return machine
}
