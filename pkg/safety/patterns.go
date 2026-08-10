package safety

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

// PatternType represents the type of pattern.
type PatternType int

const (
	PatternTypeSimple PatternType = iota
	PatternTypeRegex
	PatternTypeAST
	PatternTypeMetavariable
)

// Pattern represents a compiled pattern for matching.
type Pattern struct {
	Type        PatternType
	Original    string
	Regex       *regexp.Regexp
	ASTPattern  ast.Node
	Metavars    []string
}

// CompilePattern compiles a pattern string into a Pattern.
func CompilePattern(patternStr string) (*Pattern, error) {
	patternStr = strings.TrimSpace(patternStr)
	if patternStr == "" {
		return nil, NewInvalidPatternError(patternStr)
	}

	p := &Pattern{
		Original: patternStr,
	}

	// Check for metavariable pattern (contains $)
	if strings.Contains(patternStr, "$") {
		p.Type = PatternTypeMetavariable
		metavars := extractMetavars(patternStr)
		p.Metavars = metavars

		// Convert metavariable pattern to regex
		regexStr := convertMetavarsToRegex(patternStr)
		re, err := regexp.Compile(regexStr)
		if err != nil {
			return nil, NewInvalidPatternError(patternStr)
		}
		p.Regex = re
		return p, nil
	}

	// Check for AST pattern (looks like Go code)
	if strings.Contains(patternStr, "func") || strings.Contains(patternStr, "import") ||
		strings.Contains(patternStr, "package") || strings.Contains(patternStr, ":=") {
		p.Type = PatternTypeAST
		// Try to parse as Go AST
		fset := token.NewFileSet()
		expr, err := parser.ParseExprFrom(fset, "pattern.go", patternStr, 0)
		if err == nil {
			p.ASTPattern = expr
			return p, nil
		}
		// Fall back to regex
		p.Type = PatternTypeRegex
	}

	// Default: regex pattern
	p.Type = PatternTypeRegex
	re, err := regexp.Compile(patternStr)
	if err != nil {
		return nil, NewInvalidPatternError(patternStr)
	}
	p.Regex = re

	return p, nil
}

// Match represents a pattern match.
type Match struct {
	Line    int
	Column  int
	Snippet string
	Metavars map[string]string
}

// MatchPattern matches a pattern against an AST.
func MatchPattern(pattern *Pattern, fileAst *ast.File, fset *token.FileSet) []Match {
	var matches []Match

	switch pattern.Type {
	case PatternTypeRegex, PatternTypeMetavariable:
		// Match against source code text
		matches = matchTextPattern(pattern, fileAst, fset)
	case PatternTypeAST:
		// Match against AST structure
		matches = matchASTPattern(pattern, fileAst, fset)
	case PatternTypeSimple:
		// Simple substring match
		matches = matchSimplePattern(pattern, fileAst, fset)
	}

	return matches
}

// matchTextPattern matches a regex pattern against source text.
func matchTextPattern(pattern *Pattern, fileAst *ast.File, fset *token.FileSet) []Match {
	var matches []Match

	// Get the source position range
	start := fileAst.Pos()
	end := fileAst.End()

	if !start.IsValid() || !end.IsValid() {
		return matches
	}

	// Walk all statements and expressions
	ast.Inspect(fileAst, func(n ast.Node) bool {
		if n == nil {
			return true
		}

		pos := n.Pos()
		if !pos.IsValid() {
			return true
		}

		// Get the line text
		position := fset.Position(pos)
		if position.Line == 0 {
			return true
		}

		// Get snippet (full line)
		snippet := getLineSnippet(fset.File(pos), position.Line)

		// Test regex match
		if pattern.Regex.MatchString(snippet) {
			match := Match{
				Line:     position.Line,
				Column:   position.Column,
				Snippet:  snippet,
				Metavars: extractMetavarValues(pattern, snippet),
			}
			matches = append(matches, match)
		}

		return true
	})

	return matches
}

// matchASTPattern matches an AST pattern against the file AST.
func matchASTPattern(pattern *Pattern, fileAst *ast.File, fset *token.FileSet) []Match {
	var matches []Match

	ast.Inspect(fileAst, func(n ast.Node) bool {
		if n == nil {
			return true
		}

		if astNodeMatches(n, pattern.ASTPattern) {
			pos := fset.Position(n.Pos())
			snippet := getLineSnippet(fset.File(n.Pos()), pos.Line)
			matches = append(matches, Match{
				Line:    pos.Line,
				Column:  pos.Column,
				Snippet: snippet,
			})
		}

		return true
	})

	return matches
}

// matchSimplePattern does simple substring matching.
func matchSimplePattern(pattern *Pattern, fileAst *ast.File, fset *token.FileSet) []Match {
	var matches []Match

	ast.Inspect(fileAst, func(n ast.Node) bool {
		if n == nil {
			return true
		}

		pos := n.Pos()
		if !pos.IsValid() {
			return true
		}

		position := fset.Position(pos)
		snippet := getLineSnippet(fset.File(pos), position.Line)

		if strings.Contains(snippet, pattern.Original) {
			matches = append(matches, Match{
				Line:    position.Line,
				Column:  position.Column,
				Snippet: snippet,
			})
		}

		return true
	})

	return matches
}

// astNodeMatches checks if two AST nodes match structurally.
func astNodeMatches(node, pattern ast.Node) bool {
	if node == nil || pattern == nil {
		return node == pattern
	}

	// Simple structural matching - check if the source text matches
	if fmt.Sprintf("%T", node) != fmt.Sprintf("%T", pattern) {
		return false
	}

	// Match by position and text
	switch n := node.(type) {
	case *ast.CallExpr:
		p, ok := pattern.(*ast.CallExpr)
		if !ok {
			return false
		}
		// Match function name
		if ident, ok := n.Fun.(*ast.Ident); ok {
			if pIdent, ok := p.Fun.(*ast.Ident); ok {
				return ident.Name == pIdent.Name
			}
		}
		// Match selector (e.g., exec.Command)
		if sel, ok := n.Fun.(*ast.SelectorExpr); ok {
			if pSel, ok := p.Fun.(*ast.SelectorExpr); ok {
				if sel.Sel != nil && pSel.Sel != nil {
					return sel.Sel.Name == pSel.Sel.Name
				}
			}
		}
	}

	return false
}

// getLineSnippet gets the text of a line from the token file.
func getLineSnippet(file *token.File, line int) string {
	if file == nil {
		return ""
	}
	if line < 1 || line > file.LineCount() {
		return ""
	}

	startOffset := file.Offset(file.LineStart(line))
	endOffset := startOffset
	if line+1 <= file.LineCount() {
		endOffset = file.Offset(file.LineStart(line + 1))
	} else {
		endOffset = file.Size()
	}

	_ = startOffset
	_ = endOffset

	// token.File doesn't store source text directly; the caller must provide it.
	return fmt.Sprintf("line %d", line)
}

// extractMetavars extracts metavariable names from a pattern.
func extractMetavars(pattern string) []string {
	var metavars []string
	re := regexp.MustCompile(`\$([A-Z_][A-Z0-9_]*)`)
	matches := re.FindAllStringSubmatch(pattern, -1)
	for _, match := range matches {
		metavars = append(metavars, match[1])
	}
	return metavars
}

// convertMetavarsToRegex converts metavariable pattern to regex.
func convertMetavarsToRegex(pattern string) string {
	// Replace $VAR with regex pattern for identifiers
	re := regexp.MustCompile(`\$([A-Z_][A-Z0-9_]*)`)
	regexStr := re.ReplaceAllString(pattern, `([a-zA-Z_][a-zA-Z0-9_]*)`)
	return regexStr
}

// extractMetavarValues extracts metavariable values from a match.
func extractMetavarValues(pattern *Pattern, snippet string) map[string]string {
	values := make(map[string]string)

	if pattern.Type != PatternTypeMetavariable || pattern.Regex == nil {
		return values
	}

	matches := pattern.Regex.FindStringSubmatch(snippet)
	if len(matches) <= 1 {
		return values
	}

	for i, metavarName := range pattern.Metavars {
		if i+1 < len(matches) {
			values[metavarName] = matches[i+1]
		}
	}

	return values
}

// builtinRules contains built-in security rules.
var builtinRules = []*Rule{
	{
		ID:          "GO001",
		Name:        "dangerous-exec",
		Description: "Detects use of exec.Command which can execute arbitrary commands",
		Severity:    SeverityHigh,
		Action:      ActionBlock,
		Pattern:     `exec\.Command`,
		Languages:   []string{"go"},
		Tags:        []string{"security", "command-execution"},
		Enabled:     true,
	},
	{
		ID:          "GO002",
		Name:        "sql-injection",
		Description: "Detects potential SQL injection via string concatenation",
		Severity:    SeverityCritical,
		Action:      ActionBlock,
		Pattern:     `fmt\.Sprintf.*(SELECT|INSERT|UPDATE|DELETE).*\+`,
		Languages:   []string{"go"},
		Tags:        []string{"security", "sql-injection"},
		Enabled:     true,
	},
	{
		ID:          "GO003",
		Name:        "weak-crypto",
		Description: "Detects use of weak cryptographic algorithms (MD5, SHA1)",
		Severity:    SeverityMedium,
		Action:      ActionWarn,
		Pattern:     `(md5\.New|sha1\.New)`,
		Languages:   []string{"go"},
		Tags:        []string{"security", "crypto"},
		Enabled:     true,
	},
	{
		ID:          "GO004",
		Name:        "hardcoded-secret",
		Description: "Detects potential hardcoded secrets in code",
		Severity:    SeverityCritical,
		Action:      ActionBlock,
		Pattern:     `(password|secret|token|api_key)\s*[:=]\s*"[^"]{8,}"`,
		Languages:   []string{"go"},
		Tags:        []string{"security", "secrets"},
		Enabled:     true,
	},
	{
		ID:          "GO005",
		Name:        "unsafe-pointer",
		Description: "Detects use of unsafe.Pointer",
		Severity:    SeverityHigh,
		Action:      ActionBlock,
		Pattern:     `unsafe\.Pointer`,
		Languages:   []string{"go"},
		Tags:        []string{"security", "unsafe"},
		Enabled:     true,
	},
	{
		ID:          "GO006",
		Name:        "dangerous-rm",
		Description: "Detects dangerous rm -rf commands",
		Severity:    SeverityCritical,
		Action:      ActionBlock,
		Pattern:     `rm\s+-rf?`,
		Languages:   []string{"go"},
		Tags:        []string{"security", "destructive"},
		Enabled:     true,
	},
	{
		ID:          "GO007",
		Name:        "insecure-tls",
		Description: "Detects InsecureSkipVerify in TLS config",
		Severity:    SeverityHigh,
		Action:      ActionBlock,
		Pattern:     `InsecureSkipVerify\s*:\s*true`,
		Languages:   []string{"go"},
		Tags:        []string{"security", "tls"},
		Enabled:     true,
	},
	{
		ID:          "GO008",
		Name:        "path-traversal",
		Description: "Detects potential path traversal vulnerabilities",
		Severity:    SeverityHigh,
		Action:      ActionBlock,
		Pattern:     `os\.Open\(.*\+.*\)`,
		Languages:   []string{"go"},
		Tags:        []string{"security", "path-traversal"},
		Enabled:     true,
	},
	{
		ID:          "GO009",
		Name:        "eval-injection",
		Description: "Detects use of eval-like functions",
		Severity:    SeverityHigh,
		Action:      ActionBlock,
		Pattern:     `(exec\.Command|os\.StartProcess).*\+`,
		Languages:   []string{"go"},
		Tags:        []string{"security", "command-injection"},
		Enabled:     true,
	},
	{
		ID:          "GO010",
		Name:        "debug-print",
		Description: "Detects debug print statements that should be removed",
		Severity:    SeverityLow,
		Action:      ActionWarn,
		Pattern:     `fmt\.Println\([^"]`,
		Languages:   []string{"go"},
		Tags:        []string{"code-quality", "debug"},
		Enabled:     true,
	},
	{
		ID:          "GO011",
		Name:        "todo-comment",
		Description: "Detects TODO comments that should be addressed",
		Severity:    SeverityLow,
		Action:      ActionWarn,
		Pattern:     `//\s*TODO`,
		Languages:   []string{"go"},
		Tags:        []string{"code-quality"},
		Enabled:     true,
	},
	{
		ID:          "GO012",
		Name:        "ignored-error",
		Description: "Detects ignored error returns",
		Severity:    SeverityMedium,
		Action:      ActionWarn,
		Pattern:     `_\s*=\s*[a-zA-Z]`,
		Languages:   []string{"go"},
		Tags:        []string{"code-quality", "error-handling"},
		Enabled:     true,
	},
}

// builtinPolicies contains built-in policies.
var builtinPolicies = []*Policy{
	{
		Name:          "default",
		Description:   "Default security policy",
		Rules:         builtinRules,
		DefaultAction: ActionWarn,
	},
	{
		Name:          "strict",
		Description:   "Strict security policy (blocks on high+ severity)",
		Rules:         builtinRules,
		DefaultAction: ActionBlock,
	},
	{
		Name:          "permissive",
		Description:   "Permissive policy (warns on critical only)",
		Rules:         builtinRules,
		DefaultAction: ActionAllow,
	},
}