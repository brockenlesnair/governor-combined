package callgraph

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"

	"golang.org/x/tools/go/packages"
)

// PackageInfo holds parsed package information.
type PackageInfo struct {
	Name    string
	Path    string
	Files   []string
	Imports []string
}

type GoParser struct {
	fset        *token.FileSet
	syntaxCache map[string][]*ast.File
	typesCache  map[string]*packages.Package
}

func NewGoParser() *GoParser {
	return &GoParser{
		fset:        token.NewFileSet(),
		syntaxCache: make(map[string][]*ast.File),
		typesCache:  make(map[string]*packages.Package),
	}
}

func NewParser() *GoParser {
	return NewGoParser()
}

func (p *GoParser) ParsePackages(ctx context.Context, rootPath string) ([]*PackageInfo, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedDeps | packages.NeedTypes |
			packages.NeedSyntax | packages.NeedTypesInfo,
		Dir:  rootPath,
		Fset: p.fset,
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}

	var result []*PackageInfo
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			continue
		}

		info := &PackageInfo{
			Name:  pkg.Name,
			Path:  pkg.PkgPath,
			Files: pkg.GoFiles,
			Imports: func() []string {
				imports := make([]string, 0, len(pkg.Imports))
				for imp := range pkg.Imports {
					imports = append(imports, imp)
				}
				return imports
			}(),
		}

		p.syntaxCache[pkg.PkgPath] = pkg.Syntax
		p.typesCache[pkg.PkgPath] = pkg

		result = append(result, info)
	}

	return result, nil
}

func (p *GoParser) ParseFile(ctx context.Context, filePath string, pkgInfo *PackageInfo) ([]*Node, []*Edge, error) {
	var fileAst *ast.File

	if syntaxFiles, ok := p.syntaxCache[pkgInfo.Path]; ok {
		for i, f := range pkgInfo.Files {
			if f == filePath && i < len(syntaxFiles) {
				fileAst = syntaxFiles[i]
				break
			}
		}
	}

	if fileAst == nil {
		src, err := parser.ParseFile(p.fset, filePath, nil, parser.ParseComments)
		if err != nil {
			return nil, nil, fmt.Errorf("parse file: %w", err)
		}
		fileAst = src
	}

	visitor := &callVisitor{
		fset:      p.fset,
		filePath:  filePath,
		pkgPath:   pkgInfo.Path,
		pkgName:   pkgInfo.Name,
		nodes:     make([]*Node, 0),
		edges:     make([]*Edge, 0),
		typeMap:   make(map[string]*Node),
		methodMap: make(map[string]*Node),
	}

	ast.Inspect(fileAst, visitor.Visit)

	return visitor.nodes, visitor.edges, nil
}

// callVisitor visits AST nodes to extract call graph information.
type callVisitor struct {
	fset        *token.FileSet
	filePath    string
	pkgPath     string
	pkgName     string
	nodes       []*Node
	edges       []*Edge
	typeMap     map[string]*Node
	methodMap   map[string]*Node
	currentFunc *Node
}

func (v *callVisitor) Visit(n ast.Node) bool {
	switch node := n.(type) {
	case *ast.FuncDecl:
		v.visitFuncDecl(node)
		return true
	case *ast.CallExpr:
		v.visitCallExpr(node)
	}
	return true
}

func (v *callVisitor) visitFuncDecl(fn *ast.FuncDecl) {
	if fn.Name == nil {
		return
	}

	pos := v.fset.Position(fn.Pos())
	name := fn.Name.Name

	var receiver string
	kind := NodeKindFunction
	exported := ast.IsExported(name)

	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		kind = NodeKindMethod
		if len(fn.Recv.List[0].Names) > 0 {
			receiver = fn.Recv.List[0].Names[0].Name
		}
		if recvType := fn.Recv.List[0].Type; recvType != nil {
			if star, ok := recvType.(*ast.StarExpr); ok {
				if ident, ok := star.X.(*ast.Ident); ok {
					receiver = ident.Name
				}
			} else if ident, ok := recvType.(*ast.Ident); ok {
				receiver = ident.Name
			}
		}
	}

	nodeID := v.makeNodeID(name, receiver)
	node := &Node{
		ID:       nodeID,
		Name:     name,
		Package:  v.pkgPath,
		File:     v.filePath,
		Line:     pos.Line,
		Kind:     kind,
		Receiver: receiver,
		Exported: exported,
	}

	v.nodes = append(v.nodes, node)
	v.currentFunc = node

	if kind == NodeKindMethod {
		key := v.makeMethodKey(receiver, name)
		v.methodMap[key] = node
	}
}

func (v *callVisitor) visitCallExpr(call *ast.CallExpr) {
	if v.currentFunc == nil {
		return
	}

	callee := v.extractCallee(call)
	if callee == "" {
		return
	}

	edge := &Edge{
		From:     v.currentFunc.ID,
		To:       callee,
		CallType: v.determineCallType(call),
	}
	v.edges = append(v.edges, edge)
}

func (v *callVisitor) extractCallee(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return v.makeNodeID(fun.Name, "")
	case *ast.SelectorExpr:
		if ident, ok := fun.X.(*ast.Ident); ok {
			if ident.Obj != nil && ident.Obj.Kind == ast.Pkg {
				return v.makeExternalNodeID(ident.Name, fun.Sel.Name)
			}
			return v.makeExternalNodeID(ident.Name, fun.Sel.Name)
		}
	case *ast.StarExpr:
		if sel, ok := fun.X.(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok {
				return v.makeExternalNodeID(ident.Name, sel.Sel.Name)
			}
		}
	}
	return ""
}

func (v *callVisitor) determineCallType(call *ast.CallExpr) string {
	switch call.Fun.(type) {
	case *ast.SelectorExpr:
		return "direct"
	}
	return "direct"
}

func (v *callVisitor) makeNodeID(name, receiver string) string {
	if receiver != "" {
		return fmt.Sprintf("%s.%s.%s", v.pkgPath, receiver, name)
	}
	return fmt.Sprintf("%s.%s", v.pkgPath, name)
}

func (v *callVisitor) makeExternalNodeID(pkgOrRecv, name string) string {
	return fmt.Sprintf("%s.%s", pkgOrRecv, name)
}

func (v *callVisitor) makeMethodKey(receiver, name string) string {
	return fmt.Sprintf("%s.%s", receiver, name)
}
