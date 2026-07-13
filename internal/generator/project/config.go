package project

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const DefaultJGOVersion = "v0.1.0"

type Type string

const (
	TypeWeb   Type = "web"
	TypeGRPC  Type = "grpc"
	TypeMixed Type = "mixed"
)

// Config describes one generated project.
type Config struct {
	Name        string
	Module      string
	Type        Type
	TargetDir   string
	JGOVersion  string
	JGOReplace  string
	PackageName string
}

var (
	projectNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	modulePartPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~+-]*$`)
	versionPattern     = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
)

func (config *Config) normalizeAndValidate() error {
	config.Name = strings.TrimSpace(config.Name)
	config.Module = strings.TrimSpace(config.Module)
	config.TargetDir = strings.TrimSpace(config.TargetDir)
	config.JGOVersion = strings.TrimSpace(config.JGOVersion)
	config.JGOReplace = strings.TrimSpace(config.JGOReplace)

	if !projectNamePattern.MatchString(config.Name) || config.Name == "." || config.Name == ".." {
		return fmt.Errorf("%w: %q", ErrInvalidName, config.Name)
	}
	if !validModulePath(config.Module) {
		return fmt.Errorf("%w: %q", ErrInvalidModule, config.Module)
	}
	switch config.Type {
	case TypeWeb, TypeGRPC, TypeMixed:
	default:
		return fmt.Errorf("%w: %q", ErrInvalidType, config.Type)
	}
	if config.JGOVersion == "" {
		config.JGOVersion = DefaultJGOVersion
	}
	if !versionPattern.MatchString(config.JGOVersion) {
		return fmt.Errorf("%w: %q", ErrInvalidVersion, config.JGOVersion)
	}

	if config.TargetDir == "" {
		config.TargetDir = config.Name
	}
	target, err := filepath.Abs(config.TargetDir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTarget, err)
	}
	if target == filepath.Dir(target) {
		return fmt.Errorf("%w: refusing filesystem root %q", ErrInvalidTarget, target)
	}
	config.TargetDir = target

	if config.JGOReplace != "" {
		replacement, err := filepath.Abs(config.JGOReplace)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidReplace, err)
		}
		info, err := os.Stat(replacement)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("%w: %q is not a directory", ErrInvalidReplace, replacement)
		}
		goMod, err := os.ReadFile(filepath.Join(replacement, "go.mod"))
		if err != nil || !strings.Contains(string(goMod), "module github.com/eyesofblue/jgo") {
			return fmt.Errorf("%w: %q is not the JGO module", ErrInvalidReplace, replacement)
		}
		config.JGOReplace = replacement
	}

	config.PackageName = packageName(config.Name)
	return nil
}

func validModulePath(module string) bool {
	if module == "" || strings.ContainsAny(module, " \t\r\n\\") {
		return false
	}
	parts := strings.Split(module, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || !modulePartPattern.MatchString(part) {
			return false
		}
	}
	return true
}

func packageName(name string) string {
	name = strings.ToLower(strings.ReplaceAll(name, "-", "_"))
	if name[0] >= '0' && name[0] <= '9' {
		name = "app_" + name
	}
	return name
}

func (config Config) HasWeb() bool {
	return config.Type == TypeWeb || config.Type == TypeMixed
}

func (config Config) HasGRPC() bool {
	return config.Type == TypeGRPC || config.Type == TypeMixed
}
