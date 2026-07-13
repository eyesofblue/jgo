package command

import (
	"fmt"
	"io"

	openapigen "github.com/eyesofblue/jgo/internal/generator/openapi"
	"github.com/spf13/cobra"
)

func newAPICommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "api",
		Short: "Manage HTTP/OpenAPI contracts and generated code",
	}
	command.AddCommand(newAPIAddCommand(stdout), newAPIGenerateCommand(stdout))
	return command
}

func newAPIAddCommand(stdout io.Writer) *cobra.Command {
	options := struct {
		root         string
		method       string
		path         string
		request      []string
		requestType  string
		responseType string
		responseList bool
	}{}
	command := &cobra.Command{
		Use:   "add <operation-name>",
		Short: "Add an HTTP operation to the OpenAPI contract",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			err := openapigen.Add(openapigen.AddConfig{
				Root: options.root, Operation: args[0], Method: options.method, Path: options.path,
				Request: options.request, RequestType: options.requestType,
				ResponseType: options.responseType, ResponseList: options.responseList,
			})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "added %s %s as %s; run jgo api generate\n", options.method, options.path, args[0])
			return err
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.root, "root", ".", "JGO project root")
	flags.StringVar(&options.method, "method", "", "HTTP method: GET or POST (required)")
	flags.StringVar(&options.path, "path", "", "HTTP path (required)")
	flags.StringSliceVar(&options.request, "request", nil, "request field name:type[:required|optional][:query|header|body] (repeatable)")
	flags.StringVar(&options.requestType, "request-params", "", "Go struct in api/http/model used as the JSON request body")
	flags.StringVar(&options.responseType, "response-data", "", "primitive or Go struct in api/http/model returned in data")
	flags.BoolVar(&options.responseList, "response-list", false, "return data as an array of response-data")
	_ = command.MarkFlagRequired("method")
	_ = command.MarkFlagRequired("path")
	return command
}

func newAPIGenerateCommand(stdout io.Writer) *cobra.Command {
	var root string
	command := &cobra.Command{
		Use:   "generate",
		Short: "Generate HTTP server, client, models, and transport code",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if err := openapigen.Generate(root); err != nil {
				return err
			}
			_, err := fmt.Fprintln(stdout, "generated HTTP code from api/http/openapi.yaml")
			return err
		},
	}
	command.Flags().StringVar(&root, "root", ".", "JGO project root")
	return command
}
