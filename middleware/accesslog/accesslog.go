// Package accesslog records one structured log entry for each HTTP request.
package accesslog

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/eyesofblue/jgo/middleware"
	"github.com/eyesofblue/jgo/middleware/requestid"
)

// New creates access log middleware. A nil logger uses slog.Default.
func New(logger *slog.Logger) middleware.Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			wrapped := middleware.WrapResponseWriter(writer)
			startedAt := time.Now()
			next.ServeHTTP(wrapped, request)
			logger.InfoContext(request.Context(), "http request",
				"method", request.Method,
				"path", request.URL.Path,
				"status", wrapped.Status(),
				"bytes", wrapped.BytesWritten(),
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"request_id", requestid.FromContext(request.Context()),
				"remote_addr", request.RemoteAddr,
			)
		})
	}
}
