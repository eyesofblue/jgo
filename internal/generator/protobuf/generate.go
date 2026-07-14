package protobuf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/mod/modfile"
)

const (
	// BufVersion is the Buf release verified by JGO's real generation tests.
	BufVersion = "1.46.0"
	// ProtocGenGoVersion is the protobuf Go generator version locked by JGO.
	ProtocGenGoVersion = "1.36.7"
	// ProtocGenGoGRPCVersion is the gRPC Go generator version locked by JGO.
	ProtocGenGoGRPCVersion = "1.5.1"
)

// Tool describes one executable in JGO's locked protobuf toolchain.
type Tool struct {
	Name          string
	Package       string
	Version       string
	VersionPrefix string
}

// LockedTools returns a copy of the protobuf tools required by JGO.
func LockedTools() []Tool {
	return []Tool{
		{Name: "buf", Package: "github.com/bufbuild/buf/cmd/buf", Version: BufVersion},
		{Name: "protoc-gen-go", Package: "google.golang.org/protobuf/cmd/protoc-gen-go", Version: ProtocGenGoVersion, VersionPrefix: "v"},
		{Name: "protoc-gen-go-grpc", Package: "google.golang.org/grpc/cmd/protoc-gen-go-grpc", Version: ProtocGenGoGRPCVersion},
	}
}

// Matches reports whether version output identifies the locked tool version.
func (tool Tool) Matches(output string) bool {
	want := tool.VersionPrefix + tool.Version
	for _, field := range strings.Fields(output) {
		if strings.TrimSpace(field) == want {
			return true
		}
	}
	return false
}

type runner interface {
	Run(context.Context, string, string, ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, directory, name string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		if errorsIsExecutableMissing(err) {
			return "", fmt.Errorf("protobuf: %s is not installed; run `jgo tools install`", name)
		}
		return string(output), fmt.Errorf("protobuf: %s %s: %w: %s", name, strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func errorsIsExecutableMissing(err error) bool {
	var executableError *exec.Error
	return errors.As(err, &executableError) && executableError.Err == exec.ErrNotFound
}

// ServiceStub describes a newly created business service placeholder.
type ServiceStub struct {
	Method string
	Path   string
}

// GenerateResult describes user-owned files created during protobuf generation.
type GenerateResult struct {
	CreatedStubs []ServiceStub
	ProtocolOnly bool
	Empty        bool
}

// Generate lints protobuf contracts, invokes Buf, and updates JGO's gRPC adapters.
func Generate(root string) error {
	return generate(context.Background(), root, execRunner{})
}

// GenerateWithResult behaves like Generate and reports newly created service
// placeholders so callers can print precise implementation guidance.
func GenerateWithResult(root string) (GenerateResult, error) {
	return generateWithResult(context.Background(), root, execRunner{})
}

// CheckTools verifies the locked Buf and protobuf generator toolchain without
// changing the project or the developer's environment.
func CheckTools(root string) error {
	if root == "" {
		root = "."
	}
	return checkTools(context.Background(), root, execRunner{})
}

// Lint checks local contracts and returns false for a valid empty project.
func Lint(ctx context.Context, root string) (bool, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	hasContracts, err := HasContracts(root)
	if err != nil || !hasContracts {
		return false, err
	}
	commands := execRunner{}
	if err := checkTool(ctx, root, commands, LockedTools()[0]); err != nil {
		return false, err
	}
	if _, err := commands.Run(ctx, root, "buf", "lint"); err != nil {
		return false, err
	}
	if err := ValidateResponseContracts(root); err != nil {
		return false, err
	}
	return true, nil
}

// Breaking compares local contracts with an explicit Buf source baseline.
func Breaking(ctx context.Context, root, against string) (bool, error) {
	if strings.TrimSpace(against) == "" {
		return false, errors.New("protobuf: --against is required")
	}
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	hasContracts, err := HasContracts(root)
	if err != nil || !hasContracts {
		return false, err
	}
	commands := execRunner{}
	if err := checkTool(ctx, root, commands, LockedTools()[0]); err != nil {
		return false, err
	}
	if _, err := commands.Run(ctx, root, "buf", "breaking", "--against", against); err != nil {
		return false, err
	}
	return true, nil
}

func generate(ctx context.Context, root string, commands runner) error {
	_, err := generateWithResult(ctx, root, commands)
	return err
}

func generateWithResult(ctx context.Context, root string, commands runner) (GenerateResult, error) {
	if root == "" {
		root = "."
	}
	module, err := readModule(root)
	if err != nil {
		return GenerateResult{}, err
	}
	hasContracts, err := HasContracts(root)
	if err != nil {
		return GenerateResult{}, err
	}
	if !hasContracts {
		serviceProject, layoutErr := hasServiceProjectLayout(root)
		if layoutErr != nil {
			return GenerateResult{}, layoutErr
		}
		return GenerateResult{ProtocolOnly: !serviceProject, Empty: true}, nil
	}
	for _, configuration := range []string{"buf.yaml", "buf.gen.yaml"} {
		if info, statErr := os.Stat(filepath.Join(root, configuration)); statErr != nil || !info.Mode().IsRegular() {
			if statErr == nil {
				statErr = fmt.Errorf("not a regular file")
			}
			return GenerateResult{}, fmt.Errorf("protobuf: inspect %s: %w", configuration, statErr)
		}
	}
	serviceProject, err := hasServiceProjectLayout(root)
	if err != nil {
		return GenerateResult{}, err
	}
	if err := checkTools(ctx, root, commands); err != nil {
		return GenerateResult{}, err
	}
	if _, err := commands.Run(ctx, root, "buf", "lint"); err != nil {
		return GenerateResult{}, err
	}
	if err := ValidateResponseContracts(root); err != nil {
		return GenerateResult{}, err
	}
	if _, err := commands.Run(ctx, root, "buf", "generate"); err != nil {
		return GenerateResult{}, err
	}
	if !serviceProject {
		return GenerateResult{ProtocolOnly: true}, nil
	}

	services, err := discoverGeneratedServices(root, module)
	if err != nil {
		return GenerateResult{}, err
	}
	stubs, err := createServiceStubs(root, services)
	if err != nil {
		return GenerateResult{}, err
	}
	if err := writeTransport(root, module, services); err != nil {
		return GenerateResult{}, err
	}
	return GenerateResult{CreatedStubs: stubs}, nil
}

// HasContracts reports whether the project currently contains any local proto
// contract. Empty grpc, mixed, and proto projects intentionally return false.
func HasContracts(root string) (bool, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	directory := filepath.Join(root, filepath.FromSlash(protoRoot))
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("protobuf: inspect %s: %w", directory, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("protobuf: %s is not a directory", directory)
	}
	found := false
	err = filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("protobuf: refusing symlink %s", path)
		}
		if entry.Type().IsRegular() && filepath.Ext(entry.Name()) == ".proto" {
			found = true
		}
		return nil
	})
	return found, err
}

func hasServiceProjectLayout(root string) (bool, error) {
	required := []string{
		filepath.Join("cmd", "server", "main.go"),
		filepath.Join("internal", "service", "service.go"),
		filepath.Join("internal", "transport", "grpc", "register.go"),
	}
	found := 0
	for _, relative := range required {
		info, err := os.Stat(filepath.Join(root, relative))
		if err == nil && info.Mode().IsRegular() {
			found++
			continue
		}
		if err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("protobuf: inspect service project layout %s: %w", relative, err)
		}
	}
	if found == 0 {
		return false, nil
	}
	if found != len(required) {
		return false, fmt.Errorf("protobuf: incomplete service project layout: require cmd/server/main.go, internal/service/service.go, and internal/transport/grpc/register.go")
	}
	return true, nil
}

func checkTools(ctx context.Context, root string, commands runner) error {
	for _, tool := range LockedTools() {
		if err := checkTool(ctx, root, commands, tool); err != nil {
			return err
		}
	}
	return nil
}

func checkTool(ctx context.Context, root string, commands runner, tool Tool) error {
	output, err := commands.Run(ctx, root, tool.Name, "--version")
	if err != nil {
		return err
	}
	if !tool.Matches(output) {
		return fmt.Errorf("protobuf: %s version mismatch: require %s, got %q; run `jgo tools install`", tool.Name, tool.Version, output)
	}
	return nil
}

type generatedService struct {
	Name       string
	Package    string
	ImportPath string
	Alias      string
	Methods    []generatedMethod
}

type generatedMethod struct {
	Name         string
	BusinessName string
	Request      string
	Response     string
}

func readModule(root string) (string, error) {
	contents, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("protobuf: read go.mod: %w", err)
	}
	module := modfile.ModulePath(contents)
	if module == "" {
		return "", fmt.Errorf("protobuf: go.mod has no module directive")
	}
	return module, nil
}

func discoverGeneratedServices(root, module string) ([]generatedService, error) {
	generatedRoot := filepath.Join(root, "gen", "pb")
	var paths []string
	err := filepath.WalkDir(generatedRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), "_grpc.pb.go") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("protobuf: scan generated gRPC files: %w", err)
	}
	sort.Strings(paths)
	var services []generatedService
	methodOwners := map[string]string{}
	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("protobuf: parse generated file %s: %w", path, err)
		}
		directory, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return nil, err
		}
		importPath := module + "/" + filepath.ToSlash(directory)
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, specification := range general.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok || !strings.HasSuffix(typeSpec.Name.Name, "Server") || strings.HasPrefix(typeSpec.Name.Name, "Unsafe") {
					continue
				}
				interfaceType, ok := typeSpec.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}
				serviceName := strings.TrimSuffix(typeSpec.Name.Name, "Server")
				service := generatedService{Name: serviceName, Package: file.Name.Name, ImportPath: importPath, Alias: file.Name.Name}
				for _, field := range interfaceType.Methods.List {
					if len(field.Names) != 1 || !ast.IsExported(field.Names[0].Name) {
						continue
					}
					function, ok := field.Type.(*ast.FuncType)
					if !ok {
						continue
					}
					method, ok := unaryMethod(field.Names[0].Name, function)
					if !ok {
						return nil, fmt.Errorf("protobuf: RPC %s.%s is streaming or has an unsupported generated signature", serviceName, field.Names[0].Name)
					}
					method.BusinessName = serviceName + method.Name
					if owner, exists := methodOwners[method.BusinessName]; exists {
						return nil, fmt.Errorf("protobuf: business method %q is declared by both %s and %s", method.BusinessName, owner, serviceName)
					}
					methodOwners[method.BusinessName] = serviceName
					service.Methods = append(service.Methods, method)
				}
				services = append(services, service)
			}
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("protobuf: buf generated no *_grpc.pb.go files")
	}
	assignUniqueAliases(services)
	return services, nil
}

func unaryMethod(name string, function *ast.FuncType) (generatedMethod, bool) {
	if function.Params == nil || function.Results == nil || len(function.Params.List) != 2 || len(function.Results.List) != 2 {
		return generatedMethod{}, false
	}
	request, ok := pointedIdentifier(function.Params.List[1].Type)
	if !ok {
		return generatedMethod{}, false
	}
	response, ok := pointedIdentifier(function.Results.List[0].Type)
	if !ok {
		return generatedMethod{}, false
	}
	if identifier, ok := function.Results.List[1].Type.(*ast.Ident); !ok || identifier.Name != "error" {
		return generatedMethod{}, false
	}
	return generatedMethod{Name: name, Request: request, Response: response}, true
}

func pointedIdentifier(expression ast.Expr) (string, bool) {
	pointer, ok := expression.(*ast.StarExpr)
	if !ok {
		return "", false
	}
	identifier, ok := pointer.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return identifier.Name, true
}

func assignUniqueAliases(services []generatedService) {
	aliasesByImport := map[string]string{}
	used := map[string]bool{
		"context":   true,
		"grpc":      true,
		"service":   true,
		"stderrors": true,
		"jgoerrors": true,
	}
	for index := range services {
		if alias, ok := aliasesByImport[services[index].ImportPath]; ok {
			services[index].Alias = alias
			continue
		}
		base := services[index].Package
		alias := base
		for suffix := 2; used[alias]; suffix++ {
			alias = fmt.Sprintf("%s%d", base, suffix)
		}
		services[index].Alias = alias
		aliasesByImport[services[index].ImportPath] = alias
		used[alias] = true
	}
}

func createServiceStubs(root string, services []generatedService) ([]ServiceStub, error) {
	directory := filepath.Join(root, "internal", "service")
	existing, err := existingServiceMethods(directory)
	if err != nil {
		return nil, err
	}
	type pendingStub struct {
		path     string
		contents []byte
		method   string
	}
	var pending []pendingStub
	pendingPaths := make(map[string]string)
	for _, service := range services {
		for _, method := range service.Methods {
			if existing[method.BusinessName] {
				continue
			}
			path := filepath.Join(directory, snakeCase(method.BusinessName)+".go")
			if owner, exists := pendingPaths[path]; exists && owner != method.BusinessName {
				return nil, fmt.Errorf("protobuf: business methods %q and %q map to the same service file %s", owner, method.BusinessName, path)
			}
			pendingPaths[path] = method.BusinessName
			if _, err := os.Stat(path); err == nil {
				return nil, fmt.Errorf("protobuf: refusing to overwrite existing service file %s", path)
			} else if !os.IsNotExist(err) {
				return nil, fmt.Errorf("protobuf: inspect service file %s: %w", path, err)
			}
			source := fmt.Sprintf(`package service

import (
	"context"
	"errors"

	%s %q
)

// %s implements the %s.%s RPC. Replace this placeholder with business logic.
func (s *Service) %s(ctx context.Context, request *%s.%s) (*%s.%s, error) {
	return nil, errors.New("%s is not implemented")
}
`, service.Alias, service.ImportPath, method.BusinessName, service.Name, method.Name, method.BusinessName, service.Alias, method.Request, service.Alias, method.Response, method.BusinessName)
			formatted, err := format.Source([]byte(source))
			if err != nil {
				return nil, fmt.Errorf("protobuf: format service stub %s: %w", method.BusinessName, err)
			}
			pending = append(pending, pendingStub{path: path, contents: formatted, method: method.BusinessName})
			existing[method.BusinessName] = true
		}
	}
	for _, stub := range pending {
		if err := atomicWrite(stub.path, stub.contents); err != nil {
			return nil, fmt.Errorf("protobuf: write service stub %s: %w", stub.path, err)
		}
	}
	created := make([]ServiceStub, 0, len(pending))
	for _, stub := range pending {
		created = append(created, ServiceStub{Method: stub.method, Path: displayPath(root, stub.path)})
	}
	return created, nil
}

func existingServiceMethods(directory string) (map[string]bool, error) {
	methods := map[string]bool{}
	set := token.NewFileSet()
	packages, err := parser.ParseDir(set, directory, func(info os.FileInfo) bool { return !strings.HasSuffix(info.Name(), "_test.go") }, 0)
	if err != nil {
		return nil, fmt.Errorf("protobuf: parse service package: %w", err)
	}
	for _, pkg := range packages {
		for _, file := range pkg.Files {
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if ok && isServiceReceiver(function.Recv) {
					methods[function.Name.Name] = true
				}
			}
		}
	}
	return methods, nil
}

func isServiceReceiver(receivers *ast.FieldList) bool {
	if receivers == nil || len(receivers.List) != 1 {
		return false
	}
	expression := receivers.List[0].Type
	if pointer, ok := expression.(*ast.StarExpr); ok {
		expression = pointer.X
	}
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == "Service"
}

func writeTransport(root, module string, services []generatedService) error {
	var output bytes.Buffer
	output.WriteString("// Code generated by jgo pb generate. DO NOT EDIT.\n")
	output.WriteString("package grpctransport\n\n")
	output.WriteString("import (\n")
	if hasAnyMethods(services) {
		output.WriteString("\t\"context\"\n")
		output.WriteString("\tstderrors \"errors\"\n")
		output.WriteString(fmt.Sprintf("\tjgoerrors %q\n", "github.com/eyesofblue/jgo/errors"))
		output.WriteString("\t\"go.opentelemetry.io/otel/attribute\"\n")
		output.WriteString("\t\"go.opentelemetry.io/otel/trace\"\n")
	}
	output.WriteString(fmt.Sprintf("\t\"%s/internal/service\"\n", module))
	seenImports := map[string]bool{}
	for _, service := range services {
		if !seenImports[service.ImportPath] {
			output.WriteString(fmt.Sprintf("\t%s %q\n", service.Alias, service.ImportPath))
			seenImports[service.ImportPath] = true
		}
	}
	output.WriteString("\t\"google.golang.org/grpc\"\n)\n\n")
	for _, service := range services {
		adapter := lowerFirst(service.Name) + "Server"
		output.WriteString(fmt.Sprintf("type %s struct {\n\t%s.Unimplemented%sServer\n\tapplication *service.Service\n}\n\n", adapter, service.Alias, service.Name))
		for _, method := range service.Methods {
			output.WriteString(fmt.Sprintf("func (server *%s) %s(ctx context.Context, request *%s.%s) (*%s.%s, error) {\n", adapter, method.Name, service.Alias, method.Request, service.Alias, method.Response))
			output.WriteString(fmt.Sprintf("\tresponse, err := server.application.%s(ctx, request)\n", method.BusinessName))
			output.WriteString("\tif err == nil {\n\t\treturn response, nil\n\t}\n")
			output.WriteString("\tvar businessError *jgoerrors.Error\n")
			output.WriteString(fmt.Sprintf("\tif stderrors.As(err, &businessError) {\n\t\ttrace.SpanFromContext(ctx).SetAttributes(\n\t\t\tattribute.Int64(\"jgo.business_code\", int64(businessError.Code())),\n\t\t\tattribute.String(\"jgo.business_message\", businessError.Message()),\n\t\t)\n\t\treturn &%s.%s{Code: int32(businessError.Code()), Msg: businessError.Message()}, nil\n\t}\n", service.Alias, method.Response))
			output.WriteString("\treturn nil, err\n}\n\n")
		}
	}
	output.WriteString("func registerGenerated(registrar grpc.ServiceRegistrar, application *service.Service) {\n")
	for _, service := range services {
		adapter := lowerFirst(service.Name) + "Server"
		output.WriteString(fmt.Sprintf("\t%s.Register%sServer(registrar, &%s{application: application})\n", service.Alias, service.Name, adapter))
	}
	output.WriteString("}\n")
	formatted, err := format.Source(output.Bytes())
	if err != nil {
		return fmt.Errorf("protobuf: format generated gRPC transport: %w", err)
	}
	path := filepath.Join(root, "internal", "transport", "grpc", "register.gen.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("protobuf: create gRPC transport directory: %w", err)
	}
	return atomicWrite(path, formatted)
}

func hasAnyMethods(services []generatedService) bool {
	for _, service := range services {
		if len(service.Methods) > 0 {
			return true
		}
	}
	return false
}

func snakeCase(value string) string {
	var output []rune
	runes := []rune(value)
	for index, current := range runes {
		if unicode.IsUpper(current) && index > 0 && (unicode.IsLower(runes[index-1]) || (index+1 < len(runes) && unicode.IsLower(runes[index+1]))) {
			output = append(output, '_')
		}
		output = append(output, unicode.ToLower(current))
	}
	return string(output)
}

func lowerFirst(value string) string {
	if value == "" {
		return value
	}
	runes := []rune(value)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}
