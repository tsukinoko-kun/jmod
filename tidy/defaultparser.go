package tidy

import (
	"fmt"
	"os"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_tsx "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type DefaultParser struct{}

func init() {
	defaultParser := DefaultParser{}
	parsers[".js"] = defaultParser
	parsers[".mjs"] = defaultParser
	parsers[".cjs"] = defaultParser
	parsers[".ts"] = defaultParser
	parsers[".mts"] = defaultParser
	parsers[".cts"] = defaultParser
	parsers[".jsx"] = defaultParser
	parsers[".tsx"] = defaultParser
}

// ParseImports parses a TypeScript/TSX file and returns all imported package names
func (p DefaultParser) ParseImports(filePath string) ([]string, error) {
	// Read the file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Create parser
	parser := tree_sitter.NewParser()
	tsxLang := tree_sitter.NewLanguage(tree_sitter_tsx.LanguageTSX())
	parser.SetLanguage(tsxLang)

	// Parse the content
	tree := parser.Parse(content, nil)
	if tree == nil {
		return nil, fmt.Errorf("failed to parse file")
	}
	defer tree.Close()

	root := tree.RootNode()

	// Collect all imports
	imports := make(map[string]bool)

	// Process the AST to find imports
	processNode(root, content, imports)

	// Convert map to slice
	result := make([]string, 0, len(imports))
	for pkg := range imports {
		result = append(result, pkg)
	}

	return result, nil
}

// processNode recursively processes AST nodes to find imports
func processNode(node *tree_sitter.Node, source []byte, imports map[string]bool) {
	nodeType := node.Kind()

	switch nodeType {
	case "import_statement":
		// Handle ES6 imports: import ... from "package"
		processImportStatement(node, source, imports)

	case "call_expression":
		// Handle require() and dynamic import()
		processCallExpression(node, source, imports)
	}

	// Recursively process children
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		processNode(child, source, imports)
	}
}

// processImportStatement extracts package name from import statements
func processImportStatement(node *tree_sitter.Node, source []byte, imports map[string]bool) {
	// Find the string node (the module specifier)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "string" {
			pkgName := extractStringValue(child, source)
			if pkgName != "" && !isRelativePath(pkgName) {
				imports[pkgName] = true
			}
		}
	}
}

// processCallExpression handles require() and import() calls
func processCallExpression(node *tree_sitter.Node, source []byte, imports map[string]bool) {
	if node.ChildCount() < 2 {
		return
	}

	// Get the function being called
	fn := node.Child(0)
	if fn == nil {
		return
	}

	fnText := string(source[fn.StartByte():fn.EndByte()])

	// Check if it's require or import
	if fnText == "require" || fnText == "import" {
		// Get the arguments node
		args := node.ChildByFieldName("arguments")
		if args == nil {
			return
		}

		// Find string arguments
		for i := uint(0); i < args.ChildCount(); i++ {
			arg := args.Child(i)
			if arg.Kind() == "string" {
				pkgName := extractStringValue(arg, source)
				if pkgName != "" && !isRelativePath(pkgName) {
					imports[pkgName] = true
				}
			}
		}
	}
}

// extractStringValue extracts the actual string value from a string node
func extractStringValue(node *tree_sitter.Node, source []byte) string {
	if node.Kind() != "string" {
		return ""
	}

	// Get the raw string including quotes
	raw := string(source[node.StartByte():node.EndByte()])

	// Remove quotes (single or double)
	raw = strings.Trim(raw, "\"'`")

	return raw
}

// isRelativePath checks if a path is relative (starts with ./ or ../ or ~/)
func isRelativePath(path string) bool {
	return strings.HasPrefix(path, "./") ||
		strings.HasPrefix(path, "../") ||
		strings.HasPrefix(path, "~/") ||
		strings.HasPrefix(path, "@/") ||
		strings.HasPrefix(path, "/")
}
