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

func TestAddServiceCreatesFirstAndAdditionalPackages(t *testing.T) {
	root := filepath.Join(t.TempDir(), "company-api")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/company-api\n\ngo 1.24.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := AddService(AddServiceConfig{Root: root, Service: "UserService"})
	if err != nil {
		t.Fatal(err)
	}
	if first != "api/proto/company_api/v1/service.proto" {
		t.Fatalf("first path = %q", first)
	}
	assertProtoContains(t, filepath.Join(root, filepath.FromSlash(first)), "package company_api.v1;", "service UserService")
	second, err := AddService(AddServiceConfig{Root: root, Package: "company.order.v1", Service: "OrderService"})
	if err != nil {
		t.Fatal(err)
	}
	if second != "api/proto/company/order/v1/service.proto" {
		t.Fatalf("second path = %q", second)
	}
	assertProtoContains(t, filepath.Join(root, filepath.FromSlash(second)), "package company.order.v1;", "example.com/company-api/gen/pb/company/order/v1", "service OrderService")
	if _, err := AddService(AddServiceConfig{Root: root, Package: "company.order.v1", Service: "AdminService"}); err != nil {
		t.Fatal(err)
	}
	assertProtoContains(t, filepath.Join(root, filepath.FromSlash(second)), "service AdminService")
}

func TestAddServiceNeverOverwritesExistingPackageTarget(t *testing.T) {
	root := filepath.Join(t.TempDir(), "company-api")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/company-api\n\ngo 1.24.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "api", "proto", "company", "user", "v2", "service.proto")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("syntax = \"proto3\";\npackage accidental.v1;\n")
	if err := os.WriteFile(target, original, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := AddService(AddServiceConfig{Root: root, Package: "company.user.v2", Service: "UserService"})
	if err == nil || !strings.Contains(err.Error(), "refuse to overwrite") {
		t.Fatalf("AddService() error = %v", err)
	}
	contents, readErr := os.ReadFile(target)
	if readErr != nil || string(contents) != string(original) {
		t.Fatalf("target changed: %v\n%s", readErr, contents)
	}
}

func TestAddServiceHandlesNumericProjectAndGoKeywordPackage(t *testing.T) {
	root := filepath.Join(t.TempDir(), "123-api")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/numbered\n\ngo 1.24.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := AddService(AddServiceConfig{Root: root, Service: "NumberService"})
	if err != nil {
		t.Fatal(err)
	}
	assertProtoContains(t, filepath.Join(root, filepath.FromSlash(first)), "package app_123_api.v1;")
	keyword, err := AddService(AddServiceConfig{Root: root, Package: "type", Service: "KeywordService"})
	if err != nil {
		t.Fatal(err)
	}
	assertProtoContains(t, filepath.Join(root, filepath.FromSlash(keyword)), `option go_package = "example.com/numbered/gen/pb/type;pb";`)
}

func TestAddServicePackageSelectionRejectsMultipleFiles(t *testing.T) {
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
	_, err := AddService(AddServiceConfig{Root: root, Package: "demo.v1", Service: "AdminService"})
	if err == nil || !strings.Contains(err.Error(), "spans multiple files") || !strings.Contains(err.Error(), "--file") {
		t.Fatalf("AddService() error = %v", err)
	}
}

func TestAddServiceRejectsSymlinkedProtoTree(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n\ngo 1.24.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "api", "proto")); err != nil {
		t.Fatal(err)
	}
	if _, err := AddService(AddServiceConfig{Root: root, Service: "UserService"}); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("AddService() error = %v", err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("outside directory changed: entries=%v err=%v", entries, err)
	}
}

func assertProtoContains(t *testing.T, path string, fragments ...string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range fragments {
		if !strings.Contains(string(contents), fragment) {
			t.Fatalf("%s does not contain %q:\n%s", path, fragment, contents)
		}
	}
}
