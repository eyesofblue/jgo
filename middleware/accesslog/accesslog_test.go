package accesslog

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eyesofblue/jgo/middleware/requestid"
)

func TestMiddlewareLogsResponseMetadata(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := requestid.New(func() string { return "request-7" })(New(logger)(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte("body"))
	})))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/users", nil))

	for _, part := range []string{`"method":"POST"`, `"path":"/users"`, `"status":201`, `"bytes":4`, `"request_id":"request-7"`} {
		if !strings.Contains(logs.String(), part) {
			t.Fatalf("log %q does not contain %q", logs.String(), part)
		}
	}
}
