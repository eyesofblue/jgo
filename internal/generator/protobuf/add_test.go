package protobuf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testContract = `syntax = "proto3";

package demo.v1;

service UserService {
}
`

func TestAddCreatesRPCAndStandardResponse(t *testing.T) {
	root, path := writeContract(t, "demo/v1/service.proto", testContract)
	gotPath, err := Add(AddConfig{Root: root, Service: "UserService", RPC: "GetUser"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if gotPath != "api/proto/demo/v1/service.proto" {
		t.Fatalf("Add() path = %q", gotPath)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantFragments := []string{
		"service UserService {\n  rpc GetUser(GetUserRequest) returns (GetUserResponse);\n}",
		"message GetUserRequest {\n}",
		"message GetUserResponse {\n  int32 code = 1;\n  string msg = 2;\n}",
	}
	for _, fragment := range wantFragments {
		if !strings.Contains(string(contents), fragment) {
			t.Fatalf("contract does not contain %q:\n%s", fragment, contents)
		}
	}
}

func TestAddDoesNotMutateOnConflicts(t *testing.T) {
	tests := []struct {
		name     string
		contract string
		want     string
	}{
		{
			name:     "duplicate RPC",
			contract: strings.Replace(testContract, "service UserService {\n", "service UserService {\n  rpc GetUser(ExistingRequest) returns (ExistingResponse);\n", 1),
			want:     "RPC \"GetUser\" already exists",
		},
		{
			name:     "message collision",
			contract: testContract + "\nmessage GetUserRequest {}\n",
			want:     "message \"GetUserRequest\" already exists",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, path := writeContract(t, "demo/v1/service.proto", test.contract)
			before, _ := os.ReadFile(path)
			_, err := Add(AddConfig{Root: root, Service: "UserService", RPC: "GetUser"})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Add() error = %v, want containing %q", err, test.want)
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(before) {
				t.Fatalf("contract changed after error:\n%s", after)
			}
		})
	}
}

func TestAddRejectsAmbiguousService(t *testing.T) {
	root, _ := writeContract(t, "demo/v1/one.proto", testContract)
	path := filepath.Join(root, filepath.FromSlash("api/proto/demo/v1/two.proto"))
	if err := os.WriteFile(path, []byte(testContract), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Add(AddConfig{Root: root, Service: "UserService", RPC: "GetUser"})
	if err == nil || !strings.Contains(err.Error(), "multiple files") || !strings.Contains(err.Error(), "one.proto") || !strings.Contains(err.Error(), "two.proto") {
		t.Fatalf("Add() error = %v", err)
	}
}

func TestAddSuggestsAvailableServices(t *testing.T) {
	root, _ := writeContract(t, "demo/v1/service.proto", testContract)
	_, err := Add(AddConfig{Root: root, Service: "MissingService", RPC: "GetUser"})
	if err == nil || !strings.Contains(err.Error(), "available services: UserService") {
		t.Fatalf("Add() error = %v", err)
	}
}

func TestAddCanSelectFile(t *testing.T) {
	root, _ := writeContract(t, "demo/v1/one.proto", testContract)
	second := filepath.Join(root, filepath.FromSlash("api/proto/demo/v1/two.proto"))
	if err := os.WriteFile(second, []byte(testContract), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Add(AddConfig{
		Root: root, File: "api/proto/demo/v1/two.proto", Service: "UserService", RPC: "GetUser",
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
}

func TestAddRejectsSelectedFileOutsideProtoRoot(t *testing.T) {
	root, _ := writeContract(t, "demo/v1/service.proto", testContract)
	outside := filepath.Join(root, "outside.proto")
	if err := os.WriteFile(outside, []byte(testContract), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Add(AddConfig{Root: root, File: "outside.proto", Service: "UserService", RPC: "GetUser"})
	if err == nil || !strings.Contains(err.Error(), "must be inside") {
		t.Fatalf("Add() error = %v", err)
	}
}

func TestAddValidatesNames(t *testing.T) {
	root, _ := writeContract(t, "demo/v1/service.proto", testContract)
	for _, rpc := range []string{"", "getUser", "Get-User"} {
		_, err := Add(AddConfig{Root: root, Service: "UserService", RPC: rpc})
		if err == nil {
			t.Fatalf("Add(RPC=%q) unexpectedly succeeded", rpc)
		}
	}
}

func writeContract(t *testing.T, relativePath, contract string) (string, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, filepath.FromSlash("api/proto/"+relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contract), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, path
}
