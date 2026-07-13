package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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
