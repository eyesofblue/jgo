package openapi

import (
	"strings"
	"testing"
)

func TestRenderRoutesImportsJSONOnlyForRequestBodies(t *testing.T) {
	queryOnly := Operation{
		Name: "GetUser", Method: "GET", Path: "/get_user",
		Fields: []Field{{Name: "uid", GoName: "Uid", GoType: "int64", Source: "query", Required: true}},
	}
	source, err := renderRoutes("example.com/demo", []Operation{queryOnly})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source), "\"encoding/json\"") {
		t.Fatalf("query-only routes unexpectedly import encoding/json:\n%s", source)
	}

	withBody := queryOnly
	withBody.Name = "UpdateUser"
	withBody.Method = "POST"
	withBody.Path = "/update_user"
	withBody.Fields[0].Source = "body"
	source, err = renderRoutes("example.com/demo", []Operation{withBody})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "\"encoding/json\"") {
		t.Fatalf("body route does not import encoding/json:\n%s", source)
	}
}
