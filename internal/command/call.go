package command

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	callruntime "github.com/eyesofblue/jgo/internal/call"
	"github.com/spf13/cobra"
)

func newCallCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "call",
		Short: "Call HTTP or gRPC APIs from their contracts",
	}
	command.AddCommand(newCallHTTPCommand(stdout), newCallGRPCCommand(stdout))
	return command
}

func newCallHTTPCommand(stdout io.Writer) *cobra.Command {
	options := struct {
		root    string
		address string
		data    string
		headers []string
		timeout time.Duration
	}{}
	command := &cobra.Command{
		Use:   "http <operation-id>",
		Short: "Call an HTTP operation using the local OpenAPI contract",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			result, err := callruntime.CallHTTP(command.Context(), callruntime.HTTPConfig{
				Root: options.root, Operation: args[0], Address: options.address,
				Data: options.data, Headers: options.headers, Timeout: options.timeout,
			})
			if len(result.Body) > 0 {
				if _, writeErr := stdout.Write(result.Body); writeErr != nil {
					return writeErr
				}
			}
			return err
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.root, "root", ".", "JGO project root")
	flags.StringVar(&options.address, "addr", "", "HTTP server base address (required)")
	flags.StringVarP(&options.data, "data", "d", "{}", "request data as a JSON object")
	flags.StringArrayVarP(&options.headers, "header", "H", nil, "request header in 'Name: Value' form (repeatable)")
	flags.DurationVar(&options.timeout, "timeout", 10*time.Second, "call timeout")
	_ = command.MarkFlagRequired("addr")
	return command
}

func newCallGRPCCommand(stdout io.Writer) *cobra.Command {
	options := struct {
		root    string
		address string
		data    string
		headers []string
		timeout time.Duration
	}{}
	command := &cobra.Command{
		Use:   "grpc <service.method>",
		Short: "Dynamically call a unary gRPC method using Reflection or local proto files",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			result, err := callruntime.CallGRPC(command.Context(), callruntime.GRPCConfig{
				Root: options.root, Method: args[0], Address: options.address,
				Data: options.data, Headers: options.headers, Timeout: options.timeout,
			})
			if len(result.Body) > 0 {
				if _, writeErr := stdout.Write(result.Body); writeErr != nil {
					return writeErr
				}
			}
			return err
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.root, "root", ".", "JGO project root used for local proto fallback")
	flags.StringVar(&options.address, "addr", "", "gRPC server address (required)")
	flags.StringVarP(&options.data, "data", "d", "{}", "protobuf request as a JSON object")
	flags.StringArrayVarP(&options.headers, "header", "H", nil, "gRPC metadata in 'Name: Value' form (repeatable)")
	flags.DurationVar(&options.timeout, "timeout", 10*time.Second, "call timeout")
	_ = command.MarkFlagRequired("addr")
	return command
}

func newListCommand(stdout io.Writer) *cobra.Command {
	var root string
	command := &cobra.Command{
		Use:   "list",
		Short: "List HTTP and gRPC methods from local contracts",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			httpMethods, err := callruntime.ListHTTP(root)
			if err != nil {
				return err
			}
			grpcMethods, err := callruntime.ListGRPC(command.Context(), root)
			if err != nil {
				return err
			}
			writer := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
			for _, method := range httpMethods {
				_, _ = fmt.Fprintf(writer, "HTTP\t%s\t%s\t%s\n", method.Method, method.Path, method.Operation)
			}
			for _, method := range grpcMethods {
				kind := "unary"
				if method.ClientStreaming || method.ServerStreaming {
					kind = "stream"
				}
				_, _ = fmt.Fprintf(writer, "gRPC\t%s\t%s\n", kind, method.FullName)
			}
			return writer.Flush()
		},
	}
	command.Flags().StringVar(&root, "root", ".", "JGO project root")
	return command
}
