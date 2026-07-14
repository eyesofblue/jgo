package grpcx

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/eyesofblue/jgo/app"
	"github.com/eyesofblue/jgo/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

var _ app.Component = (*Manager)(nil)

// Manager owns named, reusable gRPC ClientConn instances. New creates the
// connections without performing network I/O; grpc-go connects on demand.
type Manager struct {
	mu          sync.Mutex
	name        string
	connections map[string]*grpc.ClientConn
	done        chan struct{}
	started     bool
	closed      bool
}

// New validates every client, creates lazy grpc.ClientConn values, and fails
// transactionally without retaining partial connections.
func New(configs map[string]Config, opts ...Option) (*Manager, error) {
	if len(configs) == 0 {
		return nil, ErrNoClients
	}
	options := defaultOptions()
	for _, option := range opts {
		if option != nil {
			option(&options)
		}
	}
	options.name = strings.TrimSpace(options.name)
	if options.name == "" {
		return nil, ErrInvalidManagerName
	}
	if options.logger == nil {
		options.logger = slog.Default()
	}

	normalized, err := normalizeConfigs(configs)
	if err != nil {
		return nil, err
	}
	namedOptions, err := normalizeDialOptions(options.dialOptions, normalized)
	if err != nil {
		return nil, err
	}

	manager := &Manager{
		name:        options.name,
		connections: make(map[string]*grpc.ClientConn, len(normalized)),
		done:        make(chan struct{}),
	}
	names := sortedNames(normalized)
	for _, name := range names {
		config := normalized[name]
		connection, err := newConnection(name, config, options.logger, namedOptions[name])
		if err != nil {
			_ = manager.closeConnections()
			return nil, err
		}
		manager.connections[name] = connection
	}
	return manager, nil
}

func normalizeConfigs(configs map[string]Config) (map[string]Config, error) {
	normalized := make(map[string]Config, len(configs))
	for rawName, config := range configs {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return nil, ErrInvalidClientName
		}
		if _, exists := normalized[name]; exists {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateClientName, name)
		}
		config.Address = strings.TrimSpace(config.Address)
		config.TLS.ServerName = strings.TrimSpace(config.TLS.ServerName)
		config.TLS.CAFile = strings.TrimSpace(config.TLS.CAFile)
		if config.Address == "" {
			return nil, fmt.Errorf("%w: %s", ErrInvalidAddress, name)
		}
		if config.Timeout < 0 {
			return nil, fmt.Errorf("%w: %s", ErrInvalidTimeout, name)
		}
		if config.Timeout == 0 {
			config.Timeout = DefaultTimeout
		}
		normalized[name] = config
	}
	return normalized, nil
}

func normalizeDialOptions(raw map[string][]grpc.DialOption, configs map[string]Config) (map[string][]grpc.DialOption, error) {
	normalized := make(map[string][]grpc.DialOption, len(raw))
	for rawName, dialOptions := range raw {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return nil, ErrInvalidClientName
		}
		if _, exists := configs[name]; !exists {
			return nil, fmt.Errorf("%w: %s", ErrUnknownClient, name)
		}
		normalized[name] = append(normalized[name], dialOptions...)
	}
	return normalized, nil
}

func newConnection(name string, config Config, logger *slog.Logger, custom []grpc.DialOption) (*grpc.ClientConn, error) {
	transportCredentials, err := credentialsFor(config.TLS)
	if err != nil {
		return nil, fmt.Errorf("grpc client %s: %w", name, err)
	}
	dialOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(transportCredentials),
		grpc.WithDisableRetry(),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler(
			otelgrpc.WithPropagators(telemetry.Propagator()),
		)),
		grpc.WithChainUnaryInterceptor(
			unaryTimeout(config.Timeout),
			unaryErrorLog(name, logger),
		),
	}
	dialOptions = append(dialOptions, custom...)
	connection, err := grpc.NewClient(config.Address, dialOptions...)
	if err != nil {
		return nil, fmt.Errorf("grpc client %s: create connection: %w", name, err)
	}
	return connection, nil
}

func credentialsFor(config TLSConfig) (credentials.TransportCredentials, error) {
	if !config.Enabled {
		return insecure.NewCredentials(), nil
	}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: config.ServerName,
	}
	if config.CAFile == "" {
		return credentials.NewTLS(tlsConfig), nil
	}
	contents, err := os.ReadFile(config.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read TLS CA file %s: %w", config.CAFile, err)
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(contents) {
		return nil, fmt.Errorf("TLS CA file %s contains no valid certificates", config.CAFile)
	}
	tlsConfig.RootCAs = roots
	return credentials.NewTLS(tlsConfig), nil
}

func (m *Manager) Name() string { return m.name }

// Conn returns a named connection for constructing generated protobuf clients.
func (m *Manager) Conn(name string) (*grpc.ClientConn, error) {
	name = strings.TrimSpace(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	connection, exists := m.connections[name]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrUnknownClient, name)
	}
	return connection, nil
}

// Start waits until the application is canceled or the manager is stopped.
func (m *Manager) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	if m.started {
		m.mu.Unlock()
		return ErrAlreadyStarted
	}
	m.started = true
	done := m.done
	m.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-done:
	}
	return nil
}

// Stop closes all connections. It is safe before Start and is idempotent.
func (m *Manager) Stop(context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	close(m.done)
	m.mu.Unlock()
	return m.closeConnections()
}

func (m *Manager) closeConnections() error {
	names := make([]string, 0, len(m.connections))
	for name := range m.connections {
		names = append(names, name)
	}
	sort.Strings(names)
	var closeErrors []error
	for _, name := range names {
		if err := m.connections[name].Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close %s: %w", name, err))
		}
	}
	return errors.Join(closeErrors...)
}

func sortedNames(configs map[string]Config) []string {
	names := make([]string, 0, len(configs))
	for name := range configs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
