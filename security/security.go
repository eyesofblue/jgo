// Package security defines infrastructure-neutral authentication and
// authorization contracts for JGO transports.
package security

import "context"

type Credential struct {
	Scheme string
	Value  string
}

type Principal struct {
	Subject    string
	Attributes map[string]string
}

type Authenticator interface {
	Authenticate(context.Context, Credential) (Principal, error)
}

type Authorizer interface {
	Authorize(context.Context, Principal, string) error
}

type principalKey struct{}

func NewContext(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

func FromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}
