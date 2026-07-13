package middleware

import (
	"bufio"
	"io"
	"net"
	"net/http"
)

// ResponseWriter records response metadata while preserving access to the
// underlying writer through Unwrap and the common optional HTTP interfaces.
type ResponseWriter interface {
	http.ResponseWriter
	Status() int
	BytesWritten() int64
	Written() bool
	Unwrap() http.ResponseWriter
}

type responseWriter struct {
	http.ResponseWriter
	status       int
	bytesWritten int64
}

// WrapResponseWriter returns a recorder around writer. Existing recorders are
// reused so nested middleware observes the same response state.
func WrapResponseWriter(writer http.ResponseWriter) ResponseWriter {
	if wrapped, ok := writer.(ResponseWriter); ok {
		return wrapped
	}
	return &responseWriter{ResponseWriter: writer}
}

func (w *responseWriter) WriteHeader(status int) {
	if w.Written() {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if !w.Written() {
		w.WriteHeader(http.StatusOK)
	}
	written, err := w.ResponseWriter.Write(data)
	w.bytesWritten += int64(written)
	return written, err
}

func (w *responseWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *responseWriter) BytesWritten() int64 { return w.bytesWritten }
func (w *responseWriter) Written() bool       { return w.status != 0 }
func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *responseWriter) Flush() {
	if !w.Written() {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *responseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (w *responseWriter) ReadFrom(reader io.Reader) (int64, error) {
	if !w.Written() {
		w.WriteHeader(http.StatusOK)
	}
	readerFrom, ok := w.ResponseWriter.(io.ReaderFrom)
	if !ok {
		return io.Copy(writerOnly{Writer: w}, reader)
	}
	written, err := readerFrom.ReadFrom(reader)
	w.bytesWritten += written
	return written, err
}

type writerOnly struct {
	io.Writer
}
