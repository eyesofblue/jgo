package call

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

const healthContract = `syntax = "proto3";
package grpc.health.v1;

service Health {
  rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
}

message HealthCheckRequest {
  string service = 1;
}

message HealthCheckResponse {
  enum ServingStatus {
    UNKNOWN = 0;
    SERVING = 1;
    NOT_SERVING = 2;
    SERVICE_UNKNOWN = 3;
  }
  ServingStatus status = 1;
}
`

func TestCallGRPCUsesReflectionAndMetadata(t *testing.T) {
	address, stop := startHealthServer(t, true)
	defer stop()
	result, err := CallGRPC(context.Background(), GRPCConfig{
		Root: t.TempDir(), Method: "Health.Check", Address: address,
		Data: `{"service":""}`, Headers: []string{"Authorization: Bearer token"},
	})
	if err != nil {
		t.Fatalf("CallGRPC() error = %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal(result.Body, &response); err != nil || response["status"] != "SERVING" {
		t.Fatalf("body = %s, decode error = %v", result.Body, err)
	}
}

func TestCallGRPCFallsBackToLocalProto(t *testing.T) {
	root := writeHealthContract(t)
	address, stop := startHealthServer(t, false)
	defer stop()
	result, err := CallGRPC(context.Background(), GRPCConfig{
		Root: root, Method: "grpc.health.v1.Health.Check", Address: address,
		Data: `{"service":""}`, Headers: []string{"Authorization: Bearer token"},
	})
	if err != nil {
		t.Fatalf("CallGRPC() error = %v", err)
	}
	if !strings.Contains(string(result.Body), "SERVING") {
		t.Fatalf("body = %s", result.Body)
	}
}

func TestCallGRPCReportsAvailableMethods(t *testing.T) {
	root := writeHealthContract(t)
	address, stop := startHealthServer(t, true)
	defer stop()
	_, err := CallGRPC(context.Background(), GRPCConfig{
		Root: root, Method: "Health.Missing", Address: address, Headers: []string{"Authorization: Bearer token"},
	})
	if err == nil || !strings.Contains(err.Error(), "available methods: grpc.health.v1.Health.Check") {
		t.Fatalf("CallGRPC() error = %v", err)
	}
}

func TestListGRPCUsesLocalDescriptors(t *testing.T) {
	methods, err := ListGRPC(context.Background(), writeHealthContract(t))
	if err != nil {
		t.Fatalf("ListGRPC() error = %v", err)
	}
	if len(methods) != 1 || methods[0].FullName != "grpc.health.v1.Health.Check" {
		t.Fatalf("methods = %+v", methods)
	}
}

func startHealthServer(t *testing.T, withReflection bool) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		incoming, _ := metadata.FromIncomingContext(ctx)
		if values := incoming.Get("authorization"); len(values) != 1 || values[0] != "Bearer token" {
			return nil, status.Error(codes.Unauthenticated, "missing authorization")
		}
		return handler(ctx, request)
	}))
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthServer)
	if withReflection {
		reflection.Register(server)
	}
	go func() { _ = server.Serve(listener) }()
	return listener.Addr().String(), func() {
		server.Stop()
		_ = listener.Close()
	}
}

func writeHealthContract(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "api", "proto", "grpc", "health", "v1", "health.proto")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(healthContract), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
