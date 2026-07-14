package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/eyesofblue/jgo/internal/generator/project"
	"github.com/spf13/cobra"
)

type newProjectOptions struct {
	module      string
	projectType string
	output      string
	jgoVersion  string
	jgoReplace  string
	goVersion   string
	skipTidy    bool
}

func newProjectCommand(stdout io.Writer) *cobra.Command {
	options := newProjectOptions{}
	command := &cobra.Command{
		Use:   "new <project-name>",
		Short: "Create a web, grpc, or mixed JGO project",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			goVersion := options.goVersion
			if goVersion == "" {
				var err error
				goVersion, err = activeGoVersion(command.Context())
				if err != nil {
					return err
				}
			} else {
				var err error
				goVersion, err = project.NormalizeGoVersion(goVersion)
				if err != nil {
					return err
				}
			}
			target := options.output
			if target == "" {
				target = args[0]
			}
			generated, err := project.Generate(project.Config{
				Name:       args[0],
				Module:     options.module,
				Type:       project.Type(options.projectType),
				TargetDir:  target,
				JGOVersion: options.jgoVersion,
				JGOReplace: options.jgoReplace,
				GoVersion:  goVersion,
				SkipTidy:   options.skipTidy,
			})
			if err != nil {
				return err
			}
			display, err := filepath.Abs(generated)
			if err != nil {
				display = generated
			}
			_, err = fmt.Fprintf(stdout, "created %s project %s at %s\nGo: %s\n", options.projectType, args[0], display, goVersion)
			return err
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.module, "module", "", "Go module path (required)")
	flags.StringVar(&options.projectType, "type", "", "project type: web, grpc, or mixed (required)")
	flags.StringVarP(&options.output, "output", "o", "", "target directory (defaults to project name)")
	flags.StringVar(&options.jgoVersion, "jgo-version", project.DefaultJGOVersion, "JGO module version")
	flags.StringVar(&options.jgoReplace, "jgo-replace", "", "local JGO module path for framework development")
	flags.StringVar(&options.goVersion, "go-version", "", "Go version for the generated module (defaults to the active Go version)")
	flags.BoolVar(&options.skipTidy, "skip-tidy", false, "skip go mod tidy and go.sum generation")
	_ = command.MarkFlagRequired("module")
	_ = command.MarkFlagRequired("type")
	return command
}

func activeGoVersion(ctx context.Context) (string, error) {
	command := exec.CommandContext(ctx, "go", "env", "GOVERSION")
	command.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("resolve active Go version with GOTOOLCHAIN=local: %w", err)
	}
	version, err := project.NormalizeGoVersion(strings.TrimSpace(string(output)))
	if err != nil {
		return "", err
	}
	return version, nil
}
