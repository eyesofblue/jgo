package rpcbinding

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	projectgen "github.com/eyesofblue/jgo/internal/generator/project"
)

func TestAddServerAndClientFromLocalReplacement(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := fakeServiceProject(t, protocol)

	server, err := BindServer(BindConfig{
		Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.Package != "example.com/company-api/gen/pb/company_api/v1" {
		t.Fatalf("server package = %q", server.Package)
	}
	client, err := BindClient(BindConfig{
		Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Name != "user" {
		t.Fatalf("client name = %q", client.Name)
	}

	assertContains(t, filepath.Join(service, "internal/transport/grpc/external.gen.go"), "RegisterUserServiceServer")
	assertContains(t, filepath.Join(service, "internal/service/user_handler.go"), "type UserHandler struct")
	assertContains(t, filepath.Join(service, "internal/service/user_handler.go"), "*Service")
	assertContains(t, filepath.Join(service, "internal/service/user_handler_get_user.go"), "func (h *UserHandler) GetUser")
	assertContains(t, filepath.Join(service, "internal/transport/grpc/external.gen.go"), "type userHandler interface")
	assertContains(t, filepath.Join(service, "internal/transport/grpc/external.gen.go"), "server.handler.GetUser")
	assertContains(t, filepath.Join(service, "internal/rpcclient/clients.gen.go"), "User pb0.UserServiceClient")
	assertContains(t, filepath.Join(service, "configs/local.yaml"), "user:")
	assertContains(t, filepath.Join(service, "configs/local.yaml"), "readiness: required")
	assertContains(t, filepath.Join(service, ".jgo/rpc.json"), `"version": 2`)
	assertContains(t, filepath.Join(service, ".jgo/rpc.json"), `"handler": "User"`)
	assertContains(t, filepath.Join(service, "go.mod"), "example.com/company-api v0.1.1")
}

func TestResolveRequiresPackageForDuplicateService(t *testing.T) {
	protocol := fakeProtocolModule(t)
	copyGeneratedService(t, protocol, "gen/pb/company_api/v2/service_grpc.pb.go", "company_apiv2")
	service := fakeServiceProject(t, protocol)
	_, err := BindClient(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true})
	if err == nil || !strings.Contains(err.Error(), "multiple packages") {
		t.Fatalf("AddClient() error = %v", err)
	}
	client, err := BindClient(BindConfig{
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

	_, err := BindServer(BindConfig{
		Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService",
	})
	if err == nil || !strings.Contains(err.Error(), "go mod tidy") {
		t.Fatalf("AddServer() error = %v", err)
	}
	assertFileEquals(t, filepath.Join(service, "go.mod"), goMod)
	assertNotExist(t, filepath.Join(service, ".jgo", "rpc.json"))
	assertNotExist(t, filepath.Join(service, "internal", "transport", "grpc", "external.gen.go"))
	assertNotExist(t, filepath.Join(service, "internal", "service", "user_handler.go"))
	assertNotExist(t, filepath.Join(service, "internal", "service", "user_handler_get_user.go"))
}

func TestAddClientRollsBackWhenTidyFails(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := fakeServiceProject(t, protocol)
	goMod := mustReadTestFile(t, filepath.Join(service, "go.mod"))
	config := mustReadTestFile(t, filepath.Join(service, "configs", "local.yaml"))
	t.Setenv("PATH", t.TempDir())

	_, err := BindClient(BindConfig{
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
			if _, err := BindServer(BindConfig{
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
			if _, err := BindClient(BindConfig{
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
	if _, err := BindClient(BindConfig{
		Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", Name: "user_primary", SkipTidy: true,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := BindClient(BindConfig{
		Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", Name: "userPrimary", SkipTidy: true,
	})
	if err == nil || !strings.Contains(err.Error(), "after Go field generation") {
		t.Fatalf("AddClient() error = %v", err)
	}
}

func TestBindClientIsIdempotentAndUpdatesVersion(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := fakeServiceProject(t, protocol)
	first, err := BindClient(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", Name: "user", Address: "dns:///user:9090", SkipTidy: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := BindClient(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.2", Service: "UserService", Name: "user", Address: "ignored:9090", SkipTidy: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.Address != second.Address || second.Version != "v0.1.2" {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	manifest := string(mustReadTestFile(t, filepath.Join(service, ".jgo", "rpc.json")))
	if strings.Count(manifest, `"name": "user"`) != 1 || !strings.Contains(manifest, `"version": "v0.1.2"`) {
		t.Fatalf("manifest:\n%s", manifest)
	}
	config := string(mustReadTestFile(t, filepath.Join(service, "configs", "local.yaml")))
	if !strings.Contains(config, "dns:///user:9090") || strings.Contains(config, "ignored:9090") {
		t.Fatalf("config:\n%s", config)
	}
}

func TestBindResolvesUnpublishedWorkspaceModule(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	protocol := filepath.Join(parent, "company-api")
	service := filepath.Join(parent, "service")
	writeTestFile(t, filepath.Join(protocol, "go.mod"), "module example.com/company-api\n\ngo 1.24.0\n")
	copyGeneratedService(t, protocol, "gen/pb/company_api/v1/service_grpc.pb.go", "company_apiv1")
	writeTestFile(t, filepath.Join(service, "go.mod"), "module example.com/service\n\ngo 1.24.0\n")
	writeTestFile(t, filepath.Join(service, "cmd/server/main.go"), "package main\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(service, "internal/service/service.go"), "package service\ntype Service struct{}\n")
	writeTestFile(t, filepath.Join(service, "configs/local.yaml"), "rpc_client: {}\n")
	workspace := filepath.Join(parent, "go.work")
	writeTestFile(t, workspace, "go 1.24.0\n\nuse (\n\t./company-api\n\t./service\n)\n")
	t.Setenv("GOWORK", workspace)
	binding, err := BindClient(BindConfig{Root: service, ModuleSpec: "example.com/company-api", Service: "UserService", SkipTidy: true})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Version != "" || binding.Package != "example.com/company-api/gen/pb/company_api/v1" {
		t.Fatalf("binding = %+v", binding)
	}
	if strings.Contains(string(mustReadTestFile(t, filepath.Join(service, "go.mod"))), "company-api") {
		t.Fatal("workspace-only bind unexpectedly wrote a fake require version")
	}
}

func TestWorkspaceBindWithStandardGoModTidy(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	protocol := filepath.Join(parent, "company-api")
	service := filepath.Join(parent, "service")
	writeTestFile(t, filepath.Join(protocol, "go.mod"), "module example.com/company-api\n\ngo 1.24.0\n\nrequire google.golang.org/grpc v1.79.1\n")
	copyGeneratedService(t, protocol, "gen/pb/company_api/v1/service_grpc.pb.go", "company_apiv1")
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projectgen.Generate(projectgen.Config{Name: "service", Module: "example.com/service", Type: projectgen.TypeWeb, TargetDir: service, JGOReplace: repositoryRoot, SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(parent, "go.work")
	writeTestFile(t, workspace, "go 1.24.0\n\nuse (\n\t./company-api\n\t./service\n)\n")
	t.Setenv("GOWORK", workspace)
	if _, err := BindClient(BindConfig{Root: service, ModuleSpec: "example.com/company-api", Service: "UserService"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = service
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go test: %v\n%s", err, output)
	}
}

func TestExplicitVersionDoesNotResolveFromWorkspace(t *testing.T) {
	parent := t.TempDir()
	protocol := filepath.Join(parent, "company-api")
	service := filepath.Join(parent, "service")
	writeTestFile(t, filepath.Join(protocol, "go.mod"), "module example.com/company-api\n\ngo 1.24.0\n")
	writeTestFile(t, filepath.Join(service, "go.mod"), "module example.com/service\n\ngo 1.24.0\n")
	workspace := filepath.Join(parent, "go.work")
	writeTestFile(t, workspace, "go 1.24.0\n\nuse (\n\t./company-api\n\t./service\n)\n")
	t.Setenv("GOWORK", workspace)
	t.Setenv("GOPROXY", "off")
	if _, err := moduleDirectory(service, "example.com/company-api", "v0.1.0"); err == nil {
		t.Fatal("explicit version unexpectedly resolved from the workspace")
	}
}

func TestVersionSpecificReplaceDoesNotApplyToAnotherVersion(t *testing.T) {
	parent := t.TempDir()
	protocol := filepath.Join(parent, "company-api")
	service := filepath.Join(parent, "service")
	writeTestFile(t, filepath.Join(protocol, "go.mod"), "module example.com/company-api\n\ngo 1.24.0\n")
	writeTestFile(t, filepath.Join(service, "go.mod"), "module example.com/service\n\ngo 1.24.0\n\nreplace example.com/company-api v0.1.0 => "+protocol+"\n")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")
	if _, err := moduleDirectory(service, "example.com/company-api", "v0.2.0"); err == nil {
		t.Fatal("v0.1.0 replacement unexpectedly resolved v0.2.0")
	}
}

func TestStreamingServiceDoesNotBlockUnaryServiceBinding(t *testing.T) {
	protocol := fakeProtocolModule(t)
	path := filepath.Join(protocol, "gen", "pb", "company_api", "v1", "service_grpc.pb.go")
	contents := strings.Replace(string(mustReadTestFile(t, path)), `import "context"`, "import (\n\t\"context\"\n\t\"google.golang.org/grpc\"\n)", 1)
	contents += `
type StreamServiceServer interface {
  Watch(*GetUserRequest, grpc.ServerStreamingServer[*GetUserResponse]) error
  mustEmbedUnimplementedStreamServiceServer()
}
`
	writeTestFile(t, path, contents)
	service := fakeServiceProject(t, protocol)
	if _, err := BindClient(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
		t.Fatalf("unary bind failed because another Service streams: %v", err)
	}
	if _, err := BindClient(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "StreamService", Name: "stream", SkipTidy: true}); err == nil || !strings.Contains(err.Error(), "streaming") {
		t.Fatalf("streaming bind error = %v", err)
	}
}

func TestHandlerStubNamesKeepInitialismMethodsDistinct(t *testing.T) {
	protocol := fakeProtocolModule(t)
	path := filepath.Join(protocol, "gen", "pb", "company_api", "v1", "service_grpc.pb.go")
	contents := strings.ReplaceAll(string(mustReadTestFile(t, path)),
		"GetUser(context.Context, *GetUserRequest) (*GetUserResponse, error)",
		"GetURL(context.Context, *GetUserRequest) (*GetUserResponse, error)\n  GetUrl(context.Context, *GetUserRequest) (*GetUserResponse, error)")
	writeTestFile(t, path, contents)
	service := fakeServiceProject(t, protocol)
	if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, filepath.Join(service, "internal", "service", "user_handler_get_u_r_l.go"), "GetURL")
	assertContains(t, filepath.Join(service, "internal", "service", "user_handler_get_url.go"), "GetUrl")
}

func TestBindClientCompileFailureRollsBackAllManagedFiles(t *testing.T) {
	protocol := fakeProtocolModule(t)
	protocolFile := filepath.Join(protocol, "gen", "pb", "company_api", "v1", "service_grpc.pb.go")
	writeTestFile(t, protocolFile, strings.Replace(string(mustReadTestFile(t, protocolFile)), "func NewUserServiceClient", "func BrokenUserServiceClient", 1))
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	service := filepath.Join(t.TempDir(), "service")
	if _, err := projectgen.Generate(projectgen.Config{Name: "service", Module: "example.com/service", Type: projectgen.TypeWeb, TargetDir: service, JGOReplace: repositoryRoot, SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	goModPath := filepath.Join(service, "go.mod")
	writeTestFile(t, goModPath, string(mustReadTestFile(t, goModPath))+"\nreplace example.com/company-api => "+protocol+"\n")
	beforeMod := mustReadTestFile(t, goModPath)
	beforeConfig := mustReadTestFile(t, filepath.Join(service, "configs", "local.yaml"))
	beforeClients := mustReadTestFile(t, filepath.Join(service, "internal", "rpcclient", "clients.gen.go"))

	_, err = BindClient(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService"})
	if err == nil || !strings.Contains(err.Error(), "compile project") {
		t.Fatalf("BindClient() error = %v", err)
	}
	assertFileEquals(t, goModPath, beforeMod)
	assertFileEquals(t, filepath.Join(service, "configs", "local.yaml"), beforeConfig)
	assertFileEquals(t, filepath.Join(service, "internal", "rpcclient", "clients.gen.go"), beforeClients)
	assertNotExist(t, filepath.Join(service, ".jgo", "rpc.json"))
	assertNotExist(t, filepath.Join(service, "go.sum"))
}

func TestUnbindClientRemovesManagedBindingAndConfiguration(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeWeb, protocol)
	if _, err := BindClient(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService"}); err != nil {
		t.Fatal(err)
	}
	if err := UnbindClient(service, "user"); err != nil {
		t.Fatal(err)
	}
	assertContains(t, filepath.Join(service, "internal", "rpcclient", "clients.gen.go"), "type Clients struct{}")
	config := string(mustReadTestFile(t, filepath.Join(service, "configs", "local.yaml")))
	if strings.Contains(config, "user:") {
		t.Fatalf("client configuration remains after unbind:\n%s", config)
	}
	manifest := string(mustReadTestFile(t, filepath.Join(service, ".jgo", "rpc.json")))
	if strings.Contains(manifest, "UserService") {
		t.Fatalf("client manifest remains after unbind:\n%s", manifest)
	}
}

func TestUnbindClientRollsBackWhenBusinessCodeStillUsesClient(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeWeb, protocol)
	if _, err := BindClient(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService"}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(service, "internal", "service", "uses_user_client.go"), "package service\n\nfunc usesUserClient(service *Service) { _ = service.RPC.User }\n")
	beforeManifest := mustReadTestFile(t, filepath.Join(service, ".jgo", "rpc.json"))
	beforeConfig := mustReadTestFile(t, filepath.Join(service, "configs", "local.yaml"))
	beforeClients := mustReadTestFile(t, filepath.Join(service, "internal", "rpcclient", "clients.gen.go"))
	if err := UnbindClient(service, "user"); err == nil || !strings.Contains(err.Error(), "compile project") {
		t.Fatalf("UnbindClient() error = %v", err)
	}
	assertFileEquals(t, filepath.Join(service, ".jgo", "rpc.json"), beforeManifest)
	assertFileEquals(t, filepath.Join(service, "configs", "local.yaml"), beforeConfig)
	assertFileEquals(t, filepath.Join(service, "internal", "rpcclient", "clients.gen.go"), beforeClients)
}

func TestUnbindServerKeepsUserOwnedImplementation(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService"}); err != nil {
		t.Fatal(err)
	}
	handler := filepath.Join(service, "internal", "service", "user_handler.go")
	stub := filepath.Join(service, "internal", "service", "user_handler_get_user.go")
	if err := UnbindServer(service, "UserService"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stub); err != nil {
		t.Fatalf("user-owned implementation was removed: %v", err)
	}
	if _, err := os.Stat(handler); err != nil {
		t.Fatalf("user-owned handler was removed: %v", err)
	}
	assertContains(t, filepath.Join(service, "internal", "transport", "grpc", "external.gen.go"), "func registerExternal(grpc.ServiceRegistrar, *service.Service) {}")
	manifest := string(mustReadTestFile(t, filepath.Join(service, ".jgo", "rpc.json")))
	if strings.Contains(manifest, "UserService") {
		t.Fatalf("server manifest remains after unbind:\n%s", manifest)
	}
}

func TestServerBindingsRequireDistinctHandlersForSameService(t *testing.T) {
	protocol := fakeProtocolModule(t)
	copyGeneratedService(t, protocol, "gen/pb/company_api/v2/service_grpc.pb.go", "company_apiv2")
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	v1 := "example.com/company-api/gen/pb/company_api/v1"
	v2 := "example.com/company-api/gen/pb/company_api/v2"
	if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Package: v1, Service: "UserService", SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Package: v2, Service: "UserService", SkipTidy: true}); err == nil || !strings.Contains(err.Error(), "handler UserHandler") {
		t.Fatalf("default handler collision error = %v", err)
	}
	second, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Package: v2, Service: "UserService", HandlerName: "UserV2", SkipTidy: true})
	if err != nil {
		t.Fatal(err)
	}
	if second.Handler != "UserV2" {
		t.Fatalf("handler = %q", second.Handler)
	}
	assertContains(t, filepath.Join(service, "internal", "service", "user_v2_handler_get_user.go"), "func (h *UserV2Handler) GetUser")
	assertOccurrenceCount(t, filepath.Join(service, "internal", "transport", "grpc", "external.gen.go"), "RegisterUserServiceServer", 2)
	if err := UnbindServer(service, "UserService"); err == nil || !strings.Contains(err.Error(), "multiple packages") {
		t.Fatalf("ambiguous UnbindServer() error = %v", err)
	}
	if err := UnbindServer(service, "UserService", v1); err != nil {
		t.Fatal(err)
	}
	assertContains(t, filepath.Join(service, "internal", "service", "user_handler.go"), "type UserHandler struct")
	assertContains(t, filepath.Join(service, "internal", "service", "user_v2_handler.go"), "type UserV2Handler struct")
	assertContains(t, filepath.Join(service, "internal", "transport", "grpc", "external.gen.go"), v2)
	if strings.Contains(string(mustReadTestFile(t, filepath.Join(service, "internal", "transport", "grpc", "external.gen.go"))), v1) {
		t.Fatal("v1 binding remains after package-qualified unbind")
	}
}

func TestBindServerPreservesAndNormalizesExplicitHandlerName(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	first, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", HandlerName: "account_handler", SkipTidy: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.2", Service: "UserService", SkipTidy: true})
	if err != nil || first.Handler != "Account" || second.Handler != "Account" {
		t.Fatalf("first=%+v second=%+v err=%v", first, second, err)
	}
	if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.2", Service: "UserService", HandlerName: "Other", SkipTidy: true}); err == nil || !strings.Contains(err.Error(), "cannot be changed") {
		t.Fatalf("rename error = %v", err)
	}
}

func TestGenerateRejectsVersionOneManifestWithMigrationGuidance(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	writeTestFile(t, filepath.Join(service, ".jgo", "rpc.json"), `{"version":1,"servers":[]}`)
	if _, err := Generate(service); err == nil || !strings.Contains(err.Error(), "cannot be upgraded automatically") || !strings.Contains(err.Error(), "back up and remove .jgo/rpc.json") || !strings.Contains(err.Error(), "<Handler>.<rpc-method>") {
		t.Fatalf("Generate() error = %v", err)
	}
}

func TestBindServerRejectsInvalidHandlerName(t *testing.T) {
	protocol := fakeProtocolModule(t)
	for _, name := range []string{"---", "Handler"} {
		service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
		if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", HandlerName: name, SkipTidy: true}); err == nil || !strings.Contains(err.Error(), "invalid handler name") {
			t.Fatalf("BindServer(%q) error = %v", name, err)
		}
		assertNotExist(t, filepath.Join(service, ".jgo", "rpc.json"))
	}
}

func TestDefaultHandlerStripsServiceSuffix(t *testing.T) {
	for service, want := range map[string]string{
		"UserService":    "User",
		"AuditHandler":   "Audit",
		"Service":        "Service",
		"HandlerService": "HandlerService",
		"Handler":        "RPC",
	} {
		if got := defaultServerHandlerName(service); got != want {
			t.Errorf("defaultServerHandlerName(%q) = %q, want %q", service, got, want)
		}
	}
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	binding, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Handler != "User" {
		t.Fatalf("handler = %q", binding.Handler)
	}
}

func TestDefaultHandlerAlsoAvoidsDoubleHandlerSuffix(t *testing.T) {
	protocol := fakeProtocolModule(t)
	path := filepath.Join(protocol, "gen", "pb", "company_api", "v1", "service_grpc.pb.go")
	contents := string(mustReadTestFile(t, path)) + `
type AuditHandlerServer interface {
  GetUser(context.Context, *GetUserRequest) (*GetUserResponse, error)
  mustEmbedUnimplementedAuditHandlerServer()
}
type UnimplementedAuditHandlerServer struct{}
func (UnimplementedAuditHandlerServer) mustEmbedUnimplementedAuditHandlerServer() {}
func RegisterAuditHandlerServer(grpc.ServiceRegistrar, AuditHandlerServer) {}
`
	writeTestFile(t, path, contents)
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	binding, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "AuditHandler", SkipTidy: true})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Handler != "Audit" {
		t.Fatalf("handler = %q", binding.Handler)
	}
	assertContains(t, filepath.Join(service, "internal", "service", "audit_handler.go"), "type AuditHandler struct")
}

func TestRPCNamedHandlerDoesNotCollideWithHandlerDeclarationFile(t *testing.T) {
	protocol := fakeProtocolModule(t)
	path := filepath.Join(protocol, "gen", "pb", "company_api", "v1", "service_grpc.pb.go")
	writeTestFile(t, path, strings.ReplaceAll(string(mustReadTestFile(t, path)), "GetUser", "Handler"))
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, filepath.Join(service, "internal", "service", "user_handler.go"), "type UserHandler struct")
	assertContains(t, filepath.Join(service, "internal", "service", "user_handler_handler.go"), "func (h *UserHandler) Handler")
}

func TestBindServerReusesCompleteUserOwnedHandler(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	customPath := filepath.Join(service, "internal", "service", "custom_user_handler.go")
	custom := `package service

import (
  "context"
  pb "example.com/company-api/gen/pb/company_api/v1"
)

type UserHandler struct { *Service }
func NewUserHandler(application *Service) *UserHandler { return &UserHandler{Service: application} }
func (h *UserHandler) GetUser(context.Context, *pb.GetUserRequest) (*pb.GetUserResponse, error) { return &pb.GetUserResponse{}, nil }
`
	writeTestFile(t, customPath, custom)
	before := mustReadTestFile(t, customPath)
	if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	assertFileEquals(t, customPath, before)
	assertNotExist(t, filepath.Join(service, "internal", "service", "user_handler.go"))
	assertNotExist(t, filepath.Join(service, "internal", "service", "user_handler_get_user.go"))
	if err := Validate(service); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestBindServerRejectsIncompleteExistingHandlerAndRollsBack(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	writeTestFile(t, filepath.Join(service, "internal", "service", "custom_user_handler.go"), "package service\n\ntype UserHandler struct { *Service }\n")
	externalPath := filepath.Join(service, "internal", "transport", "grpc", "external.gen.go")
	beforeExternal := mustReadTestFile(t, externalPath)
	beforeMod := mustReadTestFile(t, filepath.Join(service, "go.mod"))
	_, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true})
	if err == nil || !strings.Contains(err.Error(), "NewUserHandler") || !strings.Contains(err.Error(), "func(*Service) *UserHandler") {
		t.Fatalf("BindServer() error = %v", err)
	}
	assertFileEquals(t, externalPath, beforeExternal)
	assertFileEquals(t, filepath.Join(service, "go.mod"), beforeMod)
	assertNotExist(t, filepath.Join(service, ".jgo", "rpc.json"))
	assertNotExist(t, filepath.Join(service, "internal", "service", "user_handler_get_user.go"))
}

func TestBindServerRejectsWrongExistingHandlerConstructorSignature(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	writeTestFile(t, filepath.Join(service, "internal", "service", "custom_user_handler.go"), `package service

type UserHandler struct { *Service }
func NewUserHandler(*Service) *Service { return nil }
`)
	_, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true})
	if err == nil || !strings.Contains(err.Error(), "func(*Service) *UserHandler") {
		t.Fatalf("BindServer() error = %v", err)
	}
	assertNotExist(t, filepath.Join(service, ".jgo", "rpc.json"))
}

func TestBindServerRejectsUnexpectedPackageInServiceDirectory(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	writeTestFile(t, filepath.Join(service, "internal", "service", "wrong_package.go"), "package other\n\ntype UserHandler struct{}\n")
	_, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true})
	if err == nil || !strings.Contains(err.Error(), "must contain only package service") || !strings.Contains(err.Error(), "other, service") {
		t.Fatalf("BindServer() error = %v", err)
	}
	assertNotExist(t, filepath.Join(service, ".jgo", "rpc.json"))
}

func TestValidateRejectsMissingUserOwnedHandlerParts(t *testing.T) {
	protocol := fakeProtocolModule(t)
	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{name: "handler declaration", path: "user_handler.go", want: "handler type UserHandler is missing"},
		{name: "method", path: "user_handler_get_user.go", want: "UserHandler.GetUser is missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
			if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(service, "internal", "service", test.path)); err != nil {
				t.Fatal(err)
			}
			if err := Validate(service); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestValidateRejectsIncompatibleUserOwnedHandlerMethodSignatures(t *testing.T) {
	protocol := fakeProtocolModule(t)
	tests := []struct {
		name        string
		signature   string
		actual      string
		pbImport    string
		declaration string
	}{
		{name: "value receiver", signature: "func (h UserHandler) GetUser(context.Context, *pb.GetUserRequest) (*pb.GetUserResponse, error)", actual: "func (h UserHandler) GetUser"},
		{name: "wrong context", signature: "func (h *UserHandler) GetUser(string, *pb.GetUserRequest) (*pb.GetUserResponse, error)", actual: "GetUser(string"},
		{name: "wrong request", signature: "func (h *UserHandler) GetUser(context.Context, *pb.WrongRequest) (*pb.GetUserResponse, error)", actual: "*pb.WrongRequest"},
		{name: "request is not pointer", signature: "func (h *UserHandler) GetUser(context.Context, pb.GetUserRequest) (*pb.GetUserResponse, error)", actual: "pb.GetUserRequest"},
		{name: "wrong response", signature: "func (h *UserHandler) GetUser(context.Context, *pb.GetUserRequest) (*pb.WrongResponse, error)", actual: "*pb.WrongResponse"},
		{name: "response is not pointer", signature: "func (h *UserHandler) GetUser(context.Context, *pb.GetUserRequest) (pb.GetUserResponse, error)", actual: "pb.GetUserResponse"},
		{name: "wrong error", signature: "func (h *UserHandler) GetUser(context.Context, *pb.GetUserRequest) (*pb.GetUserResponse, string)", actual: "string"},
		{name: "extra parameter", signature: "func (h *UserHandler) GetUser(context.Context, *pb.GetUserRequest, bool) (*pb.GetUserResponse, error)", actual: "bool"},
		{name: "extra result", signature: "func (h *UserHandler) GetUser(context.Context, *pb.GetUserRequest) (*pb.GetUserResponse, error, bool)", actual: "bool"},
		{name: "wrong protobuf package", signature: "func (h *UserHandler) GetUser(context.Context, *pb.GetUserRequest) (*pb.GetUserResponse, error)", actual: `pb="example.com/other-api/gen/pb/company_api/v1"`, pbImport: "example.com/other-api/gen/pb/company_api/v1"},
		{name: "shadowed error", signature: "func (h *UserHandler) GetUser(context.Context, *pb.GetUserRequest) (*pb.GetUserResponse, error)", actual: `shadows predeclared "error"`, declaration: "type error string\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
			if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(service, "internal", "service", "user_handler_get_user.go")
			pbImport := test.pbImport
			if pbImport == "" {
				pbImport = "example.com/company-api/gen/pb/company_api/v1"
			}
			writeTestFile(t, path, `package service

import (
  "context"
  pb "`+pbImport+`"
)

`+test.declaration+test.signature+` { return nil, nil }
`)
			err := Validate(service)
			if err == nil || !strings.Contains(err.Error(), "UserHandler.GetUser has incompatible signature") || !strings.Contains(err.Error(), "expected: func (h *UserHandler) GetUser(context.Context, *pb.GetUserRequest) (*pb.GetUserResponse, error)") || !strings.Contains(err.Error(), "actual:") || !strings.Contains(err.Error(), test.actual) {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestValidateAcceptsEquivalentHandlerSignatureImportAliasesAndNames(t *testing.T) {
	protocol := fakeProtocolModule(t)
	for _, test := range []struct {
		name   string
		source string
	}{
		{
			name: "explicit aliases",
			source: `package service

import (
  ctx "context"
  userv1 "example.com/company-api/gen/pb/company_api/v1"
)

func (h *UserHandler) GetUser(context ctx.Context, request *userv1.GetUserRequest) (response *userv1.GetUserResponse, err error) { return nil, nil }
`,
		},
		{
			name: "protobuf declared package name",
			source: `package service

import (
  "context"
  "example.com/company-api/gen/pb/company_api/v1"
)

func (h *UserHandler) GetUser(context.Context, *company_apiv1.GetUserRequest) (*company_apiv1.GetUserResponse, error) { return nil, nil }
`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
			if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(service, "internal", "service", "user_handler_get_user.go"), test.source)
			if err := Validate(service); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestGenerateRejectsIncompatibleExistingHandlerMethodWithoutOverwritingIt(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(service, "internal", "service", "user_handler_get_user.go")
	wrong := []byte(`package service

import (
  "context"
  pb "example.com/company-api/gen/pb/company_api/v1"
)

func (h *UserHandler) GetUser(context.Context, *pb.WrongRequest) (*pb.GetUserResponse, error) { return nil, nil }
`)
	writeTestFile(t, path, string(wrong))
	if _, err := Generate(service); err == nil || !strings.Contains(err.Error(), "UserHandler.GetUser has incompatible signature") {
		t.Fatalf("Generate() error = %v", err)
	}
	assertFileEquals(t, path, wrong)
}

func TestGenerateRecreatesMissingUserOwnedHandlerParts(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"user_handler.go", "user_handler_get_user.go"} {
		if err := os.Remove(filepath.Join(service, "internal", "service", name)); err != nil {
			t.Fatal(err)
		}
	}
	changed, err := Generate(service)
	if err != nil || !changed {
		t.Fatalf("Generate() = %v, %v", changed, err)
	}
	if err := Validate(service); err != nil {
		t.Fatalf("Validate() after Generate = %v", err)
	}
}

func TestValidateRejectsGeneratedBindingContentDrift(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeMixed, protocol)
	if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := BindClient(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", Name: "user", SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	if err := Validate(service); err != nil {
		t.Fatalf("valid generated bindings rejected: %v", err)
	}

	tests := []struct {
		name        string
		path        string
		old         string
		replacement string
		want        string
	}{
		{
			name: "server registration", path: filepath.Join(service, "internal", "transport", "grpc", "external.gen.go"),
			old:         "pb0.RegisterUserServiceServer(registrar, &userServiceExternalServer0{handler: service.NewUserHandler(application)})",
			replacement: "_ = registrar",
			want:        "generated server bindings differ",
		},
		{
			name: "client field", path: filepath.Join(service, "internal", "rpcclient", "clients.gen.go"),
			old: "User pb0.UserServiceClient", replacement: "User any",
			want: "generated client bindings differ",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := mustReadTestFile(t, test.path)
			changed := strings.Replace(string(original), test.old, test.replacement, 1)
			if changed == string(original) {
				t.Fatalf("test fragment %q not found in %s", test.old, test.path)
			}
			if err := os.WriteFile(test.path, []byte(changed), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := Validate(service); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v", err)
			}
			if err := os.WriteFile(test.path, original, 0o644); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReverseOrderBindingsAreImmediatelyDeterministic(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeMixed, protocol)
	module := "example.com/company-api@v0.1.1"
	for _, serviceName := range []string{"UserService", "AdminService"} {
		if _, err := BindServer(BindConfig{Root: service, ModuleSpec: module, Service: serviceName, SkipTidy: true}); err != nil {
			t.Fatalf("BindServer(%s): %v", serviceName, err)
		}
	}
	for _, name := range []string{"zeta", "alpha"} {
		if _, err := BindClient(BindConfig{Root: service, ModuleSpec: module, Service: "UserService", Name: name, SkipTidy: true}); err != nil {
			t.Fatalf("BindClient(%s): %v", name, err)
		}
	}
	serverPath := filepath.Join(service, "internal", "transport", "grpc", "external.gen.go")
	clientPath := filepath.Join(service, "internal", "rpcclient", "clients.gen.go")
	serverBefore, clientBefore := mustReadTestFile(t, serverPath), mustReadTestFile(t, clientPath)
	if changed, err := Generate(service); err != nil || !changed {
		t.Fatalf("Generate() = %v, %v", changed, err)
	}
	assertFileEquals(t, serverPath, serverBefore)
	assertFileEquals(t, clientPath, clientBefore)
}

func TestMutateFilesRestoresOriginalPermissions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "go.mod")
	if err := os.WriteFile(path, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := mutateFiles(root, []string{"go.mod"}, func() error {
		if err := os.WriteFile(path, []byte("changed\n"), 0o644); err != nil {
			return err
		}
		if err := os.Chmod(path, 0o644); err != nil {
			return err
		}
		return errors.New("injected failure")
	})
	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("mutateFiles() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if contents := string(mustReadTestFile(t, path)); contents != "original\n" || info.Mode().Perm() != 0o600 {
		t.Fatalf("restored file = %q mode=%o", contents, info.Mode().Perm())
	}
}

func TestMutateFilesRejectsManagedSymlinks(t *testing.T) {
	tests := []struct {
		name     string
		relative string
		needle   string
		setup    func(t *testing.T, root, external string)
	}{
		{
			name:     "target",
			relative: "go.mod",
			needle:   "refuse symlink managed path",
			setup: func(t *testing.T, root, external string) {
				t.Helper()
				if err := os.Symlink(external, filepath.Join(root, "go.mod")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:     "parent",
			relative: filepath.FromSlash("configs/local.yaml"),
			needle:   "refuse symlink managed path",
			setup: func(t *testing.T, root, external string) {
				t.Helper()
				externalDirectory := filepath.Dir(external)
				if err := os.Symlink(externalDirectory, filepath.Join(root, "configs")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:     "non-regular target",
			relative: "go.mod",
			needle:   "is not a regular file",
			setup: func(t *testing.T, root, _ string) {
				t.Helper()
				if err := os.Mkdir(filepath.Join(root, "go.mod"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			externalDirectory := t.TempDir()
			external := filepath.Join(externalDirectory, filepath.Base(test.relative))
			if err := os.WriteFile(external, []byte("external sentinel\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			test.setup(t, root, external)
			called := false
			err := mutateFiles(root, []string{test.relative}, func() error {
				called = true
				return os.WriteFile(filepath.Join(root, test.relative), []byte("changed\n"), 0o644)
			})
			if err == nil || !strings.Contains(err.Error(), test.needle) {
				t.Fatalf("mutateFiles() error = %v", err)
			}
			if called {
				t.Fatal("mutation ran after managed symlink was detected")
			}
			if contents := string(mustReadTestFile(t, external)); contents != "external sentinel\n" {
				t.Fatalf("external file changed to %q", contents)
			}
		})
	}
}

func TestMutateFilesRollbackDoesNotFollowIntroducedParentSymlink(t *testing.T) {
	root := t.TempDir()
	configDirectory := filepath.Join(root, "configs")
	if err := os.Mkdir(configDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDirectory, "local.yaml")
	if err := os.WriteFile(configPath, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	externalDirectory := t.TempDir()
	external := filepath.Join(externalDirectory, "local.yaml")
	if err := os.WriteFile(external, []byte("external sentinel\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := mutateFiles(root, []string{filepath.FromSlash("configs/local.yaml")}, func() error {
		if err := os.Remove(configPath); err != nil {
			return err
		}
		if err := os.Remove(configDirectory); err != nil {
			return err
		}
		if err := os.Symlink(externalDirectory, configDirectory); err != nil {
			return err
		}
		return errors.New("injected failure")
	})
	if err == nil || !strings.Contains(err.Error(), "injected failure") || !strings.Contains(err.Error(), "refuse symlink managed path") {
		t.Fatalf("mutateFiles() error = %v", err)
	}
	if contents := string(mustReadTestFile(t, external)); contents != "external sentinel\n" {
		t.Fatalf("external file changed to %q", contents)
	}
}

func TestManifestRejectsInvalidHandlerName(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	state, err := loadManifest(service)
	if err != nil {
		t.Fatal(err)
	}
	state.Servers[0].Handler = "unexported"
	if err := saveManifest(service, state); err != nil {
		t.Fatal(err)
	}
	if err := Validate(service); err == nil || !strings.Contains(err.Error(), "invalid handler name") {
		t.Fatalf("Validate() error = %v", err)
	}
	state.Servers[0].Handler = "UserHandler"
	if err := saveManifest(service, state); err != nil {
		t.Fatal(err)
	}
	if _, err := List(service); err == nil || !strings.Contains(err.Error(), "invalid handler name") {
		t.Fatalf("List() error = %v", err)
	}
}

func TestManifestRejectsInvalidOrDuplicateMethodMetadata(t *testing.T) {
	binding := Binding{
		Module: "example.com/company-api", Package: "example.com/company-api/gen/pb/company_api/v1",
		GoPackage: "company_apiv1", Service: "UserService", Handler: "User",
		Methods: []Method{{Name: "GetUser", Request: "GetUserRequest", Response: "GetUserResponse"}},
	}
	tests := []struct {
		name   string
		change func(*Binding)
		want   string
	}{
		{name: "unexported method", change: func(value *Binding) { value.Methods[0].Name = "getUser" }, want: "invalid method metadata"},
		{name: "missing request", change: func(value *Binding) { value.Methods[0].Request = "" }, want: "invalid method metadata"},
		{name: "duplicate method", change: func(value *Binding) { value.Methods = append(value.Methods, value.Methods[0]) }, want: "duplicate method GetUser"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := binding
			candidate.Methods = append([]Method(nil), binding.Methods...)
			test.change(&candidate)
			if err := validateManifest(manifest{Version: manifestVersion, Servers: []Binding{candidate}}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateManifest() error = %v", err)
			}
		})
	}
}

func TestMissingManifestDetectsAndReconcilesStaleBindings(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeMixed, protocol)
	if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := BindClient(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(service, ".jgo", "rpc.json")); err != nil {
		t.Fatal(err)
	}
	if err := Validate(service); err == nil || !strings.Contains(err.Error(), "without manifest entries") {
		t.Fatalf("Validate() error = %v", err)
	}
	changed, err := Generate(service)
	if err != nil || !changed {
		t.Fatalf("Generate() = %v, %v", changed, err)
	}
	if err := Validate(service); err != nil {
		t.Fatalf("Validate() after reconciliation = %v", err)
	}
	assertContains(t, filepath.Join(service, "internal", "transport", "grpc", "external.gen.go"), "func registerExternal(grpc.ServiceRegistrar, *service.Service) {}")
	assertContains(t, filepath.Join(service, "internal", "rpcclient", "clients.gen.go"), "type Clients struct{}")
}

func TestValidateRejectsMissingBoundClientConfiguration(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := fakeServiceProject(t, protocol)
	if _, err := BindClient(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	if err := Validate(service); err != nil {
		t.Fatalf("valid binding rejected: %v", err)
	}
	writeTestFile(t, filepath.Join(service, "configs", "local.yaml"), "rpc_client: {}\n")
	if err := Validate(service); err == nil || !strings.Contains(err.Error(), "rpc_client.user configuration is missing") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestManifestRejectsGeneratedClientFieldCollisions(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := fakeServiceProject(t, protocol)
	writeTestFile(t, filepath.Join(service, ".jgo", "rpc.json"), `{
  "version": 2,
  "clients": [
    {"name":"user_primary","module":"example.com/company-api","version":"v0.1.1","package":"example.com/company-api/gen/pb/company_api/v1","go_package":"company_apiv1","service":"UserService","methods":[]},
    {"name":"userPrimary","module":"example.com/company-api","version":"v0.1.1","package":"example.com/company-api/gen/pb/company_api/v1","go_package":"company_apiv1","service":"UserService","methods":[]}
  ]
}`)
	if _, err := List(service); err == nil || !strings.Contains(err.Error(), "conflict after Go field generation") {
		t.Fatalf("List() error = %v", err)
	}
}

func TestGenerateRejectsServerManifestInWebProject(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeWeb, protocol)
	writeTestFile(t, filepath.Join(service, ".jgo", "rpc.json"), `{
  "version": 2,
  "servers": [
    {"module":"example.com/company-api","version":"v0.1.1","package":"example.com/company-api/gen/pb/company_api/v1","go_package":"company_apiv1","service":"UserService","handler":"User","methods":[{"name":"GetUser","request":"GetUserRequest","response":"GetUserResponse"}]}
  ]
}`)
	if _, err := Generate(service); err == nil || !strings.Contains(err.Error(), "require a grpc or mixed project") {
		t.Fatalf("Generate() error = %v", err)
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

import (
  "context"
  "google.golang.org/grpc"
)

type GetUserRequest struct{}
type GetUserResponse struct { Code int32; Msg string }
type UserServiceClient interface { GetUser(context.Context, *GetUserRequest) (*GetUserResponse, error) }
func NewUserServiceClient(grpc.ClientConnInterface) UserServiceClient { return nil }
type UserServiceServer interface {
  GetUser(context.Context, *GetUserRequest) (*GetUserResponse, error)
  mustEmbedUnimplementedUserServiceServer()
}
type UnimplementedUserServiceServer struct{}
func (UnimplementedUserServiceServer) mustEmbedUnimplementedUserServiceServer() {}
func RegisterUserServiceServer(grpc.ServiceRegistrar, UserServiceServer) {}

type AdminServiceServer interface {
  GetUser(context.Context, *GetUserRequest) (*GetUserResponse, error)
  mustEmbedUnimplementedAdminServiceServer()
}
type UnimplementedAdminServiceServer struct{}
func (UnimplementedAdminServiceServer) mustEmbedUnimplementedAdminServiceServer() {}
func RegisterAdminServiceServer(grpc.ServiceRegistrar, AdminServiceServer) {}
`)
}

func generatedBindingProject(t *testing.T, projectType projectgen.Type, protocol string) string {
	t.Helper()
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "service")
	if _, err := projectgen.Generate(projectgen.Config{Name: "service", Module: "example.com/service", Type: projectType, TargetDir: root, JGOReplace: repositoryRoot, SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	goMod := filepath.Join(root, "go.mod")
	writeTestFile(t, goMod, string(mustReadTestFile(t, goMod))+"\nreplace example.com/company-api => "+protocol+"\n")
	return root
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
