package grpcx

import (
	"context"
	"errors"
	"testing"

	"github.com/eyesofblue/jgo/security"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type testAuthenticator struct{ err error }

func (authenticator testAuthenticator) Authenticate(context.Context, security.Credential) (security.Principal, error) {
	return security.Principal{Subject: "user-1"}, authenticator.err
}

type testAuthorizer struct{ err error }

func (authorizer testAuthorizer) Authorize(context.Context, security.Principal, string) error {
	return authorizer.err
}

func TestUnarySecurity(t *testing.T) {
	interceptor := UnarySecurity(testAuthenticator{}, testAuthorizer{})
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer token"))
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/demo.User/Get"}, func(ctx context.Context, request any) (any, error) {
		principal, ok := security.FromContext(ctx)
		if !ok || principal.Subject != "user-1" {
			t.Fatalf("principal = %+v, %v", principal, ok)
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUnarySecurityRejectsMissingAndDeniedCredentials(t *testing.T) {
	interceptor := UnarySecurity(testAuthenticator{}, nil)
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) { return nil, nil })
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing credential = %v", err)
	}
	interceptor = UnarySecurity(testAuthenticator{}, testAuthorizer{err: errors.New("denied")})
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer token"))
	_, err = interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) { return nil, nil })
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("denied = %v", err)
	}
}

func TestServerTLSRejectsIncompleteConfiguration(t *testing.T) {
	_, err := New(WithAddress("127.0.0.1:0"), WithTLS(TLSConfig{Enabled: true}), WithRegister(func(grpc.ServiceRegistrar) {}))
	if err == nil {
		t.Fatal("incomplete TLS configuration accepted")
	}
}
