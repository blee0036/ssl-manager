package middleware

import (
	"context"
	"net/http"
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
	// auditInfoKey is the context key for handler-provided audit metadata.
	auditInfoKey contextKey = "audit_info"
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

// AuditInfo allows handlers to provide explicit audit target metadata
// after business logic succeeds. This overrides URL-guessed values.
type AuditInfo struct {
	TargetType string
	TargetID   string
	Operation  string
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

// SetAuditInfo allows handlers to set explicit audit target info in the request context.
// The AuditMiddleware places a *AuditInfo pointer in context before calling the handler;
// this function populates that pointer so the middleware can read it after the handler returns.
func SetAuditInfo(r *http.Request, info AuditInfo) {
	ptr, _ := r.Context().Value(auditInfoKey).(*AuditInfo)
	if ptr != nil {
		*ptr = info
	}
}

// getAuditInfo retrieves the handler-provided audit info from context.
// Returns nil if no info was set by the handler.
func getAuditInfo(ctx context.Context) *AuditInfo {
	ptr, _ := ctx.Value(auditInfoKey).(*AuditInfo)
	if ptr == nil {
		return nil
	}
	// Only return if handler actually populated it
	if ptr.TargetType == "" && ptr.TargetID == "" && ptr.Operation == "" {
		return nil
	}
	return ptr
}

// withAuditInfoPlaceholder returns a new context with an empty AuditInfo pointer
// that handlers can populate via SetAuditInfo.
func withAuditInfoPlaceholder(ctx context.Context) context.Context {
	return context.WithValue(ctx, auditInfoKey, &AuditInfo{})
}
