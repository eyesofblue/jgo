// Package recovery converts handler panics into safe JSON errors.
package recovery

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	jerrors "github.com/eyesofblue/jgo/errors"
	"github.com/eyesofblue/jgo/logx"
	"github.com/eyesofblue/jgo/middleware"
	"github.com/eyesofblue/jgo/response"
)

// New creates panic recovery middleware. A nil logger uses slog.Default.
func New(logger *slog.Logger) middleware.Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	contextLogger := logx.New(logger)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			wrapped := middleware.WrapResponseWriter(writer)
			defer func() {
				if recovered := recover(); recovered != nil {
					contextLogger.ErrorCtx(request.Context(), "http handler panic",
						"panic", recovered,
						"stack", string(debug.Stack()),
					)
					if wrapped.Written() {
						return
					}
					err := jerrors.Wrap(fmt.Errorf("panic: %v", recovered), jerrors.CodeInternal, jerrors.MessageInternal)
					_ = response.Error(wrapped, request, err)
				}
			}()
			next.ServeHTTP(wrapped, request)
		})
	}
}
