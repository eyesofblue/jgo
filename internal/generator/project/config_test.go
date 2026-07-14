package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*Config)
		want   error
	}{
		{
			name: "invalid name",
			change: func(config *Config) {
				config.Name = "../demo"
			},
			want: ErrInvalidName,
		},
		{
			name: "invalid module",
			change: func(config *Config) {
				config.Module = "example.com//demo"
			},
			want: ErrInvalidModule,
		},
		{
			name: "module with trailing dot",
			change: func(config *Config) {
				config.Module = "example.com/demo."
			},
			want: ErrInvalidModule,
		},
		{
			name: "invalid type",
			change: func(config *Config) {
				config.Type = "worker"
			},
			want: ErrInvalidType,
		},
		{
			name: "invalid version",
			change: func(config *Config) {
				config.JGOVersion = "latest"
			},
			want: ErrInvalidVersion,
		},
		{
			name: "unsupported Go version",
			change: func(config *Config) {
				config.GoVersion = "1.22.12"
			},
			want: ErrInvalidGoVersion,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := Config{
				Name:       "demo-app",
				Module:     "example.com/demo-app",
				Type:       TypeWeb,
				TargetDir:  t.TempDir() + "/project",
				JGOVersion: DefaultJGOVersion,
			}
			test.change(&config)
			if err := config.normalizeAndValidate(); !errors.Is(err, test.want) {
				t.Fatalf("normalizeAndValidate() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestConfigRejectsReplaceWithLookalikeModule(t *testing.T) {
	replacement := t.TempDir()
	if err := os.WriteFile(filepath.Join(replacement, "go.mod"), []byte("module github.com/eyesofblue/jgo-fake\n\ngo 1.24.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := Config{Name: "demo", Module: "example.com/demo", Type: TypeWeb, TargetDir: filepath.Join(t.TempDir(), "demo"), JGOReplace: replacement}
	if err := config.normalizeAndValidate(); !errors.Is(err, ErrInvalidReplace) {
		t.Fatalf("normalizeAndValidate() error = %v, want %v", err, ErrInvalidReplace)
	}
}

func TestConfigNormalizesDefaults(t *testing.T) {
	t.Parallel()

	config := Config{
		Name:      "123-Demo",
		Module:    "example.com/123-demo",
		Type:      TypeMixed,
		TargetDir: t.TempDir() + "/project",
	}
	if err := config.normalizeAndValidate(); err != nil {
		t.Fatalf("normalizeAndValidate() error = %v", err)
	}
	if config.JGOVersion != DefaultJGOVersion {
		t.Fatalf("JGOVersion = %q, want %q", config.JGOVersion, DefaultJGOVersion)
	}
	if DefaultJGOVersion != "v0.4.0" {
		t.Fatalf("DefaultJGOVersion = %q, want v0.4.0", DefaultJGOVersion)
	}
	if config.GoVersion != MinimumGoVersion {
		t.Fatalf("GoVersion = %q, want %q", config.GoVersion, MinimumGoVersion)
	}
	if config.PackageName != "app_123_demo" {
		t.Fatalf("PackageName = %q, want %q", config.PackageName, "app_123_demo")
	}
	if config.ServiceName != "App123DemoService" {
		t.Fatalf("ServiceName = %q, want %q", config.ServiceName, "App123DemoService")
	}
}

func TestNormalizeGoVersion(t *testing.T) {
	for input, want := range map[string]string{
		"1.24":      "1.24.0",
		"go1.25.12": "1.25.12",
	} {
		got, err := NormalizeGoVersion(input)
		if err != nil || got != want {
			t.Fatalf("NormalizeGoVersion(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
}

func TestServiceName(t *testing.T) {
	for input, want := range map[string]string{
		"user-rpc":      "UserRpcService",
		"order-service": "OrderService",
		"demo":          "DemoService",
	} {
		if got := serviceName(input); got != want {
			t.Fatalf("serviceName(%q) = %q, want %q", input, got, want)
		}
	}
}
