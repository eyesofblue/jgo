package app

import "time"

const defaultShutdownTimeout = 10 * time.Second

// Option configures an App.
type Option func(*App)

// WithName sets the name used to identify the application.
func WithName(name string) Option {
	return func(a *App) {
		a.name = name
	}
}

// WithShutdownTimeout limits the total time allowed for stopping components.
func WithShutdownTimeout(timeout time.Duration) Option {
	return func(a *App) {
		a.shutdownTimeout = timeout
	}
}
