package command

import (
	"fmt"
	"io"

	protobufgen "github.com/eyesofblue/jgo/internal/generator/protobuf"
	"github.com/spf13/cobra"
)

func newRPCCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "rpc",
		Short: "Manage protobuf contracts and generated gRPC code",
	}
	command.AddCommand(newRPCAddCommand(stdout), newRPCGenerateCommand(stdout))
	return command
}

func newRPCGenerateCommand(stdout io.Writer) *cobra.Command {
	var root string
	command := &cobra.Command{
		Use:   "generate",
		Short: "Lint protobuf contracts and generate gRPC code with Buf",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if err := protobufgen.Generate(root); err != nil {
				return err
			}
			_, err := fmt.Fprintln(stdout, "generated protobuf, gRPC transport, and missing service stubs")
			return err
		},
	}
	command.Flags().StringVar(&root, "root", ".", "JGO project root")
	return command
}

func newRPCAddCommand(stdout io.Writer) *cobra.Command {
	options := struct {
		root    string
		file    string
		service string
	}{}
	command := &cobra.Command{
		Use:   "add <rpc-name>",
		Short: "Add an RPC and empty request/response messages to a protobuf service",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			path, err := protobufgen.Add(protobufgen.AddConfig{
				Root: options.root, File: options.file, Service: options.service, RPC: args[0],
			})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "added %s to %s in %s; edit message fields, then run jgo rpc generate\n", args[0], options.service, path)
			return err
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.root, "root", ".", "JGO project root")
	flags.StringVar(&options.file, "file", "", "protobuf file relative to the project root (optional)")
	flags.StringVar(&options.service, "service", "", "protobuf service name (required)")
	_ = command.MarkFlagRequired("service")
	return command
}
