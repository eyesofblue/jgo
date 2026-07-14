package command

import (
	"fmt"
	"io"

	protobufgen "github.com/eyesofblue/jgo/internal/generator/protobuf"
	rpcbindinggen "github.com/eyesofblue/jgo/internal/generator/rpcbinding"
	"github.com/spf13/cobra"
)

func newRPCCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "rpc",
		Short: "Manage protobuf contracts and generated gRPC code",
	}
	command.AddCommand(newRPCPBServiceCommand(stdout), newRPCPBAPICommand(stdout), newRPCServerCommand(stdout), newRPCClientCommand(stdout), newRPCGenerateCommand(stdout))
	return command
}

func newRPCServerCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "server", Short: "Connect shared protobuf Services to a gRPC server"}
	command.AddCommand(newRPCServerAddCommand(stdout))
	return command
}

func newRPCServerAddCommand(stdout io.Writer) *cobra.Command {
	options := struct{ root, module, packagePath string }{}
	command := &cobra.Command{
		Use: "add <service-name>", Short: "Implement and register a Service from a shared protobuf module", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			binding, err := rpcbindinggen.AddServer(rpcbindinggen.AddConfig{Root: options.root, ModuleSpec: options.module, Package: options.packagePath, Service: args[0]})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "added RPC server %s from %s; implement generated Service methods\n", binding.Service, binding.Package)
			return err
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.root, "root", ".", "JGO service project root")
	flags.StringVar(&options.module, "module", "", "shared protobuf module in <module>@<version> form (required)")
	flags.StringVar(&options.packagePath, "package", "", "exact generated Go package when the Service is not unique")
	_ = command.MarkFlagRequired("module")
	return command
}

func newRPCClientCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{Use: "client", Short: "Connect typed clients from shared protobuf modules"}
	command.AddCommand(newRPCClientAddCommand(stdout))
	return command
}

func newRPCClientAddCommand(stdout io.Writer) *cobra.Command {
	options := struct{ root, module, packagePath, name, address string }{}
	command := &cobra.Command{
		Use: "add <service-name>", Short: "Add and inject a typed client for a shared protobuf Service", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			binding, err := rpcbindinggen.AddClient(rpcbindinggen.AddConfig{Root: options.root, ModuleSpec: options.module, Package: options.packagePath, Service: args[0], Name: options.name, Address: options.address})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "added RPC client %s for %s from %s; configure rpc_client.%s\n", binding.Name, binding.Service, binding.Package, binding.Name)
			return err
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.root, "root", ".", "JGO service project root")
	flags.StringVar(&options.module, "module", "", "shared protobuf module in <module>@<version> form (required)")
	flags.StringVar(&options.packagePath, "package", "", "exact generated Go package when the Service is not unique")
	flags.StringVar(&options.name, "name", "", "stable rpc_client key and injected field name (defaults from Service; cannot be renamed)")
	flags.StringVar(&options.address, "address", "127.0.0.1:9090", "initial gRPC server address")
	_ = command.MarkFlagRequired("module")
	return command
}

func newRPCPBServiceCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "pbservice",
		Short: "Manage protobuf Service definitions",
	}
	command.AddCommand(newRPCPBServiceAddCommand(stdout))
	return command
}

func newRPCPBServiceAddCommand(stdout io.Writer) *cobra.Command {
	options := struct {
		root string
		file string
	}{}
	command := &cobra.Command{
		Use:   "add <service-name>",
		Short: "Add a protobuf Service definition to a contract",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			path, err := protobufgen.AddService(protobufgen.AddServiceConfig{
				Root: options.root, File: options.file, Service: args[0],
			})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "added protobuf service %s to %s; add methods with `jgo rpc pbapi add <RPC> --service %s`\n", args[0], path, args[0])
			return err
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.root, "root", ".", "JGO project root")
	flags.StringVar(&options.file, "file", "", "protobuf file relative to the project root (optional when exactly one file exists)")
	return command
}

func newRPCPBAPICommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "pbapi",
		Short: "Manage RPC methods in protobuf Services",
	}
	command.AddCommand(newRPCPBAPIAddCommand(stdout))
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
			if result.ProtocolOnly {
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

func printCreatedServiceStubs(stdout io.Writer, result protobufgen.GenerateResult) error {
	for _, stub := range result.CreatedStubs {
		if _, err := fmt.Fprintf(stdout, "created %s; implement Service.%s\n", stub.Path, stub.Method); err != nil {
			return err
		}
	}
	return nil
}

func newRPCPBAPIAddCommand(stdout io.Writer) *cobra.Command {
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
