package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ssl-manager/ssl-manager/internal/model"
)

// writeErrorResponse writes a JSON error response.
func writeErrorResponse(w http.ResponseWriter, statusCode int, message, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	resp := model.ErrorResponse{
		Code:    statusCode,
		Message: message,
		Detail:  detail,
	}
	json.NewEncoder(w).Encode(resp)
}

// writeSuccessResponse writes a JSON success response.
func writeSuccessResponse(w http.ResponseWriter, statusCode int, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	resp := model.SuccessResponse{
		Code:    statusCode,
		Message: message,
		Data:    data,
	}
	json.NewEncoder(w).Encode(resp)
}

// isNotFoundError checks if an error is a "not found" error.
func isNotFoundError(err error) bool {
	return strings.Contains(err.Error(), "not found")
}

// isValidationError checks if an error is a validation error (e.g., missing required fields).
func isValidationError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "is required") || strings.Contains(msg, "cannot be empty")
}

// isNoRowsError checks if an error wraps sql.ErrNoRows (no rows in result set).
func isNoRowsError(err error) bool {
	return strings.Contains(err.Error(), "no rows in result set")
}
