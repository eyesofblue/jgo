package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	callruntime "github.com/eyesofblue/jgo/internal/call"
	openapigen "github.com/eyesofblue/jgo/internal/generator/openapi"
	projectgen "github.com/eyesofblue/jgo/internal/generator/project"
	protobufgen "github.com/eyesofblue/jgo/internal/generator/protobuf"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
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
			// Check the external toolchain before changing any HTTP generated files.
			if project.hasGRPC {
				if err := protobufgen.CheckTools(project.root); err != nil {
					return err
				}
				if err := protobufgen.ValidateResponseContracts(project.root); err != nil {
					return err
				}
			}
			if project.hasWeb {
				if err := openapigen.Generate(project.root); err != nil {
					return err
				}
				_, _ = fmt.Fprintln(stdout, "generated HTTP code")
			}
			if project.hasGRPC {
				result, err := protobufgen.GenerateWithResult(project.root)
				if err != nil {
					return err
				}
				if err := printCreatedServiceStubs(stdout, result); err != nil {
					return err
				}
				if result.ProtocolOnly {
					_, _ = fmt.Fprintln(stdout, "generated shared protobuf and gRPC Go packages; run go test ./...")
				} else {
					_, _ = fmt.Fprintln(stdout, "generated protobuf and gRPC code; run go test ./...")
				}
			}
			return nil
		},
	}
	command.Flags().StringVar(&root, "root", ".", "JGO project root")
	return command
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
		Use:   "run [server arguments]",
		Short: "Run the generated project's cmd/server package",
		Args:  cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			project, err := inspectProject(root)
			if err != nil {
				return err
			}
			if !project.hasServer {
				return fmt.Errorf("run project: proto projects do not have a server process")
			}
			arguments := []string{"run", "./cmd/server"}
			if len(args) > 0 {
				arguments = append(arguments, args...)
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
		if project.hasServer {
			checks = append(checks, doctorCheck{name: "JGO module dependency", err: checkJGOModule(project.root)})
		}
		if project.hasWeb {
			_, err := callruntime.ListHTTP(project.root)
			checks = append(checks, doctorCheck{name: "OpenAPI contract", err: err})
		}
		if project.hasGRPC {
			_, err := callruntime.ListGRPC(ctx, project.root)
			if err == nil {
				err = protobufgen.ValidateResponseContracts(project.root)
			}
			checks = append(checks, doctorCheck{name: "protobuf contract", err: err})
			checks = append(checks, lockedToolDoctorChecks(ctx)...)
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
