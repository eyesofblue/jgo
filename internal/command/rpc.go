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
		Short: "Bind shared protobuf Services to gRPC servers and clients",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(newRPCServerCommand(stdout), newRPCClientCommand(stdout))
	return command
}

func newRPCServerCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use: "server", Short: "Connect shared protobuf Services to a gRPC server", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	command.AddCommand(newRPCServerBindCommand(stdout), newRPCServerUnbindCommand(stdout))
	return command
}

func newRPCServerBindCommand(stdout io.Writer) *cobra.Command {
	options := struct{ root, module, packagePath, handlerName string }{}
	command := &cobra.Command{
		Use: "bind <service-name>", Short: "Bind and register a Service from a shared protobuf module", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			binding, err := rpcbindinggen.BindServer(rpcbindinggen.BindConfig{Root: options.root, ModuleSpec: options.module, Package: options.packagePath, Service: args[0], HandlerName: options.handlerName})
			if err != nil {
				return err
			}
			handlerType := binding.Handler + "Handler"
			if _, err = fmt.Fprintf(stdout, "bound RPC server %s from %s with handler %s\n", binding.Service, binding.Package, handlerType); err != nil {
				return err
			}
			for _, method := range binding.Methods {
				if _, err = fmt.Fprintf(stdout, "implement %s.%s\n", handlerType, method.Name); err != nil {
					return err
				}
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.root, "root", ".", "JGO service project root")
	flags.StringVar(&options.module, "module", "", "shared protobuf module path, optionally followed by @<version> (required)")
	flags.StringVar(&options.packagePath, "package", "", "exact generated Go package when the Service is not unique")
	flags.StringVar(&options.handlerName, "handler-name", "", "handler name prefix (optional Handler suffix is normalized; defaults from Service)")
	_ = command.MarkFlagRequired("module")
	return command
}

func newRPCServerUnbindCommand(stdout io.Writer) *cobra.Command {
	var root, packagePath string
	command := &cobra.Command{
		Use: "unbind <service-name>", Short: "Permanently remove a shared Service binding from this server", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := rpcbindinggen.UnbindServer(root, args[0], packagePath); err != nil {
				return err
			}
			_, err := fmt.Fprintf(stdout, "unbound RPC server %s; user-owned business implementations were kept\n", args[0])
			return err
		},
	}
	command.Flags().StringVar(&root, "root", ".", "JGO service project root")
	command.Flags().StringVar(&packagePath, "package", "", "exact generated Go package when same-named Services are bound")
	return command
}

func newRPCClientCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use: "client", Short: "Connect typed clients from shared protobuf modules", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	command.AddCommand(newRPCClientBindCommand(stdout), newRPCClientUnbindCommand(stdout))
	return command
}

func newRPCClientBindCommand(stdout io.Writer) *cobra.Command {
	options := struct{ root, module, packagePath, name, address string }{}
	command := &cobra.Command{
		Use: "bind <service-name>", Short: "Bind and inject a typed client for a shared protobuf Service", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			binding, err := rpcbindinggen.BindClient(rpcbindinggen.BindConfig{Root: options.root, ModuleSpec: options.module, Package: options.packagePath, Service: args[0], Name: options.name, Address: options.address})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "bound RPC client %s for %s from %s; configure rpc_client.%s\n", binding.Name, binding.Service, binding.Package, binding.Name)
			return err
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.root, "root", ".", "JGO service project root")
	flags.StringVar(&options.module, "module", "", "shared protobuf module path, optionally followed by @<version> (required)")
	flags.StringVar(&options.packagePath, "package", "", "exact generated Go package when the Service is not unique")
	flags.StringVar(&options.name, "name", "", "stable rpc_client key and injected field name (defaults from Service; cannot be renamed)")
	flags.StringVar(&options.address, "address", "127.0.0.1:9090", "initial gRPC server address")
	_ = command.MarkFlagRequired("module")
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

func newRPCClientUnbindCommand(stdout io.Writer) *cobra.Command {
	var root string
	command := &cobra.Command{
		Use: "unbind <client-name>", Short: "Permanently remove a typed RPC client binding", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := rpcbindinggen.UnbindClient(root, args[0]); err != nil {
				return err
			}
			_, err := fmt.Fprintf(stdout, "unbound RPC client %s\n", args[0])
			return err
		},
	}
	command.Flags().StringVar(&root, "root", ".", "JGO service project root")
	return command
}
