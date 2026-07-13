package grpcx

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/eyesofblue/jgo/app"
	jerrors "github.com/eyesofblue/jgo/errors"
	"github.com/eyesofblue/jgo/middleware/requestid"
	"github.com/eyesofblue/jgo/server/httpx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const bufferSize = 1024 * 1024

type testService interface {
	Echo(context.Context, *wrapperspb.StringValue) (*wrapperspb.StringValue, error)
	Fail(context.Context, *wrapperspb.StringValue) (*wrapperspb.StringValue, error)
	Panic(context.Context, *wrapperspb.StringValue) (*wrapperspb.StringValue, error)
	Block(context.Context, *wrapperspb.StringValue) (*wrapperspb.StringValue, error)
}

type testServiceImpl struct {
	blockStarted chan struct{}
	blockRelease <-chan struct{}
}

func (s *testServiceImpl) Echo(ctx context.Context, request *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	if request.Value == "request-id" {
		return wrapperspb.String(requestid.FromContext(ctx)), nil
	}
	return request, nil
}

func (s *testServiceImpl) Fail(context.Context, *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	return nil, jerrors.New(120400, "invalid user", jerrors.WithHTTPStatus(http.StatusBadRequest))
}

func (s *testServiceImpl) Panic(context.Context, *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	panic("handler panic")
}

func (s *testServiceImpl) Block(context.Context, *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	close(s.blockStarted)
	<-s.blockRelease
	return wrapperspb.String("released"), nil
}

func TestServerWithBufconn(t *testing.T) {
	listener := bufconn.Listen(bufferSize)
	server := newTestServer(t, listener, &testServiceImpl{}, true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Start(ctx) }()
	connection := dialBufconn(t, listener)
	defer connection.Close()

	var header metadata.MD
	response := new(wrapperspb.StringValue)
	callCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(RequestIDMetadataKey, "client-42"))
	if err := connection.Invoke(callCtx, "/test.Service/Echo", wrapperspb.String("request-id"), response, grpc.Header(&header)); err != nil {
		t.Fatal(err)
	}
	if response.Value != "client-42" || header.Get(RequestIDMetadataKey)[0] != "client-42" {
		t.Fatalf("response = %q, header = %v", response.Value, header)
	}

	err := connection.Invoke(context.Background(), "/test.Service/Fail", wrapperspb.String(""), new(wrapperspb.StringValue))
	if status.Code(err) != codes.InvalidArgument || status.Convert(err).Message() != "invalid user" {
		t.Fatalf("Fail() error = %v", err)
	}

	err = connection.Invoke(context.Background(), "/test.Service/Panic", wrapperspb.String(""), new(wrapperspb.StringValue))
	if status.Code(err) != codes.Internal || status.Convert(err).Message() != jerrors.MessageInternal {
		t.Fatalf("Panic() error = %v", err)
	}

	assertReflectionListsService(t, connection, "test.Service")
	if err := server.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestGracefulStopFallsBackToForceStop(t *testing.T) {
	listener := bufconn.Listen(bufferSize)
	release := make(chan struct{})
	service := &testServiceImpl{blockStarted: make(chan struct{}), blockRelease: release}
	server := newTestServer(t, listener, service, false)
	startDone := make(chan error, 1)
	go func() { startDone <- server.Start(context.Background()) }()
	connection := dialBufconn(t, listener)

	callDone := make(chan error, 1)
	go func() {
		callDone <- connection.Invoke(context.Background(), "/test.Service/Block", wrapperspb.String(""), new(wrapperspb.StringValue))
	}()
	select {
	case <-service.blockStarted:
	case <-time.After(time.Second):
		t.Fatal("blocking handler did not start")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := server.Stop(stopCtx)
	if !errors.Is(err, ErrGracefulStopTimeout) {
		t.Fatalf("Stop() error = %v, want ErrGracefulStopTimeout", err)
	}
	close(release)
	_ = connection.Close()
	select {
	case <-callDone:
	case <-time.After(time.Second):
		t.Fatal("blocked RPC did not finish")
	}
	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
}

func TestMixedApplicationLifecycle(t *testing.T) {
	grpcListener := bufconn.Listen(bufferSize)
	grpcServer := newTestServer(t, grpcListener, &testServiceImpl{}, false)
	httpServer, err := httpx.New(
		httpx.WithAddress("127.0.0.1:0"),
		httpx.WithLogger(discardLogger()),
		httpx.WithHandler(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		})),
	)
	if err != nil {
		t.Fatal(err)
	}

	application := app.New(app.WithShutdownTimeout(time.Second))
	if err := application.Add(httpServer); err != nil {
		t.Fatal(err)
	}
	if err := application.Add(grpcServer); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- application.Run(ctx) }()

	connection := dialBufconn(t, grpcListener)
	if err := connection.Invoke(context.Background(), "/test.Service/Echo", wrapperspb.String("mixed"), new(wrapperspb.StringValue)); err != nil {
		t.Fatal(err)
	}
	address := waitForHTTPAddress(t, httpServer)
	response, err := http.Get("http://" + address)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("HTTP status = %d", response.StatusCode)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("application.Run() error = %v", err)
	}
	_ = connection.Close()
}

func TestNewValidatesConfig(t *testing.T) {
	register := WithRegister(func(grpc.ServiceRegistrar) {})
	tests := []struct {
		name string
		opts []Option
		want error
	}{
		{name: "name", opts: []Option{register, WithName(" ")}, want: ErrInvalidName},
		{name: "address", opts: []Option{register, WithAddress(" ")}, want: ErrInvalidAddress},
		{name: "register", opts: nil, want: ErrNoRegisterFunctions},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(test.opts...)
			if !errors.Is(err, test.want) {
				t.Fatalf("New() error = %v, want %v", err, test.want)
			}
		})
	}
}

func newTestServer(t *testing.T, listener net.Listener, service testService, withReflection bool) *Server {
	t.Helper()
	server, err := New(
		WithListener(listener),
		WithLogger(discardLogger()),
		WithReflection(withReflection),
		WithRegister(func(registrar grpc.ServiceRegistrar) { registerTestService(registrar, service) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func dialBufconn(t *testing.T, listener *bufconn.Listener) *grpc.ClientConn {
	t.Helper()
	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func assertReflectionListsService(t *testing.T, connection *grpc.ClientConn, serviceName string) {
	t.Helper()
	client := reflectionpb.NewServerReflectionClient(connection)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&reflectionpb.ServerReflectionRequest{
		MessageRequest: &reflectionpb.ServerReflectionRequest_ListServices{ListServices: ""},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range result.GetListServicesResponse().Service {
		if service.Name == serviceName {
			cancel()
			return
		}
	}
	t.Fatalf("reflection did not list %q: %+v", serviceName, result)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitForHTTPAddress(t *testing.T, server *httpx.Server) string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		address := server.Address()
		if address != "127.0.0.1:0" {
			return address
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("HTTP server did not bind")
	return ""
}

func registerTestService(registrar grpc.ServiceRegistrar, implementation testService) {
	registrar.RegisterService(&testServiceDescription, implementation)
}

var testServiceDescription = grpc.ServiceDesc{
	ServiceName: "test.Service",
	HandlerType: (*testService)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Echo", Handler: echoHandler},
		{MethodName: "Fail", Handler: failHandler},
		{MethodName: "Panic", Handler: panicHandler},
		{MethodName: "Block", Handler: blockHandler},
	},
}

func echoHandler(srv any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	return invokeUnary(srv, ctx, decode, interceptor, "/test.Service/Echo", func(service testService, ctx context.Context, request *wrapperspb.StringValue) (any, error) {
		return service.Echo(ctx, request)
	})
}

func failHandler(srv any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	return invokeUnary(srv, ctx, decode, interceptor, "/test.Service/Fail", func(service testService, ctx context.Context, request *wrapperspb.StringValue) (any, error) {
		return service.Fail(ctx, request)
	})
}

func panicHandler(srv any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	return invokeUnary(srv, ctx, decode, interceptor, "/test.Service/Panic", func(service testService, ctx context.Context, request *wrapperspb.StringValue) (any, error) {
		return service.Panic(ctx, request)
	})
}

func blockHandler(srv any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	return invokeUnary(srv, ctx, decode, interceptor, "/test.Service/Block", func(service testService, ctx context.Context, request *wrapperspb.StringValue) (any, error) {
		return service.Block(ctx, request)
	})
}

type unaryMethod func(testService, context.Context, *wrapperspb.StringValue) (any, error)

func invokeUnary(srv any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor, method string, invoke unaryMethod) (any, error) {
	request := new(wrapperspb.StringValue)
	if err := decode(request); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return invoke(srv.(testService), ctx, request)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: method}
	handler := func(ctx context.Context, req any) (any, error) {
		return invoke(srv.(testService), ctx, req.(*wrapperspb.StringValue))
	}
	return interceptor(ctx, request, info, handler)
}
