package protobuf

import (
	"bytes"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/bufbuild/protocompile/ast"
)

// AddServiceConfig describes a protobuf Service to add to a contract file.
type AddServiceConfig struct {
	Root    string
	File    string
	Package string
	Service string
}

// AddService adds an empty protobuf Service definition. RPC methods are added
// separately with Add so one contract can contain multiple Services.
func AddService(config AddServiceConfig) (string, error) {
	if config.Root == "" {
		config.Root = "."
	}
	if err := validateIdentifier("service", config.Service); err != nil {
		return "", err
	}

	hasContracts, err := HasContracts(config.Root)
	if err != nil {
		return "", err
	}
	packageName := strings.TrimSpace(config.Package)
	if packageName != "" && config.File != "" {
		return "", fmt.Errorf("protobuf: --package and --file cannot be used together")
	}
	if packageName != "" {
		if err := validateProtoPackage(packageName); err != nil {
			return "", err
		}
		files, filesErr := allProtoFiles(config.Root)
		if filesErr != nil {
			return "", filesErr
		}
		var matches []string
		for _, path := range files {
			sourceName, packageErr := sourcePackage(path)
			if packageErr != nil {
				return "", packageErr
			}
			if sourceName == packageName {
				matches = append(matches, path)
			}
		}
		if len(matches) > 1 {
			paths := make([]string, len(matches))
			for index, path := range matches {
				paths[index] = displayPath(config.Root, path)
			}
			return "", fmt.Errorf("protobuf: package %q spans multiple files; select the target with --file: %s", packageName, strings.Join(paths, ", "))
		}
		if len(matches) == 1 {
			config.File = displayPath(config.Root, matches[0])
			packageName = ""
		}
		if packageName != "" {
			return createPackageService(config, packageName)
		}
	}
	if !hasContracts {
		if config.File != "" {
			return "", fmt.Errorf("protobuf: --file cannot select a file before the first contract exists; use --package")
		}
		return createPackageService(config, defaultProtoPackage(config.Root))
	}
	targetFiles, err := candidateFiles(config.Root, config.File)
	if err != nil {
		return "", err
	}
	if config.File == "" && len(targetFiles) != 1 {
		return "", fmt.Errorf("protobuf: found %d protobuf files under %s; select the target with --file", len(targetFiles), filepath.Join(config.Root, protoRoot))
	}
	target := targetFiles[0]
	files, err := candidateFiles(config.Root, "")
	if err != nil {
		return "", err
	}
	for _, path := range files {
		parsed, err := parseFile(path)
		if err != nil {
			return "", err
		}
		for _, declaration := range parsed.root.Decls {
			service, ok := declaration.(*ast.ServiceNode)
			if ok && service.Name.Val == config.Service {
				return "", fmt.Errorf("protobuf: service %q already exists in %s", config.Service, displayPath(config.Root, path))
			}
		}
	}

	contents, err := os.ReadFile(target)
	if err != nil {
		return "", fmt.Errorf("protobuf: read %s: %w", displayPath(config.Root, target), err)
	}
	updated := bytes.TrimRight(contents, " \t\r\n")
	updated = append(updated, []byte(fmt.Sprintf("\n\nservice %s {\n}\n", config.Service))...)
	if _, err := parse(target, updated); err != nil {
		return "", fmt.Errorf("protobuf: generated invalid service contract: %w", err)
	}
	if err := atomicWrite(target, updated); err != nil {
		return "", fmt.Errorf("protobuf: write %s: %w", displayPath(config.Root, target), err)
	}
	return displayPath(config.Root, target), nil
}

func createPackageService(config AddServiceConfig, packageName string) (string, error) {
	modulePath, err := readModule(config.Root)
	if err != nil {
		return "", err
	}
	if err := validateProtoPackage(packageName); err != nil {
		return "", err
	}
	parts := strings.Split(packageName, ".")
	relativeDir := filepath.Join(append([]string{protoRoot}, parts...)...)
	target := filepath.Join(config.Root, relativeDir, "service.proto")
	if err := rejectSymlinkPath(filepath.Join(config.Root, filepath.FromSlash(protoRoot)), target); err != nil {
		return "", err
	}
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("protobuf: refuse to overwrite existing contract %s", displayPath(config.Root, target))
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("protobuf: inspect %s: %w", displayPath(config.Root, target), err)
	}
	goPackagePath := modulePath + "/gen/pb/" + strings.Join(parts, "/")
	alias := parts[len(parts)-1]
	if len(parts) > 1 {
		alias = parts[len(parts)-2] + alias
	}
	alias = strings.ReplaceAll(alias, "_", "")
	if alias == "" || token.IsKeyword(alias) || (alias[0] >= '0' && alias[0] <= '9') {
		alias = "pb"
	}
	source := fmt.Sprintf("syntax = \"proto3\";\n\npackage %s;\n\noption go_package = %q;\n\nservice %s {\n}\n", packageName, goPackagePath+";"+alias, config.Service)
	if _, err := parse(target, []byte(source)); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := atomicWrite(target, []byte(source)); err != nil {
		return "", err
	}
	return displayPath(config.Root, target), nil
}

func defaultProtoPackage(root string) string {
	name := filepath.Base(root)
	if absolute, err := filepath.Abs(root); err == nil {
		name = filepath.Base(absolute)
	}
	name = strings.ToLower(name)
	var normalized strings.Builder
	for _, character := range name {
		if (character >= 'a' && character <= 'z') || character == '_' || (character >= '0' && character <= '9') {
			normalized.WriteRune(character)
		} else {
			normalized.WriteByte('_')
		}
	}
	name = normalized.String()
	if name == "" {
		name = "api"
	}
	if name[0] >= '0' && name[0] <= '9' {
		name = "app_" + name
	}
	return name + ".v1"
}

func validateProtoPackage(packageName string) error {
	for _, part := range strings.Split(packageName, ".") {
		if !validProtoPackagePart(part) {
			return fmt.Errorf("protobuf: invalid package %q", packageName)
		}
	}
	return nil
}

func allProtoFiles(root string) ([]string, error) {
	hasContracts, err := HasContracts(root)
	if err != nil || !hasContracts {
		return nil, err
	}
	return candidateFiles(root, "")
}

func sourcePackage(path string) (string, error) {
	parsed, err := parseFile(path)
	if err != nil {
		return "", err
	}
	for _, declaration := range parsed.root.Decls {
		if packageNode, ok := declaration.(*ast.PackageNode); ok {
			return string(packageNode.Name.AsIdentifier()), nil
		}
	}
	return "", nil
}

func validProtoPackagePart(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || character == '_' || (index > 0 && character >= '0' && character <= '9') {
			continue
		}
		return false
	}
	return true
}
