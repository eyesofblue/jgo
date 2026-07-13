// Package timeout limits HTTP handler execution and returns a standard JSON
// timeout response.
package timeout

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	jerrors "github.com/eyesofblue/jgo/errors"
	"github.com/eyesofblue/jgo/middleware"
	"github.com/eyesofblue/jgo/response"
)

var ErrHandlerTimeout = errors.New("http handler timed out")

// New creates timeout middleware. Durations less than or equal to zero disable
// the timeout and return the original handler unchanged.
func New(duration time.Duration) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		if duration <= 0 {
			return next
		}
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			ctx, cancel := context.WithTimeout(request.Context(), duration)
			defer cancel()

			buffered := newBufferWriter()
			type handlerResult struct {
				panicValue any
			}
			finished := make(chan handlerResult, 1)
			go func() {
				result := handlerResult{}
				defer func() {
					if recovered := recover(); recovered != nil {
						result.panicValue = recovered
					}
					finished <- result
				}()
				next.ServeHTTP(buffered, request.WithContext(ctx))
			}()

			select {
			case result := <-finished:
				if result.panicValue != nil {
					panic(result.panicValue)
				}
				buffered.commit(writer)
			case <-ctx.Done():
				if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
					buffered.timeout()
					return
				}
				buffered.timeout()
				err := jerrors.New(jerrors.CodeTimeout, jerrors.MessageTimeout,
					jerrors.WithHTTPStatus(http.StatusGatewayTimeout))
				_ = response.Error(writer, request.WithContext(ctx), err)
			}
		})
	}
}

type bufferWriter struct {
	mu       sync.Mutex
	header   http.Header
	body     bytes.Buffer
	status   int
	timedOut bool
}

func newBufferWriter() *bufferWriter {
	return &bufferWriter{header: make(http.Header)}
}

func (w *bufferWriter) Header() http.Header { return w.header }

func (w *bufferWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timedOut || w.status != 0 {
		return
	}
	w.status = status
}

func (w *bufferWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timedOut {
		return 0, ErrHandlerTimeout
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func (w *bufferWriter) timeout() {
	w.mu.Lock()
	w.timedOut = true
	w.mu.Unlock()
}

func (w *bufferWriter) commit(writer http.ResponseWriter) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for key, values := range w.header {
		writer.Header()[key] = append([]string(nil), values...)
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	writer.WriteHeader(status)
	_, _ = writer.Write(w.body.Bytes())
}
