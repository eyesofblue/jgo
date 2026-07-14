package security

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	coresecurity "github.com/eyesofblue/jgo/security"
)

type authenticator struct{ err error }

func (adapter authenticator) Authenticate(_ context.Context, credential coresecurity.Credential) (coresecurity.Principal, error) {
	if adapter.err != nil || credential.Scheme != "Bearer" || credential.Value != "token" {
		return coresecurity.Principal{}, adapter.err
	}
	return coresecurity.Principal{Subject: "user-1"}, nil
}

type authorizer struct {
	resource string
	err      error
}

func (adapter *authorizer) Authorize(_ context.Context, principal coresecurity.Principal, resource string) error {
	adapter.resource = resource
	if principal.Subject != "user-1" {
		return errors.New("missing principal")
	}
	return adapter.err
}

func TestMiddlewareAuthenticatesAndUsesMatchedPattern(t *testing.T) {
	authorization := &authorizer{}
	mux := http.NewServeMux()
	mux.Handle("GET /users/{id}", New(authenticator{}, authorization)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := coresecurity.FromContext(request.Context())
		if !ok || principal.Subject != "user-1" {
			t.Fatalf("principal = %+v, %v", principal, ok)
		}
		writer.WriteHeader(http.StatusNoContent)
	})))
	request := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	request.Header.Set("Authorization", "Bearer token")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || authorization.resource != "GET /users/{id}" {
		t.Fatalf("status=%d resource=%q", recorder.Code, authorization.resource)
	}
}

func TestMiddlewareRejectsMissingAndDeniedCredentials(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		authorizer *authorizer
		status     int
	}{
		{name: "missing", status: http.StatusUnauthorized},
		{name: "denied", header: "Bearer token", authorizer: &authorizer{err: errors.New("denied")}, status: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := New(authenticator{err: errors.New("invalid")}, test.authorizer)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("protected handler was called")
			}))
			if test.name == "denied" {
				handler = New(authenticator{}, test.authorizer)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					t.Fatal("protected handler was called")
				}))
			}
			request := httptest.NewRequest(http.MethodGet, "/protected", nil)
			request.Header.Set("Authorization", test.header)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			var envelope struct {
				Code int `json:"code"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil || recorder.Code != test.status || envelope.Code == 0 {
				t.Fatalf("status=%d body=%s err=%v", recorder.Code, recorder.Body.String(), err)
			}
		})
	}
}
