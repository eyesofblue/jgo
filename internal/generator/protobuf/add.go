// Package protobuf safely updates protobuf contracts and generates gRPC code.
package protobuf

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bufbuild/protocompile/ast"
	"github.com/bufbuild/protocompile/parser"
	"github.com/bufbuild/protocompile/reporter"
)

const protoRoot = "api/proto"

// AddConfig describes one RPC to add to an existing protobuf service.
type AddConfig struct {
	Root    string
	File    string
	Service string
	RPC     string
}

type parsedFile struct {
	path    string
	source  []byte
	root    *ast.FileNode
	service *ast.ServiceNode
}

// Add adds an RPC, an empty request message, and a response message with JGO's
// required business status fields to a protobuf contract.
func Add(config AddConfig) (string, error) {
	if config.Root == "" {
		config.Root = "."
	}
	if err := validateIdentifier("RPC", config.RPC); err != nil {
		return "", err
	}
	if err := validateIdentifier("service", config.Service); err != nil {
		return "", err
	}

	files, err := candidateFiles(config.Root, config.File)
	if err != nil {
		return "", err
	}
	matches := make([]parsedFile, 0, 1)
	availableServices := map[string]bool{}
	for _, path := range files {
		parsed, err := parseFile(path)
		if err != nil {
			return "", err
		}
		for _, declaration := range parsed.root.Decls {
			service, ok := declaration.(*ast.ServiceNode)
			if !ok {
				continue
			}
			availableServices[service.Name.Val] = true
			if service.Name.Val == config.Service {
				copy := parsed
				copy.service = service
				matches = append(matches, copy)
			}
		}
	}
	if len(matches) == 0 {
		available := make([]string, 0, len(availableServices))
		for service := range availableServices {
			available = append(available, service)
		}
		sort.Strings(available)
		hint := ""
		if len(available) > 0 {
			hint = "; available services: " + strings.Join(available, ", ")
		}
		return "", fmt.Errorf("protobuf: service %q not found under %s%s", config.Service, filepath.Join(config.Root, protoRoot), hint)
	}
	if len(matches) > 1 {
		paths := make([]string, 0, len(matches))
		for _, match := range matches {
			paths = append(paths, displayPath(config.Root, match.path))
		}
		return "", fmt.Errorf("protobuf: service %q is declared in multiple files; select one with --file: %s", config.Service, strings.Join(paths, ", "))
	}

	match := matches[0]
	requestName := config.RPC + "Request"
	responseName := config.RPC + "Response"
	for _, declaration := range match.root.Decls {
		if message, ok := declaration.(*ast.MessageNode); ok && (message.Name.Val == requestName || message.Name.Val == responseName) {
			return "", fmt.Errorf("protobuf: message %q already exists in %s", message.Name.Val, displayPath(config.Root, match.path))
		}
	}
	for _, declaration := range match.service.Decls {
		if rpc, ok := declaration.(*ast.RPCNode); ok && rpc.Name.Val == config.RPC {
			return "", fmt.Errorf("protobuf: RPC %q already exists in service %q", config.RPC, config.Service)
		}
	}

	closeOffset := match.root.NodeInfo(match.service.CloseBrace).Start().Offset
	if closeOffset < 0 || closeOffset > len(match.source) {
		return "", errors.New("protobuf: invalid service source location")
	}
	rpcDeclaration := fmt.Sprintf("  rpc %s(%s) returns (%s);\n", config.RPC, requestName, responseName)
	messageDeclarations := fmt.Sprintf("\n\nmessage %s {\n}\n\nmessage %s {\n  int32 code = 1;\n  string msg = 2;\n}\n", requestName, responseName)
	updated := make([]byte, 0, len(match.source)+len(rpcDeclaration)+len(messageDeclarations))
	updated = append(updated, match.source[:closeOffset]...)
	updated = append(updated, rpcDeclaration...)
	updated = append(updated, match.source[closeOffset:]...)
	updated = bytes.TrimRight(updated, " \t\r\n")
	updated = append(updated, messageDeclarations...)

	if _, err := parse(match.path, updated); err != nil {
		return "", fmt.Errorf("protobuf: generated invalid contract: %w", err)
	}
	if err := atomicWrite(match.path, updated); err != nil {
		return "", fmt.Errorf("protobuf: write %s: %w", displayPath(config.Root, match.path), err)
	}
	return displayPath(config.Root, match.path), nil
}

func candidateFiles(root, selected string) ([]string, error) {
	protoDirectory := filepath.Join(root, filepath.FromSlash(protoRoot))
	if selected != "" {
		path := selected
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if filepath.Ext(path) != ".proto" {
			return nil, fmt.Errorf("protobuf: --file must name a .proto file: %s", selected)
		}
		inside, err := pathInside(protoDirectory, path)
		if err != nil || !inside {
			return nil, fmt.Errorf("protobuf: --file must be inside %s", protoDirectory)
		}
		if err := rejectSymlinkPath(protoDirectory, path); err != nil {
			return nil, err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("protobuf: inspect %s: %w", selected, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("protobuf: --file must name a regular file, not a symlink: %s", selected)
		}
		return []string{filepath.Clean(path)}, nil
	}

	var files []string
	err := filepath.WalkDir(protoDirectory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("protobuf: refusing symlink %s", path)
		}
		if entry.Type().IsRegular() && filepath.Ext(entry.Name()) == ".proto" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("protobuf: scan %s: %w", protoDirectory, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("protobuf: no .proto files found under %s", protoDirectory)
	}
	sort.Strings(files)
	return files, nil
}

func rejectSymlinkPath(root, path string) error {
	inside, err := pathInside(root, path)
	if err != nil || !inside {
		return fmt.Errorf("protobuf: path must be inside %s", root)
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(absoluteRoot, absolutePath)
	if err != nil {
		return err
	}
	current := absoluteRoot
	parts := []string{"."}
	if relative != "." {
		parts = append(parts, strings.Split(relative, string(filepath.Separator))...)
	}
	for _, part := range parts {
		if part != "." {
			current = filepath.Join(current, part)
		}
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("protobuf: refusing symlink %s", current)
		}
	}
	return nil
}

func pathInside(directory, path string) (bool, error) {
	absoluteDirectory, err := filepath.Abs(directory)
	if err != nil {
		return false, err
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	relative, err := filepath.Rel(absoluteDirectory, absolutePath)
	if err != nil {
		return false, err
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)), nil
}

func parseFile(path string) (parsedFile, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return parsedFile{}, fmt.Errorf("protobuf: read %s: %w", path, err)
	}
	root, err := parse(path, source)
	if err != nil {
		return parsedFile{}, err
	}
	return parsedFile{path: path, source: source, root: root}, nil
}

func parse(path string, source []byte) (*ast.FileNode, error) {
	root, err := parser.Parse(path, bytes.NewReader(source), reporter.NewHandler(nil))
	if err != nil {
		return nil, fmt.Errorf("protobuf: parse %s: %w", path, err)
	}
	return root, nil
}

func validateIdentifier(kind, value string) error {
	if value == "" {
		return fmt.Errorf("protobuf: %s name is required", kind)
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || character == '_' || (index > 0 && character >= '0' && character <= '9') {
			continue
		}
		return fmt.Errorf("protobuf: invalid %s name %q", kind, value)
	}
	if value[0] < 'A' || value[0] > 'Z' {
		return fmt.Errorf("protobuf: %s name %q must start with an uppercase letter", kind, value)
	}
	return nil
}

func displayPath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(relative)
	}
	return path
}

func atomicWrite(path string, contents []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".jgo-protobuf-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
