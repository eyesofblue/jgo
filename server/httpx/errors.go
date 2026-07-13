package httpx

import "errors"

var (
	ErrInvalidName          = errors.New("httpx: empty server name")
	ErrInvalidAddress       = errors.New("httpx: empty listen address")
	ErrNilHandler           = errors.New("httpx: nil handler")
	ErrInvalidTimeout       = errors.New("httpx: timeouts must be greater than zero")
	ErrAlreadyStarted       = errors.New("httpx: server has already started")
	ErrStoppedBeforeStarted = errors.New("httpx: server was stopped before it started")
)
