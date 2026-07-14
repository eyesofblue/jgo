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
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"gopkg.in/yaml.v3"
)

const manifestVersion = 1

type Method struct {
	Name     string `json:"name"`
	Request  string `json:"request"`
	Response string `json:"response"`
}

type Binding struct {
	Name      string   `json:"name,omitempty"`
	Module    string   `json:"module"`
	Version   string   `json:"version"`
	Package   string   `json:"package"`
	GoPackage string   `json:"go_package"`
	Service   string   `json:"service"`
	Methods   []Method `json:"methods"`
	Address   string   `json:"address,omitempty"`
}

type manifest struct {
	Version int       `json:"version"`
	Servers []Binding `json:"servers,omitempty"`
	Clients []Binding `json:"clients,omitempty"`
}

type AddConfig struct {
	Root       string
	ModuleSpec string
	Package    string
	Service    string
	Name       string
	Address    string
	SkipTidy   bool
}

func AddServer(config AddConfig) (Binding, error) {
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
	for _, existing := range state.Servers {
		if existing.Package == binding.Package && existing.Service == binding.Service {
			return Binding{}, fmt.Errorf("rpc server: %s.%s is already added", binding.Package, binding.Service)
		}
		if existing.Service == binding.Service {
			return Binding{}, fmt.Errorf("rpc server: service %s is already implemented from %s; one application cannot implement two protocol versions with the same generated business method names", binding.Service, existing.Package)
		}
	}
	state.Servers = append(state.Servers, binding)
	paths := []string{
		"go.mod",
		"go.sum",
		filepath.Join(".jgo", "rpc.json"),
		filepath.Join("internal", "transport", "grpc", "external.gen.go"),
	}
	for _, method := range binding.Methods {
		paths = append(paths, filepath.Join("internal", "service", snake(binding.Service+method.Name)+".go"))
	}
	if err := mutateFiles(root, paths, func() error {
		if err := addRequirement(root, binding.Module, binding.Version); err != nil {
			return err
		}
		if err := writeServer(root, state.Servers); err != nil {
			return err
		}
		if err := createServerStubs(root, binding); err != nil {
			return err
		}
		if err := saveManifest(root, state); err != nil {
			return err
		}
		if !config.SkipTidy {
			return tidy(root)
		}
		return nil
	}); err != nil {
		return Binding{}, err
	}
	return binding, nil
}

func AddClient(config AddConfig) (Binding, error) {
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
	for _, existing := range state.Clients {
		if existing.Name == binding.Name {
			return Binding{}, fmt.Errorf("rpc client: name %q is already added", binding.Name)
		}
		if exported(existing.Name) == fieldName {
			return Binding{}, fmt.Errorf("rpc client: name %q conflicts with %q after Go field generation", binding.Name, existing.Name)
		}
	}
	state.Clients = append(state.Clients, binding)
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
		if err := addClientConfig(root, binding); err != nil {
			return err
		}
		if err := saveManifest(root, state); err != nil {
			return err
		}
		if !config.SkipTidy {
			return tidy(root)
		}
		return nil
	}); err != nil {
		return Binding{}, err
	}
	return binding, nil
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
		contents, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			states[relative] = fileState{}
			continue
		}
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
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

func restoreFiles(root string, states map[string]fileState) error {
	var rollbackErrors []error
	for relative, state := range states {
		path := filepath.Join(root, relative)
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
		}
	}
	return errors.Join(rollbackErrors...)
}

func tidy(root string) error {
	command := exec.Command("go", "mod", "tidy")
	command.Dir = root
	command.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rpc binding: go mod tidy: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func resolve(root string, config AddConfig) (Binding, error) {
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
	return matches[0], nil
}

func splitModuleSpec(spec string) (string, string, error) {
	index := strings.LastIndex(strings.TrimSpace(spec), "@")
	if index <= 0 || index == len(spec)-1 {
		return "", "", fmt.Errorf("rpc binding: --module must use <module>@<version>")
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
	for _, replacement := range file.Replace {
		if replacement.Old.Path == path && replacement.New.Version == "" {
			directory := replacement.New.Path
			if !filepath.IsAbs(directory) {
				directory = filepath.Join(root, directory)
			}
			return filepath.Clean(directory), nil
		}
	}
	command := exec.Command("go", "mod", "download", "-json", path+"@"+version)
	command.Dir = root
	command.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off")
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
						return nil, fmt.Errorf("rpc binding: %s.%s is streaming or has an unsupported generated signature", binding.Service, field.Names[0].Name)
					}
					request, rok := pointerName(function.Params.List[1].Type)
					response, sok := pointerName(function.Results.List[0].Type)
					if !rok || !sok {
						return nil, fmt.Errorf("rpc binding: %s.%s is streaming or has an unsupported generated signature", binding.Service, field.Names[0].Name)
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
	var state manifest
	if err := json.Unmarshal(contents, &state); err != nil {
		return manifest{}, err
	}
	if state.Version != manifestVersion {
		return manifest{}, fmt.Errorf("rpc binding: unsupported manifest version %d", state.Version)
	}
	return state, nil
}

func saveManifest(root string, state manifest) error {
	sort.Slice(state.Servers, func(i, j int) bool {
		return state.Servers[i].Package+state.Servers[i].Service < state.Servers[j].Package+state.Servers[j].Service
	})
	sort.Slice(state.Clients, func(i, j int) bool { return state.Clients[i].Name < state.Clients[j].Name })
	contents, _ := json.MarshalIndent(state, "", "  ")
	contents = append(contents, '\n')
	directory := filepath.Join(root, ".jgo")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "rpc.json"), contents, 0o644)
}

func writeServer(root string, bindings []Binding) error {
	var output bytes.Buffer
	output.WriteString("// Code generated by jgo rpc server add. DO NOT EDIT.\npackage grpctransport\n\nimport (\n\t\"context\"\n\tstderrors \"errors\"\n\tjgoerrors \"github.com/eyesofblue/jgo/errors\"\n\t\"go.opentelemetry.io/otel/attribute\"\n\t\"go.opentelemetry.io/otel/trace\"\n\t\"google.golang.org/grpc\"\n\t\"")
	modulePath, _ := currentModule(root)
	output.WriteString(modulePath + "/internal/service\"\n")
	packages, aliases := bindingPackages(bindings)
	for index, packagePath := range packages {
		output.WriteString(fmt.Sprintf("\tpb%d %q\n", index, packagePath))
	}
	output.WriteString(")\n\n")
	for index, binding := range bindings {
		alias := aliases[binding.Package]
		serverType := fmt.Sprintf("%sExternalServer%d", lowerFirst(binding.Service), index)
		output.WriteString(fmt.Sprintf("type %s struct { %s.Unimplemented%sServer; application *service.Service }\n\n", serverType, alias, binding.Service))
		for _, method := range binding.Methods {
			business := binding.Service + method.Name
			output.WriteString(fmt.Sprintf("func (server *%s) %s(ctx context.Context, request *%s.%s) (*%s.%s, error) {\n", serverType, method.Name, alias, method.Request, alias, method.Response))
			output.WriteString(fmt.Sprintf("\tresponse, err := server.application.%s(ctx, request)\n\tif err == nil { return response, nil }\n\tvar businessError *jgoerrors.Error\n\tif stderrors.As(err, &businessError) {\n\t\ttrace.SpanFromContext(ctx).SetAttributes(attribute.Int64(\"jgo.business_code\", int64(businessError.Code())), attribute.String(\"jgo.business_message\", businessError.Message()))\n\t\treturn &%s.%s{Code: int32(businessError.Code()), Msg: businessError.Message()}, nil\n\t}\n\treturn nil, err\n}\n\n", business, alias, method.Response))
		}
	}
	output.WriteString("func registerExternal(registrar grpc.ServiceRegistrar, application *service.Service) {\n")
	for index, binding := range bindings {
		output.WriteString(fmt.Sprintf("\t%s.Register%sServer(registrar, &%s{application: application})\n", aliases[binding.Package], binding.Service, fmt.Sprintf("%sExternalServer%d", lowerFirst(binding.Service), index)))
	}
	output.WriteString("}\n")
	return writeGo(filepath.Join(root, "internal", "transport", "grpc", "external.gen.go"), output.Bytes())
}

func createServerStubs(root string, binding Binding) error {
	for _, method := range binding.Methods {
		name := binding.Service + method.Name
		path := filepath.Join(root, "internal", "service", snake(name)+".go")
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		contents := fmt.Sprintf("package service\n\nimport (\n\t\"context\"\n\t\"errors\"\n\tpb %q\n)\n\nfunc (s *Service) %s(context.Context, *pb.%s) (*pb.%s, error) {\n\treturn nil, errors.New(%q)\n}\n", binding.Package, name, method.Request, method.Response, name+" is not implemented")
		if err := writeGo(path, []byte(contents)); err != nil {
			return err
		}
	}
	return nil
}

func writeClients(root string, bindings []Binding) error {
	modulePath, _ := currentModule(root)
	var output bytes.Buffer
	output.WriteString("// Code generated by jgo rpc client add. DO NOT EDIT.\npackage rpcclient\n\nimport (\n\t\"fmt\"\n\t\"log/slog\"\n\n\tclientgrpcx \"github.com/eyesofblue/jgo/client/grpcx\"\n\t\"" + modulePath + "/internal/config\"\n")
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
	return writeGo(filepath.Join(root, "internal", "rpcclient", "clients.gen.go"), output.Bytes())
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
	clients[binding.Name] = map[string]any{"address": binding.Address, "timeout": "3s", "tls": map[string]any{"enabled": false, "server_name": "", "ca_file": ""}}
	data["rpc_client"] = clients
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
