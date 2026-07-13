package command

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/eyesofblue/jgo/internal/generator/project"
	"github.com/spf13/cobra"
)

type newProjectOptions struct {
	module      string
	projectType string
	output      string
	jgoVersion  string
	jgoReplace  string
}

func newProjectCommand(stdout io.Writer) *cobra.Command {
	options := newProjectOptions{}
	command := &cobra.Command{
		Use:   "new <project-name>",
		Short: "Create a web, grpc, or mixed JGO project",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
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
			})
			if err != nil {
				return err
			}
			display, err := filepath.Abs(generated)
			if err != nil {
				display = generated
			}
			_, err = fmt.Fprintf(stdout, "created %s project %s at %s\n", options.projectType, args[0], display)
			return err
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.module, "module", "", "Go module path (required)")
	flags.StringVar(&options.projectType, "type", "", "project type: web, grpc, or mixed (required)")
	flags.StringVarP(&options.output, "output", "o", "", "target directory (defaults to project name)")
	flags.StringVar(&options.jgoVersion, "jgo-version", project.DefaultJGOVersion, "JGO module version")
	flags.StringVar(&options.jgoReplace, "jgo-replace", "", "local JGO module path for framework development")
	_ = command.MarkFlagRequired("module")
	_ = command.MarkFlagRequired("type")
	return command
}
