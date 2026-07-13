package openapi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

type project struct {
	root      string
	module    string
	specPath  string
	modelPath string
}

func loadProject(root string) (project, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return project{}, fmt.Errorf("%w: resolve root: %v", ErrInvalidProject, err)
	}
	goModPath := filepath.Join(absolute, "go.mod")
	contents, err := os.ReadFile(goModPath)
	if err != nil {
		return project{}, fmt.Errorf("%w: read %s: %v", ErrInvalidProject, goModPath, err)
	}
	modulePath := modfile.ModulePath(contents)
	if modulePath == "" {
		return project{}, fmt.Errorf("%w: go.mod has no module directive", ErrInvalidProject)
	}
	specPath := filepath.Join(absolute, filepath.FromSlash(SpecPath))
	if info, err := os.Stat(specPath); err != nil || info.IsDir() {
		return project{}, fmt.Errorf("%w: missing %s", ErrInvalidProject, specPath)
	}
	return project{
		root:      absolute,
		module:    modulePath,
		specPath:  specPath,
		modelPath: filepath.Join(absolute, filepath.FromSlash(ModelPath)),
	}, nil
}
