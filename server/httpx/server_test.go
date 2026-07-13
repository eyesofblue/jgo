package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/eyesofblue/jgo/app"
	"github.com/eyesofblue/jgo/response"
)

func TestServerRunsAsAppComponent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(writer http.ResponseWriter, request *http.Request) {
		_ = response.Success(writer, request, map[string]string{"message": "hello"})
	})
	server, err := New(
		WithAddress("127.0.0.1:0"),
		WithHandler(mux),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
	if err != nil {
		t.Fatal(err)
	}

	application := app.New(app.WithShutdownTimeout(time.Second))
	if err := application.Add(server); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- application.Run(ctx) }()

	address := waitForAddress(t, server)
	responseValue, err := http.Get("http://" + address + "/hello")
	if err != nil {
		t.Fatal(err)
	}
	defer responseValue.Body.Close()
	if responseValue.StatusCode != http.StatusOK || responseValue.Header.Get("X-Request-ID") == "" {
		t.Fatalf("status = %d, request ID = %q", responseValue.StatusCode, responseValue.Header.Get("X-Request-ID"))
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("application did not stop")
	}
}

func TestDefaultMiddlewareRecoversPanics(t *testing.T) {
	server, err := New(
		WithAddress("127.0.0.1:0"),
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Start(ctx) }()
	address := waitForAddress(t, server)

	result, err := http.Get("http://" + address)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	var envelope response.Envelope
	if err := json.NewDecoder(result.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusInternalServerError || envelope.Msg != "internal server error" {
		t.Fatalf("response = %d, %+v", result.StatusCode, envelope)
	}

	if err := server.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServerValidatesConfig(t *testing.T) {
	tests := []struct {
		name string
		opt  Option
		want error
	}{
		{name: "name", opt: WithName(" "), want: ErrInvalidName},
		{name: "address", opt: WithAddress(" "), want: ErrInvalidAddress},
		{name: "handler", opt: WithHandler(nil), want: ErrNilHandler},
		{name: "timeout", opt: WithReadTimeout(0), want: ErrInvalidTimeout},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(test.opt)
			if !errors.Is(err, test.want) {
				t.Fatalf("New() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestStopBeforeStartPreventsListener(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("Start() after Stop() error = %v", err)
	}
}

func waitForAddress(t *testing.T, server *Server) string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		address := server.Address()
		if address != "127.0.0.1:0" {
			return address
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("server did not bind a listener")
	return ""
}
