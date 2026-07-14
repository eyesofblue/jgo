package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbe(t *testing.T) {
	dependencyReady := false
	probe := New(func(context.Context) error {
		if dependencyReady {
			return nil
		}
		return errors.New("dependency unavailable")
	})
	mux := http.NewServeMux()
	if err := probe.Register(mux); err != nil {
		t.Fatal(err)
	}

	assertStatus(t, mux, "/healthz", http.StatusOK)
	assertStatus(t, mux, "/readyz", http.StatusServiceUnavailable)

	probe.SetReady(true)
	assertStatus(t, mux, "/readyz", http.StatusServiceUnavailable)

	dependencyReady = true
	assertStatus(t, mux, "/readyz", http.StatusOK)

	probe.SetReady(false)
	assertStatus(t, mux, "/readyz", http.StatusServiceUnavailable)
}

func TestReadinessHasHardTimeoutAndIsolatesPanics(t *testing.T) {
	blocked := make(chan struct{})
	probe := NewWithTimeout(20*time.Millisecond,
		func(context.Context) error { <-blocked; return nil },
		func(context.Context) error { panic("secret panic value") },
	)
	probe.SetReady(true)
	started := time.Now()
	recorder := httptest.NewRecorder()
	probe.Readiness(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", recorder.Code)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("readiness exceeded hard timeout: %s", elapsed)
	}
	close(blocked)
}

func TestRegisterRejectsNilMux(t *testing.T) {
	if err := New().Register(nil); !errors.Is(err, ErrNilMux) {
		t.Fatalf("Register(nil) error = %v", err)
	}
}

func assertStatus(t *testing.T, handler http.Handler, path string, want int) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != want {
		t.Fatalf("GET %s status = %d, want %d", path, recorder.Code, want)
	}
}
