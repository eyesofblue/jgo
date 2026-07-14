package rpcbinding

import (
	"bytes"
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
	assertContains(t, filepath.Join(service, "internal/service/company_api_v1_user_service_get_user.go"), "CompanyApiV1UserServiceGetUser")
	assertContains(t, filepath.Join(service, "internal/rpcclient/clients.gen.go"), "User pb0.UserServiceClient")
	assertContains(t, filepath.Join(service, "configs/local.yaml"), "user:")
	assertContains(t, filepath.Join(service, "configs/local.yaml"), "readiness: required")
	assertContains(t, filepath.Join(service, ".jgo/rpc.json"), `"version": 1`)
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
	assertNotExist(t, filepath.Join(service, "internal", "service", "company_apiv1_user_service_get_user.go"))
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
	parent := t.TempDir()
	protocol := filepath.Join(parent, "company-api")
	service := filepath.Join(parent, "service")
	writeTestFile(t, filepath.Join(protocol, "go.mod"), "module example.com/company-api\n\ngo 1.24.0\n")
	copyGeneratedService(t, protocol, "gen/pb/company_api/v1/service_grpc.pb.go", "company_apiv1")
	writeTestFile(t, filepath.Join(service, "go.mod"), "module example.com/service\n\ngo 1.24.0\n")
	writeTestFile(t, filepath.Join(service, "cmd/server/main.go"), "package main\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(service, "internal/service/service.go"), "package service\ntype Service struct{}\n")
	writeTestFile(t, filepath.Join(service, "configs/local.yaml"), "rpc_client: {}\n")
	writeTestFile(t, filepath.Join(parent, "go.work"), "go 1.24.0\n\nuse (\n\t./company-api\n\t./service\n)\n")
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
	parent := t.TempDir()
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
	writeTestFile(t, filepath.Join(parent, "go.work"), "go 1.24.0\n\nuse (\n\t./company-api\n\t./service\n)\n")
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

func TestBindServerRejectsGeneratedServiceFileNameCollision(t *testing.T) {
	protocol := fakeProtocolModule(t)
	path := filepath.Join(protocol, "gen", "pb", "company_api", "v1", "service_grpc.pb.go")
	contents := strings.Replace(string(mustReadTestFile(t, path)),
		"GetUser(context.Context, *GetUserRequest) (*GetUserResponse, error)",
		"GetURL(context.Context, *GetUserRequest) (*GetUserResponse, error)\n  GetUrl(context.Context, *GetUserRequest) (*GetUserResponse, error)", 1)
	writeTestFile(t, path, contents)
	service := fakeServiceProject(t, protocol)
	_, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true})
	if err == nil || !strings.Contains(err.Error(), "same service file") {
		t.Fatalf("BindServer() error = %v", err)
	}
	assertNotExist(t, filepath.Join(service, ".jgo", "rpc.json"))
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
	stub := filepath.Join(service, "internal", "service", "company_api_v1_user_service_get_user.go")
	if err := UnbindServer(service, "UserService"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stub); err != nil {
		t.Fatalf("user-owned implementation was removed: %v", err)
	}
	assertContains(t, filepath.Join(service, "internal", "transport", "grpc", "external.gen.go"), "func registerExternal(grpc.ServiceRegistrar, *service.Service) {}")
	manifest := string(mustReadTestFile(t, filepath.Join(service, ".jgo", "rpc.json")))
	if strings.Contains(manifest, "UserService") {
		t.Fatalf("server manifest remains after unbind:\n%s", manifest)
	}
}

func TestServerBindingsAllowSameServiceAcrossPackages(t *testing.T) {
	protocol := fakeProtocolModule(t)
	copyGeneratedService(t, protocol, "gen/pb/company_api/v2/service_grpc.pb.go", "company_apiv2")
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	v1 := "example.com/company-api/gen/pb/company_api/v1"
	v2 := "example.com/company-api/gen/pb/company_api/v2"
	for _, packagePath := range []string{v1, v2} {
		if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Package: packagePath, Service: "UserService", SkipTidy: true}); err != nil {
			t.Fatalf("bind %s: %v", packagePath, err)
		}
	}
	legacy, err := loadManifest(service)
	if err != nil {
		t.Fatal(err)
	}
	for bindingIndex := range legacy.Servers {
		for methodIndex := range legacy.Servers[bindingIndex].Methods {
			legacy.Servers[bindingIndex].Methods[methodIndex].Business = ""
		}
	}
	if err := saveManifest(service, legacy); err != nil {
		t.Fatal(err)
	}
	if changed, err := Generate(service); err != nil || !changed {
		t.Fatalf("Generate() = %v, %v", changed, err)
	}
	manifest := string(mustReadTestFile(t, filepath.Join(service, ".jgo", "rpc.json")))
	for _, expected := range []string{"CompanyApiV1UserServiceGetUser", "CompanyApiV2UserServiceGetUser"} {
		if !strings.Contains(manifest, `"business": "`+expected+`"`) {
			t.Fatalf("manifest lacks %s:\n%s", expected, manifest)
		}
	}
	assertOccurrenceCount(t, filepath.Join(service, "internal", "transport", "grpc", "external.gen.go"), "RegisterUserServiceServer", 2)
	if err := UnbindServer(service, "UserService"); err == nil || !strings.Contains(err.Error(), "multiple packages") {
		t.Fatalf("ambiguous UnbindServer() error = %v", err)
	}
	if err := UnbindServer(service, "UserService", v1); err != nil {
		t.Fatal(err)
	}
	assertContains(t, filepath.Join(service, "internal", "transport", "grpc", "external.gen.go"), v2)
	if strings.Contains(string(mustReadTestFile(t, filepath.Join(service, "internal", "transport", "grpc", "external.gen.go"))), v1) {
		t.Fatal("v1 binding remains after package-qualified unbind")
	}
}

func TestServerBindingsWithSameGoPackageUseImportPathQualifiedBusinessNames(t *testing.T) {
	protocol := fakeProtocolModule(t)
	copyGeneratedService(t, protocol, "gen/pb/company_api/v2/service_grpc.pb.go", "company_apiv1")
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	for _, packagePath := range []string{
		"example.com/company-api/gen/pb/company_api/v1",
		"example.com/company-api/gen/pb/company_api/v2",
	} {
		if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Package: packagePath, Service: "UserService", SkipTidy: true}); err != nil {
			t.Fatalf("bind %s: %v", packagePath, err)
		}
	}
	manifest := string(mustReadTestFile(t, filepath.Join(service, ".jgo", "rpc.json")))
	for _, expected := range []string{"CompanyApiV1UserServiceGetUser", "CompanyApiV2UserServiceGetUser"} {
		if !strings.Contains(manifest, `"business": "`+expected+`"`) {
			t.Fatalf("manifest lacks %s:\n%s", expected, manifest)
		}
	}
}

func TestGenerateRejectsSilentLegacyBusinessMethodMigration(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	state, err := loadManifest(service)
	if err != nil {
		t.Fatal(err)
	}
	state.Servers[0].Methods[0].Business = ""
	if err := saveManifest(service, state); err != nil {
		t.Fatal(err)
	}
	qualifiedStub := filepath.Join(service, "internal", "service", "company_api_v1_user_service_get_user.go")
	if err := os.Remove(qualifiedStub); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(service, "internal", "service", "legacy_user.go")
	if err := os.WriteFile(legacy, []byte("package service\n\nfunc (*Service) UserServiceGetUser() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(service); err == nil || !strings.Contains(err.Error(), "rename it to Service.CompanyApiV1UserServiceGetUser") {
		t.Fatalf("Generate() error = %v", err)
	}
	if err := Validate(service); err == nil || !strings.Contains(err.Error(), "rename it to Service.CompanyApiV1UserServiceGetUser") {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := os.WriteFile(legacy, []byte(`package service

import (
	"context"
	pb "example.com/company-api/gen/pb/company_api/v1"
)

func (*Service) CompanyApiV1UserServiceGetUser(context.Context, *pb.GetUserRequest) (*pb.GetUserResponse, error) {
	return nil, nil
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, err := Generate(service); err != nil || !changed {
		t.Fatalf("Generate() after migration = %v, %v", changed, err)
	}
	if _, err := os.Stat(qualifiedStub); !os.IsNotExist(err) {
		t.Fatalf("generator created a duplicate qualified stub: %v", err)
	}
	assertContains(t, filepath.Join(service, "internal", "transport", "grpc", "external.gen.go"), "application.CompanyApiV1UserServiceGetUser")
	command := exec.Command("go", "test", "./...")
	command.Dir = service
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("migrated project does not compile: %v\n%s", err, output)
	}
}

func TestImportPathNormalizationCollisionsUseStableSuffix(t *testing.T) {
	protocol := fakeProtocolModule(t)
	copyGeneratedService(t, protocol, "gen/pb/gen/company_api/v1/service_grpc.pb.go", "company_apiv1")
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	packages := []string{
		"example.com/company-api/gen/pb/company_api/v1",
		"example.com/company-api/gen/pb/gen/company_api/v1",
	}
	for _, packagePath := range packages {
		if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Package: packagePath, Service: "UserService", SkipTidy: true}); err != nil {
			t.Fatalf("bind %s: %v", packagePath, err)
		}
	}
	state, err := loadManifest(service)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Servers) != 2 || state.Servers[0].Methods[0].Business == state.Servers[1].Methods[0].Business {
		t.Fatalf("colliding paths were not disambiguated: %+v", state.Servers)
	}
	foundSuffix := false
	for _, binding := range state.Servers {
		if strings.Contains(binding.Methods[0].Business, "Path") {
			foundSuffix = true
		}
	}
	if !foundSuffix {
		t.Fatalf("collision did not receive a stable path suffix: %+v", state.Servers)
	}
}

func TestImportPathPunctuationProducesValidBusinessName(t *testing.T) {
	protocol := fakeProtocolModule(t)
	copyGeneratedService(t, protocol, "gen/pb/company.api/v3/service_grpc.pb.go", "company_apiv3")
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	binding, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Package: "example.com/company-api/gen/pb/company.api/v3", Service: "UserService", SkipTidy: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := binding.Methods[0].Business; got != "CompanyApiV3UserServiceGetUser" {
		t.Fatalf("business name = %q", got)
	}
}

func TestManifestRejectsInvalidBusinessMethodName(t *testing.T) {
	protocol := fakeProtocolModule(t)
	service := generatedBindingProject(t, projectgen.TypeGRPC, protocol)
	if _, err := BindServer(BindConfig{Root: service, ModuleSpec: "example.com/company-api@v0.1.1", Service: "UserService", SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	state, err := loadManifest(service)
	if err != nil {
		t.Fatal(err)
	}
	state.Servers[0].Methods[0].Business = "unexportedMethod"
	if err := saveManifest(service, state); err != nil {
		t.Fatal(err)
	}
	if err := Validate(service); err == nil || !strings.Contains(err.Error(), "exported Go identifier") {
		t.Fatalf("Validate() error = %v", err)
	}
	state.Servers[0].Methods[0].Business = "WrongExportedName"
	if err := saveManifest(service, state); err != nil {
		t.Fatal(err)
	}
	if err := Validate(service); err == nil || !strings.Contains(err.Error(), "does not match deterministic name") {
		t.Fatalf("Validate() deterministic-name error = %v", err)
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
  "version": 1,
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
  "version": 1,
  "servers": [
    {"module":"example.com/company-api","version":"v0.1.1","package":"example.com/company-api/gen/pb/company_api/v1","go_package":"company_apiv1","service":"UserService","methods":[{"name":"GetUser","request":"GetUserRequest","response":"GetUserResponse"}]}
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
