package http

import (
	"encoding/json"
	"net/http"
	"time"
)

// ErrorResponse represents a standard error response
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains error information
type ErrorDetail struct {
	Code      string      `json:"code"`
	Message   string      `json:"message"`
	Details   interface{} `json:"details,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// WriteJSON writes a JSON response
func WriteJSON(w http.ResponseWriter, statusCode int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(data)
}

// WriteError writes a standardized error response
func WriteError(w http.ResponseWriter, statusCode int, code, message string, details interface{}) error {
	return WriteJSON(w, statusCode, ErrorResponse{
		Error: ErrorDetail{
			Code:      code,
			Message:   message,
			Details:   details,
			Timestamp: time.Now().UTC(),
		},
	})
}

// WriteBadRequest writes a 400 error
func WriteBadRequest(w http.ResponseWriter, code, message string) error {
	return WriteError(w, http.StatusBadRequest, code, message, nil)
}

// WriteUnauthorized writes a 401 error
func WriteUnauthorized(w http.ResponseWriter, message string) error {
	return WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", message, nil)
}

// WriteForbidden writes a 403 error
func WriteForbidden(w http.ResponseWriter, message string) error {
	return WriteError(w, http.StatusForbidden, "FORBIDDEN", message, nil)
}

// WriteNotFound writes a 404 error
func WriteNotFound(w http.ResponseWriter, message string) error {
	return WriteError(w, http.StatusNotFound, "NOT_FOUND", message, nil)
}

// WriteConflict writes a 409 error
func WriteConflict(w http.ResponseWriter, code, message string, details interface{}) error {
	return WriteError(w, http.StatusConflict, code, message, details)
}

// WriteInternalError writes a 500 error
func WriteInternalError(w http.ResponseWriter, message string) error {
	return WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", message, nil)
}
