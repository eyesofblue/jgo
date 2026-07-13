package timeout

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jerrors "github.com/eyesofblue/jgo/errors"
	"github.com/eyesofblue/jgo/response"
)

func TestMiddlewareCommitsCompletedResponse(t *testing.T) {
	handler := New(time.Second)(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("X-Test", "yes")
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte("created"))
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", nil))

	if recorder.Code != http.StatusCreated || recorder.Body.String() != "created" || recorder.Header().Get("X-Test") != "yes" {
		t.Fatalf("response = %d, %q, headers=%v", recorder.Code, recorder.Body.String(), recorder.Header())
	}
}

func TestMiddlewareReturnsJSONTimeout(t *testing.T) {
	handler := New(10 * time.Millisecond)(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	var envelope response.Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusGatewayTimeout || envelope.Code != jerrors.CodeTimeout || envelope.Msg != jerrors.MessageTimeout {
		t.Fatalf("response = %d, %+v", recorder.Code, envelope)
	}
}
