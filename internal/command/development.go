package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	callruntime "github.com/eyesofblue/jgo/internal/call"
	openapigen "github.com/eyesofblue/jgo/internal/generator/openapi"
	projectgen "github.com/eyesofblue/jgo/internal/generator/project"
	protobufgen "github.com/eyesofblue/jgo/internal/generator/protobuf"
	rpcbindinggen "github.com/eyesofblue/jgo/internal/generator/rpcbinding"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
)

var (
	generateHTTP        = openapigen.Generate
	generateProtobuf    = protobufgen.GenerateWithResult
	generateRPCBindings = rpcbindinggen.Generate
)

const jgoModulePath = "github.com/eyesofblue/jgo"

type projectInfo struct {
	root      string
	module    string
	hasWeb    bool
	hasGRPC   bool
	hasServer bool
}

func newGenerateCommand(stdout io.Writer) *cobra.Command {
	var root string
	command := &cobra.Command{
		Use:   "generate",
		Short: "Generate all HTTP and gRPC code for a JGO project",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			project, err := inspectProject(root)
			if err != nil {
				return err
			}
			hasContracts, err := protobufgen.HasContracts(project.root)
			if err != nil {
				return err
			}
			// Check the external toolchain before changing any HTTP generated files.
			if hasContracts {
				if err := protobufgen.CheckTools(project.root); err != nil {
					return err
				}
				if err := protobufgen.ValidateResponseContracts(project.root); err != nil {
					return err
				}
			}
			return runProjectGenerators(project, stdout)
		},
	}
	command.Flags().StringVar(&root, "root", ".", "JGO project root")
	return command
}

func runProjectGenerators(project projectInfo, stdout io.Writer) (err error) {
	snapshot, err := snapshotGeneratorState(project.root)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if restoreErr := snapshot.restore(); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("generate: rollback: %w", restoreErr))
		}
	}()

	var output bytes.Buffer
	if project.hasWeb {
		if err := generateHTTP(project.root); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(&output, "generated HTTP code")
	}
	if project.hasGRPC {
		result, err := generateProtobuf(project.root)
		if err != nil {
			return err
		}
		if err := printCreatedServiceStubs(&output, result); err != nil {
			return err
		}
		if result.Empty {
			_, _ = fmt.Fprintln(&output, "no local protobuf contracts; nothing to generate")
		} else if result.ProtocolOnly {
			_, _ = fmt.Fprintln(&output, "generated shared protobuf and gRPC Go packages; run go test ./...")
		} else {
			_, _ = fmt.Fprintln(&output, "generated protobuf and gRPC code; run go test ./...")
		}
	}
	if project.hasServer {
		reconciled, err := generateRPCBindings(project.root)
		if err != nil {
			return err
		}
		if reconciled {
			_, _ = fmt.Fprintln(&output, "reconciled external RPC bindings")
		}
	}
	if _, err := io.Copy(stdout, &output); err != nil {
		return err
	}
	committed = true
	return nil
}

type generatorFile struct {
	contents []byte
	mode     os.FileMode
}

type generatorSnapshot struct {
	root    string
	paths   []string
	existed map[string]bool
	files   map[string]generatorFile
	links   map[string]string
	dirs    map[string]os.FileMode
}

func snapshotGeneratorState(root string) (generatorSnapshot, error) {
	paths := []string{
		"go.mod", "go.sum", filepath.FromSlash("api/http/openapi.yaml"),
		filepath.FromSlash("gen/http"), filepath.FromSlash("gen/pb"),
		filepath.FromSlash("internal/service"), filepath.FromSlash(".jgo/rpc.json"),
		filepath.FromSlash("internal/transport/http/routes.gen.go"),
		filepath.FromSlash("internal/transport/grpc/register.gen.go"),
		filepath.FromSlash("internal/transport/grpc/external.gen.go"),
		filepath.FromSlash("internal/rpcclient/clients.gen.go"),
	}
	snapshot := generatorSnapshot{root: root, paths: paths, existed: make(map[string]bool), files: make(map[string]generatorFile), links: make(map[string]string), dirs: make(map[string]os.FileMode)}
	for _, relative := range paths {
		path := filepath.Join(root, relative)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return generatorSnapshot{}, fmt.Errorf("generate: snapshot %s: %w", relative, err)
		}
		snapshot.existed[relative] = true
		if info.Mode()&os.ModeSymlink != 0 {
			return generatorSnapshot{}, fmt.Errorf("generate: refuse symlink snapshot path %s", relative)
		}
		if info.Mode().IsRegular() {
			if err := snapshot.capture(relative, path, info); err != nil {
				return generatorSnapshot{}, err
			}
			continue
		}
		if !info.IsDir() {
			return generatorSnapshot{}, fmt.Errorf("generate: snapshot path %s is not a regular file or directory", relative)
		}
		if err := filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				fileRelative, err := filepath.Rel(root, current)
				if err != nil {
					return err
				}
				if !pathWithin(fileRelative, filepath.FromSlash("internal/service")) {
					return fmt.Errorf("generate: refuse symlink snapshot path %s", current)
				}
				target, err := os.Readlink(current)
				if err != nil {
					return err
				}
				snapshot.links[filepath.Clean(fileRelative)] = target
				return nil
			}
			if entry.IsDir() {
				directoryInfo, err := entry.Info()
				if err != nil {
					return err
				}
				fileRelative, err := filepath.Rel(root, current)
				if err != nil {
					return err
				}
				snapshot.dirs[filepath.Clean(fileRelative)] = directoryInfo.Mode().Perm()
				return nil
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			fileInfo, err := entry.Info()
			if err != nil {
				return err
			}
			fileRelative, err := filepath.Rel(root, current)
			if err != nil {
				return err
			}
			return snapshot.capture(fileRelative, current, fileInfo)
		}); err != nil {
			return generatorSnapshot{}, err
		}
		if info.IsDir() {
			snapshot.dirs[filepath.Clean(relative)] = info.Mode().Perm()
		}
	}
	return snapshot, nil
}

func pathWithin(path, directory string) bool {
	relative, err := filepath.Rel(directory, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (snapshot generatorSnapshot) capture(relative, path string, info os.FileInfo) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("generate: snapshot %s: %w", relative, err)
	}
	snapshot.files[filepath.Clean(relative)] = generatorFile{contents: contents, mode: info.Mode().Perm()}
	return nil
}

func (snapshot generatorSnapshot) restore() error {
	var restoreErrors []error
	for _, relative := range snapshot.paths {
		path := filepath.Join(snapshot.root, relative)
		if !snapshot.existed[relative] {
			if err := os.RemoveAll(path); err != nil {
				restoreErrors = append(restoreErrors, err)
			}
			continue
		}
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			restoreErrors = append(restoreErrors, err)
			continue
		}
		if err == nil && info.IsDir() {
			if walkErr := filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil || entry.IsDir() {
					return walkErr
				}
				fileRelative, relErr := filepath.Rel(snapshot.root, current)
				if relErr != nil {
					return relErr
				}
				_, fileExisted := snapshot.files[filepath.Clean(fileRelative)]
				_, linkExisted := snapshot.links[filepath.Clean(fileRelative)]
				if !fileExisted && !linkExisted {
					if removeErr := os.Remove(current); removeErr != nil {
						return removeErr
					}
				}
				return nil
			}); walkErr != nil {
				restoreErrors = append(restoreErrors, walkErr)
			}
		} else if err == nil {
			if _, existed := snapshot.files[filepath.Clean(relative)]; !existed {
				if removeErr := os.Remove(path); removeErr != nil {
					restoreErrors = append(restoreErrors, removeErr)
				}
			}
		}
	}
	var extraDirectories []string
	for _, relative := range snapshot.paths {
		path := filepath.Join(snapshot.root, relative)
		if err := filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
			if os.IsNotExist(walkErr) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() {
				return nil
			}
			fileRelative, err := filepath.Rel(snapshot.root, current)
			if err != nil {
				return err
			}
			fileRelative = filepath.Clean(fileRelative)
			if _, existed := snapshot.dirs[fileRelative]; !existed {
				extraDirectories = append(extraDirectories, fileRelative)
			}
			return nil
		}); err != nil {
			restoreErrors = append(restoreErrors, err)
		}
	}
	sort.Slice(extraDirectories, func(i, j int) bool {
		return strings.Count(extraDirectories[i], string(filepath.Separator)) > strings.Count(extraDirectories[j], string(filepath.Separator))
	})
	for _, relative := range extraDirectories {
		if err := os.Remove(filepath.Join(snapshot.root, relative)); err != nil && !os.IsNotExist(err) {
			restoreErrors = append(restoreErrors, err)
		}
	}
	originalDirectories := make([]string, 0, len(snapshot.dirs))
	for relative := range snapshot.dirs {
		originalDirectories = append(originalDirectories, relative)
	}
	sort.Slice(originalDirectories, func(i, j int) bool {
		return strings.Count(originalDirectories[i], string(filepath.Separator)) < strings.Count(originalDirectories[j], string(filepath.Separator))
	})
	for _, relative := range originalDirectories {
		if err := os.MkdirAll(filepath.Join(snapshot.root, relative), 0o755); err != nil {
			restoreErrors = append(restoreErrors, err)
		}
	}
	// Restore permissions from children to parents only after the full tree
	// exists, so a read-only parent cannot prevent recreation of its children.
	for index := len(originalDirectories) - 1; index >= 0; index-- {
		relative := originalDirectories[index]
		if err := os.Chmod(filepath.Join(snapshot.root, relative), snapshot.dirs[relative]); err != nil {
			restoreErrors = append(restoreErrors, err)
		}
	}
	for relative, file := range snapshot.files {
		path := filepath.Join(snapshot.root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			restoreErrors = append(restoreErrors, err)
			continue
		}
		if err := os.WriteFile(path, file.contents, file.mode); err != nil {
			restoreErrors = append(restoreErrors, err)
			continue
		}
		if err := os.Chmod(path, file.mode); err != nil {
			restoreErrors = append(restoreErrors, err)
		}
	}
	for relative, target := range snapshot.links {
		path := filepath.Join(snapshot.root, relative)
		if current, err := os.Readlink(path); err == nil && current == target {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			restoreErrors = append(restoreErrors, err)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			restoreErrors = append(restoreErrors, err)
			continue
		}
		if err := os.Symlink(target, path); err != nil {
			restoreErrors = append(restoreErrors, err)
		}
	}
	return errors.Join(restoreErrors...)
}

func newDoctorCommand(stdout io.Writer) *cobra.Command {
	var root string
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Check the local JGO project and required development tools",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			return runDoctor(command.Context(), stdout, root)
		},
	}
	command.Flags().StringVar(&root, "root", ".", "JGO project root")
	return command
}

func newRunCommand(stdout, stderr io.Writer) *cobra.Command {
	var root string
	command := &cobra.Command{
		Use:                "run [server arguments]",
		Short:              "Run the generated project's cmd/server package",
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		RunE: func(command *cobra.Command, args []string) error {
			selectedRoot, forwarded, help, err := parseRunArguments(root, args)
			if err != nil {
				return err
			}
			if help {
				return command.Help()
			}
			project, err := inspectProject(selectedRoot)
			if err != nil {
				return err
			}
			if !project.hasServer {
				return fmt.Errorf("run project: proto projects do not have a server process")
			}
			arguments := []string{"run", "./cmd/server"}
			if len(forwarded) > 0 {
				arguments = append(arguments, forwarded...)
			}
			process := exec.CommandContext(command.Context(), "go", arguments...)
			process.Dir = project.root
			process.Env = append(os.Environ(), "GOTOOLCHAIN=local")
			process.Stdin = os.Stdin
			process.Stdout = stdout
			process.Stderr = stderr
			if err := process.Run(); err != nil {
				return fmt.Errorf("run project: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&root, "root", ".", "JGO project root")
	return command
}

func parseRunArguments(defaultRoot string, arguments []string) (string, []string, bool, error) {
	root := defaultRoot
	var forwarded []string
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "-h" || argument == "--help":
			return root, nil, true, nil
		case argument == "--":
			forwarded = append(forwarded, arguments[index+1:]...)
			return root, forwarded, false, nil
		case argument == "--root":
			if index+1 >= len(arguments) {
				return "", nil, false, fmt.Errorf("run: --root requires a value")
			}
			index++
			root = arguments[index]
		case strings.HasPrefix(argument, "--root="):
			root = strings.TrimPrefix(argument, "--root=")
		default:
			forwarded = append(forwarded, argument)
		}
	}
	return root, forwarded, false, nil
}

func newBuildCommand(stdout, stderr io.Writer) *cobra.Command {
	options := struct {
		root   string
		output string
	}{}
	command := &cobra.Command{
		Use:   "build",
		Short: "Build the generated project's server binary",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			project, err := inspectProject(options.root)
			if err != nil {
				return err
			}
			if !project.hasServer {
				return fmt.Errorf("build project: proto projects do not have a server binary; use `go build ./...` to verify generated packages")
			}
			output := strings.TrimSpace(options.output)
			if output == "" {
				output = filepath.Join(project.root, "bin", filepath.Base(project.root))
			} else if !filepath.IsAbs(output) {
				output = filepath.Join(project.root, output)
			}
			output, err = filepath.Abs(output)
			if err != nil {
				return fmt.Errorf("build project: resolve output: %w", err)
			}
			if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
				return fmt.Errorf("build project: create output directory: %w", err)
			}
			process := exec.CommandContext(command.Context(), "go", "build", "-trimpath", "-o", output, "./cmd/server")
			process.Dir = project.root
			process.Env = append(os.Environ(), "GOTOOLCHAIN=local")
			process.Stdout = stdout
			process.Stderr = stderr
			if err := process.Run(); err != nil {
				return fmt.Errorf("build project: %w", err)
			}
			_, err = fmt.Fprintf(stdout, "built %s\n", output)
			return err
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.root, "root", ".", "JGO project root")
	flags.StringVarP(&options.output, "output", "o", "", "binary output path (default bin/<project>)")
	return command
}

type doctorCheck struct {
	name string
	err  error
}

func runDoctor(ctx context.Context, stdout io.Writer, root string) error {
	var checks []doctorCheck
	project, projectErr := inspectProject(root)
	checks = append(checks, doctorCheck{name: "project", err: projectErr})
	checks = append(checks, doctorCheck{name: "Go >= " + projectgen.MinimumGoVersion, err: checkGoVersion(ctx, root)})
	if projectErr == nil {
		printWorkspaceInfo(ctx, stdout, project.root)
		if project.hasServer {
			checks = append(checks, doctorCheck{name: "JGO module dependency", err: checkJGOModule(project.root)})
		}
		if project.hasWeb {
			_, err := callruntime.ListHTTP(project.root)
			checks = append(checks, doctorCheck{name: "OpenAPI contract", err: err})
		}
		if project.hasGRPC {
			hasContracts, err := protobufgen.HasContracts(project.root)
			if err == nil && hasContracts {
				_, err = callruntime.ListGRPC(ctx, project.root)
				if err == nil {
					err = protobufgen.ValidateResponseContracts(project.root)
				}
				checks = append(checks, doctorCheck{name: "protobuf contract", err: err})
				checks = append(checks, lockedToolDoctorChecks(ctx)...)
			} else if err != nil {
				checks = append(checks, doctorCheck{name: "local contract discovery", err: err})
			} else {
				_, _ = fmt.Fprintln(stdout, "INFO  local contracts: none; Buf tools are not required")
			}
		}
		if project.hasServer {
			checks = append(checks, doctorCheck{name: "external RPC bindings", err: rpcbindinggen.Validate(project.root)})
		}
	}
	failures := 0
	for _, check := range checks {
		if check.err == nil {
			_, _ = fmt.Fprintf(stdout, "PASS  %s\n", check.name)
			continue
		}
		failures++
		_, _ = fmt.Fprintf(stdout, "FAIL  %s: %v\n", check.name, check.err)
	}
	if failures != 0 {
		return fmt.Errorf("doctor: %d check(s) failed", failures)
	}
	return nil
}

func printWorkspaceInfo(ctx context.Context, stdout io.Writer, root string) {
	command := exec.CommandContext(ctx, "go", "env", "GOWORK")
	command.Dir = root
	command.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	output, err := command.Output()
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "INFO  workspace: unavailable (%v)\n", err)
		return
	}
	workspace := strings.TrimSpace(string(output))
	if workspace == "" || workspace == "off" {
		_, _ = fmt.Fprintln(stdout, "INFO  workspace: off")
	} else {
		_, _ = fmt.Fprintf(stdout, "INFO  workspace: %s\n", workspace)
	}
	contents, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return
	}
	file, err := modfile.Parse("go.mod", contents, nil)
	if err != nil {
		return
	}
	for _, replacement := range file.Replace {
		if replacement.New.Version == "" {
			_, _ = fmt.Fprintf(stdout, "INFO  replace: %s => %s\n", replacement.Old.Path, replacement.New.Path)
		}
	}
}

func inspectProject(root string) (projectInfo, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return projectInfo{}, fmt.Errorf("project: resolve root: %w", err)
	}
	contents, err := os.ReadFile(filepath.Join(absolute, "go.mod"))
	if err != nil {
		return projectInfo{}, fmt.Errorf("project: read go.mod: %w", err)
	}
	module := modfile.ModulePath(contents)
	if module == "" {
		return projectInfo{}, fmt.Errorf("project: go.mod has no module directive")
	}
	project := projectInfo{root: absolute, module: module}
	project.hasWeb = regularFile(filepath.Join(absolute, "api", "http", "openapi.yaml"))
	project.hasGRPC = regularFile(filepath.Join(absolute, "buf.yaml")) && regularFile(filepath.Join(absolute, "buf.gen.yaml"))
	if !project.hasWeb && !project.hasGRPC {
		return projectInfo{}, fmt.Errorf("project: no OpenAPI or protobuf contract found under %s", absolute)
	}
	project.hasServer = regularFile(filepath.Join(absolute, "cmd", "server", "main.go"))
	if !project.hasServer && project.hasWeb {
		return projectInfo{}, fmt.Errorf("project: missing cmd/server/main.go")
	}
	if !project.hasServer && !project.hasGRPC {
		return projectInfo{}, fmt.Errorf("project: missing cmd/server/main.go")
	}
	return project, nil
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func checkGoVersion(ctx context.Context, root string) error {
	command := exec.CommandContext(ctx, "go", "env", "GOVERSION")
	command.Dir = root
	command.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("run go env GOVERSION: %w", err)
	}
	version := strings.TrimSpace(string(output))
	major, minor, patch, err := parseGoVersion(version)
	if err != nil {
		return err
	}
	if major < 1 || (major == 1 && minor < 24) {
		return fmt.Errorf("require Go %s or newer, got %s", projectgen.MinimumGoVersion, version)
	}
	_ = patch
	return nil
}

func parseGoVersion(version string) (int, int, int, error) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "go")
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return 0, 0, 0, fmt.Errorf("unrecognized Go version %q", version)
	}
	values := []int{0, 0, 0}
	for index := 0; index < len(parts) && index < 3; index++ {
		digits := strings.TrimLeftFunc(parts[index], func(character rune) bool { return character < '0' || character > '9' })
		for position, character := range digits {
			if character < '0' || character > '9' {
				digits = digits[:position]
				break
			}
		}
		if digits == "" {
			return 0, 0, 0, fmt.Errorf("unrecognized Go version %q", version)
		}
		value, err := strconv.Atoi(digits)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("unrecognized Go version %q", version)
		}
		values[index] = value
	}
	return values[0], values[1], values[2], nil
}

func checkJGOModule(root string) error {
	contents, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return err
	}
	file, err := modfile.Parse("go.mod", contents, nil)
	if err != nil {
		return err
	}
	if file.Module != nil && file.Module.Mod.Path == jgoModulePath {
		return nil
	}
	for _, required := range file.Require {
		if required.Mod.Path == jgoModulePath {
			return nil
		}
	}
	return fmt.Errorf("go.mod does not require %s", jgoModulePath)
}
