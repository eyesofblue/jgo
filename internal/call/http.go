// Package call implements contract-driven HTTP and gRPC debugging calls.
package call

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

const maxResponseBytes = 10 << 20

// HTTPConfig configures one OpenAPI-driven HTTP request.
type HTTPConfig struct {
	Root      string
	Operation string
	Address   string
	Data      string
	Headers   []string
	Timeout   time.Duration
	Client    *http.Client
}

// HTTPResult contains the response without conflating HTTP and business codes.
type HTTPResult struct {
	StatusCode int
	Status     string
	Body       []byte
}

// HTTPMethod describes an operation from the local OpenAPI contract.
type HTTPMethod struct {
	Operation string
	Method    string
	Path      string
}

type httpOperation struct {
	HTTPMethod
	item      *openapi3.PathItem
	operation *openapi3.Operation
}

// CallHTTP resolves an operationId and invokes the corresponding HTTP route.
func CallHTTP(ctx context.Context, config HTTPConfig) (HTTPResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if config.Root == "" {
		config.Root = "."
	}
	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Second
	}
	if strings.TrimSpace(config.Address) == "" {
		return HTTPResult{}, fmt.Errorf("call http: --addr is required")
	}
	operation, available, err := findHTTPOperation(config.Root, config.Operation)
	if err != nil {
		return HTTPResult{}, withAvailable(err, available)
	}
	input, err := decodeJSONObject(config.Data)
	if err != nil {
		return HTTPResult{}, fmt.Errorf("call http: decode --data: %w", err)
	}
	callContext, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()
	request, err := buildHTTPRequest(callContext, config, operation, input)
	if err != nil {
		return HTTPResult{}, err
	}
	client := config.Client
	if client == nil {
		client = &http.Client{}
	}
	response, err := client.Do(request)
	if err != nil {
		return HTTPResult{}, fmt.Errorf("call http: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return HTTPResult{}, fmt.Errorf("call http: read response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return HTTPResult{}, fmt.Errorf("call http: response exceeds %d bytes", maxResponseBytes)
	}
	result := HTTPResult{StatusCode: response.StatusCode, Status: response.Status, Body: formatJSON(body)}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, fmt.Errorf("call http: HTTP status %s", response.Status)
	}
	return result, nil
}

// ListHTTP returns all operations in deterministic order.
func ListHTTP(root string) ([]HTTPMethod, error) {
	operations, err := loadHTTPOperations(root)
	if err != nil {
		if os.IsNotExist(rootCause(err)) {
			return nil, nil
		}
		return nil, err
	}
	methods := make([]HTTPMethod, 0, len(operations))
	for _, operation := range operations {
		methods = append(methods, operation.HTTPMethod)
	}
	return methods, nil
}

func buildHTTPRequest(ctx context.Context, config HTTPConfig, operation httpOperation, input map[string]any) (*http.Request, error) {
	path := operation.Path
	query := make(url.Values)
	headers := make(http.Header)
	extraHeaders, err := parseHeaders(config.Headers)
	if err != nil {
		return nil, fmt.Errorf("call http: %w", err)
	}
	consumed := map[string]bool{}
	parameters := append(openapi3.Parameters{}, operation.item.Parameters...)
	parameters = append(parameters, operation.operation.Parameters...)
	for _, reference := range parameters {
		if reference == nil || reference.Value == nil {
			continue
		}
		parameter := reference.Value
		value, exists := input[parameter.Name]
		if !exists {
			if parameter.In == openapi3.ParameterInHeader && len(extraHeaders.Values(parameter.Name)) > 0 {
				continue
			}
			if parameter.Required {
				return nil, fmt.Errorf("call http: required %s parameter %q is missing", parameter.In, parameter.Name)
			}
			continue
		}
		consumed[parameter.Name] = true
		values, err := stringValues(value)
		if err != nil {
			return nil, fmt.Errorf("call http: parameter %q: %w", parameter.Name, err)
		}
		switch parameter.In {
		case openapi3.ParameterInQuery:
			for _, item := range values {
				query.Add(parameter.Name, item)
			}
		case openapi3.ParameterInHeader:
			headers.Set(parameter.Name, strings.Join(values, ","))
		case openapi3.ParameterInPath:
			if len(values) != 1 {
				return nil, fmt.Errorf("call http: path parameter %q must be scalar", parameter.Name)
			}
			placeholder := "{" + parameter.Name + "}"
			if !strings.Contains(path, placeholder) {
				return nil, fmt.Errorf("call http: path parameter %q has no placeholder", parameter.Name)
			}
			path = strings.ReplaceAll(path, placeholder, url.PathEscape(values[0]))
		default:
			return nil, fmt.Errorf("call http: unsupported parameter location %q", parameter.In)
		}
	}

	var body io.Reader
	if reference := operation.operation.RequestBody; reference != nil && reference.Value != nil {
		media := reference.Value.Content.Get("application/json")
		if media == nil || media.Schema == nil || media.Schema.Value == nil {
			return nil, fmt.Errorf("call http: operation %s has no JSON request schema", operation.Operation)
		}
		bodyValue := make(map[string]any)
		for name, value := range input {
			if !consumed[name] {
				bodyValue[name] = value
			}
		}
		if err := media.Schema.Value.VisitJSON(bodyValue); err != nil {
			return nil, fmt.Errorf("call http: request body does not match OpenAPI schema: %w", err)
		}
		encoded, err := json.Marshal(bodyValue)
		if err != nil {
			return nil, fmt.Errorf("call http: encode request body: %w", err)
		}
		body = bytes.NewReader(encoded)
		headers.Set("Content-Type", "application/json")
	}

	base, err := url.Parse(strings.TrimSpace(config.Address))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("call http: invalid --addr %q", config.Address)
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	base.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, operation.Method, base.String(), body)
	if err != nil {
		return nil, fmt.Errorf("call http: create request: %w", err)
	}
	request.Header = headers
	for name, values := range extraHeaders {
		request.Header.Del(name)
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	return request, nil
}

func loadHTTPOperations(root string) ([]httpOperation, error) {
	if root == "" {
		root = "."
	}
	path := filepath.Join(root, "api", "http", "openapi.yaml")
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	spec, err := loader.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("call http: load %s: %w", path, err)
	}
	if err := spec.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("call http: validate %s: %w", path, err)
	}
	var operations []httpOperation
	if spec.Paths != nil {
		for path, item := range spec.Paths.Map() {
			for method, operation := range item.Operations() {
				if strings.TrimSpace(operation.OperationID) == "" {
					continue
				}
				operations = append(operations, httpOperation{
					HTTPMethod: HTTPMethod{Operation: operation.OperationID, Method: strings.ToUpper(method), Path: path},
					item:       item, operation: operation,
				})
			}
		}
	}
	sort.Slice(operations, func(i, j int) bool { return operations[i].Operation < operations[j].Operation })
	return operations, nil
}

func findHTTPOperation(root, name string) (httpOperation, []string, error) {
	operations, err := loadHTTPOperations(root)
	if err != nil {
		return httpOperation{}, nil, err
	}
	available := make([]string, 0, len(operations))
	for _, operation := range operations {
		available = append(available, operation.Operation)
		if operation.Operation == name {
			return operation, available, nil
		}
	}
	return httpOperation{}, available, fmt.Errorf("call http: operation %q not found", name)
}

func decodeJSONObject(value string) (map[string]any, error) {
	if strings.TrimSpace(value) == "" {
		value = "{}"
	}
	var result map[string]any
	decoder := json.NewDecoder(strings.NewReader(value))
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("must be a JSON object")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, fmt.Errorf("must contain exactly one JSON object")
	}
	return result, nil
}

func stringValues(value any) ([]string, error) {
	switch typed := value.(type) {
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			converted, err := stringValues(item)
			if err != nil || len(converted) != 1 {
				return nil, fmt.Errorf("array elements must be scalar")
			}
			values = append(values, converted[0])
		}
		return values, nil
	case string:
		return []string{typed}, nil
	case float64, bool:
		encoded, _ := json.Marshal(typed)
		return []string{string(encoded)}, nil
	case nil:
		return []string{""}, nil
	default:
		return nil, fmt.Errorf("must be a scalar or array of scalars")
	}
}

func parseHeaders(values []string) (http.Header, error) {
	headers := make(http.Header)
	for _, raw := range values {
		name, value, found := strings.Cut(raw, ":")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !found || name == "" || strings.ContainsAny(name+value, "\r\n") {
			return nil, fmt.Errorf("invalid header %q; use 'Name: Value'", raw)
		}
		headers.Add(name, value)
	}
	return headers, nil
}

func formatJSON(contents []byte) []byte {
	contents = bytes.TrimSpace(contents)
	if len(contents) == 0 {
		return nil
	}
	var output bytes.Buffer
	if json.Indent(&output, contents, "", "  ") == nil {
		return append(output.Bytes(), '\n')
	}
	return append(contents, '\n')
}

func withAvailable(err error, methods []string) error {
	if len(methods) == 0 {
		return err
	}
	return fmt.Errorf("%w; available methods: %s", err, strings.Join(methods, ", "))
}

func rootCause(err error) error {
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err
		}
		err = unwrapped
	}
}
