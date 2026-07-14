package grpcx

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	"github.com/eyesofblue/jgo/security"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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
	tls                TLSConfig
	authenticator      security.Authenticator
	authorizer         security.Authorizer
}

type TLSConfig struct {
	Enabled      bool
	CertFile     string
	KeyFile      string
	ClientAuth   string
	ClientCAFile string
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

func WithTLS(tlsConfig TLSConfig) Option { return func(options *config) { options.tls = tlsConfig } }
func WithAuthenticator(authenticator security.Authenticator) Option {
	return func(options *config) { options.authenticator = authenticator }
}
func WithAuthorizer(authorizer security.Authorizer) Option {
	return func(options *config) { options.authorizer = authorizer }
}

func WithRegister(register ...RegisterFunc) Option {
	return func(config *config) { config.register = append(config.register, register...) }
}

// WithUnaryInterceptors adds server-level observers outside JGO lifecycle,
// error-mapping, recovery, and security interceptors. They therefore observe
// the final gRPC status returned to clients.
func WithUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor) Option {
	return func(config *config) {
		config.unaryInterceptors = append(config.unaryInterceptors, interceptors...)
	}
}

// WithStreamInterceptors is the streaming equivalent of
// WithUnaryInterceptors.
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

func serverCredentials(config TLSConfig) (credentials.TransportCredentials, error) {
	if !config.Enabled {
		return nil, nil
	}
	config.CertFile, config.KeyFile = strings.TrimSpace(config.CertFile), strings.TrimSpace(config.KeyFile)
	if config.CertFile == "" || config.KeyFile == "" {
		return nil, fmt.Errorf("grpc server TLS: cert_file and key_file are required")
	}
	certificate, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("grpc server TLS: load certificate: %w", err)
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	switch strings.TrimSpace(config.ClientAuth) {
	case "", "none":
	case "require_and_verify":
		if strings.TrimSpace(config.ClientCAFile) == "" {
			return nil, fmt.Errorf("grpc server TLS: client_ca_file is required for mTLS")
		}
		contents, err := os.ReadFile(config.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("grpc server TLS: read client CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(contents) {
			return nil, fmt.Errorf("grpc server TLS: client CA contains no valid certificates")
		}
		tlsConfig.ClientAuth, tlsConfig.ClientCAs = tls.RequireAndVerifyClientCert, pool
	default:
		return nil, fmt.Errorf("grpc server TLS: client_auth must be none or require_and_verify")
	}
	return credentials.NewTLS(tlsConfig), nil
}
