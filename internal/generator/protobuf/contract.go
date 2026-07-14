package protobuf

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bufbuild/protocompile/ast"
)

// ResponseContractWarnings reports RPC responses that do not follow JGO's
// non-blocking business response convention. It intentionally returns warnings
// so existing protobuf contracts can still be generated during migration.
func ResponseContractWarnings(root string) ([]string, error) {
	if root == "" {
		root = "."
	}
	files, err := candidateFiles(root, "")
	if err != nil {
		return nil, err
	}
	var warnings []string
	for _, path := range files {
		parsed, err := parseFile(path)
		if err != nil {
			return nil, err
		}
		messages := map[string]*ast.MessageNode{}
		for _, declaration := range parsed.root.Decls {
			if message, ok := declaration.(*ast.MessageNode); ok {
				messages[message.Name.Val] = message
			}
		}
		for _, declaration := range parsed.root.Decls {
			service, ok := declaration.(*ast.ServiceNode)
			if !ok {
				continue
			}
			for _, element := range service.Decls {
				rpc, ok := element.(*ast.RPCNode)
				if !ok {
					continue
				}
				responseName := lastIdentifier(string(rpc.Output.MessageType.AsIdentifier()))
				response := messages[responseName]
				// Imported response types cannot be checked reliably from a syntax-only
				// pass, so leave them to their owning contract.
				if response == nil || standardResponse(response) {
					continue
				}
				warnings = append(warnings, fmt.Sprintf(
					"%s.%s response %s should declare non-optional `int32 code = 1;` and `string msg = 2;` (%s)",
					service.Name.Val, rpc.Name.Val, responseName, displayPath(root, path),
				))
			}
		}
	}
	sort.Strings(warnings)
	return warnings, nil
}

func standardResponse(message *ast.MessageNode) bool {
	var codeOK, messageOK bool
	for _, declaration := range message.Decls {
		field, ok := declaration.(*ast.FieldNode)
		if !ok || field.Label.IsPresent() {
			continue
		}
		typeName := string(field.FldType.AsIdentifier())
		switch field.Name.Val {
		case "code":
			codeOK = typeName == "int32" && field.Tag.Val == 1
		case "msg":
			messageOK = typeName == "string" && field.Tag.Val == 2
		}
	}
	return codeOK && messageOK
}

func lastIdentifier(identifier string) string {
	identifier = strings.TrimPrefix(identifier, ".")
	if index := strings.LastIndexByte(identifier, '.'); index >= 0 {
		return identifier[index+1:]
	}
	return identifier
}
