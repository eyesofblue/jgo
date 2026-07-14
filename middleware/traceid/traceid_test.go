package traceid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestMiddlewareExposesTraceID(t *testing.T) {
	spanContext := testSpanContext(t)
	handler := New()(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		if got := FromContext(request.Context()); got != spanContext.TraceID().String() {
			t.Fatalf("trace ID = %q", got)
		}
		if got := SpanIDFromContext(request.Context()); got != spanContext.SpanID().String() {
			t.Fatalf("span ID = %q", got)
		}
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request = request.WithContext(trace.ContextWithSpanContext(context.Background(), spanContext))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if got := response.Header().Get(Header); got != spanContext.TraceID().String() {
		t.Fatalf("response trace ID = %q", got)
	}
}

func TestFromContextRejectsMissingSpan(t *testing.T) {
	if got := FromContext(context.Background()); got != "" {
		t.Fatalf("trace ID = %q", got)
	}
	if got := SpanIDFromContext(nil); got != "" {
		t.Fatalf("span ID = %q", got)
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
