package grpcx

import (
	"log/slog"
	"net"

	"google.golang.org/grpc"
)

// RegisterFunc registers generated gRPC service implementations.
type RegisterFunc func(grpc.ServiceRegistrar)

type config struct {
	name               string
	address            string
	listener           net.Listener
	logger             *slog.Logger
	reflection         bool
	register           []RegisterFunc
	unaryInterceptors  []grpc.UnaryServerInterceptor
	streamInterceptors []grpc.StreamServerInterceptor
	serverOptions      []grpc.ServerOption
}

func defaultConfig() config {
	return config{
		name:    "grpc",
		address: ":9090",
		logger:  slog.Default(),
	}
}

// Option configures a Server.
type Option func(*config)

func WithName(name string) Option {
	return func(config *config) { config.name = name }
}

func WithAddress(address string) Option {
	return func(config *config) { config.address = address }
}

// WithListener supplies an already bound listener. The Server owns it after
// New returns successfully. It is primarily useful for bufconn tests.
func WithListener(listener net.Listener) Option {
	return func(config *config) { config.listener = listener }
}

func WithLogger(logger *slog.Logger) Option {
	return func(config *config) { config.logger = logger }
}

func WithReflection(enabled bool) Option {
	return func(config *config) { config.reflection = enabled }
}

func WithRegister(register ...RegisterFunc) Option {
	return func(config *config) { config.register = append(config.register, register...) }
}

// WithUnaryInterceptors adds interceptors inside the JGO default interceptors.
func WithUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor) Option {
	return func(config *config) {
		config.unaryInterceptors = append(config.unaryInterceptors, interceptors...)
	}
}

// WithStreamInterceptors adds interceptors inside the JGO default interceptors.
func WithStreamInterceptors(interceptors ...grpc.StreamServerInterceptor) Option {
	return func(config *config) {
		config.streamInterceptors = append(config.streamInterceptors, interceptors...)
	}
}

// WithServerOptions supplies lower-level gRPC options such as transport
// credentials. Interceptor options must use the dedicated methods above.
func WithServerOptions(options ...grpc.ServerOption) Option {
	return func(config *config) { config.serverOptions = append(config.serverOptions, options...) }
}
