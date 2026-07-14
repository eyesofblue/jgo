package logx

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestLoggerAddsTraceFields(t *testing.T) {
	var output bytes.Buffer
	logger := New(slog.New(slog.NewJSONHandler(&output, nil)))
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext(t))

	logger.InfoCtx(ctx, "user loaded", "uid", int64(12345))

	for _, value := range []string{
		`"msg":"user loaded"`,
		`"uid":12345`,
		`"trace_id":"0123456789abcdef0123456789abcdef"`,
		`"span_id":"0123456789abcdef"`,
	} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("log %q does not contain %q", output.String(), value)
		}
	}
}

func TestLoggerWithoutSpanDoesNotAddEmptyFields(t *testing.T) {
	var output bytes.Buffer
	New(slog.New(slog.NewJSONHandler(&output, nil))).ErrorCtx(nil, "failed", "err", "boom")
	if strings.Contains(output.String(), "trace_id") || strings.Contains(output.String(), "span_id") {
		t.Fatalf("unexpected trace fields: %s", output.String())
	}
}

func TestLoggerPreservesExplicitTraceFieldsWithMixedArguments(t *testing.T) {
	var output bytes.Buffer
	logger := New(slog.New(slog.NewJSONHandler(&output, nil)))
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext(t))

	logger.InfoCtx(ctx, "explicit trace", slog.String("component", "test"), "trace_id", "caller-trace")

	if strings.Count(output.String(), `"trace_id"`) != 1 || !strings.Contains(output.String(), `"trace_id":"caller-trace"`) {
		t.Fatalf("explicit trace field was duplicated or replaced: %s", output.String())
	}
}

func spanContext(t *testing.T) trace.SpanContext {
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
