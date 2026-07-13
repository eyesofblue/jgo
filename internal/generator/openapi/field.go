package openapi

import (
	"fmt"
	"regexp"
	"strings"
)

var fieldNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

func parseFields(values []string, method string) ([]Field, error) {
	fields := make([]Field, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	defaultSource := "query"
	if method == "POST" {
		defaultSource = "body"
	}
	for _, value := range values {
		parts := strings.Split(value, ":")
		if len(parts) < 2 || len(parts) > 4 {
			return nil, fmt.Errorf("%w: %q must be name:type[:required|optional][:query|header|body]", ErrInvalidField, value)
		}
		field := Field{
			Name:   strings.TrimSpace(parts[0]),
			GoType: strings.TrimSpace(parts[1]),
			Source: defaultSource,
		}
		field.GoName = goName(field.Name)
		if !fieldNamePattern.MatchString(field.Name) || field.GoName == "" || primitiveSchema(field.GoType) == nil {
			return nil, fmt.Errorf("%w: %q", ErrInvalidField, value)
		}
		for _, qualifier := range parts[2:] {
			switch strings.ToLower(strings.TrimSpace(qualifier)) {
			case "required":
				field.Required = true
			case "optional":
				field.Required = false
			case "query", "header", "body":
				field.Source = strings.ToLower(strings.TrimSpace(qualifier))
			default:
				return nil, fmt.Errorf("%w: unknown qualifier %q", ErrInvalidField, qualifier)
			}
		}
		if field.Source == "body" && method != "POST" {
			return nil, fmt.Errorf("%w: body fields require POST", ErrInvalidField)
		}
		if _, exists := seen[field.Name]; exists {
			return nil, fmt.Errorf("%w: duplicate field %q", ErrInvalidField, field.Name)
		}
		seen[field.Name] = struct{}{}
		fields = append(fields, field)
	}
	return fields, nil
}
