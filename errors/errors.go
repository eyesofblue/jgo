// Package errors defines errors that can be transported safely across service
// boundaries without exposing their internal causes.
package errors

import (
	stderrors "errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	CodeOK              = 0
	CodeInvalidArgument = 10001
	CodeInternal        = 90001
	CodeTimeout         = 90002
)

const (
	MessageInternal = "internal server error"
	MessageTimeout  = "request timeout"
)

// Error is a public service error with an optional private cause.
type Error struct {
	code       int
	message    string
	httpStatus int
	cause      error
}

// New creates a service error. Invalid public fields are replaced by safe
// internal-error defaults.
func New(code int, message string, opts ...Option) *Error {
	e := &Error{
		code:       code,
		message:    strings.TrimSpace(message),
		httpStatus: http.StatusInternalServerError,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	e.normalize()
	return e
}

// Wrap creates a service error while preserving an internal cause for logs and
// errors.Is/errors.As. The cause is never returned by MessageOf.
func Wrap(cause error, code int, message string, opts ...Option) *Error {
	e := New(code, message, opts...)
	e.cause = cause
	return e
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.cause == nil {
		return e.message
	}
	return fmt.Sprintf("%s: %v", e.message, e.cause)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Code returns the public business error code.
func (e *Error) Code() int {
	if e == nil {
		return CodeInternal
	}
	return e.code
}

// Message returns the public, client-safe message.
func (e *Error) Message() string {
	if e == nil {
		return MessageInternal
	}
	return e.message
}

// HTTPStatus returns the HTTP status associated with the error.
func (e *Error) HTTPStatus() int {
	if e == nil {
		return http.StatusInternalServerError
	}
	return e.httpStatus
}

func (e *Error) normalize() {
	if e.code <= 0 || e.message == "" || e.httpStatus < 400 || e.httpStatus > 599 {
		e.code = CodeInternal
		e.message = MessageInternal
		e.httpStatus = http.StatusInternalServerError
	}
}

// PublicValues extracts client-safe values. Unknown errors always become a
// generic internal error, so their text cannot leak into API responses.
func PublicValues(err error) (code int, message string, httpStatus int) {
	var serviceErr *Error
	if stderrors.As(err, &serviceErr) {
		return serviceErr.Code(), serviceErr.Message(), serviceErr.HTTPStatus()
	}
	return CodeInternal, MessageInternal, http.StatusInternalServerError
}
