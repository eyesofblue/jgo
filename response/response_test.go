package response

import (
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jerrors "github.com/eyesofblue/jgo/errors"
	"github.com/eyesofblue/jgo/middleware/traceid"
	"go.opentelemetry.io/otel/trace"
)

func TestSuccess(t *testing.T) {
	handler := traceid.New()(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := Success(writer, request, map[string]int{"id": 7}); err != nil {
			t.Errorf("Success() error = %v", err)
		}
	}))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	traceID, _ := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	spanID, _ := trace.SpanIDFromHex("0123456789abcdef")
	request = request.WithContext(trace.ContextWithSpanContext(request.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	})))
	handler.ServeHTTP(response, request)

	var envelope Envelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || envelope.Code != 0 || envelope.Msg != "" {
		t.Fatalf("response = %d, %+v", response.Code, envelope)
	}
	if response.Header().Get(traceid.Header) != traceID.String() {
		t.Fatalf("X-Trace-ID = %q", response.Header().Get(traceid.Header))
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
