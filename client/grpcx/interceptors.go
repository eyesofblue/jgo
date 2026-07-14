package grpcx

import (
	"context"
	"log/slog"
	"time"

	"github.com/eyesofblue/jgo/logx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

func unaryTimeout(timeout time.Duration) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, request, response any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, callOptions ...grpc.CallOption) error {
		if ctx == nil {
			ctx = context.Background()
		}
		if deadline, exists := ctx.Deadline(); exists && time.Until(deadline) <= timeout {
			return invoker(ctx, method, request, response, connection, callOptions...)
		}
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return invoker(callCtx, method, request, response, connection, callOptions...)
	}
}

func unaryErrorLog(name string, logger *slog.Logger) grpc.UnaryClientInterceptor {
	contextLogger := logx.New(logger)
	return func(ctx context.Context, method string, request, response any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, callOptions ...grpc.CallOption) error {
		startedAt := time.Now()
		err := invoker(ctx, method, request, response, connection, callOptions...)
		if err != nil {
			contextLogger.ErrorCtx(ctx, "grpc client call failed",
				"client", name,
				"method", method,
				"grpc_code", status.Code(err).String(),
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"err", err,
			)
		}
		return err
	}
}
