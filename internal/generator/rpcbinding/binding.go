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
	"io"
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
	Name        string   `json:"name,omitempty"`
	Module      string   `json:"module"`
	Version     string   `json:"version"`
	Package     string   `json:"package"`
	GoPackage   string   `json:"go_package"`
	Service     string   `json:"service"`
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
	if len(state.Servers) > 0 && !regularFile(filepath.Join(root, "internal", "transport", "grpc", "register.go")) {
		return errors.New("rpc binding: server bindings require a grpc or mixed project")
	}
	if len(state.Servers) > 0 && !regularFile(filepath.Join(root, "internal", "transport", "grpc", "external.gen.go")) {
		return errors.New("rpc binding: generated server bindings are missing; run jgo generate")
	}
	if len(state.Clients) > 0 && !regularFile(filepath.Join(root, "internal", "rpcclient", "clients.gen.go")) {
		return errors.New("rpc binding: generated client bindings are missing; run jgo generate")
	}
	for _, existing := range append(append([]Binding(nil), state.Servers...), state.Clients...) {
		resolved, resolveErr := resolve(root, BindConfig{ModuleSpec: moduleSpec(existing), Package: existing.Package, Service: existing.Service})
		if resolveErr != nil {
			return resolveErr
		}
		if resolved.GoPackage != existing.GoPackage || !sameMethods(resolved.Methods, existing.Methods) {
			return fmt.Errorf("rpc binding: %s.%s changed since the last bind; run jgo generate", existing.Package, existing.Service)
		}
	}
	return validateClientConfiguration(root, state.Clients)
}

func validateManifest(state manifest) error {
	servers := make(map[string]struct{}, len(state.Servers))
	for _, binding := range state.Servers {
		if strings.TrimSpace(binding.Module) == "" || strings.TrimSpace(binding.Package) == "" || strings.TrimSpace(binding.Service) == "" {
			return errors.New("rpc binding: server manifest entry is incomplete")
		}
		if _, exists := servers[binding.Service]; exists {
			return fmt.Errorf("rpc binding: duplicate server service %q", binding.Service)
		}
		servers[binding.Service] = struct{}{}
	}
	if err := validateServerMethodNames(state.Servers); err != nil {
		return err
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
	}
	return nil
}

func validateServerMethodNames(bindings []Binding) error {
	businessNames := make(map[string]string)
	stubPaths := make(map[string]string)
	for _, binding := range bindings {
		for _, method := range binding.Methods {
			owner := binding.Service + "." + method.Name
			businessName := binding.Service + method.Name
			if existing, exists := businessNames[businessName]; exists && existing != owner {
				return fmt.Errorf("rpc binding: %s and %s map to the same business method %s", existing, owner, businessName)
			}
			businessNames[businessName] = owner
			stubPath := snake(businessName)
			if existing, exists := stubPaths[stubPath]; exists && existing != owner {
				return fmt.Errorf("rpc binding: %s and %s map to the same service file %s.go", existing, owner, stubPath)
			}
			stubPaths[stubPath] = owner
		}
	}
	return nil
}

func sameMethods(left, right []Method) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
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
		return false, nil
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
	if err := validateServerMethodNames(state.Servers); err != nil {
		return false, err
	}
	paths := []string{"go.mod", "go.sum", filepath.Join(".jgo", "rpc.json"), filepath.Join("internal", "rpcclient", "clients.gen.go")}
	if _, statErr := os.Stat(filepath.Join(root, "internal", "transport", "grpc", "register.go")); statErr == nil {
		paths = append(paths, filepath.Join("internal", "transport", "grpc", "external.gen.go"))
	}
	for _, binding := range state.Servers {
		for _, method := range binding.Methods {
			paths = append(paths, filepath.Join("internal", "service", snake(binding.Service+method.Name)+".go"))
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
	Root       string
	ModuleSpec string
	Package    string
	Service    string
	Name       string
	Address    string
	SkipTidy   bool
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
	for index, existing := range state.Servers {
		if existing.Service == binding.Service {
			if existing.Package != binding.Package {
				return Binding{}, fmt.Errorf("rpc server: %s is already bound from %s; bind a new protocol package as a distinct Service or unbind it first", binding.Service, existing.Package)
			}
			state.Servers[index] = binding
			replaced = true
			break
		}
	}
	if !replaced {
		state.Servers = append(state.Servers, binding)
	}
	if err := validateServerMethodNames(state.Servers); err != nil {
		return Binding{}, err
	}
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

func UnbindServer(projectRoot, service string) error {
	root, err := serviceRoot(projectRoot, true)
	if err != nil {
		return err
	}
	state, err := loadManifest(root)
	if err != nil {
		return err
	}
	found := false
	workspaceBinding := false
	servers := state.Servers[:0]
	for _, binding := range state.Servers {
		if binding.Service == service {
			found = true
			workspaceBinding = binding.Version == ""
			continue
		}
		servers = append(servers, binding)
	}
	if !found {
		return fmt.Errorf("rpc server: service %q is not bound", service)
	}
	state.Servers = servers
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
	if state.Version != manifestVersion {
		return manifest{}, fmt.Errorf("rpc binding: unsupported manifest version %d", state.Version)
	}
	if err := validateManifest(state); err != nil {
		return manifest{}, err
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
	if len(bindings) == 0 {
		// The second parameter must retain the concrete service type expected by
		// register.go, so generate it from the current module layout.
		modulePath, _ := currentModule(root)
		contents := []byte("// Code generated by jgo rpc server bind. DO NOT EDIT.\npackage grpctransport\n\nimport (\n\t\"google.golang.org/grpc\"\n\t\"" + modulePath + "/internal/service\"\n)\n\nfunc registerExternal(grpc.ServiceRegistrar, *service.Service) {}\n")
		return writeGo(filepath.Join(root, "internal", "transport", "grpc", "external.gen.go"), contents)
	}
	var output bytes.Buffer
	output.WriteString("// Code generated by jgo rpc server bind. DO NOT EDIT.\npackage grpctransport\n\nimport (\n\t\"context\"\n\tstderrors \"errors\"\n\tjgoerrors \"github.com/eyesofblue/jgo/errors\"\n\t\"go.opentelemetry.io/otel/attribute\"\n\t\"go.opentelemetry.io/otel/trace\"\n\t\"google.golang.org/grpc\"\n\t\"")
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
	if len(bindings) == 0 {
		contents := []byte("// Code generated by jgo rpc client bind. DO NOT EDIT.\npackage rpcclient\n\nimport (\n\t\"log/slog\"\n\tclientgrpcx \"github.com/eyesofblue/jgo/client/grpcx\"\n\t\"" + modulePath + "/internal/config\"\n)\n\ntype Clients struct{}\n\nfunc New(map[string]config.RPCClient, *slog.Logger) (*clientgrpcx.Manager, *Clients, error) { return nil, &Clients{}, nil }\n")
		return writeGo(filepath.Join(root, "internal", "rpcclient", "clients.gen.go"), contents)
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
