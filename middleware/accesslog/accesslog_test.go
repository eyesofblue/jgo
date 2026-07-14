package accesslog

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestMiddlewareLogsResponseMetadata(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := New(logger)(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte("body"))
	}))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/users", nil)
	request = request.WithContext(trace.ContextWithSpanContext(request.Context(), testSpanContext(t)))
	handler.ServeHTTP(response, request)

	for _, part := range []string{`"method":"POST"`, `"path":"/users"`, `"status":201`, `"bytes":4`, `"trace_id":"0123456789abcdef0123456789abcdef"`, `"span_id":"0123456789abcdef"`} {
		if !strings.Contains(logs.String(), part) {
			t.Fatalf("log %q does not contain %q", logs.String(), part)
		}
	}
}

func testSpanContext(t *testing.T) trace.SpanContext {
	t.Helper()
	traceID, err := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := trace.SpanIDFromHex("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID})
}
