package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/ssl-manager/ssl-manager/internal/database"
)

// Errors for init token flow.
var (
	ErrInitPendingNotExpired = errors.New("initialization is pending, please complete or wait for token expiry")
	ErrInvalidInitToken      = errors.New("invalid init token")
	ErrInitTokenExpired      = errors.New("init token expired")
)

// sha256Hex returns the hex-encoded SHA256 hash of the given token string.
func sha256Hex(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// IsInitStateExpired checks whether the given InitState has expired.
func IsInitStateExpired(s *database.InitState) bool {
	return time.Now().After(s.ExpiresAt)
}
