package requestid

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddlewarePreservesValidRequestID(t *testing.T) {
	var got string
	handler := New(func() string { return "generated" })(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		got = FromContext(request.Context())
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(Header, "client-id_123")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got != "client-id_123" || response.Header().Get(Header) != "client-id_123" {
		t.Fatalf("request ID = %q, response header = %q", got, response.Header().Get(Header))
	}
}

func TestMiddlewareReplacesUnsafeRequestID(t *testing.T) {
	var got string
	handler := New(func() string { return "generated" })(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		got = FromContext(request.Context())
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(Header, "line\nbreak")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got != "generated" || response.Header().Get(Header) != "generated" {
		t.Fatalf("request ID = %q, response header = %q", got, response.Header().Get(Header))
	}
}
