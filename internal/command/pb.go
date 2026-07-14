package command

import (
	"fmt"
	"io"

	protobufgen "github.com/eyesofblue/jgo/internal/generator/protobuf"
	"github.com/spf13/cobra"
)

func newPBCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use: "pb", Short: "Author and generate local protobuf contracts", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	command.AddCommand(newPBServiceCommand(stdout), newPBMethodCommand(stdout), newPBGenerateCommand(stdout), newPBLintCommand(stdout), newPBBreakingCommand(stdout))
	return command
}

func newPBLintCommand(stdout io.Writer) *cobra.Command {
	var root string
	command := &cobra.Command{
		Use: "lint", Short: "Lint local protobuf contracts", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			ran, err := protobufgen.Lint(command.Context(), root)
			if err != nil {
				return err
			}
			if !ran {
				_, err = fmt.Fprintln(stdout, "no local protobuf contracts; nothing to lint")
			} else {
				_, err = fmt.Fprintln(stdout, "protobuf lint passed")
			}
			return err
		},
	}
	command.Flags().StringVar(&root, "root", ".", "JGO project root")
	return command
}

func newPBBreakingCommand(stdout io.Writer) *cobra.Command {
	var root, against string
	command := &cobra.Command{
		Use: "breaking", Short: "Check protobuf compatibility against an explicit baseline", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			ran, err := protobufgen.Breaking(command.Context(), root, against)
			if err != nil {
				return err
			}
			if !ran {
				_, err = fmt.Fprintln(stdout, "no local protobuf contracts; nothing to compare")
			} else {
				_, err = fmt.Fprintln(stdout, "protobuf breaking check passed")
			}
			return err
		},
	}
	command.Flags().StringVar(&root, "root", ".", "JGO project root")
	command.Flags().StringVar(&against, "against", "", "Buf source used as the compatibility baseline (required)")
	_ = command.MarkFlagRequired("against")
	return command
}

func newPBServiceCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use: "service", Short: "Manage protobuf Service definitions", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	command.AddCommand(newPBServiceAddCommand(stdout))
	return command
}

func newPBServiceAddCommand(stdout io.Writer) *cobra.Command {
	options := struct{ root, file, packageName string }{}
	command := &cobra.Command{
		Use: "add <service-name>", Short: "Add a protobuf Service definition", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			path, err := protobufgen.AddService(protobufgen.AddServiceConfig{Root: options.root, File: options.file, Package: options.packageName, Service: args[0]})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "added protobuf service %s to %s; add methods with `jgo pb method add <Method> --service %s`\n", args[0], path, args[0])
			return err
		},
	}
	command.Flags().StringVar(&options.root, "root", ".", "JGO project root")
	command.Flags().StringVar(&options.file, "file", "", "protobuf file relative to the project root")
	command.Flags().StringVar(&options.packageName, "package", "", "protobuf package to select or create (first contract defaults to <project>.v1)")
	return command
}

func newPBMethodCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use: "method", Short: "Manage methods in protobuf Services", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	command.AddCommand(newPBMethodAddCommand(stdout))
	return command
}

func newPBMethodAddCommand(stdout io.Writer) *cobra.Command {
	options := struct{ root, file, service string }{}
	command := &cobra.Command{
		Use: "add <method-name>", Short: "Add a method with request and standard response messages", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			path, err := protobufgen.Add(protobufgen.AddConfig{Root: options.root, File: options.file, Service: options.service, RPC: args[0]})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "added %s to %s in %s\nresponse code/msg use fields 1/2; add business fields from 3, then run jgo pb generate\n", args[0], options.service, path)
			return err
		},
	}
	command.Flags().StringVar(&options.root, "root", ".", "JGO project root")
	command.Flags().StringVar(&options.file, "file", "", "protobuf file relative to the project root")
	command.Flags().StringVar(&options.service, "service", "", "protobuf Service name (required)")
	_ = command.MarkFlagRequired("service")
	return command
}

func newPBGenerateCommand(stdout io.Writer) *cobra.Command {
	var root string
	command := &cobra.Command{
		Use: "generate", Short: "Lint local protobuf contracts and generate Go code with Buf", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			result, err := protobufgen.GenerateWithResult(root)
			if err != nil {
				return err
			}
			if err := printCreatedServiceStubs(stdout, result); err != nil {
				return err
			}
			if result.Empty {
				_, err = fmt.Fprintln(stdout, "no local protobuf contracts; nothing to generate")
			} else if result.ProtocolOnly {
				_, err = fmt.Fprintln(stdout, "generated shared protobuf and gRPC Go packages; run go test ./...")
			} else {
				_, err = fmt.Fprintln(stdout, "generated protobuf and gRPC transport; run go test ./...")
			}
			return err
		},
	}
	command.Flags().StringVar(&root, "root", ".", "JGO project root")
	return command
}
