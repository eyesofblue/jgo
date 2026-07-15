// Package rpcbinding connects shared generated protobuf modules to JGO services.
package rpcbinding

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"gopkg.in/yaml.v3"
)

const manifestVersion = 2

type Method struct {
	Name     string `json:"name"`
	Request  string `json:"request"`
	Response string `json:"response"`
}

type Binding struct {
	Name        string   `json:"name,omitempty"`
	Module      string   `json:"module"`
	Version     string   `json:"version"`
	Package     string   `json:"package"`
	GoPackage   string   `json:"go_package"`
	Service     string   `json:"service"`
	Handler     string   `json:"handler,omitempty"`
	Methods     []Method `json:"methods"`
	Address     string   `json:"address,omitempty"`
	unsupported string   `json:"-"`
}

type manifest struct {
	Version int       `json:"version"`
	Servers []Binding `json:"servers,omitempty"`
	Clients []Binding `json:"clients,omitempty"`
}

// Snapshot is the public, read-only view used by list and doctor.
type Snapshot struct {
	Servers []Binding
	Clients []Binding
}

// Validate performs a read-only validation of the external RPC binding
// manifest, its protocol modules, and generated client configuration.
func Validate(projectRoot string) error {
	root, err := serviceRoot(projectRoot, false)
	if err != nil {
		return err
	}
	state, err := loadManifest(root)
	if err != nil {
		return err
	}
	if err := validateManifest(state); err != nil {
		return err
	}
	if len(state.Servers) == 0 && len(state.Clients) == 0 {
		stale, staleErr := bindingOutputsStale(root)
		if staleErr != nil {
			return staleErr
		}
		if stale {
			return errors.New("rpc binding: generated bindings exist without manifest entries; run jgo generate")
		}
	}
	if len(state.Servers) > 0 && !regularFile(filepath.Join(root, "internal", "transport", "grpc", "register.go")) {
		return errors.New("rpc binding: server bindings require a grpc or mixed project")
	}
	if len(state.Servers) > 0 && !regularFile(filepath.Join(root, "internal", "transport", "grpc", "external.gen.go")) {
		return errors.New("rpc binding: generated server bindings are missing; run jgo generate")
	}
	if len(state.Clients) > 0 && !regularFile(filepath.Join(root, "internal", "rpcclient", "clients.gen.go")) {
		return errors.New("rpc binding: generated client bindings are missing; run jgo generate")
	}
	canonicalizeManifest(&state)
	if err := validateGeneratedBindingFiles(root, state); err != nil {
		return err
	}
	for _, binding := range state.Servers {
		if err := validateServerHandler(root, binding, true); err != nil {
			return err
		}
	}
	for _, existing := range append(append([]Binding(nil), state.Servers...), state.Clients...) {
		resolved, resolveErr := resolve(root, BindConfig{ModuleSpec: moduleSpec(existing), Package: existing.Package, Service: existing.Service})
		if resolveErr != nil {
			return resolveErr
		}
		resolved.Handler = existing.Handler
		if resolved.GoPackage != existing.GoPackage || !sameMethods(resolved.Methods, existing.Methods) {
			return fmt.Errorf("rpc binding: %s.%s changed since the last bind; run jgo generate", existing.Package, existing.Service)
		}
	}
	return validateClientConfiguration(root, state.Clients)
}

func validateGeneratedBindingFiles(root string, state manifest) error {
	checks := []struct {
		enabled  bool
		path     string
		expected func() ([]byte, error)
		kind     string
	}{
		{enabled: len(state.Servers) > 0, path: filepath.Join(root, "internal", "transport", "grpc", "external.gen.go"), expected: func() ([]byte, error) { return renderServer(root, state.Servers) }, kind: "server"},
		{enabled: len(state.Clients) > 0, path: filepath.Join(root, "internal", "rpcclient", "clients.gen.go"), expected: func() ([]byte, error) { return renderClients(root, state.Clients) }, kind: "client"},
	}
	for _, check := range checks {
		if !check.enabled {
			continue
		}
		expected, err := check.expected()
		if err != nil {
			return err
		}
		actual, err := os.ReadFile(check.path)
		if err != nil {
			return err
		}
		if !bytes.Equal(actual, expected) {
			return fmt.Errorf("rpc binding: generated %s bindings differ from the manifest; run jgo generate", check.kind)
		}
	}
	return nil
}

func validateManifest(state manifest) error {
	servers := make(map[string]struct{}, len(state.Servers))
	handlers := make(map[string]string, len(state.Servers))
	serviceFiles := make(map[string]string)
	for _, binding := range state.Servers {
		if strings.TrimSpace(binding.Module) == "" || strings.TrimSpace(binding.Package) == "" || strings.TrimSpace(binding.Service) == "" {
			return errors.New("rpc binding: server manifest entry is incomplete")
		}
		if !validHandlerName(binding.Handler) {
			return fmt.Errorf("rpc binding: server manifest has invalid handler name %q", binding.Handler)
		}
		key := serverBindingKey(binding)
		if _, exists := servers[key]; exists {
			return fmt.Errorf("rpc binding: duplicate server service %s.%s", binding.Package, binding.Service)
		}
		servers[key] = struct{}{}
		handlerType := serverHandlerType(binding)
		if existing, exists := handlers[handlerType]; exists {
			return fmt.Errorf("rpc binding: handler %s is used by both %s and %s; choose a different --handler-name", handlerType, existing, binding.Package+":"+binding.Service)
		}
		handlers[handlerType] = binding.Package + ":" + binding.Service
		if err := validateBindingMethods(binding); err != nil {
			return err
		}
		owner := binding.Package + ":" + binding.Service
		for _, path := range append([]string{handlerDeclarationPath(binding)}, serverStubPaths(binding)...) {
			if existing, exists := serviceFiles[path]; exists {
				return fmt.Errorf("rpc binding: %s and %s map to the same service file %s", existing, owner, path)
			}
			serviceFiles[path] = owner
		}
	}
	clients := make(map[string]struct{}, len(state.Clients))
	fields := make(map[string]string, len(state.Clients))
	for _, binding := range state.Clients {
		if !validName(binding.Name) || strings.TrimSpace(binding.Module) == "" || strings.TrimSpace(binding.Package) == "" || strings.TrimSpace(binding.Service) == "" {
			return fmt.Errorf("rpc binding: client manifest entry %q is incomplete", binding.Name)
		}
		if _, exists := clients[binding.Name]; exists {
			return fmt.Errorf("rpc binding: duplicate client name %q", binding.Name)
		}
		clients[binding.Name] = struct{}{}
		field := exported(binding.Name)
		if existing, exists := fields[field]; exists {
			return fmt.Errorf("rpc binding: client names %q and %q conflict after Go field generation", existing, binding.Name)
		}
		fields[field] = binding.Name
		if err := validateBindingMethods(binding); err != nil {
			return err
		}
	}
	return nil
}

func validateBindingMethods(binding Binding) error {
	methods := make(map[string]struct{}, len(binding.Methods))
	for _, method := range binding.Methods {
		if !token.IsIdentifier(method.Name) || !ast.IsExported(method.Name) || !token.IsIdentifier(method.Request) || !ast.IsExported(method.Request) || !token.IsIdentifier(method.Response) || !ast.IsExported(method.Response) {
			return fmt.Errorf("rpc binding: %s.%s has invalid method metadata", binding.Package, binding.Service)
		}
		if _, exists := methods[method.Name]; exists {
			return fmt.Errorf("rpc binding: %s.%s contains duplicate method %s", binding.Package, binding.Service, method.Name)
		}
		methods[method.Name] = struct{}{}
	}
	return nil
}

func sameMethods(left, right []Method) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Name != right[index].Name || left[index].Request != right[index].Request || left[index].Response != right[index].Response {
			return false
		}
	}
	return true
}

func serverBindingKey(binding Binding) string {
	return binding.Package + "\x00" + binding.Service
}

func defaultServerHandlerName(service string) string {
	original := strings.TrimSpace(service)
	base := strings.TrimSuffix(original, "Service")
	if strings.HasSuffix(base, "Handler") {
		base = strings.TrimSuffix(base, "Handler")
		if base == "" {
			base = original
		}
	}
	if base == "" {
		base = original
	}
	if strings.HasSuffix(base, "Handler") {
		base = "RPC"
	}
	return base
}

func normalizeHandlerName(value string) string {
	return strings.TrimSuffix(exported(strings.TrimSpace(value)), "Handler")
}

func validHandlerName(value string) bool {
	return token.IsIdentifier(value) && ast.IsExported(value) && !strings.HasSuffix(value, "Handler")
}

func serverHandlerType(binding Binding) string { return binding.Handler + "Handler" }

func serverHandlerConstructor(binding Binding) string { return "New" + serverHandlerType(binding) }

func serverStubPath(binding Binding, method Method) string {
	return snake(serverHandlerType(binding)) + "_" + snake(method.Name) + ".go"
}

func serverStubPaths(binding Binding) []string {
	paths := make([]string, 0, len(binding.Methods))
	for _, method := range binding.Methods {
		paths = append(paths, serverStubPath(binding, method))
	}
	return paths
}

func handlerDeclarationPath(binding Binding) string {
	return snake(serverHandlerType(binding)) + ".go"
}

type serviceMethodDeclaration struct {
	function     *ast.FuncDecl
	file         *ast.File
	shadowsError bool
}

func serviceMethods(directory, receiverName string) (map[string][]serviceMethodDeclaration, error) {
	pkg, err := parseServicePackage(directory)
	if err != nil {
		return nil, err
	}
	shadowsError := packageDeclaresName(pkg, "error")
	methods := make(map[string][]serviceMethodDeclaration)
	for _, file := range pkg.Files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || len(function.Recv.List) != 1 {
				continue
			}
			receiver := unparen(function.Recv.List[0].Type)
			if pointer, ok := receiver.(*ast.StarExpr); ok {
				receiver = unparen(pointer.X)
			}
			if identifier, ok := receiver.(*ast.Ident); ok && identifier.Name == receiverName {
				methods[function.Name.Name] = append(methods[function.Name.Name], serviceMethodDeclaration{function: function, file: file, shadowsError: shadowsError})
			}
		}
	}
	return methods, nil
}

func packageDeclaresName(pkg *ast.Package, name string) bool {
	for _, file := range pkg.Files {
		for _, declaration := range file.Decls {
			switch value := declaration.(type) {
			case *ast.FuncDecl:
				if value.Recv == nil && value.Name.Name == name {
					return true
				}
			case *ast.GenDecl:
				for _, spec := range value.Specs {
					switch item := spec.(type) {
					case *ast.TypeSpec:
						if item.Name.Name == name {
							return true
						}
					case *ast.ValueSpec:
						for _, declared := range item.Names {
							if declared.Name == name {
								return true
							}
						}
					}
				}
			}
		}
	}
	return false
}

func validateServiceMethodSignature(binding Binding, method Method, declaration serviceMethodDeclaration) error {
	function := declaration.function
	typeName := serverHandlerType(binding)
	expected := fmt.Sprintf("func (h *%s) %s(context.Context, *pb.%s) (*pb.%s, error)", typeName, method.Name, method.Request, method.Response)
	if validServiceMethodSignature(binding, method, declaration) {
		return nil
	}
	actual := formatFunctionSignature(function)
	var details []string
	if imports := formatImportPaths(importPaths(declaration.file, binding)); imports != "" {
		details = append(details, "imports: "+imports)
	}
	if declaration.shadowsError {
		details = append(details, `package declaration shadows predeclared "error"`)
	}
	if len(details) > 0 {
		actual += " [" + strings.Join(details, "; ") + "]"
	}
	return fmt.Errorf("rpc binding: user-owned handler method %s.%s has incompatible signature\nexpected: %s\nactual:   %s", typeName, method.Name, expected, actual)
}

func validServiceMethodSignature(binding Binding, method Method, declaration serviceMethodDeclaration) bool {
	function := declaration.function
	if function == nil || function.Type.TypeParams != nil || function.Recv == nil || len(function.Recv.List) != 1 {
		return false
	}
	receiver, ok := unparen(function.Recv.List[0].Type).(*ast.StarExpr)
	if !ok {
		return false
	}
	receiverType, ok := unparen(receiver.X).(*ast.Ident)
	if !ok || receiverType.Name != serverHandlerType(binding) {
		return false
	}
	parameters := fieldTypes(function.Type.Params)
	results := fieldTypes(function.Type.Results)
	if len(parameters) != 2 || len(results) != 2 {
		return false
	}
	imports := importPaths(declaration.file, binding)
	return selectorFromImport(parameters[0], imports, "context", "Context") &&
		pointedSelectorFromImport(parameters[1], imports, binding.Package, method.Request) &&
		pointedSelectorFromImport(results[0], imports, binding.Package, method.Response) &&
		!declaration.shadowsError && imports["error"] == "" &&
		isIdentifier(results[1], "error")
}

func fieldTypes(fields *ast.FieldList) []ast.Expr {
	if fields == nil {
		return nil
	}
	var result []ast.Expr
	for _, field := range fields.List {
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for range count {
			result = append(result, field.Type)
		}
	}
	return result
}

func importPaths(file *ast.File, binding Binding) map[string]string {
	result := make(map[string]string)
	if file == nil {
		return result
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
		} else {
			switch path {
			case "context":
				name = "context"
			case binding.Package:
				name = binding.GoPackage
			}
		}
		if name != "" && name != "." && name != "_" {
			result[name] = path
		}
	}
	return result
}

func formatImportPaths(imports map[string]string) string {
	names := make([]string, 0, len(imports))
	for name := range imports {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s=%q", name, imports[name]))
	}
	return strings.Join(parts, ", ")
}

func selectorFromImport(expression ast.Expr, imports map[string]string, importPath, name string) bool {
	selector, ok := unparen(expression).(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != name {
		return false
	}
	prefix, ok := unparen(selector.X).(*ast.Ident)
	return ok && imports[prefix.Name] == importPath
}

func pointedSelectorFromImport(expression ast.Expr, imports map[string]string, importPath, name string) bool {
	pointer, ok := unparen(expression).(*ast.StarExpr)
	return ok && selectorFromImport(pointer.X, imports, importPath, name)
}

func isIdentifier(expression ast.Expr, name string) bool {
	identifier, ok := unparen(expression).(*ast.Ident)
	return ok && identifier.Name == name
}

func formatFunctionSignature(function *ast.FuncDecl) string {
	if function == nil {
		return "<missing>"
	}
	copy := *function
	copy.Doc = nil
	copy.Body = nil
	var output bytes.Buffer
	if err := printer.Fprint(&output, token.NewFileSet(), &copy); err != nil {
		return "<unprintable>"
	}
	return output.String()
}

func parseServicePackage(directory string) (*ast.Package, error) {
	packages, err := parser.ParseDir(token.NewFileSet(), directory, func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, fmt.Errorf("rpc binding: parse service package: %w", err)
	}
	pkg, exists := packages["service"]
	if !exists || len(packages) != 1 {
		names := make([]string, 0, len(packages))
		for name := range packages {
			names = append(names, name)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("rpc binding: internal/service must contain only package service, found %s", strings.Join(names, ", "))
	}
	return pkg, nil
}

type serviceHandlerDeclarations struct {
	types     map[string]int
	functions map[string][]*ast.FuncDecl
	other     map[string]int
}

func inspectServiceHandlerDeclarations(directory string) (serviceHandlerDeclarations, error) {
	pkg, err := parseServicePackage(directory)
	if err != nil {
		return serviceHandlerDeclarations{}, err
	}
	declarations := serviceHandlerDeclarations{types: make(map[string]int), functions: make(map[string][]*ast.FuncDecl), other: make(map[string]int)}
	for _, file := range pkg.Files {
		for _, declaration := range file.Decls {
			switch value := declaration.(type) {
			case *ast.FuncDecl:
				if value.Recv == nil {
					declarations.functions[value.Name.Name] = append(declarations.functions[value.Name.Name], value)
				}
			case *ast.GenDecl:
				for _, spec := range value.Specs {
					switch item := spec.(type) {
					case *ast.TypeSpec:
						declarations.types[item.Name.Name]++
					case *ast.ValueSpec:
						for _, name := range item.Names {
							declarations.other[name.Name]++
						}
					}
				}
			}
		}
	}
	return declarations, nil
}

func validHandlerConstructor(function *ast.FuncDecl, handlerType string) bool {
	if function == nil || function.Type.TypeParams != nil || function.Type.Params == nil || function.Type.Results == nil || len(function.Type.Params.List) != 1 || len(function.Type.Results.List) != 1 {
		return false
	}
	if len(function.Type.Params.List[0].Names) > 1 || len(function.Type.Results.List[0].Names) > 1 {
		return false
	}
	parameter, ok := unparen(function.Type.Params.List[0].Type).(*ast.StarExpr)
	if !ok {
		return false
	}
	parameterType, ok := unparen(parameter.X).(*ast.Ident)
	if !ok || parameterType.Name != "Service" {
		return false
	}
	result, ok := unparen(function.Type.Results.List[0].Type).(*ast.StarExpr)
	if !ok {
		return false
	}
	resultType, ok := unparen(result.X).(*ast.Ident)
	return ok && resultType.Name == handlerType
}

func unparen(expression ast.Expr) ast.Expr {
	for {
		parenthesized, ok := expression.(*ast.ParenExpr)
		if !ok {
			return expression
		}
		expression = parenthesized.X
	}
}

func validateServerHandler(root string, binding Binding, requireMethods bool) error {
	directory := filepath.Join(root, "internal", "service")
	typeName := serverHandlerType(binding)
	constructorName := serverHandlerConstructor(binding)
	declarations, err := inspectServiceHandlerDeclarations(directory)
	if err != nil {
		return err
	}
	if declarations.types[typeName] != 1 || declarations.other[typeName] != 0 || len(declarations.functions[typeName]) != 0 {
		return fmt.Errorf("rpc binding: user-owned handler type %s is missing or conflicts with another declaration", typeName)
	}
	constructors := declarations.functions[constructorName]
	if len(constructors) != 1 || declarations.types[constructorName] != 0 || declarations.other[constructorName] != 0 || !validHandlerConstructor(constructors[0], typeName) {
		return fmt.Errorf("rpc binding: user-owned handler constructor %s must have signature func(*Service) *%s", constructorName, typeName)
	}
	if !requireMethods {
		return nil
	}
	methods, err := serviceMethods(directory, typeName)
	if err != nil {
		return err
	}
	for _, method := range binding.Methods {
		declarations := methods[method.Name]
		if len(declarations) == 0 {
			return fmt.Errorf("rpc binding: user-owned handler method %s.%s is missing; run jgo generate", typeName, method.Name)
		}
		if len(declarations) != 1 {
			return fmt.Errorf("rpc binding: user-owned handler method %s.%s is declared %d times", typeName, method.Name, len(declarations))
		}
		if err := validateServiceMethodSignature(binding, method, declarations[0]); err != nil {
			return err
		}
	}
	return nil
}

func bindingOutputsStale(root string) (bool, error) {
	paths := []struct {
		path    string
		marker  string
		enabled bool
	}{
		{path: filepath.Join(root, "internal", "transport", "grpc", "external.gen.go"), marker: "Register", enabled: regularFile(filepath.Join(root, "internal", "transport", "grpc", "register.go"))},
		{path: filepath.Join(root, "internal", "rpcclient", "clients.gen.go"), marker: "type Clients struct {", enabled: true},
	}
	for _, candidate := range paths {
		if !candidate.enabled {
			continue
		}
		contents, err := os.ReadFile(candidate.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if strings.Contains(string(contents), candidate.marker) {
			return true, nil
		}
	}
	return false, nil
}

func reconcileEmptyBindings(root string) (bool, error) {
	stale, err := bindingOutputsStale(root)
	if err != nil || !stale {
		return false, err
	}
	paths := []string{"go.mod", "go.sum", filepath.Join("internal", "rpcclient", "clients.gen.go")}
	hasServer := regularFile(filepath.Join(root, "internal", "transport", "grpc", "register.go"))
	if hasServer {
		paths = append(paths, filepath.Join("internal", "transport", "grpc", "external.gen.go"))
	}
	err = mutateFiles(root, paths, func() error {
		if hasServer {
			if err := writeServer(root, nil); err != nil {
				return err
			}
		}
		if err := writeClients(root, nil); err != nil {
			return err
		}
		if err := tidy(root); err != nil {
			return err
		}
		return compile(root)
	})
	return err == nil, err
}

func validateClientConfiguration(root string, bindings []Binding) error {
	if len(bindings) == 0 {
		return nil
	}
	contents, err := os.ReadFile(filepath.Join(root, "configs", "local.yaml"))
	if err != nil {
		return fmt.Errorf("rpc binding: read client configuration: %w", err)
	}
	var data map[string]any
	if err := yaml.Unmarshal(contents, &data); err != nil {
		return fmt.Errorf("rpc binding: decode client configuration: %w", err)
	}
	clients, _ := data["rpc_client"].(map[string]any)
	for _, binding := range bindings {
		value, exists := clients[binding.Name]
		if !exists {
			return fmt.Errorf("rpc binding: rpc_client.%s configuration is missing", binding.Name)
		}
		configuration, _ := value.(map[string]any)
		address, _ := configuration["address"].(string)
		if strings.TrimSpace(address) == "" {
			return fmt.Errorf("rpc binding: rpc_client.%s.address is required", binding.Name)
		}
	}
	return nil
}

func List(projectRoot string) (Snapshot, error) {
	root, err := serviceRoot(projectRoot, false)
	if err != nil {
		return Snapshot{}, err
	}
	state, err := loadManifest(root)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Servers: append([]Binding(nil), state.Servers...), Clients: append([]Binding(nil), state.Clients...)}, nil
}

// Generate reconciles all generated RPC binding files from .jgo/rpc.json.
func Generate(projectRoot string) (bool, error) {
	root, err := serviceRoot(projectRoot, false)
	if err != nil {
		return false, err
	}
	state, err := loadManifest(root)
	if err != nil {
		return false, err
	}
	if len(state.Servers) == 0 && len(state.Clients) == 0 {
		return reconcileEmptyBindings(root)
	}
	if len(state.Servers) > 0 && !regularFile(filepath.Join(root, "internal", "transport", "grpc", "register.go")) {
		return false, errors.New("rpc binding: server bindings require a grpc or mixed project")
	}
	resolvedServers := make([]Binding, 0, len(state.Servers))
	for _, existing := range state.Servers {
		binding, resolveErr := resolve(root, BindConfig{ModuleSpec: moduleSpec(existing), Package: existing.Package, Service: existing.Service})
		if resolveErr != nil {
			return false, resolveErr
		}
		binding.Handler = existing.Handler
		resolvedServers = append(resolvedServers, binding)
	}
	resolvedClients := make([]Binding, 0, len(state.Clients))
	for _, existing := range state.Clients {
		binding, resolveErr := resolve(root, BindConfig{ModuleSpec: moduleSpec(existing), Package: existing.Package, Service: existing.Service})
		if resolveErr != nil {
			return false, resolveErr
		}
		binding.Name, binding.Address = existing.Name, existing.Address
		resolvedClients = append(resolvedClients, binding)
	}
	state.Servers, state.Clients = resolvedServers, resolvedClients
	canonicalizeManifest(&state)
	if err := validateManifest(state); err != nil {
		return false, err
	}
	paths := []string{"go.mod", "go.sum", filepath.Join(".jgo", "rpc.json"), filepath.Join("internal", "rpcclient", "clients.gen.go")}
	if _, statErr := os.Stat(filepath.Join(root, "internal", "transport", "grpc", "register.go")); statErr == nil {
		paths = append(paths, filepath.Join("internal", "transport", "grpc", "external.gen.go"))
	}
	for _, binding := range state.Servers {
		paths = append(paths, filepath.Join("internal", "service", handlerDeclarationPath(binding)))
		for _, method := range binding.Methods {
			paths = append(paths, filepath.Join("internal", "service", serverStubPath(binding, method)))
		}
	}
	err = mutateFiles(root, paths, func() error {
		for _, binding := range append(append([]Binding(nil), state.Servers...), state.Clients...) {
			if err := addRequirement(root, binding.Module, binding.Version); err != nil {
				return err
			}
		}
		if regularFile(filepath.Join(root, "internal", "transport", "grpc", "register.go")) {
			if err := writeServer(root, state.Servers); err != nil {
				return err
			}
		}
		for _, binding := range state.Servers {
			if err := createServerHandler(root, binding); err != nil {
				return err
			}
			if err := createServerStubs(root, binding); err != nil {
				return err
			}
		}
		if err := writeClients(root, state.Clients); err != nil {
			return err
		}
		if err := saveManifest(root, state); err != nil {
			return err
		}
		if hasWorkspaceBindings(state) {
			return nil
		}
		return tidy(root)
	})
	return err == nil, err
}

func moduleSpec(binding Binding) string {
	if binding.Version == "" {
		return binding.Module
	}
	return binding.Module + "@" + binding.Version
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

type BindConfig struct {
	Root        string
	ModuleSpec  string
	Package     string
	Service     string
	Name        string
	Address     string
	HandlerName string
	SkipTidy    bool
}

func BindServer(config BindConfig) (Binding, error) {
	root, err := serviceRoot(config.Root, true)
	if err != nil {
		return Binding{}, err
	}
	binding, err := resolve(root, config)
	if err != nil {
		return Binding{}, err
	}
	state, err := loadManifest(root)
	if err != nil {
		return Binding{}, err
	}
	replaced := false
	requestedHandler := strings.TrimSpace(config.HandlerName)
	normalizedHandler := normalizeHandlerName(requestedHandler)
	if requestedHandler != "" && !validHandlerName(normalizedHandler) {
		return Binding{}, fmt.Errorf("rpc server: invalid handler name %q", config.HandlerName)
	}
	for index, existing := range state.Servers {
		if serverBindingKey(existing) == serverBindingKey(binding) {
			binding.Handler = existing.Handler
			if normalizedHandler != "" && normalizedHandler != existing.Handler {
				return Binding{}, fmt.Errorf("rpc server: %s.%s is already bound to handler %s; handler names cannot be changed", existing.Package, existing.Service, existing.Handler)
			}
			state.Servers[index] = binding
			replaced = true
			break
		}
	}
	if !replaced {
		binding.Handler = normalizedHandler
		if binding.Handler == "" {
			binding.Handler = defaultServerHandlerName(binding.Service)
		}
		if !validHandlerName(binding.Handler) {
			return Binding{}, fmt.Errorf("rpc server: invalid handler name %q", config.HandlerName)
		}
		state.Servers = append(state.Servers, binding)
	}
	canonicalizeManifest(&state)
	if err := validateManifest(state); err != nil {
		return Binding{}, err
	}
	paths := []string{
		"go.mod",
		"go.sum",
		filepath.Join(".jgo", "rpc.json"),
		filepath.Join("internal", "transport", "grpc", "external.gen.go"),
		filepath.Join("internal", "service", handlerDeclarationPath(binding)),
	}
	for _, method := range binding.Methods {
		paths = append(paths, filepath.Join("internal", "service", serverStubPath(binding, method)))
	}
	if err := mutateFiles(root, paths, func() error {
		if err := addRequirement(root, binding.Module, binding.Version); err != nil {
			return err
		}
		if err := writeServer(root, state.Servers); err != nil {
			return err
		}
		if err := createServerHandler(root, binding); err != nil {
			return err
		}
		if err := createServerStubs(root, binding); err != nil {
			return err
		}
		if err := saveManifest(root, state); err != nil {
			return err
		}
		if config.SkipTidy {
			return nil
		}
		if binding.Version != "" {
			if err := tidy(root); err != nil {
				return err
			}
		}
		return compile(root)
	}); err != nil {
		return Binding{}, err
	}
	return binding, nil
}

func BindClient(config BindConfig) (Binding, error) {
	root, err := serviceRoot(config.Root, false)
	if err != nil {
		return Binding{}, err
	}
	binding, err := resolve(root, config)
	if err != nil {
		return Binding{}, err
	}
	binding.Name = strings.TrimSpace(config.Name)
	if binding.Name == "" {
		binding.Name = defaultClientName(binding.Service)
	}
	if !validName(binding.Name) {
		return Binding{}, fmt.Errorf("rpc client: invalid client name %q", binding.Name)
	}
	fieldName := exported(binding.Name)
	if fieldName == "" {
		return Binding{}, fmt.Errorf("rpc client: name %q does not produce a usable Go field", binding.Name)
	}
	binding.Address = strings.TrimSpace(config.Address)
	if binding.Address == "" {
		binding.Address = "127.0.0.1:9090"
	}
	state, err := loadManifest(root)
	if err != nil {
		return Binding{}, err
	}
	replaced := false
	for index, existing := range state.Clients {
		if existing.Name == binding.Name {
			if existing.Service != binding.Service {
				return Binding{}, fmt.Errorf("rpc client: name %q is already bound to %s; client names cannot be repurposed", binding.Name, existing.Service)
			}
			if existing.Package != binding.Package {
				return Binding{}, fmt.Errorf("rpc client: name %q is bound to %s; use a new name for a different protocol package", binding.Name, existing.Package)
			}
			binding.Address = existing.Address
			state.Clients[index] = binding
			replaced = true
			break
		}
		if exported(existing.Name) == fieldName {
			return Binding{}, fmt.Errorf("rpc client: name %q conflicts with %q after Go field generation", binding.Name, existing.Name)
		}
	}
	if !replaced {
		state.Clients = append(state.Clients, binding)
	}
	canonicalizeManifest(&state)
	paths := []string{
		"go.mod",
		"go.sum",
		filepath.Join(".jgo", "rpc.json"),
		filepath.Join("configs", "local.yaml"),
		filepath.Join("internal", "rpcclient", "clients.gen.go"),
	}
	if err := mutateFiles(root, paths, func() error {
		if err := addRequirement(root, binding.Module, binding.Version); err != nil {
			return err
		}
		if err := writeClients(root, state.Clients); err != nil {
			return err
		}
		if !replaced {
			if err := addClientConfig(root, binding); err != nil {
				return err
			}
		}
		if err := saveManifest(root, state); err != nil {
			return err
		}
		if config.SkipTidy {
			return nil
		}
		if binding.Version != "" {
			if err := tidy(root); err != nil {
				return err
			}
		}
		return compile(root)
	}); err != nil {
		return Binding{}, err
	}
	return binding, nil
}

// AddConfig and AddServer/AddClient are intentionally not retained: v0.4
// replaces the pre-release add vocabulary with bind.

func UnbindServer(projectRoot, service string, packagePaths ...string) error {
	root, err := serviceRoot(projectRoot, true)
	if err != nil {
		return err
	}
	state, err := loadManifest(root)
	if err != nil {
		return err
	}
	packagePath := ""
	if len(packagePaths) > 0 {
		packagePath = strings.TrimSpace(packagePaths[0])
	}
	if packagePath == "" {
		matches := 0
		for _, binding := range state.Servers {
			if binding.Service == service {
				matches++
			}
		}
		if matches > 1 {
			return fmt.Errorf("rpc server: service %q is bound from multiple packages; specify --package", service)
		}
	}
	found := false
	workspaceBinding := false
	servers := state.Servers[:0]
	for _, binding := range state.Servers {
		if binding.Service == service && (packagePath == "" || binding.Package == packagePath) {
			found = true
			workspaceBinding = binding.Version == ""
			continue
		}
		servers = append(servers, binding)
	}
	if !found {
		if packagePath != "" {
			return fmt.Errorf("rpc server: service %q is not bound from %s", service, packagePath)
		}
		return fmt.Errorf("rpc server: service %q is not bound", service)
	}
	state.Servers = servers
	canonicalizeManifest(&state)
	paths := []string{"go.mod", "go.sum", filepath.Join(".jgo", "rpc.json"), filepath.Join("internal", "transport", "grpc", "external.gen.go")}
	return mutateFiles(root, paths, func() error {
		if err := writeServer(root, state.Servers); err != nil {
			return err
		}
		if err := saveManifest(root, state); err != nil {
			return err
		}
		if !workspaceBinding {
			if err := tidy(root); err != nil {
				return err
			}
		}
		return compile(root)
	})
}

func UnbindClient(projectRoot, name string) error {
	root, err := serviceRoot(projectRoot, false)
	if err != nil {
		return err
	}
	state, err := loadManifest(root)
	if err != nil {
		return err
	}
	found := false
	workspaceBinding := false
	clients := state.Clients[:0]
	for _, binding := range state.Clients {
		if binding.Name == name {
			found = true
			workspaceBinding = binding.Version == ""
			continue
		}
		clients = append(clients, binding)
	}
	if !found {
		return fmt.Errorf("rpc client: name %q is not bound", name)
	}
	state.Clients = clients
	canonicalizeManifest(&state)
	paths := []string{"go.mod", "go.sum", filepath.Join(".jgo", "rpc.json"), filepath.Join("configs", "local.yaml"), filepath.Join("internal", "rpcclient", "clients.gen.go")}
	return mutateFiles(root, paths, func() error {
		if err := writeClients(root, state.Clients); err != nil {
			return err
		}
		if err := removeClientConfig(root, name); err != nil {
			return err
		}
		if err := saveManifest(root, state); err != nil {
			return err
		}
		if !workspaceBinding && !hasWorkspaceBindings(state) {
			if err := tidy(root); err != nil {
				return err
			}
		}
		return compile(root)
	})
}

func hasWorkspaceBindings(state manifest) bool {
	for _, binding := range state.Servers {
		if binding.Version == "" {
			return true
		}
	}
	for _, binding := range state.Clients {
		if binding.Version == "" {
			return true
		}
	}
	return false
}

type fileState struct {
	contents []byte
	mode     os.FileMode
	exists   bool
}

// mutateFiles makes generator updates transactional, including go mod tidy.
// Every path that the mutation may write must be declared by the caller.
func mutateFiles(root string, paths []string, mutate func() error) error {
	states := make(map[string]fileState, len(paths))
	for _, relative := range paths {
		path := filepath.Join(root, relative)
		info, err := inspectManagedPath(root, relative)
		if err != nil {
			return err
		}
		if info == nil {
			states[relative] = fileState{}
			continue
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		states[relative] = fileState{contents: contents, mode: info.Mode().Perm(), exists: true}
	}
	if err := mutate(); err != nil {
		if rollbackErr := restoreFiles(root, states); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("rpc binding: rollback: %w", rollbackErr))
		}
		return err
	}
	return nil
}

// inspectManagedPath rejects links and special files in a managed path. Checking
// every existing component is important: Lstat on only the final file would
// still allow an intermediate directory link to redirect writes outside root.
func inspectManagedPath(root, relative string) (os.FileInfo, error) {
	clean := filepath.Clean(relative)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("rpc binding: invalid managed path %q", relative)
	}
	parts := strings.Split(clean, string(filepath.Separator))
	current := root
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("rpc binding: inspect managed path %s: %w", clean, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("rpc binding: refuse symlink managed path %s", clean)
		}
		if index < len(parts)-1 {
			if !info.IsDir() {
				return nil, fmt.Errorf("rpc binding: managed path parent %s is not a directory", current)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("rpc binding: managed path %s is not a regular file", clean)
		}
		return info, nil
	}
	return nil, nil
}

func restoreFiles(root string, states map[string]fileState) error {
	var rollbackErrors []error
	for relative, state := range states {
		path := filepath.Join(root, relative)
		if _, err := inspectManagedPath(root, relative); err != nil {
			rollbackErrors = append(rollbackErrors, err)
			continue
		}
		if !state.exists {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				rollbackErrors = append(rollbackErrors, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			rollbackErrors = append(rollbackErrors, err)
			continue
		}
		if err := os.WriteFile(path, state.contents, state.mode); err != nil {
			rollbackErrors = append(rollbackErrors, err)
			continue
		}
		if err := os.Chmod(path, state.mode); err != nil {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	return errors.Join(rollbackErrors...)
}

func tidy(root string) error {
	command := exec.Command("go", "mod", "tidy")
	command.Dir = root
	command.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rpc binding: go mod tidy: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func compile(root string) error {
	// go build validates production packages without executing package init or
	// TestMain code as `go test -run ^$` would do during a generator command.
	command := exec.Command("go", "build", "./...")
	command.Dir = root
	command.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rpc binding: compile project: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func resolve(root string, config BindConfig) (Binding, error) {
	path, version, err := splitModuleSpec(config.ModuleSpec)
	if err != nil {
		return Binding{}, err
	}
	directory, err := moduleDirectory(root, path, version)
	if err != nil {
		return Binding{}, err
	}
	candidates, err := scanServices(directory, path)
	if err != nil {
		return Binding{}, err
	}
	wantedPackage := strings.TrimSpace(config.Package)
	var matches []Binding
	for _, candidate := range candidates {
		if candidate.Service == config.Service && (wantedPackage == "" || candidate.Package == wantedPackage) {
			candidate.Module, candidate.Version = path, version
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return Binding{}, fmt.Errorf("rpc binding: service %q not found in %s", config.Service, config.ModuleSpec)
	}
	if len(matches) > 1 {
		packages := make([]string, 0, len(matches))
		for _, match := range matches {
			packages = append(packages, match.Package)
		}
		sort.Strings(packages)
		return Binding{}, fmt.Errorf("rpc binding: service %q exists in multiple packages: %s; select one with --package", config.Service, strings.Join(packages, ", "))
	}
	if matches[0].unsupported != "" {
		return Binding{}, errors.New(matches[0].unsupported)
	}
	return matches[0], nil
}

func splitModuleSpec(spec string) (string, string, error) {
	spec = strings.TrimSpace(spec)
	index := strings.LastIndex(spec, "@")
	if index < 0 {
		if err := module.CheckPath(spec); err != nil {
			return "", "", fmt.Errorf("rpc binding: invalid module path %q: %w", spec, err)
		}
		return spec, "", nil
	}
	if index == 0 || index == len(spec)-1 {
		return "", "", fmt.Errorf("rpc binding: --module must use <module> or <module>@<version>")
	}
	path, version := spec[:index], spec[index+1:]
	if err := module.Check(path, version); err != nil {
		return "", "", fmt.Errorf("rpc binding: invalid module %q: %w", spec, err)
	}
	return path, version, nil
}

func moduleDirectory(root, path, version string) (string, error) {
	contents, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	file, err := modfile.Parse("go.mod", contents, nil)
	if err != nil {
		return "", err
	}
	if version == "" {
		if directory, ok, err := workspaceModuleDirectory(root, path); err != nil {
			return "", err
		} else if ok {
			return directory, nil
		}
		return "", fmt.Errorf("rpc binding: module %s has no version and is not present in the active go.work", path)
	}
	for _, replacement := range file.Replace {
		matchesVersion := replacement.Old.Version == "" || replacement.Old.Version == version
		if replacement.Old.Path == path && matchesVersion && replacement.New.Version == "" {
			directory := replacement.New.Path
			if !filepath.IsAbs(directory) {
				directory = filepath.Join(root, directory)
			}
			return filepath.Clean(directory), nil
		}
	}
	command := exec.Command("go", "mod", "download", "-json", path+"@"+version)
	command.Dir = root
	command.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("rpc binding: download %s@%s: %w", path, version, err)
	}
	var result struct{ Dir, Error string }
	if err := json.Unmarshal(output, &result); err != nil || result.Dir == "" {
		return "", fmt.Errorf("rpc binding: resolve %s@%s: %s", path, version, result.Error)
	}
	return result.Dir, nil
}

func workspaceModuleDirectory(root, wanted string) (string, bool, error) {
	command := exec.Command("go", "env", "GOWORK")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return "", false, fmt.Errorf("rpc binding: inspect go.work: %w", err)
	}
	workPath := strings.TrimSpace(string(output))
	if workPath == "" || workPath == "off" {
		return "", false, nil
	}
	contents, err := os.ReadFile(workPath)
	if err != nil {
		return "", false, fmt.Errorf("rpc binding: read %s: %w", workPath, err)
	}
	work, err := modfile.ParseWork(workPath, contents, nil)
	if err != nil {
		return "", false, fmt.Errorf("rpc binding: parse %s: %w", workPath, err)
	}
	for _, use := range work.Use {
		directory := use.Path
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(filepath.Dir(workPath), directory)
		}
		modContents, readErr := os.ReadFile(filepath.Join(directory, "go.mod"))
		if readErr != nil {
			return "", false, fmt.Errorf("rpc binding: read workspace module %s: %w", directory, readErr)
		}
		if modfile.ModulePath(modContents) == wanted {
			return filepath.Clean(directory), true, nil
		}
	}
	return "", false, nil
}

func scanServices(root, modulePath string) ([]Binding, error) {
	generated := filepath.Join(root, "gen", "pb")
	var paths []string
	err := filepath.WalkDir(generated, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), "_grpc.pb.go") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("rpc binding: scan generated packages: %w", err)
	}
	sort.Strings(paths)
	var bindings []Binding
	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return nil, err
		}
		relative, _ := filepath.Rel(root, filepath.Dir(path))
		importPath := modulePath + "/" + filepath.ToSlash(relative)
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, specification := range general.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok || !strings.HasSuffix(typeSpec.Name.Name, "Server") || strings.HasPrefix(typeSpec.Name.Name, "Unsafe") || strings.HasPrefix(typeSpec.Name.Name, "Unimplemented") {
					continue
				}
				interfaceType, ok := typeSpec.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}
				binding := Binding{Package: importPath, GoPackage: file.Name.Name, Service: strings.TrimSuffix(typeSpec.Name.Name, "Server")}
				for _, field := range interfaceType.Methods.List {
					if len(field.Names) != 1 || !ast.IsExported(field.Names[0].Name) {
						continue
					}
					function, ok := field.Type.(*ast.FuncType)
					if !ok || function.Params == nil || function.Results == nil || len(function.Params.List) != 2 || len(function.Results.List) != 2 {
						binding.unsupported = fmt.Sprintf("rpc binding: %s.%s is streaming or has an unsupported generated signature", binding.Service, field.Names[0].Name)
						break
					}
					request, rok := pointerName(function.Params.List[1].Type)
					response, sok := pointerName(function.Results.List[0].Type)
					if !rok || !sok {
						binding.unsupported = fmt.Sprintf("rpc binding: %s.%s is streaming or has an unsupported generated signature", binding.Service, field.Names[0].Name)
						break
					}
					binding.Methods = append(binding.Methods, Method{Name: field.Names[0].Name, Request: request, Response: response})
				}
				bindings = append(bindings, binding)
			}
		}
	}
	return bindings, nil
}

func pointerName(expression ast.Expr) (string, bool) {
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

func serviceRoot(root string, requireGRPC bool) (string, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(absolute, "cmd", "server", "main.go")); err != nil {
		return "", fmt.Errorf("rpc binding: not a JGO service project")
	}
	if requireGRPC {
		if _, err := os.Stat(filepath.Join(absolute, "internal", "transport", "grpc", "register.go")); err != nil {
			return "", fmt.Errorf("rpc server: project has no gRPC server; use a grpc or mixed project")
		}
	}
	return absolute, nil
}

func addRequirement(root, path, version string) error {
	if version == "" {
		return nil
	}
	modPath := filepath.Join(root, "go.mod")
	contents, err := os.ReadFile(modPath)
	if err != nil {
		return err
	}
	file, err := modfile.Parse("go.mod", contents, nil)
	if err != nil {
		return err
	}
	if err := file.AddRequire(path, version); err != nil {
		return err
	}
	formatted, err := file.Format()
	if err != nil {
		return err
	}
	return os.WriteFile(modPath, formatted, 0o644)
}

func loadManifest(root string) (manifest, error) {
	path := filepath.Join(root, ".jgo", "rpc.json")
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return manifest{Version: manifestVersion}, nil
	}
	if err != nil {
		return manifest{}, err
	}
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(contents, &header); err != nil {
		return manifest{}, fmt.Errorf("rpc binding: decode manifest: %w", err)
	}
	if header.Version == 1 {
		return manifest{}, errors.New("rpc binding: manifest version 1 is from JGO v0.4 and cannot be upgraded automatically; back up and remove .jgo/rpc.json, rebind each RPC server/client with JGO v0.5, and move server implementations from Service.<generated-name> to <Handler>.<rpc-method>")
	}
	if header.Version != manifestVersion {
		return manifest{}, fmt.Errorf("rpc binding: unsupported manifest version %d", header.Version)
	}
	var state manifest
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return manifest{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values are not allowed")
		}
		return manifest{}, fmt.Errorf("rpc binding: decode manifest: %w", err)
	}
	if err := validateManifest(state); err != nil {
		return manifest{}, err
	}
	return state, nil
}

func saveManifest(root string, state manifest) error {
	canonicalizeManifest(&state)
	contents, _ := json.MarshalIndent(state, "", "  ")
	contents = append(contents, '\n')
	directory := filepath.Join(root, ".jgo")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "rpc.json"), contents, 0o644)
}

func canonicalizeManifest(state *manifest) {
	if state == nil {
		return
	}
	sort.Slice(state.Servers, func(i, j int) bool {
		if state.Servers[i].Package != state.Servers[j].Package {
			return state.Servers[i].Package < state.Servers[j].Package
		}
		return state.Servers[i].Service < state.Servers[j].Service
	})
	sort.Slice(state.Clients, func(i, j int) bool { return state.Clients[i].Name < state.Clients[j].Name })
}

func writeServer(root string, bindings []Binding) error {
	contents, err := renderServer(root, bindings)
	if err != nil {
		return err
	}
	return writeGo(filepath.Join(root, "internal", "transport", "grpc", "external.gen.go"), contents)
}

func renderServer(root string, bindings []Binding) ([]byte, error) {
	modulePath, err := currentModule(root)
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		// The second parameter must retain the concrete service type expected by
		// register.go, so generate it from the current module layout.
		contents := []byte("// Code generated by jgo rpc server bind. DO NOT EDIT.\npackage grpctransport\n\nimport (\n\t\"google.golang.org/grpc\"\n\t\"" + modulePath + "/internal/service\"\n)\n\nfunc registerExternal(grpc.ServiceRegistrar, *service.Service) {}\n")
		return format.Source(contents)
	}
	var output bytes.Buffer
	output.WriteString("// Code generated by jgo rpc server bind. DO NOT EDIT.\npackage grpctransport\n\nimport (\n\t\"context\"\n\tstderrors \"errors\"\n\tjgoerrors \"github.com/eyesofblue/jgo/errors\"\n\t\"go.opentelemetry.io/otel/attribute\"\n\t\"go.opentelemetry.io/otel/trace\"\n\t\"google.golang.org/grpc\"\n\t\"")
	output.WriteString(modulePath + "/internal/service\"\n")
	packages, aliases := bindingPackages(bindings)
	for index, packagePath := range packages {
		output.WriteString(fmt.Sprintf("\tpb%d %q\n", index, packagePath))
	}
	output.WriteString(")\n\n")
	for index, binding := range bindings {
		alias := aliases[binding.Package]
		serverType := fmt.Sprintf("%sExternalServer%d", lowerFirst(binding.Service), index)
		handlerInterface := lowerFirst(binding.Handler) + "Handler"
		output.WriteString(fmt.Sprintf("type %s interface {\n", handlerInterface))
		for _, method := range binding.Methods {
			output.WriteString(fmt.Sprintf("\t%s(context.Context, *%s.%s) (*%s.%s, error)\n", method.Name, alias, method.Request, alias, method.Response))
		}
		output.WriteString("}\n\n")
		output.WriteString(fmt.Sprintf("type %s struct { %s.Unimplemented%sServer; handler %s }\n\n", serverType, alias, binding.Service, handlerInterface))
		for _, method := range binding.Methods {
			output.WriteString(fmt.Sprintf("func (server *%s) %s(ctx context.Context, request *%s.%s) (*%s.%s, error) {\n", serverType, method.Name, alias, method.Request, alias, method.Response))
			output.WriteString(fmt.Sprintf("\tresponse, err := server.handler.%s(ctx, request)\n\tif err == nil { return response, nil }\n\tvar businessError *jgoerrors.Error\n\tif stderrors.As(err, &businessError) {\n\t\ttrace.SpanFromContext(ctx).SetAttributes(attribute.Int64(\"jgo.business_code\", int64(businessError.Code())), attribute.String(\"jgo.business_message\", businessError.Message()))\n\t\treturn &%s.%s{Code: int32(businessError.Code()), Msg: businessError.Message()}, nil\n\t}\n\treturn nil, err\n}\n\n", method.Name, alias, method.Response))
		}
	}
	output.WriteString("func registerExternal(registrar grpc.ServiceRegistrar, application *service.Service) {\n")
	for index, binding := range bindings {
		output.WriteString(fmt.Sprintf("\t%s.Register%sServer(registrar, &%s{handler: service.%s(application)})\n", aliases[binding.Package], binding.Service, fmt.Sprintf("%sExternalServer%d", lowerFirst(binding.Service), index), serverHandlerConstructor(binding)))
	}
	output.WriteString("}\n")
	formatted, err := format.Source(output.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated server bindings: %w\n%s", err, output.Bytes())
	}
	return formatted, nil
}

func createServerHandler(root string, binding Binding) error {
	directory := filepath.Join(root, "internal", "service")
	typeName := serverHandlerType(binding)
	constructorName := serverHandlerConstructor(binding)
	declarations, err := inspectServiceHandlerDeclarations(directory)
	if err != nil {
		return err
	}
	if declarations.types[typeName] != 0 || declarations.other[typeName] != 0 || len(declarations.functions[typeName]) != 0 {
		return validateServerHandler(root, binding, false)
	}
	if declarations.types[constructorName] != 0 || declarations.other[constructorName] != 0 || len(declarations.functions[constructorName]) != 0 {
		return fmt.Errorf("rpc binding: cannot create %s because %s already exists", typeName, constructorName)
	}
	path := filepath.Join(directory, handlerDeclarationPath(binding))
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("rpc binding: refusing to overwrite existing handler file %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	contents := fmt.Sprintf("package service\n\n// %s implements the %s RPC service. This file is user-owned.\ntype %s struct { *Service }\n\nfunc %s(application *Service) *%s {\n\treturn &%s{Service: application}\n}\n", typeName, binding.Service, typeName, serverHandlerConstructor(binding), typeName, typeName)
	return writeGo(path, []byte(contents))
}

func createServerStubs(root string, binding Binding) error {
	receiver := serverHandlerType(binding)
	existing, err := serviceMethods(filepath.Join(root, "internal", "service"), receiver)
	if err != nil {
		return err
	}
	for _, method := range binding.Methods {
		declarations := existing[method.Name]
		if len(declarations) > 1 {
			return fmt.Errorf("rpc binding: user-owned handler method %s.%s is declared %d times", receiver, method.Name, len(declarations))
		}
		if len(declarations) == 1 {
			if err := validateServiceMethodSignature(binding, method, declarations[0]); err != nil {
				return err
			}
			continue
		}
		path := filepath.Join(root, "internal", "service", serverStubPath(binding, method))
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("rpc binding: refusing to overwrite existing service file %s", path)
		} else if !os.IsNotExist(err) {
			return err
		}
		contents := fmt.Sprintf("package service\n\nimport (\n\t\"context\"\n\t\"errors\"\n\tpb %q\n)\n\nfunc (h *%s) %s(context.Context, *pb.%s) (*pb.%s, error) {\n\treturn nil, errors.New(%q)\n}\n", binding.Package, receiver, method.Name, method.Request, method.Response, receiver+"."+method.Name+" is not implemented")
		if err := writeGo(path, []byte(contents)); err != nil {
			return err
		}
		existing[method.Name] = []serviceMethodDeclaration{{}}
	}
	return nil
}

func writeClients(root string, bindings []Binding) error {
	contents, err := renderClients(root, bindings)
	if err != nil {
		return err
	}
	return writeGo(filepath.Join(root, "internal", "rpcclient", "clients.gen.go"), contents)
}

func renderClients(root string, bindings []Binding) ([]byte, error) {
	modulePath, err := currentModule(root)
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		contents := []byte("// Code generated by jgo rpc client bind. DO NOT EDIT.\npackage rpcclient\n\nimport (\n\t\"log/slog\"\n\tclientgrpcx \"github.com/eyesofblue/jgo/client/grpcx\"\n\t\"" + modulePath + "/internal/config\"\n)\n\ntype Clients struct{}\n\nfunc New(map[string]config.RPCClient, *slog.Logger) (*clientgrpcx.Manager, *Clients, error) { return nil, &Clients{}, nil }\n")
		return format.Source(contents)
	}
	var output bytes.Buffer
	output.WriteString("// Code generated by jgo rpc client bind. DO NOT EDIT.\npackage rpcclient\n\nimport (\n\t\"fmt\"\n\t\"log/slog\"\n\n\tclientgrpcx \"github.com/eyesofblue/jgo/client/grpcx\"\n\t\"" + modulePath + "/internal/config\"\n")
	packages, aliases := bindingPackages(bindings)
	for index, packagePath := range packages {
		output.WriteString(fmt.Sprintf("\tpb%d %q\n", index, packagePath))
	}
	output.WriteString(")\n\ntype Clients struct {\n")
	for _, binding := range bindings {
		output.WriteString(fmt.Sprintf("\t%s %s.%sClient\n", exported(binding.Name), aliases[binding.Package], binding.Service))
	}
	output.WriteString("}\n\nfunc New(configuration map[string]config.RPCClient, logger *slog.Logger) (*clientgrpcx.Manager, *Clients, error) {\n\truntimeConfig := make(map[string]clientgrpcx.Config)\n")
	for _, binding := range bindings {
		output.WriteString(fmt.Sprintf("\t%sConfig, ok := configuration[%q]\n\tif !ok { return nil, nil, fmt.Errorf(\"rpc_client.%s is required\") }\n\truntimeConfig[%q] = clientgrpcx.Config{Address: %sConfig.Address, Timeout: %sConfig.Timeout.Duration, TLS: clientgrpcx.TLSConfig{Enabled: %sConfig.TLS.Enabled, ServerName: %sConfig.TLS.ServerName, CAFile: %sConfig.TLS.CAFile}}\n", binding.Name, binding.Name, binding.Name, binding.Name, binding.Name, binding.Name, binding.Name, binding.Name, binding.Name))
	}
	output.WriteString("\tmanager, err := clientgrpcx.New(runtimeConfig, clientgrpcx.WithLogger(logger))\n\tif err != nil { return nil, nil, err }\n\tclients := &Clients{}\n")
	for _, binding := range bindings {
		output.WriteString(fmt.Sprintf("\t%sConn, err := manager.Conn(%q)\n\tif err != nil { _ = manager.Stop(nil); return nil, nil, err }\n\tclients.%s = %s.New%sClient(%sConn)\n", binding.Name, binding.Name, exported(binding.Name), aliases[binding.Package], binding.Service, binding.Name))
	}
	output.WriteString("\treturn manager, clients, nil\n}\n")
	formatted, err := format.Source(output.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated client bindings: %w\n%s", err, output.Bytes())
	}
	return formatted, nil
}

func bindingPackages(bindings []Binding) ([]string, map[string]string) {
	var packages []string
	aliases := make(map[string]string)
	for _, binding := range bindings {
		if _, exists := aliases[binding.Package]; exists {
			continue
		}
		aliases[binding.Package] = fmt.Sprintf("pb%d", len(packages))
		packages = append(packages, binding.Package)
	}
	return packages, aliases
}

func addClientConfig(root string, binding Binding) error {
	path := filepath.Join(root, "configs", "local.yaml")
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var data map[string]any
	if err := yaml.Unmarshal(contents, &data); err != nil {
		return err
	}
	clients, _ := data["rpc_client"].(map[string]any)
	if clients == nil {
		clients = map[string]any{}
	}
	if _, exists := clients[binding.Name]; exists {
		return fmt.Errorf("rpc client: config rpc_client.%s already exists", binding.Name)
	}
	clients[binding.Name] = map[string]any{"address": binding.Address, "timeout": "3s", "readiness": "required", "tls": map[string]any{"enabled": false, "server_name": "", "ca_file": ""}}
	data["rpc_client"] = clients
	updated, err := yaml.Marshal(data)
	if err != nil {
		return err
	}
	return os.WriteFile(path, updated, 0o644)
}

func removeClientConfig(root, name string) error {
	path := filepath.Join(root, "configs", "local.yaml")
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var data map[string]any
	if err := yaml.Unmarshal(contents, &data); err != nil {
		return err
	}
	clients, _ := data["rpc_client"].(map[string]any)
	if clients != nil {
		delete(clients, name)
		data["rpc_client"] = clients
	}
	updated, err := yaml.Marshal(data)
	if err != nil {
		return err
	}
	return os.WriteFile(path, updated, 0o644)
}

func currentModule(root string) (string, error) {
	contents, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	path := modfile.ModulePath(contents)
	if path == "" {
		return "", errors.New("go.mod has no module")
	}
	return path, nil
}
func writeGo(path string, contents []byte) error {
	formatted, err := format.Source(contents)
	if err != nil {
		return fmt.Errorf("format %s: %w\n%s", path, err, contents)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, formatted, 0o644)
}
func validName(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if unicode.IsLetter(r) || r == '_' || (i > 0 && unicode.IsDigit(r)) {
			continue
		}
		return false
	}
	return true
}
func defaultClientName(service string) string {
	value := strings.TrimSuffix(service, "Service")
	return snake(value)
}
func exported(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '_' || r == '-' })
	var b strings.Builder
	for _, part := range parts {
		if part != "" {
			runes := []rune(part)
			runes[0] = unicode.ToUpper(runes[0])
			b.WriteString(string(runes))
		}
	}
	return b.String()
}
func lowerFirst(value string) string {
	if value == "" {
		return value
	}
	runes := []rune(value)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}
func snake(value string) string {
	var out []rune
	runes := []rune(value)
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			out = append(out, '_')
		}
		out = append(out, unicode.ToLower(r))
	}
	return string(out)
}
