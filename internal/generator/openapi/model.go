package openapi

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/getkin/kin-openapi/openapi3"
)

type modelCatalog struct {
	definitions map[string]*ast.StructType
	docs        map[string]string
	importPath  string
}

func loadModels(directory, modulePath string) (*modelCatalog, error) {
	catalog := &modelCatalog{
		definitions: make(map[string]*ast.StructType),
		docs:        make(map[string]string),
		importPath:  modulePath + "/" + ModelPath,
	}
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return catalog, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read model directory: %w", err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			files = append(files, filepath.Join(directory, entry.Name()))
		}
	}
	sort.Strings(files)
	fileSet := token.NewFileSet()
	for _, path := range files {
		file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse model file %s: %w", path, err)
		}
		if file.Name.Name != "model" {
			return nil, fmt.Errorf("%w: %s must use package model", ErrInvalidProject, path)
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || !typeSpec.Name.IsExported() {
					continue
				}
				structure, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				catalog.definitions[typeSpec.Name.Name] = structure
				doc := typeSpec.Doc
				if doc == nil {
					doc = general.Doc
				}
				if doc != nil {
					catalog.docs[typeSpec.Name.Name] = strings.TrimSpace(doc.Text())
				}
			}
		}
	}
	return catalog, nil
}

func (catalog *modelCatalog) Has(name string) bool {
	_, ok := catalog.definitions[name]
	return ok
}

func (catalog *modelCatalog) Schemas() (openapi3.Schemas, error) {
	names := make([]string, 0, len(catalog.definitions))
	for name := range catalog.definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	schemas := make(openapi3.Schemas, len(names))
	for _, name := range names {
		schema, err := catalog.structSchema(catalog.definitions[name])
		if err != nil {
			return nil, fmt.Errorf("model %s: %w", name, err)
		}
		schema.Description = catalog.docs[name]
		schema.Extensions = map[string]any{
			"x-go-type": "model." + name,
			"x-go-type-import": map[string]any{
				"name": "model",
				"path": catalog.importPath,
			},
		}
		schemas[name] = &openapi3.SchemaRef{Value: schema}
	}
	return schemas, nil
}

func (catalog *modelCatalog) structSchema(structure *ast.StructType) (*openapi3.Schema, error) {
	schema := objectSchema()
	for _, field := range structure.Fields.List {
		if len(field.Names) == 0 {
			return nil, fmt.Errorf("%w: embedded fields are not supported", ErrUnsupportedModelType)
		}
		for _, name := range field.Names {
			if !name.IsExported() {
				continue
			}
			jsonName, omitempty, skip, err := jsonField(name.Name, field.Tag)
			if err != nil {
				return nil, err
			}
			if skip {
				continue
			}
			property, pointer, err := catalog.schemaForExpr(field.Type)
			if err != nil {
				return nil, fmt.Errorf("field %s: %w", name.Name, err)
			}
			if property.Value != nil && field.Doc != nil {
				property.Value.Description = strings.TrimSpace(field.Doc.Text())
			} else if property.Value != nil && field.Comment != nil {
				property.Value.Description = strings.TrimSpace(field.Comment.Text())
			}
			schema.Properties[jsonName] = property
			if !omitempty && !pointer {
				schema.Required = append(schema.Required, jsonName)
			}
		}
	}
	sort.Strings(schema.Required)
	return schema, nil
}

func (catalog *modelCatalog) schemaForExpr(expression ast.Expr) (*openapi3.SchemaRef, bool, error) {
	switch typed := expression.(type) {
	case *ast.StarExpr:
		ref, _, err := catalog.schemaForExpr(typed.X)
		if ref != nil && ref.Value != nil {
			ref.Value.Nullable = true
		}
		return ref, true, err
	case *ast.Ident:
		if schema := primitiveSchema(typed.Name); schema != nil {
			return &openapi3.SchemaRef{Value: schema}, false, nil
		}
		if catalog.Has(typed.Name) {
			return &openapi3.SchemaRef{Ref: "#/components/schemas/" + typed.Name}, false, nil
		}
		return nil, false, fmt.Errorf("%w: %s", ErrUnsupportedModelType, typed.Name)
	case *ast.ArrayType:
		item, _, err := catalog.schemaForExpr(typed.Elt)
		if err != nil {
			return nil, false, err
		}
		return &openapi3.SchemaRef{Value: arraySchema(item)}, false, nil
	case *ast.MapType:
		key, ok := typed.Key.(*ast.Ident)
		if !ok || key.Name != "string" {
			return nil, false, fmt.Errorf("%w: map keys must be string", ErrUnsupportedModelType)
		}
		value, _, err := catalog.schemaForExpr(typed.Value)
		if err != nil {
			return nil, false, err
		}
		schema := objectSchema()
		schema.AdditionalProperties.Schema = value
		return &openapi3.SchemaRef{Value: schema}, false, nil
	case *ast.SelectorExpr:
		packageName, ok := typed.X.(*ast.Ident)
		if ok && packageName.Name == "time" && typed.Sel.Name == "Time" {
			schema := stringSchema()
			schema.Format = "date-time"
			return &openapi3.SchemaRef{Value: schema}, false, nil
		}
	case *ast.StructType:
		schema, err := catalog.structSchema(typed)
		if err != nil {
			return nil, false, err
		}
		return &openapi3.SchemaRef{Value: schema}, false, nil
	}
	return nil, false, fmt.Errorf("%w: %T", ErrUnsupportedModelType, expression)
}

func jsonField(goName string, tag *ast.BasicLit) (name string, omitempty, skip bool, err error) {
	name = goName
	if tag == nil {
		return name, false, false, nil
	}
	value, err := strconv.Unquote(tag.Value)
	if err != nil {
		return "", false, false, fmt.Errorf("invalid struct tag: %w", err)
	}
	jsonTag := reflect.StructTag(value).Get("json")
	if jsonTag == "" {
		return name, false, false, nil
	}
	parts := strings.Split(jsonTag, ",")
	if parts[0] == "-" {
		return "", false, true, nil
	}
	if parts[0] != "" {
		name = parts[0]
	}
	for _, option := range parts[1:] {
		if option == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty, false, nil
}

func goName(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '_' || r == '-' })
	var result strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		result.WriteString(string(runes))
	}
	return result.String()
}

func primitiveSchema(goType string) *openapi3.Schema {
	var schema *openapi3.Schema
	switch goType {
	case "string":
		schema = stringSchema()
	case "bool":
		schema = typedSchema(openapi3.TypeBoolean)
	case "int", "int32":
		schema = typedSchema(openapi3.TypeInteger)
		schema.Format = "int32"
	case "int64":
		schema = typedSchema(openapi3.TypeInteger)
		schema.Format = "int64"
	case "float32":
		schema = typedSchema(openapi3.TypeNumber)
		schema.Format = "float"
	case "float64":
		schema = typedSchema(openapi3.TypeNumber)
		schema.Format = "double"
	}
	return schema
}

func typedSchema(kind string) *openapi3.Schema {
	types := openapi3.Types{kind}
	return &openapi3.Schema{Type: &types}
}

func objectSchema() *openapi3.Schema {
	schema := typedSchema(openapi3.TypeObject)
	schema.Properties = make(openapi3.Schemas)
	return schema
}

func stringSchema() *openapi3.Schema {
	return typedSchema(openapi3.TypeString)
}

func arraySchema(item *openapi3.SchemaRef) *openapi3.Schema {
	schema := typedSchema(openapi3.TypeArray)
	schema.Items = item
	return schema
}
