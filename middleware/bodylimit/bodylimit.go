// Package bodylimit bounds HTTP request bodies before application decoding.
package bodylimit

import (
	"net/http"

	"github.com/eyesofblue/jgo/middleware"
)

// New limits request bodies to maxBytes. Callers must pass a positive limit.
func New(maxBytes int64) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Body != nil {
				request.Body = http.MaxBytesReader(writer, request.Body, maxBytes)
			}
			next.ServeHTTP(writer, request)
		})
	}
}
