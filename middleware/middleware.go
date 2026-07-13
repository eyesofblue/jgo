// Package middleware contains composable net/http middleware primitives.
package middleware

import "net/http"

// Middleware wraps an HTTP handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares in declaration order. The first middleware is the
// outermost middleware and sees the request first.
func Chain(handler http.Handler, middlewares ...Middleware) http.Handler {
	if handler == nil {
		handler = http.NotFoundHandler()
	}
	for i := len(middlewares) - 1; i >= 0; i-- {
		if middlewares[i] != nil {
			handler = middlewares[i](handler)
		}
	}
	return handler
}
