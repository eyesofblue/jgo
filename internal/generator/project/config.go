package project

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

const (
	DefaultJGOVersion = "v0.5.0"
	MinimumGoVersion  = "1.24.0"
)

type Type string

const (
	TypeWeb   Type = "web"
	TypeGRPC  Type = "grpc"
	TypeMixed Type = "mixed"
	TypeProto Type = "proto"
)

// Config describes one generated project.
type Config struct {
	Name        string
	Module      string
	Type        Type
	TargetDir   string
	JGOVersion  string
	JGOReplace  string
	GoVersion   string
	SkipTidy    bool
	PackageName string
	ServiceName string
}

var (
	projectNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	versionPattern     = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
)

func (config *Config) normalizeAndValidate() error {
	config.Name = strings.TrimSpace(config.Name)
	config.Module = strings.TrimSpace(config.Module)
	config.TargetDir = strings.TrimSpace(config.TargetDir)
	config.JGOVersion = strings.TrimSpace(config.JGOVersion)
	config.JGOReplace = strings.TrimSpace(config.JGOReplace)
	config.GoVersion = strings.TrimSpace(config.GoVersion)

	if !projectNamePattern.MatchString(config.Name) || config.Name == "." || config.Name == ".." {
		return fmt.Errorf("%w: %q", ErrInvalidName, config.Name)
	}
	if module.CheckPath(config.Module) != nil {
		return fmt.Errorf("%w: %q", ErrInvalidModule, config.Module)
	}
	switch config.Type {
	case TypeWeb, TypeGRPC, TypeMixed, TypeProto:
	default:
		return fmt.Errorf("%w: %q", ErrInvalidType, config.Type)
	}
	if config.JGOVersion == "" {
		config.JGOVersion = DefaultJGOVersion
	}
	if !versionPattern.MatchString(config.JGOVersion) {
		return fmt.Errorf("%w: %q", ErrInvalidVersion, config.JGOVersion)
	}
	goVersion, err := NormalizeGoVersion(config.GoVersion)
	if err != nil {
		return err
	}
	config.GoVersion = goVersion

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
		if err != nil || modfile.ModulePath(goMod) != "github.com/eyesofblue/jgo" {
			return fmt.Errorf("%w: %q is not the JGO module", ErrInvalidReplace, replacement)
		}
		config.JGOReplace = replacement
	}

	config.PackageName = packageName(config.Name)
	config.ServiceName = serviceName(config.Name)
	return nil
}

// NormalizeGoVersion validates a Go release and returns the go.mod form.
func NormalizeGoVersion(version string) (string, error) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "go")
	if version == "" {
		version = MinimumGoVersion
	}
	parts := strings.Split(version, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return "", fmt.Errorf("%w: %q", ErrInvalidGoVersion, version)
	}
	values := []int{0, 0, 0}
	for index, part := range parts {
		if part == "" {
			return "", fmt.Errorf("%w: %q", ErrInvalidGoVersion, version)
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return "", fmt.Errorf("%w: %q", ErrInvalidGoVersion, version)
		}
		values[index] = value
	}
	if values[0] < 1 || (values[0] == 1 && values[1] < 24) {
		return "", fmt.Errorf("%w: require Go %s or newer, got %s", ErrInvalidGoVersion, MinimumGoVersion, version)
	}
	return fmt.Sprintf("%d.%d.%d", values[0], values[1], values[2]), nil
}

func packageName(name string) string {
	name = strings.ToLower(strings.ReplaceAll(name, "-", "_"))
	if name[0] >= '0' && name[0] <= '9' {
		name = "app_" + name
	}
	return name
}

func serviceName(name string) string {
	parts := strings.FieldsFunc(name, func(character rune) bool {
		return character == '-' || character == '_'
	})
	var builder strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		builder.WriteString(strings.ToUpper(part[:1]))
		builder.WriteString(part[1:])
	}
	name = builder.String()
	if name[0] >= '0' && name[0] <= '9' {
		name = "App" + name
	}
	if !strings.HasSuffix(strings.ToLower(name), "service") {
		name += "Service"
	}
	return name
}

func (config Config) HasWeb() bool {
	return config.Type == TypeWeb || config.Type == TypeMixed
}

func (config Config) HasGRPC() bool {
	return config.Type == TypeGRPC || config.Type == TypeMixed || config.Type == TypeProto
}

func (config Config) IsProto() bool { return config.Type == TypeProto }
