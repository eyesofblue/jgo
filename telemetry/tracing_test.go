package telemetry

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
)

func TestTracingCreatesContextWithoutExporter(t *testing.T) {
	tracing, err := NewTracing(context.Background(), TracingConfig{ServiceName: "test-service"})
	if err != nil {
		t.Fatal(err)
	}
	defer tracing.Stop(context.Background())

	ctx, span := otel.Tracer("test").Start(context.Background(), "operation")
	defer span.End()
	if !span.SpanContext().IsValid() || !span.SpanContext().TraceID().IsValid() {
		t.Fatalf("invalid span context: %+v", span.SpanContext())
	}
	if span.IsRecording() {
		t.Fatal("span should not be recorded when export is disabled")
	}
	if ctx == nil {
		t.Fatal("context is nil")
	}
	if err := tracing.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := tracing.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStopUnblocksStart(t *testing.T) {
	tracing, err := NewTracing(context.Background(), TracingConfig{ServiceName: "lifecycle-test"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- tracing.Start(context.Background()) }()
	if err := tracing.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start did not return after Stop")
	}
}

func TestTracingValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config TracingConfig
		part   string
	}{
		{name: "service name", config: TracingConfig{}, part: "service name"},
		{name: "sample ratio", config: TracingConfig{ServiceName: "test", SampleRatio: 2}, part: "sample ratio"},
		{name: "endpoint", config: TracingConfig{ServiceName: "test", Exporter: OTLPConfig{Enabled: true}}, part: "endpoint"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewTracing(context.Background(), test.config)
			if err == nil || !strings.Contains(err.Error(), test.part) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
