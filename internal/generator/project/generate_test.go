package project

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestGenerateProjectTrees(t *testing.T) {
	t.Parallel()

	for _, projectType := range []Type{TypeWeb, TypeGRPC, TypeMixed, TypeProto} {
		projectType := projectType
		t.Run(string(projectType), func(t *testing.T) {
			t.Parallel()
			target := filepath.Join(t.TempDir(), "demo-app")
			got, err := Generate(Config{
				Name:      "demo-app",
				Module:    "example.com/demo-app",
				Type:      projectType,
				TargetDir: target,
				SkipTidy:  true,
			})
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			if got != target {
				t.Fatalf("Generate() target = %q, want %q", got, target)
			}

			actual := strings.Join(projectTree(t, target), "\n") + "\n"
			goldenPath := filepath.Join("testdata", string(projectType)+".golden")
			golden, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden file: %v", err)
			}
			if actual != string(golden) {
				t.Fatalf("generated tree differs from %s\nactual:\n%s", goldenPath, actual)
			}
			if projectType != TypeWeb {
				hasContracts, err := protobufContractsExist(target)
				if err != nil || hasContracts {
					t.Fatalf("new %s project must start without a business protobuf contract: found=%v err=%v", projectType, hasContracts, err)
				}
			}
			readme, err := os.ReadFile(filepath.Join(target, "README.md"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(readme), "jgo list") || strings.Contains(string(readme), "jgo rpc pbapi") || strings.Contains(string(readme), "jgo rpc server add") {
				t.Fatalf("generated README lacks current workflow or contains removed commands:\n%s", readme)
			}
			if projectType == TypeProto {
				if _, err := os.Stat(filepath.Join(target, "cmd", "server", "main.go")); !os.IsNotExist(err) {
					t.Fatalf("proto project unexpectedly contains a server: %v", err)
				}
				goMod, err := os.ReadFile(filepath.Join(target, "go.mod"))
				if err != nil || strings.Contains(string(goMod), "github.com/eyesofblue/jgo") {
					t.Fatalf("proto project has a JGO runtime dependency: %v\n%s", err, goMod)
				}
			}
		})
	}
}

func protobufContractsExist(root string) (bool, error) {
	found := false
	err := filepath.WalkDir(filepath.Join(root, "api", "proto"), func(path string, entry os.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".proto" {
			found = true
		}
		return nil
	})
	return found, err
}

func TestGenerateAcceptsEmptyTarget(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if _, err := Generate(testConfig(target)); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "go.mod")); err != nil {
		t.Fatalf("generated go.mod: %v", err)
	}
}

func TestGenerateRejectsUnsafeTargetsWithoutChangingThem(t *testing.T) {
	t.Parallel()

	t.Run("nonempty directory", func(t *testing.T) {
		t.Parallel()
		target := t.TempDir()
		sentinel := filepath.Join(target, "keep.txt")
		if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		_, err := Generate(testConfig(target))
		if !errors.Is(err, ErrTargetNotEmpty) {
			t.Fatalf("Generate() error = %v, want %v", err, ErrTargetNotEmpty)
		}
		contents, readErr := os.ReadFile(sentinel)
		if readErr != nil || string(contents) != "keep" {
			t.Fatalf("sentinel changed: contents = %q, error = %v", contents, readErr)
		}
	})

	t.Run("file", func(t *testing.T) {
		t.Parallel()
		target := filepath.Join(t.TempDir(), "demo")
		if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		_, err := Generate(testConfig(target))
		if !errors.Is(err, ErrTargetExists) {
			t.Fatalf("Generate() error = %v, want %v", err, ErrTargetExists)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation may require additional privileges on Windows")
		}
		t.Parallel()
		root := t.TempDir()
		target := filepath.Join(root, "demo")
		if err := os.Symlink(filepath.Join(root, "missing"), target); err != nil {
			t.Fatalf("Symlink() error = %v", err)
		}
		_, err := Generate(testConfig(target))
		if !errors.Is(err, ErrTargetIsSymlink) {
			t.Fatalf("Generate() error = %v, want %v", err, ErrTargetIsSymlink)
		}
	})
}

func TestGeneratedProjectsCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping generated-project compilation in short mode")
	}

	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	for _, projectType := range []Type{TypeWeb, TypeGRPC, TypeMixed, TypeProto} {
		projectType := projectType
		t.Run(string(projectType), func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "demo-app")
			_, err := Generate(Config{
				Name:       "demo-app",
				Module:     "example.com/demo-app",
				Type:       projectType,
				TargetDir:  target,
				JGOReplace: repositoryRoot,
			})
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			if _, err := os.Stat(filepath.Join(target, "go.sum")); err != nil {
				t.Fatalf("generated go.sum: %v", err)
			}
			runGo(t, target, "test", "./...")
			runGo(t, target, "build", "./...")
		})
	}
}

func TestGenerateTidyFailureLeavesNoProject(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "demo")
	_, err = Generate(Config{
		Name: "demo", Module: "example.com/demo", Type: TypeWeb,
		TargetDir: target, JGOReplace: repositoryRoot, GoVersion: "99.0.0",
	})
	if !errors.Is(err, ErrTidyFailed) {
		t.Fatalf("Generate() error = %v, want %v", err, ErrTidyFailed)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("target exists after tidy failure: %v", statErr)
	}
}

func projectTree(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	}); err != nil {
		t.Fatalf("walk generated project: %v", err)
	}
	sort.Strings(files)
	return files
}

func testConfig(target string) Config {
	return Config{
		Name:      "demo",
		Module:    "example.com/demo",
		Type:      TypeWeb,
		TargetDir: target,
		SkipTidy:  true,
	}
}

func runGo(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("go", arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s failed: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}
