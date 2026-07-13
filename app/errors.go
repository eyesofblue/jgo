package app

import (
	"errors"
	"fmt"
)

var (
	ErrNoComponents       = errors.New("app: no components registered")
	ErrNilComponent       = errors.New("app: nil component")
	ErrEmptyComponentName = errors.New("app: empty component name")
	ErrDuplicateComponent = errors.New("app: duplicate component name")
	ErrInvalidName        = errors.New("app: empty application name")
	ErrInvalidTimeout     = errors.New("app: shutdown timeout must be greater than zero")
	ErrAppStarted         = errors.New("app: application has already started")
	ErrAlreadyRun         = errors.New("app: application can only run once")
	ErrShutdownTimeout    = errors.New("app: shutdown timed out")
)

// ComponentError identifies the component and lifecycle operation that failed.
type ComponentError struct {
	Op   string
	Name string
	Err  error
}

func (e *ComponentError) Error() string {
	return fmt.Sprintf("app: component %q %s: %v", e.Name, e.Op, e.Err)
}

func (e *ComponentError) Unwrap() error {
	return e.Err
}
