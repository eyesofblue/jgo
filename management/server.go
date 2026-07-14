// Package management provides a dedicated operational HTTP server.
package management

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/eyesofblue/jgo/app"
	"github.com/eyesofblue/jgo/readiness"
)

var _ app.Component = (*Server)(nil)

type Config struct {
	Address         string
	MetricsEnabled  bool
	MetricsHandler  http.Handler
	Readiness       *readiness.Registry
	ShutdownTimeout time.Duration
	Logger          *slog.Logger
}

type Server struct {
	mu       sync.Mutex
	config   Config
	listener net.Listener
	server   *http.Server
	started  bool
	stopped  bool
}

var (
	ErrAlreadyStarted        = errors.New("management: server has already started")
	ErrMetricsHandlerMissing = errors.New("management: metrics handler is required when metrics are enabled")
)

func New(config Config) (*Server, error) {
	config.Address = strings.TrimSpace(config.Address)
	if config.Address == "" {
		config.Address = ":9091"
	}
	if config.ShutdownTimeout <= 0 {
		config.ShutdownTimeout = 5 * time.Second
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.Readiness == nil {
		config.Readiness = readiness.New(time.Second)
	}
	if config.MetricsEnabled && config.MetricsHandler == nil {
		return nil, ErrMetricsHandlerMissing
	}
	return &Server{config: config}, nil
}

func (server *Server) Name() string { return "management" }
func (server *Server) Address() string {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.listener != nil {
		return server.listener.Addr().String()
	}
	return server.config.Address
}

func (server *Server) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	server.mu.Lock()
	if server.started {
		server.mu.Unlock()
		return ErrAlreadyStarted
	}
	if server.stopped {
		server.mu.Unlock()
		return nil
	}
	server.started = true
	listener, err := net.Listen("tcp", server.config.Address)
	if err != nil {
		server.mu.Unlock()
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(writer http.ResponseWriter, request *http.Request) {
		report := server.config.Readiness.Check(request.Context())
		status := http.StatusOK
		if !report.Ready {
			status = http.StatusServiceUnavailable
		}
		writeJSON(writer, status, report)
	})
	if server.config.MetricsEnabled && server.config.MetricsHandler != nil {
		mux.Handle("GET /metrics", server.config.MetricsHandler)
	}
	httpServer := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	server.listener, server.server = listener, httpServer
	server.mu.Unlock()
	server.config.Logger.InfoContext(ctx, "management server starting", "address", listener.Addr().String())
	err = httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (server *Server) Stop(ctx context.Context) error {
	server.mu.Lock()
	server.stopped = true
	httpServer := server.server
	listener := server.listener
	server.mu.Unlock()
	if httpServer == nil {
		if listener != nil {
			return listener.Close()
		}
		return nil
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), server.config.ShutdownTimeout)
		defer cancel()
	}
	if err := httpServer.Shutdown(ctx); err != nil {
		_ = httpServer.Close()
		return fmt.Errorf("management: shutdown: %w", err)
	}
	return nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
