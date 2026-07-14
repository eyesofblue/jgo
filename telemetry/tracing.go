package telemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/eyesofblue/jgo/app"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

const (
	componentName = "telemetry-tracing"
)

var _ app.Component = (*Tracing)(nil)

// TracingConfig configures trace creation, sampling, and optional OTLP export.
// Trace contexts remain active when Exporter.Enabled is false; spans are then
// not recorded or sent outside the process.
type TracingConfig struct {
	ServiceName string
	SampleRatio float64
	Exporter    OTLPConfig
}

// OTLPConfig configures the OTLP/gRPC trace exporter.
type OTLPConfig struct {
	Enabled  bool
	Endpoint string
	Insecure bool
	Headers  map[string]string
}

// Tracing owns the process-wide OpenTelemetry tracer provider.
type Tracing struct {
	provider *sdktrace.TracerProvider
	done     chan struct{}
	stopOnce sync.Once
	stopErr  error
}

// NewTracing installs a process-wide tracer provider and W3C propagator.
func NewTracing(ctx context.Context, config TracingConfig) (*Tracing, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	config.ServiceName = strings.TrimSpace(config.ServiceName)
	if config.ServiceName == "" {
		return nil, errors.New("telemetry: service name is required")
	}
	if config.SampleRatio < 0 || config.SampleRatio > 1 {
		return nil, errors.New("telemetry: sample ratio must be between 0 and 1")
	}

	options := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(config.ServiceName),
		)),
	}
	if !config.Exporter.Enabled {
		options = append(options, sdktrace.WithSampler(sdktrace.NeverSample()))
	} else {
		exporter, err := newExporter(ctx, config.Exporter)
		if err != nil {
			return nil, err
		}
		options = append(options,
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(config.SampleRatio))),
			sdktrace.WithBatcher(exporter),
		)
	}

	provider := sdktrace.NewTracerProvider(options...)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(Propagator())
	return &Tracing{provider: provider, done: make(chan struct{})}, nil
}

func newExporter(ctx context.Context, config OTLPConfig) (*otlptrace.Exporter, error) {
	endpoint := strings.TrimSpace(config.Endpoint)
	if endpoint == "" {
		return nil, errors.New("telemetry: OTLP endpoint is required when export is enabled")
	}
	options := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(endpoint)}
	if config.Insecure {
		options = append(options, otlptracegrpc.WithInsecure())
	}
	if len(config.Headers) > 0 {
		options = append(options, otlptracegrpc.WithHeaders(config.Headers))
	}
	exporter, err := otlptracegrpc.New(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("telemetry: create OTLP exporter: %w", err)
	}
	return exporter, nil
}

func (t *Tracing) Name() string { return componentName }

// Start waits for application cancellation. Provider setup happens in
// NewTracing so HTTP and gRPC components may safely start concurrently.
func (t *Tracing) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
	case <-t.done:
	}
	return nil
}

// Stop flushes pending spans and shuts down the exporter exactly once.
func (t *Tracing) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	t.stopOnce.Do(func() {
		close(t.done)
		t.stopErr = t.provider.Shutdown(ctx)
	})
	return t.stopErr
}
