// Package security provides HTTP authentication and authorization middleware.
package security

import (
	"net/http"
	"strings"

	jerrors "github.com/eyesofblue/jgo/errors"
	"github.com/eyesofblue/jgo/middleware"
	"github.com/eyesofblue/jgo/response"
	coresecurity "github.com/eyesofblue/jgo/security"
)

// New authenticates the Authorization header and authorizes the matched HTTP
// route. Nil adapters preserve the framework's opt-in security behavior.
func New(authenticator coresecurity.Authenticator, authorizer coresecurity.Authorizer) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		if authenticator == nil && authorizer == nil {
			return next
		}
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			ctx := request.Context()
			principal := coresecurity.Principal{}
			if authenticator != nil {
				credential, ok := credentialFromRequest(request)
				if !ok {
					writeSecurityError(writer, request, http.StatusUnauthorized, "authentication required")
					return
				}
				value, err := authenticator.Authenticate(ctx, credential)
				if err != nil {
					writeSecurityError(writer, request, http.StatusUnauthorized, "authentication failed")
					return
				}
				principal = value
				ctx = coresecurity.NewContext(ctx, principal)
			} else if value, ok := coresecurity.FromContext(ctx); ok {
				principal = value
			}
			if authorizer != nil {
				resource := request.Pattern
				if resource == "" {
					resource = request.Method + " " + request.URL.Path
				}
				if err := authorizer.Authorize(ctx, principal, resource); err != nil {
					writeSecurityError(writer, request, http.StatusForbidden, "permission denied")
					return
				}
			}
			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}
}

func credentialFromRequest(request *http.Request) (coresecurity.Credential, bool) {
	parts := strings.SplitN(strings.TrimSpace(request.Header.Get("Authorization")), " ", 2)
	if len(parts) != 2 || parts[0] == "" || strings.TrimSpace(parts[1]) == "" {
		return coresecurity.Credential{}, false
	}
	return coresecurity.Credential{Scheme: parts[0], Value: strings.TrimSpace(parts[1])}, true
}

func writeSecurityError(writer http.ResponseWriter, request *http.Request, status int, message string) {
	err := jerrors.New(jerrors.CodeInvalidArgument, message, jerrors.WithHTTPStatus(status))
	_ = response.Error(writer, request, err)
}
