package recovery

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eyesofblue/jgo/response"
)

func TestMiddlewareRecoversBeforeResponseIsWritten(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := New(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	var envelope response.Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusInternalServerError || envelope.Msg != "internal server error" {
		t.Fatalf("response = %d, %+v", recorder.Code, envelope)
	}
	if !strings.Contains(logs.String(), "boom") {
		t.Fatalf("panic log missing: %s", logs.String())
	}
}

func TestMiddlewareDoesNotAppendJSONAfterCommittedResponse(t *testing.T) {
	handler := New(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusAccepted)
		_, _ = writer.Write([]byte("partial"))
		panic("boom")
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusAccepted || recorder.Body.String() != "partial" {
		t.Fatalf("response = %d, %q", recorder.Code, recorder.Body.String())
	}
}
