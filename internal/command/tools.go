package command

import (
	"context"
	"debug/buildinfo"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	protobufgen "github.com/eyesofblue/jgo/internal/generator/protobuf"
	"github.com/spf13/cobra"
)

func newToolsCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "tools",
		Short: "Install and inspect JGO's locked protobuf tools",
	}
	command.AddCommand(newToolsInstallCommand(stdout), newToolsCheckCommand(stdout))
	return command
}

func newToolsInstallCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install JGO's locked protobuf tools into the active Go environment",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := checkGoVersion(command.Context(), "."); err != nil {
				return fmt.Errorf("tools install: %w", err)
			}
			bin, err := activeGoBin(command.Context())
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(stdout, "install directory: %s\n", bin)
			for _, tool := range protobufgen.LockedTools() {
				if err := installLockedTool(command.Context(), tool); err != nil {
					return err
				}
				status := inspectTool(command.Context(), filepath.Join(bin, tool.Name), tool)
				if status.err != nil {
					return fmt.Errorf("tools install: verify %s: %w", tool.Name, status.err)
				}
				_, _ = fmt.Fprintf(stdout, "installed %-20s %s (built with %s)\n", tool.Name, tool.Version, status.goVersion)
			}
			return nil
		},
	}
}

func newToolsCheckCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Check JGO's locked protobuf tools without changing the environment",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			failures := 0
			for _, tool := range protobufgen.LockedTools() {
				path, err := resolveToolPath(command.Context(), tool.Name)
				if err != nil {
					failures++
					_, _ = fmt.Fprintf(stdout, "FAIL  %-20s not found in PATH\n", tool.Name)
					continue
				}
				status := inspectTool(command.Context(), path, tool)
				if status.err != nil {
					failures++
					_, _ = fmt.Fprintf(stdout, "FAIL  %-20s %v\n", tool.Name, status.err)
					continue
				}
				_, _ = fmt.Fprintf(stdout, "PASS  %-20s %s  %s  built-with=%s\n", tool.Name, tool.Version, path, status.goVersion)
			}
			if failures != 0 {
				return fmt.Errorf("tools check: %d tool(s) failed; run `jgo tools install`", failures)
			}
			return nil
		},
	}
}

type toolStatus struct {
	goVersion string
	err       error
}

func inspectTool(ctx context.Context, path string, tool protobufgen.Tool) toolStatus {
	process := exec.CommandContext(ctx, path, "--version")
	output, err := process.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if strings.Contains(message, "LC_UUID") {
			return toolStatus{err: fmt.Errorf("cannot start %s: missing LC_UUID; rebuild it with Go 1.24 or newer", path)}
		}
		return toolStatus{err: fmt.Errorf("cannot run %s: %w: %s", path, err, message)}
	}
	if !tool.Matches(string(output)) {
		return toolStatus{err: fmt.Errorf("version mismatch: require %s, got %q", tool.Version, strings.TrimSpace(string(output)))}
	}
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return toolStatus{goVersion: "unknown"}
	}
	return toolStatus{goVersion: info.GoVersion}
}

func installLockedTool(ctx context.Context, tool protobufgen.Tool) error {
	process := exec.CommandContext(ctx, "go", "install", tool.Package+"@v"+tool.Version)
	process.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	output, err := process.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tools install: %s %s: %w: %s", tool.Name, tool.Version, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func activeGoBin(ctx context.Context) (string, error) {
	process := exec.CommandContext(ctx, "go", "env", "GOBIN", "GOPATH")
	process.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	output, err := process.Output()
	if err != nil {
		return "", fmt.Errorf("tools install: resolve Go binary directory: %w", err)
	}
	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return "", fmt.Errorf("tools install: unexpected go env output %q", strings.TrimSpace(string(output)))
	}
	if bin := strings.TrimSpace(lines[0]); bin != "" {
		return bin, nil
	}
	gopath := strings.Split(strings.TrimSpace(lines[1]), string(os.PathListSeparator))[0]
	if gopath == "" {
		return "", fmt.Errorf("tools install: GOPATH is empty")
	}
	return filepath.Join(gopath, "bin"), nil
}

func lockedToolDoctorChecks(ctx context.Context) []doctorCheck {
	checks := make([]doctorCheck, 0, len(protobufgen.LockedTools()))
	for _, tool := range protobufgen.LockedTools() {
		path, err := resolveToolPath(ctx, tool.Name)
		if err != nil {
			checks = append(checks, doctorCheck{name: tool.Name + " " + tool.Version, err: fmt.Errorf("not found in PATH")})
			continue
		}
		status := inspectTool(ctx, path, tool)
		name := fmt.Sprintf("%s %s (%s; built with %s)", tool.Name, tool.Version, path, status.goVersion)
		checks = append(checks, doctorCheck{name: name, err: status.err})
	}
	return checks
}

func resolveToolPath(ctx context.Context, name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	if _, err := buildinfo.ReadFile(path); err == nil {
		return path, nil
	}
	goenv, err := exec.LookPath("goenv")
	if err != nil {
		return path, nil
	}
	process := exec.CommandContext(ctx, goenv, "which", name)
	output, err := process.Output()
	if err != nil {
		return path, nil
	}
	candidate := strings.TrimSpace(string(output))
	if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
		return candidate, nil
	}
	return path, nil
}
