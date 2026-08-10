package treesitter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
)

// Parser implements callgraph.SourceParser using tree-sitter grammars.
// It supports Python, Rust, TypeScript, and JavaScript via grammar-specific
// query patterns. Thread-safe: parser instances are pooled via sync.Pool.
type Parser struct {
	grammars map[Language]*GrammarInfo
	pool     sync.Pool
}

// NewParser creates a Tree-sitter parser with all supported grammars.
func NewParser() *Parser {
	p := &Parser{
		grammars: buildGrammarRegistry(),
	}
	p.pool = sync.Pool{
		New: func() any {
			return sitter.NewParser()
		},
	}
	return p
}

// getParser acquires a parser from the pool and configures it for the given language.
func (p *Parser) getParser(lang Language) *sitter.Parser {
	par := p.pool.Get().(*sitter.Parser)
	if gi, ok := p.grammars[lang]; ok {
		par.SetLanguage(gi.Language)
	}
	return par
}

// putParser returns a parser to the pool.
func (p *Parser) putParser(par *sitter.Parser) {
	p.pool.Put(par)
}

// ParsePackages discovers all supported source files under rootPath and returns
// PackageInfo entries grouped by directory. Each directory is treated as a
// logical "package" with all its supported source files.
func (p *Parser) ParsePackages(ctx context.Context, rootPath string) ([]*callgraph.PackageInfo, error) {
	info, err := os.Stat(rootPath)
	if err != nil {
		return nil, fmt.Errorf("access path %s: %w", rootPath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path %s is not a directory", rootPath)
	}

	// Collect files by directory (each dir = a package)
	dirFiles := make(map[string][]string)

	err = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip inaccessible entries
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Skip hidden directories and common non-source dirs
		if d.IsDir() {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" || base == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if isSupportedExt(ext) {
			dir := filepath.Dir(path)
			dirFiles[dir] = append(dirFiles[dir], path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory: %w", err)
	}

	var packages []*callgraph.PackageInfo
	// Sort directories for deterministic output
	dirs := make([]string, 0, len(dirFiles))
	for dir := range dirFiles {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		files := dirFiles[dir]
		sort.Strings(files)

		// Derive package name from the directory name
		pkgName := filepath.Base(dir)

		pkg := &callgraph.PackageInfo{
			Name:  pkgName,
			Path:  dir,
			Files: files,
		}
		packages = append(packages, pkg)
	}

	return packages, nil
}

// ParseFile parses a single source file using the appropriate tree-sitter grammar
// and extracts function/method nodes and call edges.
func (p *Parser) ParseFile(ctx context.Context, filePath string, pkgInfo *callgraph.PackageInfo) ([]*callgraph.Node, []*callgraph.Edge, error) {
	lang := DetectLanguage(filePath)
	if lang == LangUnknown {
		return nil, nil, fmt.Errorf("unsupported language for file %s", filePath)
	}

	gi, ok := p.grammars[lang]
	if !ok {
		return nil, nil, fmt.Errorf("no grammar registered for language %s (file %s)", lang, filePath)
	}

	src, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("read file %s: %w", filePath, err)
	}

	// Parse with pooled parser
	par := p.getParser(lang)
	defer p.putParser(par)

	tree, err := par.ParseCtx(ctx, nil, src)
	if err != nil {
		return nil, nil, fmt.Errorf("parse file %s: %w", filePath, err)
	}
	defer tree.Close()

	root := tree.RootNode()
	fileName := filepath.Base(filePath)
	pkgPath := pkgInfo.Path

	// Phase 1: Extract all function/method definitions
	type funcDef struct {
		node       *callgraph.Node
		startByte  uint32
		endByte    uint32
	}
	var funcDefs []funcDef

	funcQuery, err := sitter.NewQuery([]byte(gi.FuncQuery), gi.Language)
	if err != nil {
		return nil, nil, fmt.Errorf("compile func query for %s: %w", lang, err)
	}
	funcCursor := sitter.NewQueryCursor()
	funcCursor.Exec(funcQuery, root)

	for {
		m, ok := funcCursor.NextMatch()
		if !ok {
			break
		}

		var funcName string
		var funcNode *sitter.Node
		var params string

		for _, c := range m.Captures {
			name := funcQuery.CaptureNameForId(c.Index)
			content := c.Node.Content(src)
			switch name {
			case "func_name":
				funcName = content
				funcNode = c.Node
			case "params":
				params = content
			}
		}

		// Try to capture the definition node from @func_def
		for _, c := range m.Captures {
			if funcQuery.CaptureNameForId(c.Index) == "func_def" {
				funcNode = c.Node
				break
			}
		}

		if funcName == "" || funcNode == nil {
			continue
		}

		kind := callgraph.NodeKindFunction
		receiver := ""
		exported := isExported(lang, funcName, funcNode, src)

		nID := nodeID(pkgPath, fileName, funcName)
		node := &callgraph.Node{
			ID:        nID,
			Name:      funcName,
			Package:   pkgPath,
			File:      filePath,
			Line:      int(funcNode.StartPoint().Row) + 1,
			Kind:      kind,
			Receiver:  receiver,
			Exported:  exported,
			Signature: params,
		}

		funcDefs = append(funcDefs, funcDef{
			node:      node,
			startByte: funcNode.StartByte(),
			endByte:   funcNode.EndByte(),
		})
	}

	// Phase 2: Extract call sites and link them to containing functions
	callQuery, err := sitter.NewQuery([]byte(gi.CallQuery), gi.Language)
	if err != nil {
		return nil, nil, fmt.Errorf("compile call query for %s: %w", lang, err)
	}
	callCursor := sitter.NewQueryCursor()
	callCursor.Exec(callQuery, root)

	var edges []*callgraph.Edge
	seen := make(map[string]bool) // deduplicate edges

	for {
		m, ok := callCursor.NextMatch()
		if !ok {
			break
		}

		var callName string
		var callObj string
		var callNode *sitter.Node

		for _, c := range m.Captures {
			name := callQuery.CaptureNameForId(c.Index)
			content := c.Node.Content(src)
			switch name {
			case "call_name":
				callName = content
			case "method_name":
				callName = content
			case "obj":
				callObj = content
			}
		}

		// Get the full call node from @call capture
		for _, c := range m.Captures {
			if callQuery.CaptureNameForId(c.Index) == "call" {
				callNode = c.Node
				break
			}
		}

		if callName == "" || callNode == nil {
			continue
		}

		// Build the call target ID
		calleeID := callName
		if callObj != "" {
			calleeID = fmt.Sprintf("%s.%s", callObj, callName)
		}

		// Find the containing function for this call
		var containingFunc *callgraph.Node
		callPos := callNode.StartByte()
		for i := range funcDefs {
			fd := &funcDefs[i]
			if callPos >= fd.startByte && callPos < fd.endByte {
				containingFunc = fd.node
				break
			}
		}

		if containingFunc == nil {
			continue // call outside any known function
		}

		edgeKey := fmt.Sprintf("%s->%s", containingFunc.ID, calleeID)
		if !seen[edgeKey] {
			seen[edgeKey] = true
			edges = append(edges, &callgraph.Edge{
				From:     containingFunc.ID,
				To:       calleeID,
				CallType: "direct",
			})
		}
	}

	// Collect nodes from funcDefs
	nodes := make([]*callgraph.Node, 0, len(funcDefs))
	for i := range funcDefs {
		nodes = append(nodes, funcDefs[i].node)
	}

	return nodes, edges, nil
}

// isExportedPython checks if a Python name is considered "exported".
// In Python, names not starting with underscore are public.
func isExportedPython(lang Language, name string) bool {
	if lang != LangPython {
		return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
	}
	return len(name) > 0 && name[0] != '_'
}

// isExportedRust checks if a Rust function is public by looking for the
// "pub" keyword in the parent node text.
func isExportedRust(node *sitter.Node, src []byte) bool {
	if node == nil {
		return false
	}
	parent := node.Parent()
	if parent == nil {
		return false
	}
	// Check the text of the parent for "pub " before the function definition
	parentContent := parent.Content(src)
	textBeforeFunc := parentContent
	// Look for "pub " prefix in the parent content
	return strings.HasPrefix(strings.TrimSpace(textBeforeFunc), "pub ") ||
		strings.Contains(textBeforeFunc, "pub fn ") ||
		strings.Contains(textBeforeFunc, "pub async fn ")
}
