package project

import (
	"errors"
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
	if DefaultJGOVersion != "v0.1.0" {
		t.Fatalf("DefaultJGOVersion = %q, want v0.1.0", DefaultJGOVersion)
	}
	if config.PackageName != "app_123_demo" {
		t.Fatalf("PackageName = %q, want %q", config.PackageName, "app_123_demo")
	}
}
