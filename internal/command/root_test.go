package command

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewCommandCreatesProject(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join(testFileDirectory(t), "..", ".."))
	target := filepath.Join(t.TempDir(), "demo")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Execute(&stdout, &stderr, []string{
		"new", "demo",
		"--module", "example.com/demo",
		"--type", "web",
		"--output", target,
		"--jgo-replace", repositoryRoot,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v, stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "created web project demo") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(target, "cmd", "server", "main.go")); err != nil {
		t.Fatalf("generated main: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "go.sum")); err != nil {
		t.Fatalf("generated go.sum: %v", err)
	}
}

func TestNewCommandRequiresModuleAndType(t *testing.T) {
	var output bytes.Buffer
	err := Execute(&output, &output, []string{"new", "demo"})
	if err == nil || !strings.Contains(err.Error(), "required flag") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestNewCommandCanSkipTidy(t *testing.T) {
	target := filepath.Join(t.TempDir(), "offline")
	var output bytes.Buffer
	err := Execute(&output, &output, []string{
		"new", "offline", "--module", "example.com/offline", "--type", "web",
		"--output", target, "--go-version", "1.24.0", "--skip-tidy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "go.sum")); !os.IsNotExist(err) {
		t.Fatalf("go.sum exists with --skip-tidy: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(target, "go.mod"))
	if err != nil || !strings.Contains(string(contents), "go 1.24.0") {
		t.Fatalf("go.mod = %q, %v", contents, err)
	}
}

func TestAPIHelpUsesConfirmedStructFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Execute(&stdout, &stderr, []string{"api", "add", "--help"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	help := stdout.String()
	for _, flag := range []string{"--request-params", "--response-data", "--response-list"} {
		if !strings.Contains(help, flag) {
			t.Fatalf("help does not contain %s:\n%s", flag, help)
		}
	}
	for _, oldFlag := range []string{"--request-type", "--response-type"} {
		if strings.Contains(help, oldFlag) {
			t.Fatalf("help unexpectedly contains old flag %s:\n%s", oldFlag, help)
		}
	}
}

func TestRPCAddCommandUpdatesContract(t *testing.T) {
	root := t.TempDir()
	contract := filepath.Join(root, "api", "proto", "demo", "v1", "service.proto")
	if err := os.MkdirAll(filepath.Dir(contract), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contract, []byte("syntax = \"proto3\";\nservice UserService {\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := Execute(&output, &output, []string{"rpc", "add", "GetUser", "--service", "UserService", "--root", root})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "response code/msg use fields 1/2; add business fields from 3") {
		t.Fatalf("stdout = %q", output.String())
	}
	contents, err := os.ReadFile(contract)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"int32 code = 1;", "string msg = 2;"} {
		if !strings.Contains(string(contents), fragment) {
			t.Fatalf("contract does not contain %q:\n%s", fragment, contents)
		}
	}
}

func TestCallHTTPCommand(t *testing.T) {
	root := t.TempDir()
	writeCommandContract(t, root, "api/http/openapi.yaml", `openapi: 3.0.3
info: {title: test, version: 0.1.0}
paths:
  /get_user:
    get:
      operationId: GetUser
      parameters:
        - {name: uid, in: query, required: true, schema: {type: integer}}
      responses:
        "200": {description: ok}
`)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("uid") != "12345" {
			t.Errorf("query = %s", request.URL.RawQuery)
		}
		_, _ = io.WriteString(writer, `{"code":0,"msg":"","data":{"uid":12345}}`)
	}))
	defer server.Close()
	var output bytes.Buffer
	err := Execute(&output, &output, []string{
		"call", "http", "GetUser", "--root", root, "--addr", server.URL, "--data", `{"uid":12345}`,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "\n  \"code\": 0") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestListCommandShowsHTTPAndGRPCMethods(t *testing.T) {
	root := t.TempDir()
	writeCommandContract(t, root, "api/http/openapi.yaml", `openapi: 3.0.3
info: {title: test, version: 0.1.0}
paths:
  /get_user:
    get:
      operationId: GetUser
      responses:
        "200": {description: ok}
`)
	writeCommandContract(t, root, "api/proto/demo/v1/service.proto", `syntax = "proto3";
package demo.v1;
service UserService {
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
}
message GetUserRequest {}
message GetUserResponse {}
`)
	var output bytes.Buffer
	if err := Execute(&output, &output, []string{"list", "--root", root}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, fragment := range []string{"HTTP  GET", "/get_user", "GetUser", "gRPC  unary", "demo.v1.UserService.GetUser"} {
		if !strings.Contains(output.String(), fragment) {
			t.Fatalf("output does not contain %q:\n%s", fragment, output.String())
		}
	}
}

func TestCallHelpDocumentsSharedFlags(t *testing.T) {
	for _, protocol := range []string{"http", "grpc"} {
		var output bytes.Buffer
		if err := Execute(&output, &output, []string{"call", protocol, "--help"}); err != nil {
			t.Fatal(err)
		}
		for _, flag := range []string{"--addr", "--data", "--header", "--timeout"} {
			if !strings.Contains(output.String(), flag) {
				t.Fatalf("%s help does not contain %s:\n%s", protocol, flag, output.String())
			}
		}
	}
}

func TestRootExposesDeveloperCommandsCompletionAndVersion(t *testing.T) {
	var output bytes.Buffer
	if err := Execute(&output, &output, []string{"--help"}); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"build", "completion", "doctor", "generate", "run", "tools"} {
		if !strings.Contains(output.String(), command) {
			t.Fatalf("root help does not contain %q:\n%s", command, output.String())
		}
	}

	output.Reset()
	if err := Execute(&output, &output, []string{"completion", "bash"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "bash completion V2 for jgo") {
		t.Fatalf("unexpected Bash completion:\n%s", output.String())
	}

	output.Reset()
	if err := Execute(&output, &output, []string{"--version"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "jgo version ") {
		t.Fatalf("version output = %q", output.String())
	}
}

func writeCommandContract(t *testing.T, root, relative, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testFileDirectory(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return workingDirectory
}
