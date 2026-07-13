package response

import (
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jerrors "github.com/eyesofblue/jgo/errors"
	"github.com/eyesofblue/jgo/middleware/requestid"
)

func TestSuccess(t *testing.T) {
	handler := requestid.New(func() string { return "request-1" })(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := Success(writer, request, map[string]int{"id": 7}); err != nil {
			t.Errorf("Success() error = %v", err)
		}
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	var envelope Envelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || envelope.Code != 0 || envelope.Msg != "" {
		t.Fatalf("response = %d, %+v", response.Code, envelope)
	}
	if response.Header().Get("X-Request-ID") != "request-1" {
		t.Fatalf("X-Request-ID = %q", response.Header().Get("X-Request-ID"))
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || fields["code"] == nil || fields["msg"] == nil || fields["data"] == nil {
		t.Fatalf("response fields = %s, want exactly code/msg/data", response.Body.String())
	}
}

func TestErrorDoesNotExposeUnknownError(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := Error(response, request, stderrors.New("database password")); err != nil {
		t.Fatal(err)
	}

	var envelope Envelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusInternalServerError || envelope.Code != jerrors.CodeInternal || envelope.Msg != jerrors.MessageInternal || envelope.Data != nil {
		t.Fatalf("response = %d, %+v", response.Code, envelope)
	}
	if !strings.Contains(response.Body.String(), `"data":null`) {
		t.Fatalf("error response must contain null data: %s", response.Body.String())
	}
}
