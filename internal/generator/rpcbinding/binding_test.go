package rpcbinding

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddServerAndClientFromLocalReplacement(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := fakeServiceProject(t, protocol)

	server, err := AddServer(AddConfig{
		Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.Package != "example.com/company-api/gen/pb/company_api/v1" {
		t.Fatalf("server package = %q", server.Package)
	}
	client, err := AddClient(AddConfig{
		Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Name != "user" {
		t.Fatalf("client name = %q", client.Name)
	}

	assertContains(t, filepath.Join(service, "internal/transport/grpc/external.gen.go"), "RegisterUserServiceServer")
	assertContains(t, filepath.Join(service, "internal/service/user_service_get_user.go"), "UserServiceGetUser")
	assertContains(t, filepath.Join(service, "internal/rpcclient/clients.gen.go"), "User pb0.UserServiceClient")
	assertContains(t, filepath.Join(service, "configs/local.yaml"), "user:")
	assertContains(t, filepath.Join(service, ".jgo/rpc.json"), `"version": 1`)
	assertContains(t, filepath.Join(service, "go.mod"), "example.com/company-api v0.1.1")
}

func TestResolveRequiresPackageForDuplicateService(t *testing.T) {
	protocol := fakeProtocolModule(t)
	copyGeneratedService(t, protocol, "gen/pb/company_api/v2/service_grpc.pb.go", "company_apiv2")
	service := fakeServiceProject(t, protocol)
	_, err := AddClient(AddConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true})
	if err == nil || !strings.Contains(err.Error(), "multiple packages") {
		t.Fatalf("AddClient() error = %v", err)
	}
	client, err := AddClient(AddConfig{
		Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService",
		Package:  "example.com/company-api/gen/pb/company_api/v2",
		SkipTidy: true,
	})
	if err != nil || client.Package != "example.com/company-api/gen/pb/company_api/v2" {
		t.Fatalf("AddClient() = %+v, %v", client, err)
	}
}

func TestAddServerRollsBackWhenTidyFails(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := fakeServiceProject(t, protocol)
	goMod := mustReadTestFile(t, filepath.Join(service, "go.mod"))
	t.Setenv("PATH", t.TempDir())

	_, err := AddServer(AddConfig{
		Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService",
	})
	if err == nil || !strings.Contains(err.Error(), "go mod tidy") {
		t.Fatalf("AddServer() error = %v", err)
	}
	assertFileEquals(t, filepath.Join(service, "go.mod"), goMod)
	assertNotExist(t, filepath.Join(service, ".jgo", "rpc.json"))
	assertNotExist(t, filepath.Join(service, "internal", "transport", "grpc", "external.gen.go"))
	assertNotExist(t, filepath.Join(service, "internal", "service", "user_service_get_user.go"))
}

func TestAddClientRollsBackWhenTidyFails(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := fakeServiceProject(t, protocol)
	goMod := mustReadTestFile(t, filepath.Join(service, "go.mod"))
	config := mustReadTestFile(t, filepath.Join(service, "configs", "local.yaml"))
	t.Setenv("PATH", t.TempDir())

	_, err := AddClient(AddConfig{
		Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService",
	})
	if err == nil || !strings.Contains(err.Error(), "go mod tidy") {
		t.Fatalf("AddClient() error = %v", err)
	}
	assertFileEquals(t, filepath.Join(service, "go.mod"), goMod)
	assertFileEquals(t, filepath.Join(service, "configs", "local.yaml"), config)
	assertNotExist(t, filepath.Join(service, ".jgo", "rpc.json"))
	assertNotExist(t, filepath.Join(service, "internal", "rpcclient", "clients.gen.go"))
}

func TestGeneratedImportsAreDeduplicatedByPackage(t *testing.T) {
	protocol := fakeProtocolModule(t)

	t.Run("multiple services on one server", func(t *testing.T) {
		service := fakeServiceProject(t, protocol)
		for _, serviceName := range []string{"UserService", "AdminService"} {
			if _, err := AddServer(AddConfig{
				Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: serviceName, SkipTidy: true,
			}); err != nil {
				t.Fatal(err)
			}
		}
		assertOccurrenceCount(t, filepath.Join(service, "internal", "transport", "grpc", "external.gen.go"), `"example.com/company-api/gen/pb/company_api/v1"`, 1)
	})

	t.Run("multiple instances of one client", func(t *testing.T) {
		service := fakeServiceProject(t, protocol)
		for _, name := range []string{"user_primary", "user_backup"} {
			if _, err := AddClient(AddConfig{
				Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", Name: name, SkipTidy: true,
			}); err != nil {
				t.Fatal(err)
			}
		}
		assertOccurrenceCount(t, filepath.Join(service, "internal", "rpcclient", "clients.gen.go"), `"example.com/company-api/gen/pb/company_api/v1"`, 1)
	})
}

func TestAddClientRejectsGeneratedFieldCollision(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := fakeServiceProject(t, protocol)
	if _, err := AddClient(AddConfig{
		Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", Name: "user_primary", SkipTidy: true,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := AddClient(AddConfig{
		Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", Name: "userPrimary", SkipTidy: true,
	})
	if err == nil || !strings.Contains(err.Error(), "after Go field generation") {
		t.Fatalf("AddClient() error = %v", err)
	}
}

func fakeProtocolModule(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "company-api")
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/company-api\n\ngo 1.24.0\n")
	copyGeneratedService(t, root, "gen/pb/company_api/v1/service_grpc.pb.go", "company_apiv1")
	return root
}

func copyGeneratedService(t *testing.T, root, relative, packageName string) {
	t.Helper()
	writeTestFile(t, filepath.Join(root, filepath.FromSlash(relative)), `package `+packageName+`

import "context"

type GetUserRequest struct{}
type GetUserResponse struct { Code int32; Msg string }
type UserServiceClient interface { GetUser(context.Context, *GetUserRequest) (*GetUserResponse, error) }
type UserServiceServer interface {
  GetUser(context.Context, *GetUserRequest) (*GetUserResponse, error)
  mustEmbedUnimplementedUserServiceServer()
}
type UnimplementedUserServiceServer struct{}

type AdminServiceServer interface {
  GetUser(context.Context, *GetUserRequest) (*GetUserResponse, error)
  mustEmbedUnimplementedAdminServiceServer()
}
type UnimplementedAdminServiceServer struct{}
`)
}

func fakeServiceProject(t *testing.T, protocol string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "service")
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/service\n\ngo 1.24.0\n\nreplace example.com/company-api => "+protocol+"\n")
	writeTestFile(t, filepath.Join(root, "cmd/server/main.go"), "package main\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(root, "internal/transport/grpc/register.go"), "package grpctransport\n")
	writeTestFile(t, filepath.Join(root, "internal/service/service.go"), "package service\ntype Service struct{}\n")
	writeTestFile(t, filepath.Join(root, "configs/local.yaml"), "rpc_client: {}\n")
	return root
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertContains(t *testing.T, path, fragment string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(contents), fragment) {
		t.Fatalf("%s does not contain %q: %v\n%s", path, fragment, err, contents)
	}
}

func mustReadTestFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func assertFileEquals(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s changed after rollback\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s exists after rollback: %v", path, err)
	}
}

func assertOccurrenceCount(t *testing.T, path, fragment string, want int) {
	t.Helper()
	contents := mustReadTestFile(t, path)
	if got := strings.Count(string(contents), fragment); got != want {
		t.Fatalf("%s contains %q %d times, want %d\n%s", path, fragment, got, want, contents)
	}
}
