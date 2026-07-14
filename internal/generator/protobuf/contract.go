package protobuf

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ValidateResponseContracts requires every RPC response to declare JGO's
// non-optional business status fields, including responses imported from other
// protobuf files.
func ValidateResponseContracts(root string) error {
	compiled, protoDirectory, err := compileResponseContracts(root)
	if err != nil {
		return err
	}
	var violations []string
	for _, file := range compiled {
		services := file.Services()
		for serviceIndex := 0; serviceIndex < services.Len(); serviceIndex++ {
			service := services.Get(serviceIndex)
			methods := service.Methods()
			for methodIndex := 0; methodIndex < methods.Len(); methodIndex++ {
				method := methods.Get(methodIndex)
				response := method.Output()
				if standardResponse(response) {
					continue
				}
				contractPath := filepath.Join(protoDirectory, filepath.FromSlash(file.Path()))
				violations = append(violations, fmt.Sprintf(
					"%s response %s must declare non-optional `int32 code = 1;` and `string msg = 2;` (%s)",
					method.FullName(), response.FullName(), displayPath(root, contractPath),
				))
			}
		}
	}
	if len(violations) == 0 {
		return nil
	}
	sort.Strings(violations)
	return fmt.Errorf("protobuf: invalid RPC response contract:\n- %s", strings.Join(violations, "\n- "))
}

func compileResponseContracts(root string) ([]protoreflect.FileDescriptor, string, error) {
	if root == "" {
		root = "."
	}
	protoDirectory := filepath.Join(root, filepath.FromSlash(protoRoot))
	files, err := candidateFiles(root, "")
	if err != nil {
		return nil, "", err
	}
	paths := make([]string, 0, len(files))
	for _, path := range files {
		relative, err := filepath.Rel(protoDirectory, path)
		if err != nil {
			return nil, "", fmt.Errorf("protobuf: resolve contract path %s: %w", path, err)
		}
		paths = append(paths, filepath.ToSlash(relative))
	}
	sort.Strings(paths)
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{ImportPaths: []string{protoDirectory}}),
	}
	compiled, err := compiler.Compile(context.Background(), paths...)
	if err != nil {
		return nil, "", fmt.Errorf("protobuf: compile response contracts: %w", err)
	}
	descriptors := make([]protoreflect.FileDescriptor, len(compiled))
	for index, file := range compiled {
		descriptors[index] = file
	}
	return descriptors, protoDirectory, nil
}

func standardResponse(message protoreflect.MessageDescriptor) bool {
	return standardResponseField(message.Fields().ByName("code"), 1, protoreflect.Int32Kind) &&
		standardResponseField(message.Fields().ByName("msg"), 2, protoreflect.StringKind)
}

func standardResponseField(field protoreflect.FieldDescriptor, number protoreflect.FieldNumber, kind protoreflect.Kind) bool {
	return field != nil && field.Number() == number && field.Kind() == kind &&
		!field.HasPresence() && !field.IsList() && !field.IsMap()
}
