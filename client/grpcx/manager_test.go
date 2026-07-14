package grpcx

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eyesofblue/jgo/middleware/traceid"
	"github.com/eyesofblue/jgo/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const testBufferSize = 1024 * 1024

type managerTestService interface {
	Echo(context.Context, *wrapperspb.StringValue) (*wrapperspb.StringValue, error)
	Block(context.Context, *wrapperspb.StringValue) (*wrapperspb.StringValue, error)
	Fail(context.Context, *wrapperspb.StringValue) (*wrapperspb.StringValue, error)
}

type managerTestServiceImpl struct {
	failCalls atomic.Int64
}

func (s *managerTestServiceImpl) Echo(ctx context.Context, request *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	if request.Value == "trace-id" {
		return wrapperspb.String(traceid.FromContext(ctx)), nil
	}
	return request, nil
}

func (s *managerTestServiceImpl) Block(ctx context.Context, _ *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *managerTestServiceImpl) Fail(context.Context, *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	s.failCalls.Add(1)
	return nil, status.Error(codes.Unavailable, "temporary failure")
}

func TestManagerCallsRPCWithTraceAndNoRetry(t *testing.T) {
	listener, server, implementation := startManagerTestServer(t)
	defer server.Stop()
	manager := newBufconnManager(t, listener, 200*time.Millisecond, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	defer manager.Stop(context.Background())
	connection, err := manager.Conn("user")
	if err != nil {
		t.Fatal(err)
	}

	response := new(wrapperspb.StringValue)
	if err := connection.Invoke(context.Background(), "/test.Manager/Echo", wrapperspb.String("hello"), response); err != nil {
		t.Fatal(err)
	}
	if response.Value != "hello" {
		t.Fatalf("response = %q", response.Value)
	}

	ctx := trace.ContextWithSpanContext(context.Background(), fixedSpanContext(t))
	if err := connection.Invoke(ctx, "/test.Manager/Echo", wrapperspb.String("trace-id"), response); err != nil {
		t.Fatal(err)
	}
	if response.Value != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("trace ID = %q", response.Value)
	}

	err = connection.Invoke(ctx, "/test.Manager/Fail", wrapperspb.String(""), new(wrapperspb.StringValue))
	if status.Code(err) != codes.Unavailable || implementation.failCalls.Load() != 1 {
		t.Fatalf("Fail() = %v, calls = %d", err, implementation.failCalls.Load())
	}
}

func TestManagerAppliesConfiguredTimeout(t *testing.T) {
	listener, server, _ := startManagerTestServer(t)
	defer server.Stop()
	var logs bytes.Buffer
	manager := newBufconnManager(t, listener, 30*time.Millisecond, slog.New(slog.NewJSONHandler(&logs, nil)))
	defer manager.Stop(context.Background())
	connection, _ := manager.Conn("user")

	startedAt := time.Now()
	err := connection.Invoke(context.Background(), "/test.Manager/Block", wrapperspb.String(""), new(wrapperspb.StringValue))
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("Block() = %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("configured timeout took %s", elapsed)
	}
	if !strings.Contains(logs.String(), `"grpc_code":"DeadlineExceeded"`) {
		t.Fatalf("transport error log missing: %s", logs.String())
	}
}

func TestCallerDeadlineTakesPriority(t *testing.T) {
	listener, server, _ := startManagerTestServer(t)
	defer server.Stop()
	manager := newBufconnManager(t, listener, time.Second, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	defer manager.Stop(context.Background())
	connection, _ := manager.Conn("user")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	startedAt := time.Now()
	err := connection.Invoke(ctx, "/test.Manager/Block", wrapperspb.String(""), new(wrapperspb.StringValue))
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("Block() = %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("caller deadline took %s", elapsed)
	}
}

func TestConfiguredTimeoutTakesPriorityOverLaterCallerDeadline(t *testing.T) {
	listener, server, _ := startManagerTestServer(t)
	defer server.Stop()
	manager := newBufconnManager(t, listener, 30*time.Millisecond, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	defer manager.Stop(context.Background())
	connection, _ := manager.Conn("user")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	startedAt := time.Now()
	err := connection.Invoke(ctx, "/test.Manager/Block", wrapperspb.String(""), new(wrapperspb.StringValue))
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("Block() = %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("configured timeout took %s", elapsed)
	}
}

func TestUnavailableDependencyDoesNotBlockStartup(t *testing.T) {
	manager, err := New(map[string]Config{
		"user": {Address: "passthrough:///127.0.0.1:1", Timeout: 100 * time.Millisecond},
	}, WithLogger(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))))
	if err != nil {
		t.Fatal(err)
	}
	startDone := make(chan error, 1)
	go func() { startDone <- manager.Start(context.Background()) }()
	connection, err := manager.Conn("user")
	if err != nil {
		t.Fatal(err)
	}
	err = connection.Invoke(context.Background(), "/test.Manager/Echo", wrapperspb.String(""), new(wrapperspb.StringValue))
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Invoke() = %v", err)
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-startDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start did not return after Stop")
	}
	if _, err := manager.Conn("user"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Conn() after Stop = %v", err)
	}
}

func TestManagerValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		configs map[string]Config
		options []Option
		want    error
	}{
		{name: "no clients", configs: nil, want: ErrNoClients},
		{name: "manager name", configs: validConfigs(), options: []Option{WithName(" ")}, want: ErrInvalidManagerName},
		{name: "client name", configs: map[string]Config{" ": {Address: "localhost:9090"}}, want: ErrInvalidClientName},
		{name: "duplicate normalized name", configs: map[string]Config{"user": {Address: "localhost:9090"}, " user ": {Address: "localhost:9091"}}, want: ErrDuplicateClientName},
		{name: "address", configs: map[string]Config{"user": {}}, want: ErrInvalidAddress},
		{name: "timeout", configs: map[string]Config{"user": {Address: "localhost:9090", Timeout: -time.Second}}, want: ErrInvalidTimeout},
		{name: "unknown dial options", configs: validConfigs(), options: []Option{WithDialOptions("missing", grpc.WithTransportCredentials(insecure.NewCredentials()))}, want: ErrUnknownClient},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, err := New(test.configs, test.options...)
			if manager != nil {
				_ = manager.Stop(context.Background())
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("New() = %v, want %v", err, test.want)
			}
		})
	}
}

func TestTLSCAFileValidation(t *testing.T) {
	_, err := New(map[string]Config{
		"user": {
			Address: "localhost:9090",
			TLS:     TLSConfig{Enabled: true, CAFile: t.TempDir() + "/missing.pem"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "read TLS CA file") {
		t.Fatalf("New() error = %v", err)
	}
}

func validConfigs() map[string]Config {
	return map[string]Config{"user": {Address: "localhost:9090"}}
}

func newBufconnManager(t *testing.T, listener *bufconn.Listener, timeout time.Duration, logger *slog.Logger) *Manager {
	t.Helper()
	manager, err := New(map[string]Config{
		"user": {Address: "passthrough:///bufnet", Timeout: timeout},
	},
		WithLogger(logger),
		WithDialOptions("user", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func startManagerTestServer(t *testing.T) (*bufconn.Listener, *grpc.Server, *managerTestServiceImpl) {
	t.Helper()
	listener := bufconn.Listen(testBufferSize)
	server := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler(
		otelgrpc.WithPropagators(telemetry.Propagator()),
	)))
	implementation := &managerTestServiceImpl{}
	server.RegisterService(&managerTestServiceDescription, implementation)
	go func() { _ = server.Serve(listener) }()
	return listener, server, implementation
}

func fixedSpanContext(t *testing.T) trace.SpanContext {
	t.Helper()
	traceID, err := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := trace.SpanIDFromHex("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
}

var managerTestServiceDescription = grpc.ServiceDesc{
	ServiceName: "test.Manager",
	HandlerType: (*managerTestService)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Echo", Handler: managerEchoHandler},
		{MethodName: "Block", Handler: managerBlockHandler},
		{MethodName: "Fail", Handler: managerFailHandler},
	},
}

func managerEchoHandler(server any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	return invokeManagerUnary(server, ctx, decode, interceptor, "/test.Manager/Echo", func(service managerTestService, callCtx context.Context, request *wrapperspb.StringValue) (any, error) {
		return service.Echo(callCtx, request)
	})
}

func managerBlockHandler(server any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	return invokeManagerUnary(server, ctx, decode, interceptor, "/test.Manager/Block", func(service managerTestService, callCtx context.Context, request *wrapperspb.StringValue) (any, error) {
		return service.Block(callCtx, request)
	})
}

func managerFailHandler(server any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	return invokeManagerUnary(server, ctx, decode, interceptor, "/test.Manager/Fail", func(service managerTestService, callCtx context.Context, request *wrapperspb.StringValue) (any, error) {
		return service.Fail(callCtx, request)
	})
}

func invokeManagerUnary(
	server any,
	ctx context.Context,
	decode func(any) error,
	interceptor grpc.UnaryServerInterceptor,
	method string,
	invoke func(managerTestService, context.Context, *wrapperspb.StringValue) (any, error),
) (any, error) {
	request := new(wrapperspb.StringValue)
	if err := decode(request); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return invoke(server.(managerTestService), ctx, request)
	}
	info := &grpc.UnaryServerInfo{Server: server, FullMethod: method}
	handler := func(handlerCtx context.Context, value any) (any, error) {
		return invoke(server.(managerTestService), handlerCtx, value.(*wrapperspb.StringValue))
	}
	return interceptor(ctx, request, info, handler)
}
