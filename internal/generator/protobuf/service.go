package protobuf

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bufbuild/protocompile/ast"
)

// AddServiceConfig describes a protobuf Service to add to a contract file.
type AddServiceConfig struct {
	Root    string
	File    string
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
