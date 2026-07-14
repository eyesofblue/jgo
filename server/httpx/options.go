package httpx

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/eyesofblue/jgo/middleware"
)

const (
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 15 * time.Second
	defaultWriteTimeout      = 30 * time.Second
	defaultIdleTimeout       = 60 * time.Second
	defaultRequestTimeout    = 30 * time.Second
	defaultMaxBodyBytes      = int64(4 << 20)
)

type config struct {
	name              string
	address           string
	handler           http.Handler
	logger            *slog.Logger
	readHeaderTimeout time.Duration
	readTimeout       time.Duration
	writeTimeout      time.Duration
	idleTimeout       time.Duration
	requestTimeout    time.Duration
	maxBodyBytes      int64
	defaultMiddleware bool
	middlewares       []middleware.Middleware
	outerMiddlewares  []middleware.Middleware
}

func defaultConfig() config {
	return config{
		name:              "http",
		address:           ":8080",
		handler:           http.NotFoundHandler(),
		logger:            slog.Default(),
		readHeaderTimeout: defaultReadHeaderTimeout,
		readTimeout:       defaultReadTimeout,
		writeTimeout:      defaultWriteTimeout,
		idleTimeout:       defaultIdleTimeout,
		requestTimeout:    defaultRequestTimeout,
		maxBodyBytes:      defaultMaxBodyBytes,
		defaultMiddleware: true,
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

func WithHandler(handler http.Handler) Option {
	return func(config *config) { config.handler = handler }
}

func WithLogger(logger *slog.Logger) Option {
	return func(config *config) { config.logger = logger }
}

func WithReadHeaderTimeout(timeout time.Duration) Option {
	return func(config *config) { config.readHeaderTimeout = timeout }
}

func WithReadTimeout(timeout time.Duration) Option {
	return func(config *config) { config.readTimeout = timeout }
}

func WithWriteTimeout(timeout time.Duration) Option {
	return func(config *config) { config.writeTimeout = timeout }
}

func WithIdleTimeout(timeout time.Duration) Option {
	return func(config *config) { config.idleTimeout = timeout }
}

func WithRequestTimeout(timeout time.Duration) Option {
	return func(config *config) { config.requestTimeout = timeout }
}

// WithMaxBodyBytes sets the maximum request body size accepted by the server.
func WithMaxBodyBytes(maxBytes int64) Option {
	return func(config *config) { config.maxBodyBytes = maxBytes }
}

// WithDefaultMiddleware controls OpenTelemetry tracing, trace ID response,
// access log, timeout, and recovery middleware. It is enabled by default.
func WithDefaultMiddleware(enabled bool) Option {
	return func(config *config) { config.defaultMiddleware = enabled }
}

// WithMiddleware adds application middleware inside the JGO default stack.
func WithMiddleware(middlewares ...middleware.Middleware) Option {
	return func(config *config) {
		config.middlewares = append(config.middlewares, middlewares...)
	}
}

// WithOuterMiddleware adds middleware around the timeout, recovery, and
// application stack while keeping it inside OpenTelemetry trace context. It is
// intended for server-level accounting such as RED metrics.
func WithOuterMiddleware(middlewares ...middleware.Middleware) Option {
	return func(config *config) {
		config.outerMiddlewares = append(config.outerMiddlewares, middlewares...)
	}
}
