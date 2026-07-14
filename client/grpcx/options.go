package grpcx

import (
	"log/slog"

	"google.golang.org/grpc"
)

type managerOptions struct {
	name        string
	logger      *slog.Logger
	dialOptions map[string][]grpc.DialOption
}

func defaultOptions() managerOptions {
	return managerOptions{
		name:        "grpc-clients",
		logger:      slog.Default(),
		dialOptions: make(map[string][]grpc.DialOption),
	}
}

// Option configures a Manager.
type Option func(*managerOptions)

// WithName changes the app.Component name used by this manager.
func WithName(name string) Option {
	return func(options *managerOptions) { options.name = name }
}

// WithLogger sets the structured logger used for transport failures.
func WithLogger(logger *slog.Logger) Option {
	return func(options *managerOptions) { options.logger = logger }
}

// WithDialOptions adds low-level options to one named client. It is intended
// for custom dialers, resolvers, and test transports. Transport credentials,
// tracing, timeout, and retry behavior remain owned by JGO.
func WithDialOptions(name string, dialOptions ...grpc.DialOption) Option {
	return func(options *managerOptions) {
		options.dialOptions[name] = append(options.dialOptions[name], dialOptions...)
	}
}
