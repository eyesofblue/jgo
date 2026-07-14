// Package command implements the JGO command-line interface.
package command

import (
	"io"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version can be set with -ldflags for release archives.
var Version = ""

// NewRootCommand constructs a testable JGO root command.
func NewRootCommand(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "jgo",
		Short:         "Build and maintain JGO services",
		Version:       currentVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(newProjectCommand(stdout))
	root.AddCommand(newAPICommand(stdout))
	root.AddCommand(newPBCommand(stdout))
	root.AddCommand(newRPCCommand(stdout))
	root.AddCommand(newToolsCommand(stdout))
	root.AddCommand(newCallCommand(stdout))
	root.AddCommand(newListCommand(stdout))
	root.AddCommand(newGenerateCommand(stdout))
	root.AddCommand(newDoctorCommand(stdout))
	root.AddCommand(newRunCommand(stdout, stderr))
	root.AddCommand(newBuildCommand(stdout, stderr))
	return root
}

func currentVersion() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// Execute runs the process-level root command.
func Execute(stdout, stderr io.Writer, args []string) error {
	root := NewRootCommand(stdout, stderr)
	root.SetArgs(args)
	return root.Execute()
}
