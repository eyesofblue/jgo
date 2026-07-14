package protobuf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateResponseContractsRejectsMissingAndOptionalStatusFields(t *testing.T) {
	contract := `syntax = "proto3";
package demo.v1;
service UserService {
  rpc Good(GoodRequest) returns (GoodResponse);
  rpc Missing(MissingRequest) returns (MissingResponse);
  rpc Optional(OptionalRequest) returns (OptionalResponse);
}
message GoodRequest {}
message GoodResponse {
  int32 code = 1;
  string msg = 2;
}
message MissingRequest {}
message MissingResponse {}
message OptionalRequest {}
message OptionalResponse {
  optional int32 code = 1;
  string msg = 2;
}
`
	root, _ := writeContract(t, "demo/v1/service.proto", contract)
	err := ValidateResponseContracts(root)
	if err == nil || !strings.Contains(err.Error(), "UserService.Missing") || !strings.Contains(err.Error(), "UserService.Optional") {
		t.Fatalf("ValidateResponseContracts() error = %v", err)
	}
}

func TestValidateResponseContractsChecksImportedResponse(t *testing.T) {
	serviceContract := `syntax = "proto3";
package demo.v1;
import "demo/v1/common.proto";
service UserService {
  rpc GetUser(GetUserRequest) returns (SharedResponse);
}
message GetUserRequest {}
`
	root, _ := writeContract(t, "demo/v1/service.proto", serviceContract)
	_, _ = writeContractAtRoot(t, root, "demo/v1/common.proto", `syntax = "proto3";
package demo.v1;
message SharedResponse {}
`)
	err := ValidateResponseContracts(root)
	if err == nil || !strings.Contains(err.Error(), "demo.v1.UserService.GetUser") || !strings.Contains(err.Error(), "demo.v1.SharedResponse") {
		t.Fatalf("ValidateResponseContracts() error = %v", err)
	}
}

func writeContractAtRoot(t *testing.T, root, relativePath, contract string) (string, string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash("api/proto/"+relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contract), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, path
}
