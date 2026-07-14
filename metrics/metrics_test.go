package metrics

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jgoerrors "github.com/eyesofblue/jgo/errors"
	"github.com/eyesofblue/jgo/response"
	"github.com/eyesofblue/jgo/server/httpx"
)

func TestHTTPMetricsCaptureBoundedBusinessCode(t *testing.T) {
	catalog := jgoerrors.MustCatalog(jgoerrors.Define(40401, "NOT_FOUND", "not found", http.StatusNotFound))
	serviceMetrics, err := New(context.Background(), OTLPConfig{}, catalog)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /get_user", func(writer http.ResponseWriter, request *http.Request) {
		_ = response.Error(writer, request, catalog.FromCode(40401, "not found"))
	})
	handler := serviceMetrics.HTTPMiddleware(mux)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/get_user", nil))
	metricsRecorder := httptest.NewRecorder()
	serviceMetrics.Handler().ServeHTTP(metricsRecorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body, _ := io.ReadAll(metricsRecorder.Result().Body)
	text := string(body)
	if !strings.Contains(text, `business_code="40401"`) || !strings.Contains(text, `route="/get_user"`) {
		t.Fatalf("metrics:\n%s", text)
	}
}

func TestHTTPMetricsDoNotUseRawPathAsRoute(t *testing.T) {
	serviceMetrics, err := New(context.Background(), OTLPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handler := serviceMetrics.HTTPMiddleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/private-id", nil))
	metricsRecorder := httptest.NewRecorder()
	serviceMetrics.Handler().ServeHTTP(metricsRecorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body, _ := io.ReadAll(metricsRecorder.Result().Body)
	text := string(body)
	if strings.Contains(text, "private-id") || !strings.Contains(text, `route="unknown"`) {
		t.Fatalf("metrics:\n%s", text)
	}
}

func TestHTTPMetricsKeepFirstStatusCode(t *testing.T) {
	serviceMetrics, err := New(context.Background(), OTLPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusCreated)
		writer.WriteHeader(http.StatusInternalServerError)
	})
	serviceMetrics.HTTPMiddleware(mux).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/status", nil))
	metricsRecorder := httptest.NewRecorder()
	serviceMetrics.Handler().ServeHTTP(metricsRecorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body, _ := io.ReadAll(metricsRecorder.Result().Body)
	text := string(body)
	if !strings.Contains(text, `status="201"`) || strings.Contains(text, `status="500"`) {
		t.Fatalf("metrics:\n%s", text)
	}
}

func TestNewMergesAllCatalogsAndRejectsConflicts(t *testing.T) {
	first := jgoerrors.MustCatalog(jgoerrors.Define(41001, "USER_MISSING", "user missing", http.StatusNotFound))
	second := jgoerrors.MustCatalog(jgoerrors.Define(42001, "ORDER_MISSING", "order missing", http.StatusNotFound))
	serviceMetrics, err := New(context.Background(), OTLPConfig{}, first, second)
	if err != nil {
		t.Fatal(err)
	}
	if got := serviceMetrics.normalizeBusinessCode(42001); got != "42001" {
		t.Fatalf("normalizeBusinessCode() = %q", got)
	}
	conflict := jgoerrors.MustCatalog(jgoerrors.Define(41001, "OTHER", "other", http.StatusConflict))
	if _, err := New(context.Background(), OTLPConfig{}, first, conflict); err == nil {
		t.Fatal("conflicting catalogs were accepted")
	}
}

type panickingBusinessCode struct{}

func (*panickingBusinessCode) GetCode() int32 { panic("broken getter") }

func TestBusinessCodeObserverCannotPanicRPC(t *testing.T) {
	serviceMetrics, err := New(context.Background(), OTLPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if got := serviceMetrics.businessCode((*panickingBusinessCode)(nil)); got != "unknown" {
		t.Fatalf("businessCode() = %q", got)
	}
}

func TestHTTPMetricsObserveFinalBusinessTimeoutAndPanicResponses(t *testing.T) {
	catalog := jgoerrors.MustCatalog(
		jgoerrors.Define(40401, "NOT_FOUND", "not found", http.StatusNotFound),
		jgoerrors.Define(jgoerrors.CodeInternal, "INTERNAL", jgoerrors.MessageInternal, http.StatusInternalServerError),
		jgoerrors.Define(jgoerrors.CodeTimeout, "TIMEOUT", jgoerrors.MessageTimeout, http.StatusGatewayTimeout),
	)
	serviceMetrics, err := New(context.Background(), OTLPConfig{}, catalog)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /business", func(writer http.ResponseWriter, request *http.Request) {
		_ = response.Error(writer, request, catalog.FromCode(40401, "not found"))
	})
	mux.HandleFunc("GET /slow", func(_ http.ResponseWriter, request *http.Request) {
		response.ObserveRoute(request)
		<-request.Context().Done()
	})
	mux.HandleFunc("GET /panic", func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	server, err := httpx.New(
		httpx.WithAddress("127.0.0.1:0"),
		httpx.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		httpx.WithRequestTimeout(10*time.Millisecond),
		httpx.WithHandler(mux),
		httpx.WithOuterMiddleware(serviceMetrics.HTTPMiddleware),
	)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Start(context.Background()) }()
	deadline := time.Now().Add(time.Second)
	for server.Address() == "127.0.0.1:0" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	for path, want := range map[string]int{"/business": http.StatusNotFound, "/slow": http.StatusGatewayTimeout, "/panic": http.StatusInternalServerError} {
		result, requestErr := http.Get("http://" + server.Address() + path)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		_ = result.Body.Close()
		if result.StatusCode != want {
			t.Fatalf("GET %s = %d, want %d", path, result.StatusCode, want)
		}
	}
	if err := server.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	metricsRecorder := httptest.NewRecorder()
	serviceMetrics.Handler().ServeHTTP(metricsRecorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body, _ := io.ReadAll(metricsRecorder.Result().Body)
	text := string(body)
	for _, labels := range []string{
		"business_code=\"40401\",method=\"GET\",route=\"/business\",status=\"404\"",
		"business_code=\"90002\",method=\"GET\",route=\"/slow\",status=\"504\"",
		"business_code=\"90001\",method=\"GET\",route=\"/panic\",status=\"500\"",
	} {
		if !strings.Contains(text, labels) {
			t.Fatalf("metrics missing %q:\n%s", labels, text)
		}
	}
}
