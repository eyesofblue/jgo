// Package httpx provides a safe net/http server component for app.App.
package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/eyesofblue/jgo/app"
	"github.com/eyesofblue/jgo/middleware"
	"github.com/eyesofblue/jgo/middleware/accesslog"
	"github.com/eyesofblue/jgo/middleware/recovery"
	"github.com/eyesofblue/jgo/middleware/requestid"
	timeoutmw "github.com/eyesofblue/jgo/middleware/timeout"
)

var _ app.Component = (*Server)(nil)

// Server adapts net/http.Server to the JGO component lifecycle.
type Server struct {
	mu            sync.Mutex
	name          string
	address       string
	handler       http.Handler
	logger        *slog.Logger
	readHeader    time.Duration
	read          time.Duration
	write         time.Duration
	idle          time.Duration
	server        *http.Server
	listener      net.Listener
	started       bool
	stopRequested bool
}

// New creates an HTTP server with safe timeout and middleware defaults.
func New(opts ...Option) (*Server, error) {
	config := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&config)
		}
	}
	if config.logger == nil {
		config.logger = slog.Default()
	}
	if err := validate(config); err != nil {
		return nil, err
	}

	handler := middleware.Chain(config.handler, config.middlewares...)
	if config.defaultMiddleware {
		handler = middleware.Chain(handler,
			requestid.New(nil),
			accesslog.New(config.logger),
			timeoutmw.New(config.requestTimeout),
			recovery.New(config.logger),
		)
	}

	return &Server{
		name:       strings.TrimSpace(config.name),
		address:    strings.TrimSpace(config.address),
		handler:    handler,
		logger:     config.logger,
		readHeader: config.readHeaderTimeout,
		read:       config.readTimeout,
		write:      config.writeTimeout,
		idle:       config.idleTimeout,
	}, nil
}

func validate(config config) error {
	if strings.TrimSpace(config.name) == "" {
		return ErrInvalidName
	}
	if strings.TrimSpace(config.address) == "" {
		return ErrInvalidAddress
	}
	if config.handler == nil {
		return ErrNilHandler
	}
	if config.readHeaderTimeout <= 0 || config.readTimeout <= 0 || config.writeTimeout <= 0 ||
		config.idleTimeout <= 0 || (config.defaultMiddleware && config.requestTimeout <= 0) {
		return ErrInvalidTimeout
	}
	return nil
}

func (s *Server) Name() string { return s.name }

// Address returns the configured address before Start and the bound listener
// address after Start. This is useful when binding to port 0 in tests.
func (s *Server) Address() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.address
}

// Start binds the listen address and serves until Stop is called.
func (s *Server) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return ErrAlreadyStarted
	}
	if s.stopRequested || ctx.Err() != nil {
		s.mu.Unlock()
		return nil
	}
	s.started = true

	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.listener = listener
	s.server = &http.Server{
		Addr:              s.address,
		Handler:           s.handler,
		ReadHeaderTimeout: s.readHeader,
		ReadTimeout:       s.read,
		WriteTimeout:      s.write,
		IdleTimeout:       s.idle,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	server := s.server
	s.mu.Unlock()

	s.logger.InfoContext(ctx, "http server starting", "name", s.name, "address", listener.Addr().String())
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Stop gracefully shuts down the HTTP server. Calling Stop before Start is
// safe and prevents a later Start from opening a listener.
func (s *Server) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	s.stopRequested = true
	server := s.server
	s.mu.Unlock()
	if server == nil {
		return nil
	}
	s.logger.InfoContext(ctx, "http server stopping", "name", s.name)
	return server.Shutdown(ctx)
}
