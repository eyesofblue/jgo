package grpcx

import "errors"

var (
	ErrNoClients           = errors.New("grpc client: at least one client is required")
	ErrInvalidManagerName  = errors.New("grpc client: manager name is required")
	ErrInvalidClientName   = errors.New("grpc client: client name is required")
	ErrDuplicateClientName = errors.New("grpc client: duplicate normalized client name")
	ErrInvalidAddress      = errors.New("grpc client: address is required")
	ErrInvalidTimeout      = errors.New("grpc client: timeout must be positive")
	ErrUnknownClient       = errors.New("grpc client: unknown client")
	ErrClosed              = errors.New("grpc client: manager is closed")
	ErrAlreadyStarted      = errors.New("grpc client: manager already started")
)
