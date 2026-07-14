package bodylimit

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewLimitsRequestBody(t *testing.T) {
	var readErr error
	handler := New(3)(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		_, readErr = io.ReadAll(request.Body)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader("1234")))
	var tooLarge *http.MaxBytesError
	if !errors.As(readErr, &tooLarge) {
		t.Fatalf("read error = %v, want *http.MaxBytesError", readErr)
	}
}
