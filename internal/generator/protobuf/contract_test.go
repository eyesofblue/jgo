package protobuf

import (
	"strings"
	"testing"
)

func TestResponseContractWarnings(t *testing.T) {
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
	warnings, err := ResponseContractWarnings(root)
	if err != nil {
		t.Fatalf("ResponseContractWarnings() error = %v", err)
	}
	joined := strings.Join(warnings, "\n")
	if len(warnings) != 2 || !strings.Contains(joined, "UserService.Missing") || !strings.Contains(joined, "UserService.Optional") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestLastIdentifier(t *testing.T) {
	for input, want := range map[string]string{
		"GetUserResponse":          "GetUserResponse",
		"demo.v1.GetUserResponse":  "GetUserResponse",
		".demo.v1.GetUserResponse": "GetUserResponse",
	} {
		if got := lastIdentifier(input); got != want {
			t.Errorf("lastIdentifier(%q) = %q, want %q", input, got, want)
		}
	}
}
