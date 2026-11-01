// Package apperr defines error kinds shared by services, HTTP handlers and CLI.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Error is an error with an HTTP status and a stable machine readable code.
type Error struct {
	Status int
	Code   string
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func newf(status int, code, format string, a ...any) *Error {
	return &Error{Status: status, Code: code, Msg: fmt.Sprintf(format, a...)}
}

// Constructors.
func BadRequest(format string, a ...any) error {
	return newf(http.StatusBadRequest, "bad_request", format, a...)
}
func NotFound(what string) error { return newf(http.StatusNotFound, "not_found", "%s not found", what) }
func Forbidden(format string, a ...any) error {
	return newf(http.StatusForbidden, "forbidden", format, a...)
}
func Conflict(format string, a ...any) error {
	return newf(http.StatusConflict, "conflict", format, a...)
}
func TooMany(format string, a ...any) error {
	return newf(http.StatusTooManyRequests, "rate_limited", format, a...)
}

// Sentinel errors.
var (
	ErrUnauthorized = &Error{Status: http.StatusUnauthorized, Code: "unauthorized", Msg: "authentication required"}
	ErrForbidden    = &Error{Status: http.StatusForbidden, Code: "forbidden", Msg: "permission denied"}
	ErrInvalidLogin = &Error{Status: http.StatusUnauthorized, Code: "invalid_credentials", Msg: "invalid username or password"}
)

// Status returns the HTTP status for err (500 for unknown errors).
func Status(err error) (int, string, string) {
	var e *Error
	if errors.As(err, &e) {
		return e.Status, e.Code, e.Msg
	}
	return http.StatusInternalServerError, "internal", "internal server error"
}
