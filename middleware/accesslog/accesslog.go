// Package accesslog records one structured log entry for each HTTP request.
package accesslog

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/eyesofblue/jgo/logx"
	"github.com/eyesofblue/jgo/middleware"
)

// New creates access log middleware. A nil logger uses slog.Default.
func New(logger *slog.Logger) middleware.Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	contextLogger := logx.New(logger)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			wrapped := middleware.WrapResponseWriter(writer)
			startedAt := time.Now()
			next.ServeHTTP(wrapped, request)
			contextLogger.InfoCtx(request.Context(), "http request",
				"method", request.Method,
				"path", request.URL.Path,
				"status", wrapped.Status(),
				"bytes", wrapped.BytesWritten(),
				"duration_ms", time.Since(startedAt).Milliseconds(),
				"remote_addr", request.RemoteAddr,
			)
		})
	}
}
