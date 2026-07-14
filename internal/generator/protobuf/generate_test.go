package protobuf

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls []string
}

func (runner *fakeRunner) Run(_ context.Context, directory, name string, arguments ...string) (string, error) {
	call := name + " " + strings.Join(arguments, " ")
	runner.calls = append(runner.calls, call)
	if len(arguments) == 1 && arguments[0] == "--version" {
		switch name {
		case "buf":
			return "1.46.0", nil
		case "protoc-gen-go":
			return "protoc-gen-go v1.36.7", nil
		case "protoc-gen-go-grpc":
			return "protoc-gen-go-grpc 1.5.1", nil
		}
	}
	if call == "buf generate" {
		path := filepath.Join(directory, "gen", "pb", "demo", "v1", "service_grpc.pb.go")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		contents := `package demov1

import "context"

type UserServiceServer interface {
	GetUser(context.Context, *GetUserRequest) (*GetUserResponse, error)
	mustEmbedUnimplementedUserServiceServer()
}
`
		return "", os.WriteFile(path, []byte(contents), 0o644)
	}
	return "", nil
}

func TestGenerateRunsBufAndCreatesOnlyMissingServiceStubs(t *testing.T) {
	root := generatedProjectRoot(t)
	runner := &fakeRunner{}
	result, err := generateWithResult(context.Background(), root, runner)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if len(result.CreatedStubs) != 1 || result.CreatedStubs[0].Method != "UserServiceGetUser" || result.CreatedStubs[0].Path != "internal/service/user_service_get_user.go" {
		t.Fatalf("GenerateResult = %+v", result)
	}
	wantCalls := []string{
		"buf --version", "protoc-gen-go --version", "protoc-gen-go-grpc --version", "buf lint", "buf generate",
	}
	if strings.Join(runner.calls, "\n") != strings.Join(wantCalls, "\n") {
		t.Fatalf("calls = %v, want %v", runner.calls, wantCalls)
	}

	stubPath := filepath.Join(root, "internal", "service", "user_service_get_user.go")
	stub, err := os.ReadFile(stubPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stub), "func (s *Service) UserServiceGetUser") || !strings.Contains(string(stub), `errors.New("UserServiceGetUser is not implemented")`) {
		t.Fatalf("unexpected service stub:\n%s", stub)
	}
	transportPath := filepath.Join(root, "internal", "transport", "grpc", "register.gen.go")
	transport, err := os.ReadFile(transportPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"type userServiceServer struct",
		"application.UserServiceGetUser(ctx, request)",
		"stderrors.As(err, &businessError)",
		`attribute.Int64("jgo.business_code", int64(businessError.Code()))`,
		`attribute.String("jgo.business_message", businessError.Message())`,
		"&demov1.GetUserResponse{Code: int32(businessError.Code()), Msg: businessError.Message()}",
		"RegisterUserServiceServer",
	} {
		if !strings.Contains(string(transport), fragment) {
			t.Fatalf("transport does not contain %q:\n%s", fragment, transport)
		}
	}

	custom := []byte("package service\n\nfunc (s *Service) UserServiceGetUser() {}\n")
	if err := os.WriteFile(stubPath, custom, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err = generateWithResult(context.Background(), root, &fakeRunner{})
	if err != nil {
		t.Fatalf("second generate() error = %v", err)
	}
	if len(result.CreatedStubs) != 0 {
		t.Fatalf("second GenerateResult = %+v", result)
	}
	after, _ := os.ReadFile(stubPath)
	if string(after) != string(custom) {
		t.Fatalf("existing service stub was overwritten:\n%s", after)
	}
}

func TestGenerateRejectsToolVersionMismatchBeforeBufLint(t *testing.T) {
	root := generatedProjectRoot(t)
	runner := versionMismatchRunner{fakeRunner: fakeRunner{}}
	err := generate(context.Background(), root, &runner)
	if err == nil || !strings.Contains(err.Error(), "version mismatch") {
		t.Fatalf("generate() error = %v", err)
	}
}

func TestGenerateProtocolProjectOnlyWritesPublicPackages(t *testing.T) {
	root := generatedProjectRoot(t)
	for _, relative := range []string{
		"cmd/server/main.go",
		"internal/service/service.go",
		"internal/transport/grpc/register.go",
	} {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Fatal(err)
		}
	}

	result, err := generateWithResult(context.Background(), root, &fakeRunner{})
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if !result.ProtocolOnly || len(result.CreatedStubs) != 0 {
		t.Fatalf("GenerateResult = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "gen", "pb", "demo", "v1", "service_grpc.pb.go")); err != nil {
		t.Fatalf("generated public package: %v", err)
	}
	for _, relative := range []string{
		"internal/service/user_service_get_user.go",
		"internal/transport/grpc/register.gen.go",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); !os.IsNotExist(err) {
			t.Fatalf("protocol project unexpectedly generated %s: %v", relative, err)
		}
	}
}

func TestGenerateRejectsIncompleteServiceLayoutBeforeRunningTools(t *testing.T) {
	root := generatedProjectRoot(t)
	if err := os.Remove(filepath.Join(root, "internal", "transport", "grpc", "register.go")); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	_, err := generateWithResult(context.Background(), root, runner)
	if err == nil || !strings.Contains(err.Error(), "incomplete service project layout") {
		t.Fatalf("generate() error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("tools ran before layout validation: %v", runner.calls)
	}
}

func TestDiscoverGeneratedServicesRejectsServiceFileNameCollision(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "gen", "pb", "demo", "v1", "service_grpc.pb.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := `package demov1
import "context"
type DemoServiceServer interface {
	GetURL(context.Context, *GetURLRequest) (*GetURLResponse, error)
	GetUrl(context.Context, *GetUrlRequest) (*GetUrlResponse, error)
}
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "service"), 0o755); err != nil {
		t.Fatal(err)
	}
	services, err := discoverGeneratedServices(root, "example.com/demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createServiceStubs(root, services); err == nil || !strings.Contains(err.Error(), "same service file") {
		t.Fatalf("createServiceStubs() error = %v", err)
	}
}

func TestGenerateRejectsNonstandardResponseBeforeBufGenerate(t *testing.T) {
	root := generatedProjectRoot(t)
	contract := filepath.Join(root, "api", "proto", "demo", "v1", "service.proto")
	contents, err := os.ReadFile(contract)
	if err != nil {
		t.Fatal(err)
	}
	contents = []byte(strings.Replace(string(contents), "  int32 code = 1;\n  string msg = 2;\n", "", 1))
	if err := os.WriteFile(contract, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	err = generate(context.Background(), root, runner)
	if err == nil || !strings.Contains(err.Error(), "invalid RPC response contract") {
		t.Fatalf("generate() error = %v", err)
	}
	if strings.Contains(strings.Join(runner.calls, "\n"), "buf generate") {
		t.Fatalf("buf generate ran for invalid response: %v", runner.calls)
	}
}

type versionMismatchRunner struct {
	fakeRunner
}

func (runner *versionMismatchRunner) Run(ctx context.Context, directory, name string, arguments ...string) (string, error) {
	if name == "buf" && len(arguments) == 1 && arguments[0] == "--version" {
		return "1.47.0", nil
	}
	return runner.fakeRunner.Run(ctx, directory, name, arguments...)
}

func generatedProjectRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":       "module example.com/demo\n\ngo 1.24.0\n",
		"buf.yaml":     "version: v2\nmodules:\n  - path: api/proto\n",
		"buf.gen.yaml": "version: v2\nplugins: []\n",
		"api/proto/demo/v1/service.proto": `syntax = "proto3";
package demo.v1;
service UserService {
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
}
message GetUserRequest {}
message GetUserResponse {
  int32 code = 1;
  string msg = 2;
}
`,
		"internal/service/service.go":         "package service\n\ntype Service struct{}\n\n// GetUser simulates an HTTP operation with the same name as the RPC.\nfunc (s *Service) GetUser() {}\n",
		"cmd/server/main.go":                  "package main\nfunc main() {}\n",
		"internal/transport/grpc/register.go": "package grpctransport\n",
	}
	for relative, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

var _ runner = (*fakeRunner)(nil)
