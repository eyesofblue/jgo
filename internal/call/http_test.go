package call

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const httpContract = `openapi: 3.0.3
info:
  title: test
  version: 0.1.0
paths:
  /get_user:
    get:
      operationId: GetUser
      parameters:
        - name: uid
          in: query
          required: true
          schema:
            type: integer
        - name: X-Tenant-ID
          in: header
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
  /update_user:
    post:
      operationId: UpdateUser
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [uid, profile]
              properties:
                uid:
                  type: integer
                profile:
                  type: object
      responses:
        "200":
          description: ok
`

func TestCallHTTPMapsQueryHeaderAndFormatsResponse(t *testing.T) {
	root := writeHTTPContract(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/get_user" || request.URL.Query().Get("uid") != "12345" {
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.String())
		}
		if request.Header.Get("X-Tenant-ID") != "tenant-a" || request.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("unexpected headers: %v", request.Header)
		}
		_, _ = io.WriteString(writer, `{"code":0,"msg":"","data":{"uid":12345}}`)
	}))
	defer server.Close()

	result, err := CallHTTP(context.Background(), HTTPConfig{
		Root: root, Operation: "GetUser", Address: server.URL,
		Data: `{"uid":12345}`, Headers: []string{"X-Tenant-ID: tenant-a", "Authorization: Bearer token"},
	})
	if err != nil {
		t.Fatalf("CallHTTP() error = %v", err)
	}
	if result.StatusCode != http.StatusOK || !strings.Contains(string(result.Body), "\n  \"code\": 0") {
		t.Fatalf("result = %+v, body=%s", result, result.Body)
	}
}

func TestCallHTTPMapsComplexJSONBody(t *testing.T) {
	root := writeHTTPContract(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request: %s %v", request.Method, request.Header)
		}
		if string(body) != `{"profile":{"name":"Albert"},"uid":12345}` {
			t.Errorf("body = %s", body)
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, `{"code":0,"msg":"","data":{}}`)
	}))
	defer server.Close()

	_, err := CallHTTP(context.Background(), HTTPConfig{
		Root: root, Operation: "UpdateUser", Address: server.URL,
		Data: `{"uid":12345,"profile":{"name":"Albert"}}`,
	})
	if err != nil {
		t.Fatalf("CallHTTP() error = %v", err)
	}
}

func TestCallHTTPReportsAvailableMethods(t *testing.T) {
	root := writeHTTPContract(t)
	_, err := CallHTTP(context.Background(), HTTPConfig{Root: root, Operation: "Missing", Address: "http://127.0.0.1"})
	if err == nil || !strings.Contains(err.Error(), "available methods: GetUser, UpdateUser") {
		t.Fatalf("CallHTTP() error = %v", err)
	}
}

func TestCallHTTPKeepsHTTPStatusSeparateFromBodyCode(t *testing.T) {
	root := writeHTTPContract(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, `{"code":10001,"msg":"invalid uid","data":null}`)
	}))
	defer server.Close()
	result, err := CallHTTP(context.Background(), HTTPConfig{
		Root: root, Operation: "GetUser", Address: server.URL,
		Data: `{"uid":12345,"X-Tenant-ID":"tenant-a"}`,
	})
	if err == nil || result.StatusCode != http.StatusBadRequest || !strings.Contains(string(result.Body), "10001") {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func writeHTTPContract(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "api", "http", "openapi.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(httpContract), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
