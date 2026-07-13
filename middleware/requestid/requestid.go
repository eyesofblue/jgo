// Package requestid propagates or creates a safe request identifier.
package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"unicode"

	"github.com/eyesofblue/jgo/middleware"
)

const Header = "X-Request-ID"

const maxLength = 128

type contextKey struct{}

// Generator creates a request identifier.
type Generator func() string

// New creates request ID middleware. A nil generator uses a cryptographically
// random 128-bit identifier.
func New(generator Generator) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			id := Ensure(request.Header.Get(Header), generator)
			writer.Header().Set(Header, id)
			ctx := WithContext(request.Context(), id)
			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}
}

// Ensure returns value when it is a safe request ID, otherwise it creates one.
func Ensure(value string, generator Generator) string {
	value = strings.TrimSpace(value)
	if valid(value) {
		return value
	}
	if generator == nil {
		generator = randomID
	}
	return generator()
}

// WithContext stores id in ctx for downstream logs and handlers.
func WithContext(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, id)
}

// FromContext returns the request identifier stored in ctx.
func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}

func randomID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(data[:])
}

func valid(id string) bool {
	if id == "" || len(id) > maxLength {
		return false
	}
	for _, char := range id {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			continue
		}
		switch char {
		case '-', '_', '.', ':':
			continue
		default:
			return false
		}
	}
	return true
}
