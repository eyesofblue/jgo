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
	// BufVersion is the newest Buf release compatible with JGO's Go 1.22.0 baseline.
	BufVersion = "1.46.0"
	// ProtocGenGoVersion is the protobuf Go generator version locked by JGO.
	ProtocGenGoVersion = "1.36.7"
	// ProtocGenGoGRPCVersion is the gRPC Go generator version locked by JGO.
	ProtocGenGoGRPCVersion = "1.5.1"
)

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
			return "", fmt.Errorf("protobuf: %s is not installed; install JGO's locked tools with `make tools`", name)
		}
		return string(output), fmt.Errorf("protobuf: %s %s: %w: %s", name, strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func errorsIsExecutableMissing(err error) bool {
	var executableError *exec.Error
	return errors.As(err, &executableError) && executableError.Err == exec.ErrNotFound
}

// Generate lints protobuf contracts, invokes Buf, and updates JGO's gRPC adapters.
func Generate(root string) error {
	return generate(context.Background(), root, execRunner{})
}

// CheckTools verifies the locked Buf and protobuf generator toolchain without
// changing the project or the developer's environment.
func CheckTools(root string) error {
	if root == "" {
		root = "."
	}
	return checkTools(context.Background(), root, execRunner{})
}

func generate(ctx context.Context, root string, commands runner) error {
	if root == "" {
		root = "."
	}
	module, err := readModule(root)
	if err != nil {
		return err
	}
	for _, configuration := range []string{"buf.yaml", "buf.gen.yaml"} {
		if info, statErr := os.Stat(filepath.Join(root, configuration)); statErr != nil || !info.Mode().IsRegular() {
			if statErr == nil {
				statErr = fmt.Errorf("not a regular file")
			}
			return fmt.Errorf("protobuf: inspect %s: %w", configuration, statErr)
		}
	}
	if err := checkTools(ctx, root, commands); err != nil {
		return err
	}
	if _, err := commands.Run(ctx, root, "buf", "lint"); err != nil {
		return err
	}
	if _, err := commands.Run(ctx, root, "buf", "generate"); err != nil {
		return err
	}

	services, err := discoverGeneratedServices(root, module)
	if err != nil {
		return err
	}
	if err := createServiceStubs(root, services); err != nil {
		return err
	}
	return writeTransport(root, module, services)
}

func checkTools(ctx context.Context, root string, commands runner) error {
	tools := []struct {
		name    string
		version string
		prefix  string
	}{
		{name: "buf", version: BufVersion, prefix: ""},
		{name: "protoc-gen-go", version: ProtocGenGoVersion, prefix: "v"},
		{name: "protoc-gen-go-grpc", version: ProtocGenGoGRPCVersion, prefix: ""},
	}
	for _, tool := range tools {
		output, err := commands.Run(ctx, root, tool.name, "--version")
		if err != nil {
			return err
		}
		if !versionMatches(output, tool.version, tool.prefix) {
			return fmt.Errorf("protobuf: %s version mismatch: require %s, got %q; run `make tools`", tool.name, tool.version, output)
		}
	}
	return nil
}

func versionMatches(output, version, prefix string) bool {
	fields := strings.Fields(output)
	if len(fields) == 0 {
		return false
	}
	want := prefix + version
	for _, field := range fields {
		if strings.TrimSpace(field) == want {
			return true
		}
	}
	return false
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
	used := map[string]bool{}
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

func createServiceStubs(root string, services []generatedService) error {
	directory := filepath.Join(root, "internal", "service")
	existing, err := existingServiceMethods(directory)
	if err != nil {
		return err
	}
	type pendingStub struct {
		path     string
		contents []byte
	}
	var pending []pendingStub
	for _, service := range services {
		for _, method := range service.Methods {
			if existing[method.BusinessName] {
				continue
			}
			path := filepath.Join(directory, snakeCase(method.BusinessName)+".go")
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("protobuf: refusing to overwrite existing service file %s", path)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("protobuf: inspect service file %s: %w", path, err)
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
				return fmt.Errorf("protobuf: format service stub %s: %w", method.BusinessName, err)
			}
			pending = append(pending, pendingStub{path: path, contents: formatted})
			existing[method.BusinessName] = true
		}
	}
	for _, stub := range pending {
		if err := atomicWrite(stub.path, stub.contents); err != nil {
			return fmt.Errorf("protobuf: write service stub %s: %w", stub.path, err)
		}
	}
	return nil
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
	output.WriteString("// Code generated by jgo rpc generate. DO NOT EDIT.\n")
	output.WriteString("package grpctransport\n\n")
	output.WriteString("import (\n")
	for _, service := range services {
		if len(service.Methods) > 0 {
			output.WriteString("\t\"context\"\n")
			break
		}
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
			output.WriteString(fmt.Sprintf("\treturn server.application.%s(ctx, request)\n}\n\n", method.BusinessName))
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
