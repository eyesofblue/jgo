// Package response writes the standard JGO HTTP/JSON response envelope.
package response

import (
	"context"
	"encoding/json"
	"net/http"

	jerrors "github.com/eyesofblue/jgo/errors"
)

// Envelope is the standard HTTP API response shape.
type Envelope struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data"`
}

// JSON writes value as JSON with the provided HTTP status.
func JSON(writer http.ResponseWriter, status int, value any) error {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(status)
	return json.NewEncoder(writer).Encode(value)
}

// Success writes a successful response envelope.
func Success(writer http.ResponseWriter, request *http.Request, data any) error {
	ObserveRoute(request)
	setBusinessCode(writer, request, jerrors.CodeOK)
	return JSON(writer, http.StatusOK, Envelope{
		Code: jerrors.CodeOK,
		Msg:  "",
		Data: data,
	})
}

// Error writes a client-safe error response. Unknown error text is not exposed.
func Error(writer http.ResponseWriter, request *http.Request, err error) error {
	code, message, status := jerrors.PublicValues(err)
	ObserveRoute(request)
	setBusinessCode(writer, request, code)
	return JSON(writer, status, Envelope{
		Code: code,
		Msg:  message,
		Data: nil,
	})
}

type businessCodeWriter interface{ SetBusinessCode(int) }
type routeWriter interface{ SetRoute(string) }
type observerKey struct{}

// WithObserver installs request-scoped response metadata collection. Context
// propagation keeps it intact when timeout middleware clones a request or
// buffers its ResponseWriter.
func WithObserver(request *http.Request, observer any) *http.Request {
	if request == nil || observer == nil {
		return request
	}
	return request.WithContext(context.WithValue(request.Context(), observerKey{}, observer))
}

// ObserveRoute records the matched ServeMux pattern as soon as a generated
// handler starts, before business work can block or time out.
func ObserveRoute(request *http.Request) {
	if request == nil || request.Pattern == "" {
		return
	}
	if observer, ok := request.Context().Value(observerKey{}).(routeWriter); ok {
		observer.SetRoute(request.Pattern)
	}
}

func setBusinessCode(writer http.ResponseWriter, request *http.Request, code int) {
	if request != nil {
		if observer, ok := request.Context().Value(observerKey{}).(businessCodeWriter); ok {
			observer.SetBusinessCode(code)
			return
		}
	}
	if target, ok := writer.(businessCodeWriter); ok {
		target.SetBusinessCode(code)
	}
}
