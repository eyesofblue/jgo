package openapi

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	projectgen "github.com/eyesofblue/jgo/internal/generator/project"
)

func TestAddAndGenerateWithGoStructModels(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	root := filepath.Join(t.TempDir(), "demo")
	if _, err := projectgen.Generate(projectgen.Config{
		Name: "demo", Module: "example.com/demo", Type: projectgen.TypeWeb,
		TargetDir: root, JGOReplace: repositoryRoot, SkipTidy: true,
	}); err != nil {
		t.Fatalf("generate project: %v", err)
	}
	modelSource := `package model

type Address struct {
	City string ` + "`json:\"city\"`" + `
}

type UserInfo struct {
	UID int64 ` + "`json:\"uid\"`" + `
	Nickname string ` + "`json:\"nickname\"`" + `
	Address *Address ` + "`json:\"address,omitempty\"`" + `
	Tags []string ` + "`json:\"tags,omitempty\"`" + `
}

type UpdateUserRequest struct {
	UID int64 ` + "`json:\"uid\"`" + `
	Nickname string ` + "`json:\"nickname\"`" + `
	Address *Address ` + "`json:\"address,omitempty\"`" + `
}
`
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(ModelPath), "user.go"), []byte(modelSource), 0o644); err != nil {
		t.Fatalf("write models: %v", err)
	}

	operations := []AddConfig{
		{
			Root: root, Operation: "GetUser", Method: "GET", Path: "/get_user",
			Request: []string{"uid:int64:required"}, ResponseType: "UserInfo",
		},
		{
			Root: root, Operation: "UpdateUser", Method: "POST", Path: "/update_user",
			RequestType: "UpdateUserRequest", ResponseType: "UserInfo",
		},
		{
			Root: root, Operation: "ListUsers", Method: "GET", Path: "/list_users",
			ResponseType: "UserInfo", ResponseList: true,
		},
	}
	for _, operation := range operations {
		if err := Add(operation); err != nil {
			t.Fatalf("Add(%s) error = %v", operation.Operation, err)
		}
	}
	addedContract, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(SpecPath)))
	if err != nil || !strings.Contains(string(addedContract), "operationId") {
		t.Fatalf("contract after Add: error=%v\n%s", err, addedContract)
	}

	getUserService := filepath.Join(root, "internal", "service", "get_user.go")
	contents, err := os.ReadFile(getUserService)
	if err != nil {
		t.Fatalf("read service stub: %v", err)
	}
	if !strings.Contains(string(contents), "`json:\"uid\"`") || strings.Contains(string(contents), "`json:\"\\\"uid\\\"\"`") {
		t.Fatalf("service stub has an invalid JSON tag:\n%s", contents)
	}
	contents = append(contents, []byte("\n// user implementation marker\n")...)
	if err := os.WriteFile(getUserService, contents, 0o644); err != nil {
		t.Fatalf("modify service stub: %v", err)
	}

	if err := Generate(root); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	first := generatedSnapshot(t, root)
	if err := Generate(root); err != nil {
		t.Fatalf("second Generate() error = %v", err)
	}
	second := generatedSnapshot(t, root)
	if first != second {
		t.Fatal("repeated generation changed managed output")
	}
	preserved, err := os.ReadFile(getUserService)
	if err != nil || !strings.Contains(string(preserved), "user implementation marker") {
		t.Fatalf("service implementation was overwritten: error=%v", err)
	}

	contract, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(SpecPath)))
	if err != nil {
		t.Fatalf("read OpenAPI contract: %v", err)
	}
	for _, expected := range []string{
		"operationId: GetUser", "operationId: UpdateUser", "operationId: ListUsers",
		"x-go-type: model.UserInfo", "x-go-type: model.UpdateUserRequest",
		"responseList: true", "requestType: UpdateUserRequest",
	} {
		if !strings.Contains(string(contract), expected) {
			t.Fatalf("OpenAPI contract does not contain %q:\n%s", expected, contract)
		}
	}
	routes, err := os.ReadFile(filepath.Join(root, "internal", "transport", "http", "routes.gen.go"))
	if err != nil {
		t.Fatalf("read generated routes: %v", err)
	}
	if !strings.Contains(string(routes), "var body model.UpdateUserRequest") || !strings.Contains(string(routes), "params httpgen.GetUserParams") || !strings.Contains(string(routes), "response.ObserveRoute(request)") {
		t.Fatalf("generated routes do not include typed request handling:\n%s", routes)
	}

	writeHTTPImplementations(t, root)
	writeHTTPIntegrationTest(t, root)
	runGo(t, root, "mod", "tidy")
	runGo(t, root, "test", "./...")
	runGo(t, root, "build", "./...")
}

func writeHTTPImplementations(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"get_user.go": `package service

import (
	"context"
	"example.com/demo/api/http/model"
)

type GetUserRequest struct {
	Uid int64 ` + "`json:\"uid\"`" + `
}

func (service *Service) GetUser(ctx context.Context, request GetUserRequest) (model.UserInfo, error) {
	return model.UserInfo{UID: request.Uid, Nickname: "Albert"}, nil
}
`,
		"update_user.go": `package service

import (
	"context"
	"example.com/demo/api/http/model"
)

func (service *Service) UpdateUser(ctx context.Context, request model.UpdateUserRequest) (model.UserInfo, error) {
	return model.UserInfo{UID: request.UID, Nickname: request.Nickname, Address: request.Address}, nil
}
`,
		"list_users.go": `package service

import (
	"context"
	"example.com/demo/api/http/model"
)

func (service *Service) ListUsers(ctx context.Context) ([]model.UserInfo, error) {
	return []model.UserInfo{{UID: 1, Nickname: "Albert"}}, nil
}
`,
	}
	for name, contents := range files {
		path := filepath.Join(root, "internal", "service", name)
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write service implementation %s: %v", name, err)
		}
	}
}

func writeHTTPIntegrationTest(t *testing.T, root string) {
	t.Helper()
	contents := `package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eyesofblue/jgo/middleware/bodylimit"
	securitymw "github.com/eyesofblue/jgo/middleware/security"
	coresecurity "github.com/eyesofblue/jgo/security"
	"example.com/demo/internal/service"
)

type envelope struct {
	Code int ` + "`json:\"code\"`" + `
	Msg string ` + "`json:\"msg\"`" + `
	Data json.RawMessage ` + "`json:\"data\"`" + `
}

type testAuthenticator struct{}
func (testAuthenticator) Authenticate(context.Context, coresecurity.Credential) (coresecurity.Principal, error) {
	return coresecurity.Principal{Subject: "test"}, nil
}

func TestGeneratedRoutes(t *testing.T) {
	handler := NewHandler(service.New(nil))

	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/get_user?uid=42", nil))
	assertEnvelope(t, get, http.StatusOK, 0, ` + "`\"uid\":42`" + `)

	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/get_user", nil))
	assertEnvelope(t, missing, http.StatusBadRequest, 10001, ` + "`\"data\":null`" + `)

	update := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(http.MethodPost, "/update_user", strings.NewReader(` + "`{\"uid\":7,\"nickname\":\"Alice\",\"address\":{\"city\":\"Shanghai\"}}`" + `))
	updateRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(update, updateRequest)
	assertEnvelope(t, update, http.StatusOK, 0, ` + "`\"city\":\"Shanghai\"`" + `)

	missingBodyField := httptest.NewRecorder()
	missingBodyRequest := httptest.NewRequest(http.MethodPost, "/update_user", strings.NewReader(` + "`{}`" + `))
	missingBodyRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(missingBodyField, missingBodyRequest)
	assertEnvelope(t, missingBodyField, http.StatusBadRequest, 10001, ` + "`\"data\":null`" + `)

	trailing := httptest.NewRecorder()
	trailingRequest := httptest.NewRequest(http.MethodPost, "/update_user", strings.NewReader(` + "`{\"uid\":7}{\"uid\":8}`" + `))
	trailingRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(trailing, trailingRequest)
	assertEnvelope(t, trailing, http.StatusBadRequest, 10001, ` + "`\"data\":null`" + `)

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/list_users", nil))
	assertEnvelope(t, list, http.StatusOK, 0, ` + "`\"data\":[`" + `)

	unauthenticated := httptest.NewRecorder()
	secured := NewHandler(service.New(nil), securitymw.New(testAuthenticator{}, nil))
	secured.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/get_user", nil))
	assertEnvelope(t, unauthenticated, http.StatusUnauthorized, 10001, ` + "`\"data\":null`" + `)

	oversized := httptest.NewRecorder()
	limited := bodylimit.New(16)(NewHandler(service.New(nil)))
	oversizedRequest := httptest.NewRequest(http.MethodPost, "/update_user", strings.NewReader(` + "`{\"uid\":7,\"nickname\":\"this is too large\"}`" + `))
	oversizedRequest.Header.Set("Content-Type", "application/json")
	limited.ServeHTTP(oversized, oversizedRequest)
	assertEnvelope(t, oversized, http.StatusRequestEntityTooLarge, 10001, ` + "`request body too large`" + `)
}

func assertEnvelope(t *testing.T, recorder *httptest.ResponseRecorder, status, code int, fragment string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, status, recorder.Body.String())
	}
	var response envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != code || !strings.Contains(recorder.Body.String(), fragment) {
		t.Fatalf("response = %s, want code %d and fragment %q", recorder.Body.String(), code, fragment)
	}
}
`
	path := filepath.Join(root, "internal", "transport", "http", "routes_e2e_test.go")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write HTTP integration test: %v", err)
	}
}

func TestCommitGeneratedOutputsRollsBackEarlierWrites(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.go")
	second := filepath.Join(root, "second.go")
	if err := os.WriteFile(first, []byte("original first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("original second"), 0o644); err != nil {
		t.Fatal(err)
	}
	writes := 0
	err := commitGeneratedOutputs([]generatedOutput{{path: first, contents: []byte("changed first")}, {path: second, contents: []byte("changed second")}}, func(output generatedOutput) error {
		writes++
		if writes == 2 {
			return errors.New("injected commit failure")
		}
		return atomicWrite(output.path, output.contents)
	})
	if err == nil || !strings.Contains(err.Error(), "injected commit failure") {
		t.Fatalf("commitGeneratedOutputs() error = %v", err)
	}
	for path, want := range map[string]string{first: "original first", second: "original second"} {
		contents, readErr := os.ReadFile(path)
		if readErr != nil || string(contents) != want {
			t.Fatalf("%s = %q, %v; want %q", path, contents, readErr, want)
		}
	}
}

func TestAddRejectsComplexGETBody(t *testing.T) {
	operation, err := normalizeOperation(AddConfig{
		Operation: "Search", Method: "GET", Path: "/search", RequestType: "SearchRequest",
	})
	if err == nil || operation.Name != "" {
		t.Fatalf("normalizeOperation() = %+v, %v; want error", operation, err)
	}
}

func TestParseFieldsRejectsGeneratedGoNameCollision(t *testing.T) {
	_, err := parseFields([]string{"user-id:string", "user_id:string"}, http.MethodGet)
	if err == nil || !strings.Contains(err.Error(), "both map to Go field UserId") {
		t.Fatalf("parseFields() error = %v", err)
	}
}

func TestAddRejectsExistingServiceMethodWithoutChangingContract(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "demo")
	if _, err := projectgen.Generate(projectgen.Config{Name: "demo", Module: "example.com/demo", Type: projectgen.TypeWeb, TargetDir: root, JGOReplace: repositoryRoot, SkipTidy: true}); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(root, "internal", "service", "custom.go")
	if err := os.WriteFile(custom, []byte("package service\n\nfunc (*Service) GetUser() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	contractPath := filepath.Join(root, filepath.FromSlash(SpecPath))
	before, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	err = Add(AddConfig{Root: root, Operation: "GetUser", Method: http.MethodGet, Path: "/get_user"})
	if err == nil || !strings.Contains(err.Error(), "already implemented") {
		t.Fatalf("Add() error = %v", err)
	}
	after, readErr := os.ReadFile(contractPath)
	if readErr != nil || !bytes.Equal(after, before) {
		t.Fatalf("contract changed after rejected add: error=%v", readErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "internal", "service", "get_user.go")); !os.IsNotExist(statErr) {
		t.Fatalf("service stub created after rejected add: %v", statErr)
	}
}

func generatedSnapshot(t *testing.T, root string) string {
	t.Helper()
	paths := []string{
		filepath.FromSlash(SpecPath),
		filepath.Join(filepath.FromSlash(GeneratedDir), "api.gen.go"),
		filepath.Join("internal", "transport", "http", "routes.gen.go"),
	}
	var output strings.Builder
	for _, path := range paths {
		contents, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("read generated file %s: %v", path, err)
		}
		output.WriteString(path)
		output.Write(contents)
	}
	return output.String()
}

func runGo(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("go", arguments...)
	command.Dir = root
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s failed: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}
