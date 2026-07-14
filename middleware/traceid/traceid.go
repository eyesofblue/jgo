// Package traceid exposes trace and span identifiers stored in OpenTelemetry
// contexts and adds the current trace ID to HTTP responses.
package traceid

import (
	"context"
	"net/http"

	"github.com/eyesofblue/jgo/middleware"
	"go.opentelemetry.io/otel/trace"
)

// Header is the convenience HTTP response header containing the W3C trace ID.
// Cross-process propagation uses the standard traceparent header.
const Header = "X-Trace-ID"

// New creates middleware that exposes the active OpenTelemetry trace ID in
// the HTTP response. It must run inside the OpenTelemetry HTTP handler.
func New() middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if id := FromContext(request.Context()); id != "" {
				writer.Header().Set(Header, id)
			}
			next.ServeHTTP(writer, request)
		})
	}
}

// FromContext returns the active OpenTelemetry trace ID.
func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return ""
	}
	return spanContext.TraceID().String()
}

// SpanIDFromContext returns the active OpenTelemetry span ID.
func SpanIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return ""
	}
	return spanContext.SpanID().String()
}
