package project

import (
	"bytes"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	templatefs "github.com/eyesofblue/jgo/internal/template"
)

// Generate creates a project transactionally. Templates are fully rendered in
// a sibling temporary directory before the target path is changed.
func Generate(input Config) (string, error) {
	config := input
	if err := config.normalizeAndValidate(); err != nil {
		return "", err
	}
	targetWasEmpty, err := inspectTarget(config.TargetDir)
	if err != nil {
		return "", err
	}

	parent := filepath.Dir(config.TargetDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("create target parent: %w", err)
	}
	temporary, err := os.MkdirTemp(parent, "."+filepath.Base(config.TargetDir)+".jgo-")
	if err != nil {
		return "", fmt.Errorf("create temporary project: %w", err)
	}
	defer os.RemoveAll(temporary)

	templates, err := templatefs.Project()
	if err != nil {
		return "", fmt.Errorf("open embedded templates: %w", err)
	}
	sets := []string{"common"}
	if config.HasWeb() {
		sets = append(sets, "web")
	}
	if config.HasGRPC() {
		sets = append(sets, "grpc")
	}
	sets = append(sets, filepath.ToSlash(filepath.Join("main", string(config.Type))))
	for _, set := range sets {
		if err := renderSet(templates, set, temporary, config); err != nil {
			return "", err
		}
	}
	if err := validateGenerated(temporary, config); err != nil {
		return "", err
	}

	if targetWasEmpty {
		if err := os.Remove(config.TargetDir); err != nil {
			return "", fmt.Errorf("replace empty target: %w", err)
		}
	}
	if err := os.Rename(temporary, config.TargetDir); err != nil {
		if targetWasEmpty {
			_ = os.Mkdir(config.TargetDir, 0o755)
		}
		return "", fmt.Errorf("commit generated project: %w", err)
	}
	return config.TargetDir, nil
}

func inspectTarget(target string) (wasEmpty bool, err error) {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect target: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("%w: %s", ErrTargetIsSymlink, target)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%w: %s", ErrTargetExists, target)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return false, fmt.Errorf("read target directory: %w", err)
	}
	if len(entries) != 0 {
		return false, fmt.Errorf("%w: %s", ErrTargetNotEmpty, target)
	}
	return true, nil
}

func renderSet(templates fs.FS, set, destination string, config Config) error {
	return fs.WalkDir(templates, set, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(set, path)
		if err != nil {
			return err
		}
		relative = filepath.Clean(strings.TrimSuffix(relative, ".tmpl"))
		relative = strings.Replace(relative, filepath.Join("api", "proto", "project"), filepath.Join("api", "proto", config.PackageName), 1)
		if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%w: %s", ErrUnsafeTemplate, path)
		}
		contents, err := fs.ReadFile(templates, path)
		if err != nil {
			return fmt.Errorf("read template %s: %w", path, err)
		}
		rendered, err := render(path, contents, config)
		if err != nil {
			return err
		}
		outputPath := filepath.Join(destination, relative)
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return fmt.Errorf("create template directory: %w", err)
		}
		if strings.HasSuffix(outputPath, ".go") {
			rendered, err = format.Source(rendered)
			if err != nil {
				return fmt.Errorf("format generated Go file %s: %w", relative, err)
			}
		}
		if err := os.WriteFile(outputPath, rendered, 0o644); err != nil {
			return fmt.Errorf("write generated file %s: %w", relative, err)
		}
		return nil
	})
}

func render(name string, contents []byte, config Config) ([]byte, error) {
	tmpl, err := template.New(name).Option("missingkey=error").Parse(string(contents))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, config); err != nil {
		return nil, fmt.Errorf("render template %s: %w", name, err)
	}
	return output.Bytes(), nil
}

func validateGenerated(root string, config Config) error {
	required := []string{"go.mod", "README.md", "Makefile", "cmd/server/main.go", "internal/service/service.go"}
	if config.HasWeb() {
		required = append(required, "api/http/openapi.yaml", "internal/transport/http/routes.go")
	}
	if config.HasGRPC() {
		required = append(required, "api/proto/"+config.PackageName+"/v1/service.proto", "internal/transport/grpc/register.go", "internal/transport/grpc/register.gen.go", "buf.yaml", "buf.gen.yaml")
	}
	sort.Strings(required)
	for _, relative := range required {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil || info.IsDir() {
			return fmt.Errorf("%w: missing %s", ErrGeneratedInvalid, relative)
		}
	}
	return nil
}
