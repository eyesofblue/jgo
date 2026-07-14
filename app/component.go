// Package app manages the lifecycle of the components that make up a process.
package app

import "context"

// Component is a long-running part of an application, such as an HTTP or gRPC
// server. Start blocks until the component stops. Stop asks the component to
// stop accepting new work and release its resources.
type Component interface {
	Name() string
	Start(context.Context) error
	Stop(context.Context) error
}

// StartupNotifier is implemented by components that can report when their
// startup boundary (for example, binding a listener) has completed.
type StartupNotifier interface {
	Started() <-chan struct{}
}
