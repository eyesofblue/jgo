// Package response writes the standard JGO HTTP/JSON response envelope.
package response

import (
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
	return JSON(writer, http.StatusOK, Envelope{
		Code: jerrors.CodeOK,
		Msg:  "",
		Data: data,
	})
}

// Error writes a client-safe error response. Unknown error text is not exposed.
func Error(writer http.ResponseWriter, request *http.Request, err error) error {
	code, message, status := jerrors.PublicValues(err)
	return JSON(writer, status, Envelope{
		Code: code,
		Msg:  message,
		Data: nil,
	})
}
