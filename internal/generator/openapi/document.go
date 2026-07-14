package openapi

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"
)

const operationExtension = "x-jgo-operation"

var operationPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

func Add(input AddConfig) error {
	project, err := loadProject(input.Root)
	if err != nil {
		return err
	}
	operation, err := normalizeOperation(input)
	if err != nil {
		return err
	}
	catalog, err := loadModels(project.modelPath, project.module)
	if err != nil {
		return err
	}
	if operation.RequestType != "" && !catalog.Has(operation.RequestType) {
		return fmt.Errorf("%w: request type %s", ErrModelNotFound, operation.RequestType)
	}
	if operation.ResponseType != "" && !isPrimitive(operation.ResponseType) && !catalog.Has(operation.ResponseType) {
		return fmt.Errorf("%w: response type %s", ErrModelNotFound, operation.ResponseType)
	}

	spec, err := loadSpecFile(project.specPath)
	if err != nil {
		return fmt.Errorf("load OpenAPI contract: %w", err)
	}
	if err := ensureOperationAvailable(spec, operation); err != nil {
		return err
	}
	if err := syncModelSchemas(spec, catalog); err != nil {
		return err
	}
	addOperation(spec, operation, catalog)
	contents, err := marshalAndValidate(spec)
	if err != nil {
		return err
	}
	if exists, err := serviceMethodExists(serviceDirectory(project.root), operation.Name); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("%w: Service.%s is already implemented in the service package", ErrServiceFileExists, operation.Name)
	}

	stubPath := filepath.Join(project.root, "internal", "service", snakeCase(operation.Name)+".go")
	if _, err := os.Lstat(stubPath); err == nil {
		return fmt.Errorf("%w: %s", ErrServiceFileExists, stubPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect service implementation: %w", err)
	}
	stub, err := renderServiceStub(project.module, operation)
	if err != nil {
		return err
	}
	if err := writeContractAndStub(project.specPath, contents, stubPath, stub); err != nil {
		return err
	}
	return nil
}

func normalizeOperation(input AddConfig) (Operation, error) {
	operation := Operation{
		Name:         strings.TrimSpace(input.Operation),
		Method:       strings.ToUpper(strings.TrimSpace(input.Method)),
		Path:         strings.TrimSpace(input.Path),
		RequestType:  strings.TrimSpace(input.RequestType),
		ResponseType: strings.TrimSpace(input.ResponseType),
		ResponseList: input.ResponseList,
	}
	if !operationPattern.MatchString(operation.Name) {
		return Operation{}, fmt.Errorf("%w: operation name %q must be exported Go identifier", ErrInvalidOperation, operation.Name)
	}
	if operation.Method != http.MethodGet && operation.Method != http.MethodPost {
		return Operation{}, fmt.Errorf("%w: %q", ErrInvalidMethod, operation.Method)
	}
	if operation.Path == "" || !strings.HasPrefix(operation.Path, "/") || strings.ContainsAny(operation.Path, "?# ") {
		return Operation{}, fmt.Errorf("%w: %q", ErrInvalidPath, operation.Path)
	}
	if operation.RequestType != "" && len(input.Request) != 0 {
		return Operation{}, fmt.Errorf("%w: --request and --request-params are mutually exclusive", ErrInvalidOperation)
	}
	if operation.RequestType != "" && operation.Method != http.MethodPost {
		return Operation{}, fmt.Errorf("%w: --request-params describes a JSON body and requires POST", ErrInvalidOperation)
	}
	if operation.ResponseList && operation.ResponseType == "" {
		return Operation{}, fmt.Errorf("%w: response list requires --response-data", ErrInvalidOperation)
	}
	fields, err := parseFields(input.Request, operation.Method)
	if err != nil {
		return Operation{}, err
	}
	operation.Fields = fields
	return operation, nil
}

func ensureOperationAvailable(spec *openapi3.T, operation Operation) error {
	if spec.Paths == nil {
		spec.Paths = openapi3.NewPaths()
	}
	for path, item := range spec.Paths.Map() {
		for method, existing := range item.Operations() {
			if existing.OperationID == operation.Name {
				return fmt.Errorf("%w: %s is already %s %s", ErrDuplicateOperation, operation.Name, strings.ToUpper(method), path)
			}
		}
	}
	if item := spec.Paths.Value(operation.Path); item != nil && item.GetOperation(operation.Method) != nil {
		return fmt.Errorf("%w: %s %s", ErrDuplicateRoute, operation.Method, operation.Path)
	}
	return nil
}

func syncModelSchemas(spec *openapi3.T, catalog *modelCatalog) error {
	schemas, err := catalog.Schemas()
	if err != nil {
		return err
	}
	if spec.Components == nil {
		components := openapi3.NewComponents()
		spec.Components = &components
	}
	if spec.Components.Schemas == nil {
		spec.Components.Schemas = make(openapi3.Schemas)
	}
	for name, schema := range spec.Components.Schemas {
		if isManagedModelSchema(schema, catalog.importPath) {
			delete(spec.Components.Schemas, name)
		}
	}
	for name, schema := range schemas {
		spec.Components.Schemas[name] = schema
	}
	spec.Components.Schemas["JGOErrorResponse"] = &openapi3.SchemaRef{Value: envelopeSchema(nil)}
	return nil
}

func isManagedModelSchema(ref *openapi3.SchemaRef, importPath string) bool {
	if ref == nil || ref.Value == nil {
		return false
	}
	goType, ok := ref.Value.Extensions["x-go-type"].(string)
	if !ok || !strings.HasPrefix(goType, "model.") {
		return false
	}
	goImport, ok := ref.Value.Extensions["x-go-type-import"].(map[string]any)
	if !ok {
		return false
	}
	path, ok := goImport["path"].(string)
	return ok && path == importPath
}

func addOperation(spec *openapi3.T, operation Operation, catalog *modelCatalog) {
	contract := openapi3.NewOperation()
	contract.OperationID = operation.Name
	contract.Extensions = map[string]any{operationExtension: operationMetadata(operation)}

	bodyFields := make([]Field, 0, len(operation.Fields))
	for _, field := range operation.Fields {
		if field.Source == "body" {
			bodyFields = append(bodyFields, field)
			continue
		}
		parameter := openapi3.NewQueryParameter(field.Name)
		if field.Source == "header" {
			parameter = openapi3.NewHeaderParameter(field.Name)
		}
		parameter.Required = field.Required
		parameter.Schema = &openapi3.SchemaRef{Value: primitiveSchema(field.GoType)}
		contract.Parameters = append(contract.Parameters, &openapi3.ParameterRef{Value: parameter})
	}
	if operation.RequestType != "" {
		if operation.Method == http.MethodGet {
			addModelQueryParameters(contract, catalog, operation.RequestType)
		} else {
			contract.RequestBody = &openapi3.RequestBodyRef{Value: openapi3.NewRequestBody().WithRequired(true).WithJSONSchemaRef(componentRef(operation.RequestType))}
		}
	} else if len(bodyFields) != 0 {
		requestSchema := objectSchema()
		for _, field := range bodyFields {
			requestSchema.Properties[field.Name] = &openapi3.SchemaRef{Value: primitiveSchema(field.GoType)}
			if field.Required {
				requestSchema.Required = append(requestSchema.Required, field.Name)
			}
		}
		sort.Strings(requestSchema.Required)
		contract.RequestBody = &openapi3.RequestBodyRef{Value: openapi3.NewRequestBody().WithRequired(true).WithJSONSchema(requestSchema)}
	}

	data := responseDataSchema(operation)
	success := openapi3.NewResponse().WithDescription("successful response").WithJSONSchema(envelopeSchema(data))
	errorResponse := &openapi3.ResponseRef{Ref: "#/components/schemas/JGOErrorResponse"}
	_ = errorResponse
	contract.Responses = openapi3.NewResponses(
		openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{Value: success}),
		openapi3.WithName("default", openapi3.NewResponse().WithDescription("error response").WithJSONSchemaRef(componentRef("JGOErrorResponse"))),
	)
	spec.AddOperation(operation.Path, operation.Method, contract)
}

func addModelQueryParameters(operation *openapi3.Operation, catalog *modelCatalog, name string) {
	structure := catalog.definitions[name]
	for _, astField := range structure.Fields.List {
		if len(astField.Names) == 0 {
			continue
		}
		for _, fieldName := range astField.Names {
			if !fieldName.IsExported() {
				continue
			}
			jsonName, omitempty, skip, _ := jsonField(fieldName.Name, astField.Tag)
			if skip {
				continue
			}
			schema, pointer, err := catalog.schemaForExpr(astField.Type)
			if err != nil {
				continue
			}
			parameter := openapi3.NewQueryParameter(jsonName)
			parameter.Required = !omitempty && !pointer
			parameter.Schema = schema
			operation.Parameters = append(operation.Parameters, &openapi3.ParameterRef{Value: parameter})
		}
	}
}

func responseDataSchema(operation Operation) *openapi3.SchemaRef {
	if operation.ResponseType == "" {
		schema := objectSchema()
		schema.Nullable = true
		return &openapi3.SchemaRef{Value: schema}
	}
	var data *openapi3.SchemaRef
	if primitive := primitiveSchema(operation.ResponseType); primitive != nil {
		data = &openapi3.SchemaRef{Value: primitive}
	} else {
		data = componentRef(operation.ResponseType)
	}
	if operation.ResponseList {
		return &openapi3.SchemaRef{Value: arraySchema(data)}
	}
	return data
}

func envelopeSchema(data *openapi3.SchemaRef) *openapi3.Schema {
	if data == nil {
		dataSchema := objectSchema()
		dataSchema.Nullable = true
		data = &openapi3.SchemaRef{Value: dataSchema}
	}
	schema := objectSchema()
	schema.Required = []string{"code", "data", "msg"}
	schema.Properties["code"] = &openapi3.SchemaRef{Value: primitiveSchema("int")}
	schema.Properties["msg"] = &openapi3.SchemaRef{Value: stringSchema()}
	schema.Properties["data"] = data
	return schema
}

func componentRef(name string) *openapi3.SchemaRef {
	return &openapi3.SchemaRef{Ref: "#/components/schemas/" + name}
}

func operationMetadata(operation Operation) map[string]any {
	fields := make([]map[string]any, 0, len(operation.Fields))
	for _, field := range operation.Fields {
		fields = append(fields, map[string]any{
			"name": field.Name, "goName": field.GoName, "goType": field.GoType,
			"source": field.Source, "required": field.Required,
		})
	}
	return map[string]any{
		"requestType": operation.RequestType, "responseType": operation.ResponseType,
		"responseList": operation.ResponseList, "fields": fields,
	}
}

func marshalAndValidate(spec *openapi3.T) ([]byte, error) {
	contents, err := yaml.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("marshal OpenAPI contract: %w", err)
	}
	reloaded, err := loadSpecData(contents)
	if err != nil {
		return nil, fmt.Errorf("validate OpenAPI contract: %w", err)
	}
	if err := reloaded.Validate(context.Background()); err != nil {
		return nil, fmt.Errorf("validate OpenAPI contract: %w", err)
	}
	return contents, nil
}

func loadSpecFile(path string) (*openapi3.T, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadSpecData(contents)
}

func writeContractAndStub(contractPath string, contract []byte, stubPath string, stub []byte) error {
	if err := os.MkdirAll(filepath.Dir(stubPath), 0o755); err != nil {
		return fmt.Errorf("create service directory: %w", err)
	}
	stubFile, err := os.OpenFile(stubPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create service implementation: %w", err)
	}
	committed := false
	defer func() {
		_ = stubFile.Close()
		if !committed {
			_ = os.Remove(stubPath)
		}
	}()
	if _, err := stubFile.Write(stub); err != nil {
		return fmt.Errorf("write service implementation: %w", err)
	}
	if err := stubFile.Close(); err != nil {
		return fmt.Errorf("close service implementation: %w", err)
	}
	if err := atomicWrite(contractPath, contract); err != nil {
		return err
	}
	committed = true
	return nil
}

func atomicWrite(path string, contents []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect output %s: %w", path, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".jgo-")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set output permissions: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary output: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("commit output %s: %w", path, err)
	}
	return nil
}
