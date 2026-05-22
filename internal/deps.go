//go:build tools

package internal

// This file ensures core dependencies are tracked in go.mod.
// These imports will be used by actual implementation code in later tasks.
import (
	_ "github.com/golang-jwt/jwt/v5"
	_ "github.com/google/uuid"
	_ "github.com/leanovate/gopter"
	_ "golang.org/x/crypto/bcrypt"
)
