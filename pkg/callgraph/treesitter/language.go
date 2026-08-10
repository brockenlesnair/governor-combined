package treesitter

import (
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// Language represents a supported programming language.
type Language int

const (
	LangUnknown    Language = iota
	LangPython              // .py
	LangRust                // .rs
	LangTypeScript          // .ts, .tsx
	LangJavaScript          // .js, .jsx
)

// String returns the human-readable name of the language.
func (l Language) String() string {
	switch l {
	case LangPython:
		return "Python"
	case LangRust:
		return "Rust"
	case LangTypeScript:
		return "TypeScript"
	case LangJavaScript:
		return "JavaScript"
	default:
		return "Unknown"
	}
}

// DetectLanguage maps a file extension to a Language.
func DetectLanguage(filePath string) Language {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".py":
		return LangPython
	case ".rs":
		return LangRust
	case ".ts", ".tsx":
		return LangTypeScript
	case ".js", ".jsx":
		return LangJavaScript
	default:
		return LangUnknown
	}
}

// GrammarInfo holds a tree-sitter grammar and its associated query patterns
// for extracting function definitions and call sites.
type GrammarInfo struct {
	Language      *sitter.Language
	FuncQuery     string // tree-sitter query to find function definitions
	CallQuery     string // tree-sitter query to find function calls
	FuncNodeTypes []string
	CallNodeTypes []string
}

// supportedExtensions returns all file extensions this parser supports.
func supportedExtensions() []string {
	return []string{".py", ".rs", ".ts", ".tsx", ".js", ".jsx"}
}

// buildGrammarRegistry returns a map of Language → GrammarInfo for all
// supported languages. Each entry contains the grammar and query patterns
// tailored to that language's AST node types.
func buildGrammarRegistry() map[Language]*GrammarInfo {
	return map[Language]*GrammarInfo{
		LangPython: {
			Language:      python.GetLanguage(),
			FuncQuery:     pythonFuncQuery,
			CallQuery:     pythonCallQuery,
			FuncNodeTypes: []string{"function_definition"},
			CallNodeTypes: []string{"call"},
		},
		LangRust: {
			Language:      rust.GetLanguage(),
			FuncQuery:     rustFuncQuery,
			CallQuery:     rustCallQuery,
			FuncNodeTypes: []string{"function_item"},
			CallNodeTypes: []string{"call_expression"},
		},
		LangTypeScript: {
			Language:      typescript.GetLanguage(),
			FuncQuery:     typescriptFuncQuery,
			CallQuery:     typescriptCallQuery,
			FuncNodeTypes: []string{"function_declaration", "method_definition", "arrow_function", "function"},
			CallNodeTypes: []string{"call_expression"},
		},
		LangJavaScript: {
			Language:      javascript.GetLanguage(),
			FuncQuery:     javascriptFuncQuery,
			CallQuery:     javascriptCallQuery,
			FuncNodeTypes: []string{"function_declaration", "method_definition", "arrow_function", "function"},
			CallNodeTypes: []string{"call_expression"},
		},
	}
}

// isSupportedExt checks if the file extension is one we can parse.
func isSupportedExt(ext string) bool {
	for _, e := range supportedExtensions() {
		if strings.ToLower(ext) == e {
			return true
		}
	}
	return false
}

// nodeTypeSet returns a map for O(1) membership checks.
func nodeTypeSet(types []string) map[string]struct{} {
	m := make(map[string]struct{}, len(types))
	for _, t := range types {
		m[t] = struct{}{}
	}
	return m
}

func isExported(lang Language, name string, node *sitter.Node, src []byte) bool {
	switch lang {
	case LangPython:
		return len(name) > 0 && name[0] != '_'
	case LangRust:
		return isExportedRust(node, src)
	case LangTypeScript, LangJavaScript:
		return isExportedJS(node, src)
	default:
		return false
	}
}

func isExportedJS(node *sitter.Node, src []byte) bool {
	if node == nil {
		return false
	}
	parent := node.Parent()
	if parent == nil {
		return false
	}
	parentContent := parent.Content(src)
	return strings.HasPrefix(strings.TrimSpace(parentContent), "export ")
}

// fileID builds the canonical file identifier from package path and file name.
// Format: "package_path/file_name"
func fileID(pkgPath, fileName string) string {
	return fmt.Sprintf("%s/%s", pkgPath, fileName)
}

// nodeID builds the canonical node identifier.
// Format: "package_path/file_name:function_name"
func nodeID(pkgPath, fileName, funcName string) string {
	return fmt.Sprintf("%s/%s:%s", pkgPath, fileName, funcName)
}
