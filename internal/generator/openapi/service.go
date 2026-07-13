package openapi

import (
	"bytes"
	"fmt"
	"go/format"
	"regexp"
	"strings"
)

var (
	firstCapPattern = regexp.MustCompile(`(.)([A-Z][a-z]+)`)
	allCapPattern   = regexp.MustCompile(`([a-z0-9])([A-Z])`)
)

func renderServiceStub(modulePath string, operation Operation) ([]byte, error) {
	var output bytes.Buffer
	output.WriteString("package service\n\n")
	imports := []string{"\"context\"", "\"errors\""}
	if operation.RequestType != "" || (operation.ResponseType != "" && !isPrimitive(operation.ResponseType)) {
		imports = append(imports, fmt.Sprintf("%q", modulePath+"/"+ModelPath))
	}
	output.WriteString("import (\n\t" + strings.Join(imports, "\n\t") + "\n)\n\n")
	if len(operation.Fields) != 0 {
		fmt.Fprintf(&output, "// %sRequest contains the business inputs for %s.\n", operation.Name, operation.Name)
		fmt.Fprintf(&output, "type %sRequest struct {\n", operation.Name)
		for _, field := range operation.Fields {
			goType := field.GoType
			if !field.Required {
				goType = "*" + goType
			}
			fmt.Fprintf(&output, "\t%s %s `json:%q`\n", field.GoName, goType, field.Name)
		}
		output.WriteString("}\n\n")
	}

	requestType := operation.ServiceRequestType()
	responseType := operation.ServiceResponseType()
	fmt.Fprintf(&output, "// %s implements the %s business operation.\n", operation.Name, operation.Name)
	fmt.Fprintf(&output, "func (service *Service) %s(ctx context.Context", operation.Name)
	if requestType != "" {
		fmt.Fprintf(&output, ", request %s", requestType)
	}
	output.WriteString(") ")
	if responseType == "" {
		output.WriteString("error {\n")
		output.WriteString("\t_ = service\n\t_ = ctx\n")
		if requestType != "" {
			output.WriteString("\t_ = request\n")
		}
		output.WriteString("\treturn errors.New(\"not implemented\")\n}\n")
	} else {
		fmt.Fprintf(&output, "(%s, error) {\n", responseType)
		output.WriteString("\t_ = service\n\t_ = ctx\n")
		if requestType != "" {
			output.WriteString("\t_ = request\n")
		}
		fmt.Fprintf(&output, "\tvar zero %s\n", responseType)
		output.WriteString("\treturn zero, errors.New(\"not implemented\")\n}\n")
	}
	formatted, err := format.Source(output.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format service implementation: %w\n%s", err, output.String())
	}
	return formatted, nil
}

func snakeCase(value string) string {
	value = firstCapPattern.ReplaceAllString(value, `${1}_${2}`)
	value = allCapPattern.ReplaceAllString(value, `${1}_${2}`)
	return strings.ToLower(value)
}
