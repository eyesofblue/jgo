// Package metrics provides bounded-cardinality RED metrics for JGO transports.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eyesofblue/jgo/app"
	jgoerrors "github.com/eyesofblue/jgo/errors"
	"github.com/eyesofblue/jgo/response"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

var _ app.Component = (*Metrics)(nil)

type OTLPConfig struct {
	Enabled     bool
	Endpoint    string
	Insecure    bool
	ServiceName string
}

type Metrics struct {
	registry         *prometheus.Registry
	httpRequests     *prometheus.CounterVec
	httpDuration     *prometheus.HistogramVec
	grpcRequests     *prometheus.CounterVec
	grpcDuration     *prometheus.HistogramVec
	catalog          *jgoerrors.Catalog
	provider         *sdkmetric.MeterProvider
	otelHTTPRequests otelmetric.Int64Counter
	otelHTTPDuration otelmetric.Float64Histogram
	otelGRPCRequests otelmetric.Int64Counter
	otelGRPCDuration otelmetric.Float64Histogram
}

func New(ctx context.Context, otlp OTLPConfig, catalogs ...*jgoerrors.Catalog) (*Metrics, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	metrics := &Metrics{
		registry:     registry,
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "jgo_http_server_requests_total", Help: "HTTP requests handled by the service."}, []string{"method", "route", "status", "business_code"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "jgo_http_server_request_duration_seconds", Help: "HTTP request duration.", Buckets: prometheus.DefBuckets}, []string{"method", "route"}),
		grpcRequests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "jgo_grpc_server_requests_total", Help: "gRPC requests handled by the service."}, []string{"method", "grpc_code", "business_code"}),
		grpcDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "jgo_grpc_server_request_duration_seconds", Help: "gRPC request duration.", Buckets: prometheus.DefBuckets}, []string{"method"}),
	}
	if len(catalogs) > 0 {
		catalog, err := jgoerrors.MergeCatalogs(catalogs...)
		if err != nil {
			return nil, fmt.Errorf("metrics: merge error catalogs: %w", err)
		}
		metrics.catalog = catalog
	}
	registry.MustRegister(metrics.httpRequests, metrics.httpDuration, metrics.grpcRequests, metrics.grpcDuration)
	if otlp.Enabled {
		endpoint := strings.TrimSpace(otlp.Endpoint)
		if endpoint == "" {
			return nil, errors.New("metrics: OTLP endpoint is required")
		}
		options := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(endpoint)}
		if otlp.Insecure {
			options = append(options, otlpmetricgrpc.WithInsecure())
		}
		exporter, err := otlpmetricgrpc.New(ctx, options...)
		if err != nil {
			return nil, err
		}
		res, err := resource.New(ctx, resource.WithAttributes(attribute.String("service.name", otlp.ServiceName)))
		if err != nil {
			return nil, err
		}
		metrics.provider = sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)), sdkmetric.WithResource(res))
		meter := metrics.provider.Meter("github.com/eyesofblue/jgo")
		metrics.otelHTTPRequests, _ = meter.Int64Counter("jgo.http.server.requests")
		metrics.otelHTTPDuration, _ = meter.Float64Histogram("jgo.http.server.request.duration", otelmetric.WithUnit("s"))
		metrics.otelGRPCRequests, _ = meter.Int64Counter("jgo.grpc.server.requests")
		metrics.otelGRPCDuration, _ = meter.Float64Histogram("jgo.grpc.server.request.duration", otelmetric.WithUnit("s"))
	}
	return metrics, nil
}

func (metrics *Metrics) Name() string { return "metrics" }
func (metrics *Metrics) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	<-ctx.Done()
	return nil
}
func (metrics *Metrics) Stop(ctx context.Context) error {
	if metrics.provider == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return metrics.provider.Shutdown(ctx)
}
func (metrics *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(metrics.registry, promhttp.HandlerOpts{})
}

func (metrics *Metrics) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: writer, status: http.StatusOK}
		request = response.WithObserver(request, recorder)
		next.ServeHTTP(recorder, request)
		businessValue, route := recorder.snapshot()
		if route == "" {
			route = request.Pattern
		}
		route = strings.TrimPrefix(route, request.Method+" ")
		if route == "" {
			route = "unknown"
		}
		duration := time.Since(started).Seconds()
		businessCode := metrics.normalizeBusinessCode(businessValue)
		metrics.httpRequests.WithLabelValues(request.Method, route, strconv.Itoa(recorder.status), businessCode).Inc()
		metrics.httpDuration.WithLabelValues(request.Method, route).Observe(duration)
		if metrics.otelHTTPRequests != nil {
			metrics.otelHTTPRequests.Add(request.Context(), 1, otelmetric.WithAttributes(attribute.String("http.request.method", request.Method), attribute.String("http.route", route), attribute.Int("http.response.status_code", recorder.status), attribute.String("jgo.business_code", businessCode)))
			metrics.otelHTTPDuration.Record(request.Context(), duration, otelmetric.WithAttributes(attribute.String("http.request.method", request.Method), attribute.String("http.route", route)))
		}
	})
}

func (metrics *Metrics) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		started := time.Now()
		response, err := handler(ctx, request)
		businessCode := metrics.businessCode(response)
		duration, grpcCode := time.Since(started).Seconds(), status.Code(err).String()
		metrics.grpcRequests.WithLabelValues(info.FullMethod, grpcCode, businessCode).Inc()
		metrics.grpcDuration.WithLabelValues(info.FullMethod).Observe(duration)
		if metrics.otelGRPCRequests != nil {
			metrics.otelGRPCRequests.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("rpc.method", info.FullMethod), attribute.String("rpc.grpc.status_code", grpcCode), attribute.String("jgo.business_code", businessCode)))
			metrics.otelGRPCDuration.Record(ctx, duration, otelmetric.WithAttributes(attribute.String("rpc.method", info.FullMethod)))
		}
		return response, err
	}
}

func (metrics *Metrics) businessCode(response any) (code string) {
	code = "0"
	// Metrics must never turn an otherwise valid RPC response into a panic.
	// Generated protobuf getters are nil-safe, but custom response wrappers may
	// not be, so isolate observer failures from transport behavior.
	defer func() {
		if recover() != nil {
			code = "unknown"
		}
	}()
	value, ok := response.(interface{ GetCode() int32 })
	if !ok {
		return code
	}
	valueCode := value.GetCode()
	if valueCode == 0 {
		return code
	}
	return metrics.normalizeBusinessCode(int(valueCode))
}

func (metrics *Metrics) normalizeBusinessCode(code int) string {
	if code == 0 {
		return "0"
	}
	if metrics.catalog != nil {
		if _, known := metrics.catalog.LookupCode(code); known {
			return strconv.Itoa(code)
		}
	}
	return "unknown"
}

type statusRecorder struct {
	http.ResponseWriter
	mu           sync.Mutex
	status       int
	businessCode int
	route        string
	finalized    bool
	wroteHeader  bool
}

func (recorder *statusRecorder) WriteHeader(status int) {
	if recorder.wroteHeader {
		return
	}
	recorder.wroteHeader = true
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

func (recorder *statusRecorder) SetBusinessCode(code int) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if !recorder.finalized {
		recorder.businessCode = code
	}
}

func (recorder *statusRecorder) SetRoute(route string) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if !recorder.finalized && recorder.route == "" && route != "" {
		recorder.route = route
	}
}

func (recorder *statusRecorder) snapshot() (int, string) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.finalized = true
	return recorder.businessCode, recorder.route
}
