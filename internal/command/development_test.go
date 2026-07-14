package command

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	projectgen "github.com/eyesofblue/jgo/internal/generator/project"
)

func TestDoctorPassesForGeneratedWebProject(t *testing.T) {
	root := generatedWebProject(t)
	var output bytes.Buffer
	if err := Execute(&output, &output, []string{"doctor", "--root", root}); err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, output.String())
	}
	for _, check := range []string{"PASS  project", "PASS  Go >= 1.24.0", "PASS  JGO module dependency", "PASS  OpenAPI contract"} {
		if !strings.Contains(output.String(), check) {
			t.Fatalf("doctor output does not contain %q:\n%s", check, output.String())
		}
	}
}

func TestDoctorRejectsBrokenExternalBindingManifest(t *testing.T) {
	for name, manifest := range map[string]string{
		"invalid JSON":  "{not-json",
		"unknown field": `{"version":1,"unexpected":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			root := generatedWebProject(t)
			writeCommandContract(t, root, ".jgo/rpc.json", manifest)
			var output bytes.Buffer
			err := Execute(&output, &output, []string{"doctor", "--root", root})
			if err == nil || !strings.Contains(output.String(), "FAIL  external RPC bindings") {
				t.Fatalf("doctor error = %v\n%s", err, output.String())
			}
		})
	}
}

func TestGenerateCommandDetectsWebContract(t *testing.T) {
	root := generatedWebProject(t)
	var output bytes.Buffer
	if err := Execute(&output, &output, []string{"generate", "--root", root}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "generated HTTP code") {
		t.Fatalf("output = %q", output.String())
	}
	for _, relative := range []string{"gen/http/api.gen.go", "internal/transport/http/routes.gen.go"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("generated %s: %v", relative, err)
		}
	}
}

func TestEmptyGRPCProjectDoesNotRequireBuf(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "empty-grpc")
	_, err = projectgen.Generate(projectgen.Config{Name: "empty-grpc", Module: "example.com/empty-grpc", Type: projectgen.TypeGRPC, TargetDir: root, JGOReplace: repositoryRoot, SkipTidy: true})
	if err != nil {
		t.Fatal(err)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(goBinary, filepath.Join(bin, "go")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	var output bytes.Buffer
	if err := Execute(&output, &output, []string{"generate", "--root", root}); err != nil {
		t.Fatalf("generate = %v", err)
	}
	if !strings.Contains(output.String(), "no local protobuf contracts") {
		t.Fatalf("output = %q", output.String())
	}
	output.Reset()
	if err := Execute(&output, &output, []string{"doctor", "--root", root}); err != nil {
		t.Fatalf("doctor = %v\n%s", err, output.String())
	}
	if strings.Contains(output.String(), "buf") || strings.Contains(output.String(), "protoc-gen") {
		t.Fatalf("empty project checked protobuf tools:\n%s", output.String())
	}
}

func TestProtoProjectRejectsRunAndServerBuild(t *testing.T) {
	root := filepath.Join(t.TempDir(), "company-api")
	_, err := projectgen.Generate(projectgen.Config{
		Name: "company-api", Module: "example.com/company-api", Type: projectgen.TypeProto,
		TargetDir: root, SkipTidy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Execute(&output, &output, []string{"run", "--root", root}); err == nil || !strings.Contains(err.Error(), "do not have a server process") {
		t.Fatalf("run error = %v", err)
	}
	if err := Execute(&output, &output, []string{"build", "--root", root}); err == nil || !strings.Contains(err.Error(), "do not have a server binary") {
		t.Fatalf("build error = %v", err)
	}
}

func TestBuildAndRunCommands(t *testing.T) {
	root := minimalRunnableProject(t)
	var output bytes.Buffer
	if err := Execute(&output, &output, []string{"run", "--root", root, "hello"}); err != nil {
		t.Fatalf("run error = %v, output=%s", err, output.String())
	}
	if !strings.Contains(output.String(), "hello") {
		t.Fatalf("run output = %q", output.String())
	}

	output.Reset()
	if err := Execute(&output, &output, []string{"build", "--root", root}); err != nil {
		t.Fatalf("build error = %v, output=%s", err, output.String())
	}
	binary := filepath.Join(root, "bin", filepath.Base(root))
	if info, err := os.Stat(binary); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("binary %s: info=%v error=%v", binary, info, err)
	}
}

func TestParseRunArgumentsForwardsServiceFlags(t *testing.T) {
	root, forwarded, help, err := parseRunArguments(".", []string{"--root", "/tmp/service", "--config", "configs/prod.yaml", "--grpc-address=:9191"})
	if err != nil || help || root != "/tmp/service" {
		t.Fatalf("parse = %q %v %v %v", root, forwarded, help, err)
	}
	want := []string{"--config", "configs/prod.yaml", "--grpc-address=:9191"}
	if strings.Join(forwarded, "|") != strings.Join(want, "|") {
		t.Fatalf("forwarded = %v", forwarded)
	}
}

func TestParseGoVersion(t *testing.T) {
	for input, want := range map[string][3]int{
		"go1.22.0":  {1, 22, 0},
		"go1.25.12": {1, 25, 12},
		"go1.24rc1": {1, 24, 0},
	} {
		major, minor, patch, err := parseGoVersion(input)
		if err != nil || [3]int{major, minor, patch} != want {
			t.Fatalf("parseGoVersion(%q) = %d.%d.%d, %v; want %v", input, major, minor, patch, err, want)
		}
	}
}

func generatedWebProject(t *testing.T) string {
	t.Helper()
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "demo-web")
	_, err = projectgen.Generate(projectgen.Config{
		Name: "demo-web", Module: "example.com/demo-web", Type: projectgen.TypeWeb,
		TargetDir: root, JGOReplace: repositoryRoot, SkipTidy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func minimalRunnableProject(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "runnable")
	writeCommandContract(t, root, "go.mod", "module example.com/runnable\n\ngo 1.24.0\n")
	writeCommandContract(t, root, "api/http/openapi.yaml", `openapi: 3.0.3
info: {title: test, version: 0.1.0}
paths: {}
`)
	writeCommandContract(t, root, "cmd/server/main.go", `package main
import (
	"fmt"
	"os"
)
func main() { fmt.Println(os.Args[1]) }
`)
	return root
}
