package openapi

import "strings"

const (
	SpecPath     = "api/http/openapi.yaml"
	ModelPath    = "api/http/model"
	GeneratedDir = "gen/http"
)

type AddConfig struct {
	Root         string
	Operation    string
	Method       string
	Path         string
	Request      []string
	RequestType  string
	ResponseType string
	ResponseList bool
}

type Field struct {
	Name     string
	GoName   string
	GoType   string
	Source   string
	Required bool
}

type Operation struct {
	Name         string
	Method       string
	Path         string
	Fields       []Field
	RequestType  string
	ResponseType string
	ResponseList bool
}

func (operation Operation) HasRequest() bool {
	return len(operation.Fields) != 0 || operation.RequestType != ""
}

func (operation Operation) ServiceRequestType() string {
	if operation.RequestType != "" {
		return "model." + operation.RequestType
	}
	if len(operation.Fields) != 0 {
		return operation.Name + "Request"
	}
	return ""
}

func (operation Operation) ServiceResponseType() string {
	if operation.ResponseType == "" {
		return ""
	}
	typeName := operation.ResponseType
	if !isPrimitive(typeName) {
		typeName = "model." + typeName
	}
	if operation.ResponseList {
		return "[]" + typeName
	}
	return typeName
}

func isPrimitive(value string) bool {
	switch strings.TrimSpace(value) {
	case "string", "bool", "int", "int32", "int64", "float32", "float64":
		return true
	default:
		return false
	}
}
