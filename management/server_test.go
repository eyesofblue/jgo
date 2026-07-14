package management

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/eyesofblue/jgo/readiness"
)

func TestHealthAndReadinessEndpoints(t *testing.T) {
	checks := readiness.New(time.Second)
	_ = checks.Add("database", true, readiness.CheckFunc(func(context.Context) error { return errors.New("down") }))
	server, err := New(Config{Address: "127.0.0.1:0", Readiness: checks})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Start(context.Background()) }()
	deadline := time.Now().Add(2 * time.Second)
	for server.Address() == "127.0.0.1:0" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	assertManagementStatus(t, "http://"+server.Address()+"/healthz", http.StatusOK)
	assertManagementStatus(t, "http://"+server.Address()+"/readyz", http.StatusServiceUnavailable)
	if err := server.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServerRejectsInvalidMetricsAndDuplicateStart(t *testing.T) {
	if _, err := New(Config{MetricsEnabled: true}); !errors.Is(err, ErrMetricsHandlerMissing) {
		t.Fatalf("New() error = %v", err)
	}
	server, err := New(Config{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Start(nil) }()
	deadline := time.Now().Add(time.Second)
	for server.Address() == "127.0.0.1:0" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := server.Start(context.Background()); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second Start() error = %v", err)
	}
	if err := server.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func assertManagementStatus(t *testing.T, url string, want int) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != want {
		t.Fatalf("GET %s = %d, want %d", url, response.StatusCode, want)
	}
}
