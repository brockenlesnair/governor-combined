package safety

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// ObfuscationDetector detects obfuscation techniques in code.
type ObfuscationDetector struct{}

// NewObfuscationDetector creates a new obfuscation detector.
func NewObfuscationDetector() *ObfuscationDetector {
	return &ObfuscationDetector{}
}

// Detect detects obfuscation in a file.
func (d *ObfuscationDetector) Detect(fileAst *ast.File, fset *token.FileSet, file FileInput) []Finding {
	var findings []Finding

	// Detect string concatenation patterns
	findings = append(findings, d.detectStringConcatenation(fileAst, fset, file)...)

	// Detect reflection usage
	findings = append(findings, d.detectReflection(fileAst, fset, file)...)

	// Detect dynamic calls
	findings = append(findings, d.detectDynamicCalls(fileAst, fset, file)...)

	// Detect hex/base64 encoded strings
	findings = append(findings, d.detectEncodedStrings(fileAst, fset, file)...)

	return findings
}

// detectStringConcatenation detects suspicious string concatenation patterns.
func (d *ObfuscationDetector) detectStringConcatenation(fileAst *ast.File, fset *token.FileSet, file FileInput) []Finding {
	var findings []Finding

	ast.Inspect(fileAst, func(n ast.Node) bool {
		binExpr, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}

		if binExpr.Op != token.ADD {
			return true
		}

		// Check if both sides are string literals or variables
		if isStringConcat(binExpr) {
			pos := fset.Position(binExpr.Pos())
			findings = append(findings, Finding{
				ID:         fmt.Sprintf("obfusc-concat-%d", pos.Line),
				RuleID:     "OBFUSC001",
				Severity:   SeverityMedium,
				Action:     ActionWarn,
				File:       file.Path,
				Line:       pos.Line,
				Column:     pos.Column,
				Message:    "Suspicious string concatenation pattern (possible obfuscation)",
				RiskScore:  3.5,
				Suggestion: "Review the concatenation for hidden malicious behavior",
			})
		}

		return true
	})

	return findings
}

// detectReflection detects suspicious use of reflection.
func (d *ObfuscationDetector) detectReflection(fileAst *ast.File, fset *token.FileSet, file FileInput) []Finding {
	var findings []Finding

	ast.Inspect(fileAst, func(n ast.Node) bool {
		callExpr, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Check for reflect.ValueOf, reflect.New, etc.
		if sel, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "reflect" {
				pos := fset.Position(callExpr.Pos())
				findings = append(findings, Finding{
					ID:         fmt.Sprintf("obfusc-reflect-%d", pos.Line),
					RuleID:     "OBFUSC002",
					Severity:   SeverityMedium,
					Action:     ActionWarn,
					File:       file.Path,
					Line:       pos.Line,
					Column:     pos.Column,
					Message:    "Use of reflection (possible dynamic code execution)",
					RiskScore:  3.0,
					Suggestion: "Verify reflection usage is legitimate",
				})
			}
		}

		return true
	})

	return findings
}

// detectDynamicCalls detects dynamic function calls.
func (d *ObfuscationDetector) detectDynamicCalls(fileAst *ast.File, fset *token.FileSet, file FileInput) []Finding {
	var findings []Finding

	ast.Inspect(fileAst, func(n ast.Node) bool {
		callExpr, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Check for interface{} calls (dynamic dispatch)
		if _, ok := callExpr.Fun.(*ast.InterfaceType); ok {
			pos := fset.Position(callExpr.Pos())
			findings = append(findings, Finding{
				ID:         fmt.Sprintf("obfusc-dyn-%d", pos.Line),
				RuleID:     "OBFUSC003",
				Severity:   SeverityLow,
				Action:     ActionWarn,
				File:       file.Path,
				Line:       pos.Line,
				Column:     pos.Column,
				Message:    "Dynamic function call detected",
				RiskScore:  1.5,
				Suggestion: "Consider using typed interfaces for better safety",
			})
		}

		return true
	})

	return findings
}

// detectEncodedStrings detects hex/base64 encoded strings that might be obfuscated.
func (d *ObfuscationDetector) detectEncodedStrings(fileAst *ast.File, fset *token.FileSet, file FileInput) []Finding {
	var findings []Finding

	ast.Inspect(fileAst, func(n ast.Node) bool {
		basicLit, ok := n.(*ast.BasicLit)
		if !ok {
			return true
		}

		if basicLit.Kind != token.STRING {
			return true
		}

		value := basicLit.Value
		// Look for long hex strings
		if len(value) > 50 && isHexString(value) {
			pos := fset.Position(basicLit.Pos())
			findings = append(findings, Finding{
				ID:         fmt.Sprintf("obfusc-hex-%d", pos.Line),
				RuleID:     "OBFUSC004",
				Severity:   SeverityLow,
				Action:     ActionWarn,
				File:       file.Path,
				Line:       pos.Line,
				Column:     pos.Column,
				Message:    "Long hex string detected (possible encoded payload)",
				RiskScore:  2.0,
				Suggestion: "Review the hex string for malicious content",
			})
		}

		return true
	})

	return findings
}

// isStringConcat checks if a binary expression is a string concatenation.
func isStringConcat(expr *ast.BinaryExpr) bool {
	if expr.Op != token.ADD {
		return false
	}

	// Check if either side contains string literals
	if hasStringLiteral(expr.X) || hasStringLiteral(expr.Y) {
		return true
	}

	return false
}

// hasStringLiteral checks if an expression contains string literals.
func hasStringLiteral(expr ast.Expr) bool {
	if expr == nil {
		return false
	}

	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.BinaryExpr:
		return hasStringLiteral(e.X) || hasStringLiteral(e.Y)
	case *ast.ParenExpr:
		return hasStringLiteral(e.X)
	}

	return false
}

// isHexString checks if a string contains hex characters.
func isHexString(s string) bool {
	if !strings.HasPrefix(s, "\"") || !strings.HasSuffix(s, "\"") {
		return false
	}

	inner := s[1 : len(s)-1]
	if len(inner) < 20 {
		return false
	}

	hexChars := 0
	for _, c := range inner {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			hexChars++
		}
	}

	return float64(hexChars)/float64(len(inner)) > 0.7
}