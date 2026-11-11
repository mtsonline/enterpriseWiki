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
	// Detail is passed to the client as "detail" (e.g. the token of a
	// sign-in that needs confirmation).
	Detail string
}

func (e *Error) Error() string { return e.Msg }

func newf(status int, code, format string, a ...any) *Error {
	return &Error{Status: status, Code: code, Msg: fmt.Sprintf(format, a...)}
}

// Constructors.
// New returns an error with a status and code.
func New(status int, code, msg string) error { return newf(status, code, "%s", msg) }

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

// LinkRequired: a sign-in uses the e-mail address of an existing account;
// the user confirms the link by signing in with that account (token).
func LinkRequired(token string) error {
	return &Error{Status: http.StatusConflict, Code: "link_required", Detail: token,
		Msg: "an account with this e-mail address exists: sign in with it once to link this sign-in"}
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
	// invalid input that only the database noticed (e.g. NUL characters,
	// invalid UTF-8): a client error, not an internal one
	var sc interface{ SQLState() string }
	if errors.As(err, &sc) {
		switch sc.SQLState() {
		case "22021", "22P05", "22001", "22003", "22007", "22008", "2201B", "22023", "22P02":
			return http.StatusBadRequest, "bad_request", "invalid characters or values in the request"
		}
	}
	return http.StatusInternalServerError, "internal", "internal server error"
}
