package grpcx

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync/atomic"

	jerrors "github.com/eyesofblue/jgo/errors"
	"github.com/eyesofblue/jgo/middleware/requestid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const RequestIDMetadataKey = "x-request-id"

// UnaryRequestID injects an incoming or generated request ID into the handler
// context and response headers.
func UnaryRequestID(generator requestid.Generator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		id := requestIDFromIncomingContext(ctx, generator)
		ctx = requestid.WithContext(ctx, id)
		_ = grpc.SetHeader(ctx, metadata.Pairs(RequestIDMetadataKey, id))
		return handler(ctx, req)
	}
}

// StreamRequestID injects an incoming or generated request ID into a stream
// context and response headers.
func StreamRequestID(generator requestid.Generator) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		id := requestIDFromIncomingContext(stream.Context(), generator)
		ctx := requestid.WithContext(stream.Context(), id)
		_ = stream.SetHeader(metadata.Pairs(RequestIDMetadataKey, id))
		return handler(srv, &contextServerStream{ServerStream: stream, ctx: ctx})
	}
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
	logger = defaultLogger(logger)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (response any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(ctx, "grpc handler panic",
					"method", info.FullMethod,
					"panic", recovered,
					"stack", string(debug.Stack()),
					"request_id", requestid.FromContext(ctx),
				)
				err = jerrors.Wrap(fmt.Errorf("panic: %v", recovered), jerrors.CodeInternal, jerrors.MessageInternal)
			}
		}()
		return handler(ctx, req)
	}
}

// StreamRecovery converts a streaming handler panic into an internal service error.
func StreamRecovery(logger *slog.Logger) grpc.StreamServerInterceptor {
	logger = defaultLogger(logger)
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(stream.Context(), "grpc stream handler panic",
					"method", info.FullMethod,
					"panic", recovered,
					"stack", string(debug.Stack()),
					"request_id", requestid.FromContext(stream.Context()),
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
		UnaryRequestID(nil),
		UnaryErrorMapper(),
		UnaryRecovery(logger),
	}
}

func defaultStreamInterceptors(logger *slog.Logger, activity *activityTracker) []grpc.StreamServerInterceptor {
	return []grpc.StreamServerInterceptor{
		activity.streamInterceptor(),
		StreamRequestID(nil),
		StreamErrorMapper(),
		StreamRecovery(logger),
	}
}

func requestIDFromIncomingContext(ctx context.Context, generator requestid.Generator) string {
	var value string
	if incoming, ok := metadata.FromIncomingContext(ctx); ok {
		values := incoming.Get(RequestIDMetadataKey)
		if len(values) > 0 {
			value = values[0]
		}
	}
	return requestid.Ensure(value, generator)
}

func defaultLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}

type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextServerStream) Context() context.Context { return s.ctx }

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
