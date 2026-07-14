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
			result, err := protobufgen.GenerateWithResult(root)
			if err != nil {
				return err
			}
			if err := printCreatedServiceStubs(stdout, result); err != nil {
				return err
			}
			_, err = fmt.Fprintln(stdout, "generated protobuf and gRPC transport; run go test ./...")
			return err
		},
	}
	command.Flags().StringVar(&root, "root", ".", "JGO project root")
	return command
}

func printCreatedServiceStubs(stdout io.Writer, result protobufgen.GenerateResult) error {
	for _, stub := range result.CreatedStubs {
		if _, err := fmt.Fprintf(stdout, "created %s; implement Service.%s\n", stub.Path, stub.Method); err != nil {
			return err
		}
	}
	return nil
}

func newRPCAddCommand(stdout io.Writer) *cobra.Command {
	options := struct {
		root    string
		file    string
		service string
	}{}
	command := &cobra.Command{
		Use:   "add <rpc-name>",
		Short: "Add an RPC with request and standard response messages to a protobuf service",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			path, err := protobufgen.Add(protobufgen.AddConfig{
				Root: options.root, File: options.file, Service: options.service, RPC: args[0],
			})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "added %s to %s in %s\nresponse code/msg use fields 1/2; add business fields from 3, then run jgo rpc generate\n", args[0], options.service, path)
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
