package grpcx

import "errors"

var (
	ErrInvalidName         = errors.New("grpcx: empty server name")
	ErrInvalidAddress      = errors.New("grpcx: empty listen address")
	ErrNoRegisterFunctions = errors.New("grpcx: no service register functions")
	ErrAlreadyStarted      = errors.New("grpcx: server has already started")
	ErrGracefulStopTimeout = errors.New("grpcx: graceful stop timed out")
)
