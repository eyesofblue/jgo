// Package grpcx manages reusable outbound gRPC client connections.
package grpcx

import "time"

const DefaultTimeout = 3 * time.Second

// Config describes one named RPC dependency.
type Config struct {
	Address string
	Timeout time.Duration
	TLS     TLSConfig
}

// TLSConfig configures transport security for one RPC dependency. TLS is
// disabled by default for local development.
type TLSConfig struct {
	Enabled    bool
	ServerName string
	CAFile     string
}
