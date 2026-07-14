// Package grpcx provides a gRPC server component for app.App.
package grpcx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"

	"github.com/eyesofblue/jgo/app"
	"github.com/eyesofblue/jgo/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var _ app.Component = (*Server)(nil)
var _ app.StartupNotifier = (*Server)(nil)

// Server adapts grpc.Server to the JGO component lifecycle.
type Server struct {
	mu            sync.Mutex
	name          string
	address       string
	listener      net.Listener
	logger        *slog.Logger
	reflection    bool
	register      []RegisterFunc
	unary         []grpc.UnaryServerInterceptor
	stream        []grpc.StreamServerInterceptor
	options       []grpc.ServerOption
	activity      *activityTracker
	server        *grpc.Server
	started       bool
	startup       chan struct{}
	startupOnce   sync.Once
	stopRequested bool
}

// New creates a gRPC server with OpenTelemetry, error mapping, and recovery
// enabled by default.
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
	transportCredentials, err := serverCredentials(config.tls)
	if err != nil {
		return nil, err
	}
	if transportCredentials != nil {
		config.serverOptions = append(config.serverOptions, grpc.Creds(transportCredentials))
	}

	activity := newActivityTracker()
	unary := append([]grpc.UnaryServerInterceptor(nil), config.unaryInterceptors...)
	unary = append(unary, defaultUnaryInterceptors(config.logger, activity)...)
	stream := append([]grpc.StreamServerInterceptor(nil), config.streamInterceptors...)
	stream = append(stream, defaultStreamInterceptors(config.logger, activity)...)
	if config.authenticator != nil || config.authorizer != nil {
		unary = append(unary, UnarySecurity(config.authenticator, config.authorizer))
		stream = append(stream, StreamSecurity(config.authenticator, config.authorizer))
	}
	return &Server{
		name:       strings.TrimSpace(config.name),
		address:    strings.TrimSpace(config.address),
		listener:   config.listener,
		logger:     config.logger,
		reflection: config.reflection,
		register:   append([]RegisterFunc(nil), config.register...),
		unary:      unary,
		stream:     stream,
		options:    append([]grpc.ServerOption(nil), config.serverOptions...),
		activity:   activity,
		startup:    make(chan struct{}),
	}, nil
}

func validate(config config) error {
	if strings.TrimSpace(config.name) == "" {
		return ErrInvalidName
	}
	if config.listener == nil && strings.TrimSpace(config.address) == "" {
		return ErrInvalidAddress
	}
	registerCount := 0
	for _, register := range config.register {
		if register != nil {
			registerCount++
		}
	}
	if registerCount == 0 {
		return ErrNoRegisterFunctions
	}
	return nil
}

func (s *Server) Name() string { return s.name }

func (s *Server) Started() <-chan struct{} { return s.startup }

func (s *Server) Address() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.address
}

// Start registers services and serves until Stop is called.
func (s *Server) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	listener, server, err := s.prepare(ctx)
	if err != nil || server == nil {
		return err
	}

	s.logger.InfoContext(ctx, "grpc server starting", "name", s.name, "address", listener.Addr().String())
	err = server.Serve(listener)
	if errors.Is(err, grpc.ErrServerStopped) {
		return nil
	}
	return err
}

func (s *Server) prepare(ctx context.Context) (net.Listener, *grpc.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil, nil, ErrAlreadyStarted
	}
	if s.stopRequested || ctx.Err() != nil {
		return nil, nil, nil
	}
	s.started = true

	listener := s.listener
	var err error
	if listener == nil {
		listener, err = net.Listen("tcp", s.address)
		if err != nil {
			return nil, nil, err
		}
		s.listener = listener
	}

	options := append([]grpc.ServerOption(nil), s.options...)
	options = append(options,
		grpc.StatsHandler(otelgrpc.NewServerHandler(
			otelgrpc.WithPropagators(telemetry.Propagator()),
		)),
		grpc.ChainUnaryInterceptor(s.unary...),
		grpc.ChainStreamInterceptor(s.stream...),
	)
	server := grpc.NewServer(options...)
	// Publish the server before invoking user-supplied registration callbacks.
	// If one panics, App's recovery can still call Stop without deadlocking or
	// leaking the listener; the deferred unlock above always releases the mutex.
	s.server = server
	for _, register := range s.register {
		if register != nil {
			register(server)
		}
	}
	if s.reflection {
		reflection.Register(server)
	}
	s.startupOnce.Do(func() { close(s.startup) })
	return listener, server, nil
}

// Stop attempts a graceful stop until ctx expires, then force-stops the server.
func (s *Server) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	s.stopRequested = true
	server := s.server
	listener := s.listener
	s.mu.Unlock()

	if server == nil {
		if listener != nil {
			return listener.Close()
		}
		return nil
	}

	s.logger.InfoContext(ctx, "grpc server stopping", "name", s.name)
	s.activity.startDraining()
	if err := s.activity.wait(ctx); err != nil {
		server.Stop()
		return fmt.Errorf("%w: %v", ErrGracefulStopTimeout, err)
	}
	server.GracefulStop()
	return nil
}
