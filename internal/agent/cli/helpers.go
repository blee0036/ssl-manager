package cli

import "strings"

// MaskToken masks a token string for display purposes.
// If the token length is >= 8, it keeps the last 8 characters visible
// and replaces all preceding characters with '*'.
// If the token length is < 8, all characters are replaced with '*'.
func MaskToken(token string) string {
	if len(token) < 8 {
		return strings.Repeat("*", len(token))
	}
	masked := strings.Repeat("*", len(token)-8)
	return masked + token[len(token)-8:]
}

// ValidateURL checks whether the given string is a valid URL
// by verifying it starts with "http://" or "https://".
func ValidateURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
