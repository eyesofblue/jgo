package grpcx

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync/atomic"

	jerrors "github.com/eyesofblue/jgo/errors"
	"github.com/eyesofblue/jgo/logx"
	"github.com/eyesofblue/jgo/security"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func UnarySecurity(authenticator security.Authenticator, authorizer security.Authorizer) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		secured, err := secureContext(ctx, info.FullMethod, authenticator, authorizer)
		if err != nil {
			return nil, err
		}
		return handler(secured, req)
	}
}

func StreamSecurity(authenticator security.Authenticator, authorizer security.Authorizer) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		secured, err := secureContext(stream.Context(), info.FullMethod, authenticator, authorizer)
		if err != nil {
			return err
		}
		return handler(srv, &serverStreamContext{ServerStream: stream, ctx: secured})
	}
}

type serverStreamContext struct {
	grpc.ServerStream
	ctx context.Context
}

func (stream *serverStreamContext) Context() context.Context { return stream.ctx }

func secureContext(ctx context.Context, resource string, authenticator security.Authenticator, authorizer security.Authorizer) (context.Context, error) {
	principal := security.Principal{}
	if authenticator != nil {
		credential, ok := incomingCredential(ctx)
		if !ok {
			return ctx, status.Error(codes.Unauthenticated, "authentication required")
		}
		value, err := authenticator.Authenticate(ctx, credential)
		if err != nil {
			return ctx, status.Error(codes.Unauthenticated, "authentication failed")
		}
		principal = value
		ctx = security.NewContext(ctx, principal)
	} else if value, ok := security.FromContext(ctx); ok {
		principal = value
	}
	if authorizer != nil {
		if err := authorizer.Authorize(ctx, principal, resource); err != nil {
			return ctx, status.Error(codes.PermissionDenied, "permission denied")
		}
	}
	return ctx, nil
}

func incomingCredential(ctx context.Context) (security.Credential, bool) {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) == 0 {
		return security.Credential{}, false
	}
	parts := strings.SplitN(strings.TrimSpace(values[0]), " ", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return security.Credential{}, false
	}
	return security.Credential{Scheme: parts[0], Value: parts[1]}, true
}

// UnaryErrorMapper converts application errors into gRPC status errors.
func UnaryErrorMapper() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		response, err := handler(ctx, req)
		return response, StatusError(err)
	}
}

// StreamErrorMapper converts application errors into gRPC status errors.
func StreamErrorMapper() grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return StatusError(handler(srv, stream))
	}
}

// UnaryRecovery converts a unary handler panic into an internal service error.
func UnaryRecovery(logger *slog.Logger) grpc.UnaryServerInterceptor {
	contextLogger := logx.New(defaultLogger(logger))
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (response any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				contextLogger.ErrorCtx(ctx, "grpc handler panic",
					"method", info.FullMethod,
					"panic", recovered,
					"stack", string(debug.Stack()),
				)
				err = jerrors.Wrap(fmt.Errorf("panic: %v", recovered), jerrors.CodeInternal, jerrors.MessageInternal)
			}
		}()
		return handler(ctx, req)
	}
}

// StreamRecovery converts a streaming handler panic into an internal service error.
func StreamRecovery(logger *slog.Logger) grpc.StreamServerInterceptor {
	contextLogger := logx.New(defaultLogger(logger))
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				contextLogger.ErrorCtx(stream.Context(), "grpc stream handler panic",
					"method", info.FullMethod,
					"panic", recovered,
					"stack", string(debug.Stack()),
				)
				err = jerrors.Wrap(fmt.Errorf("panic: %v", recovered), jerrors.CodeInternal, jerrors.MessageInternal)
			}
		}()
		return handler(srv, stream)
	}
}

func defaultUnaryInterceptors(logger *slog.Logger, activity *activityTracker) []grpc.UnaryServerInterceptor {
	return []grpc.UnaryServerInterceptor{
		activity.unaryInterceptor(),
		UnaryErrorMapper(),
		UnaryRecovery(logger),
	}
}

func defaultStreamInterceptors(logger *slog.Logger, activity *activityTracker) []grpc.StreamServerInterceptor {
	return []grpc.StreamServerInterceptor{
		activity.streamInterceptor(),
		StreamErrorMapper(),
		StreamRecovery(logger),
	}
}

func defaultLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}

type activityTracker struct {
	draining atomic.Bool
	active   atomic.Int64
	changed  chan struct{}
}

func newActivityTracker() *activityTracker {
	return &activityTracker{changed: make(chan struct{}, 1)}
}

func (t *activityTracker) unaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !t.enter() {
			return nil, status.Error(codes.Unavailable, "server is shutting down")
		}
		defer t.leave()
		return handler(ctx, req)
	}
}

func (t *activityTracker) streamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !t.enter() {
			return status.Error(codes.Unavailable, "server is shutting down")
		}
		defer t.leave()
		return handler(srv, stream)
	}
}

func (t *activityTracker) enter() bool {
	if t.draining.Load() {
		return false
	}
	t.active.Add(1)
	if t.draining.Load() {
		t.active.Add(-1)
		t.notify()
		return false
	}
	return true
}

func (t *activityTracker) leave() {
	t.active.Add(-1)
	t.notify()
}

func (t *activityTracker) startDraining() {
	t.draining.Store(true)
	t.notify()
}

func (t *activityTracker) wait(ctx context.Context) error {
	for {
		if t.active.Load() == 0 {
			return nil
		}
		select {
		case <-t.changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (t *activityTracker) notify() {
	select {
	case t.changed <- struct{}{}:
	default:
	}
}
