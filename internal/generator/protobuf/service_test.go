package protobuf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddService(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "api", "proto", "demo", "v1", "service.proto")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := `syntax = "proto3";
package demo.v1;
service DemoService {
}
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := AddService(AddServiceConfig{Root: root, Service: "UserService"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "api/proto/demo/v1/service.proto" {
		t.Fatalf("AddService() path = %q", got)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "service UserService {\n}") {
		t.Fatalf("service missing:\n%s", updated)
	}
	if _, err := AddService(AddServiceConfig{Root: root, Service: "UserService"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate AddService() error = %v", err)
	}
}

func TestAddServiceRequiresFileWhenContractHasMultipleFiles(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"api/proto/demo/v1/user.proto", "api/proto/demo/v1/order.proto"} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("syntax = \"proto3\";\npackage demo.v1;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := AddService(AddServiceConfig{Root: root, Service: "UserService"}); err == nil || !strings.Contains(err.Error(), "select the target with --file") {
		t.Fatalf("AddService() error = %v", err)
	}
	if _, err := AddService(AddServiceConfig{
		Root: root, File: "api/proto/demo/v1/user.proto", Service: "UserService",
	}); err != nil {
		t.Fatal(err)
	}
}
