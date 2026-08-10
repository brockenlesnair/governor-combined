package safety

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestObfuscationDetector_NoFindings(t *testing.T) {
	d := NewObfuscationDetector()
	src := `package main
func main() {}
`
	fset := token.NewFileSet()
	fileAst, _ := parser.ParseFile(fset, "test.go", src, 0)
	findings := d.Detect(fileAst, fset, FileInput{Path: "test.go", Language: "go"})
	if len(findings) != 0 {
		t.Errorf("clean code should produce 0 obfuscation findings, got %d", len(findings))
	}
}

func TestDetectStringConcatenation(t *testing.T) {
	d := NewObfuscationDetector()
	src := `package main
func main() {
	x := "hello" + "world"
	_ = x
}`
	fset := token.NewFileSet()
	fileAst, _ := parser.ParseFile(fset, "test.go", src, 0)
	findings := d.Detect(fileAst, fset, FileInput{Path: "test.go", Language: "go"})
	found := false
	for _, f := range findings {
		if f.RuleID == "OBFUSC001" {
			found = true
			if f.Severity != SeverityMedium {
				t.Errorf("expected medium severity, got %d", f.Severity)
			}
		}
	}
	if !found {
		t.Error("expected OBFUSC001 finding for string concatenation")
	}
}

func TestDetectReflection(t *testing.T) {
	d := NewObfuscationDetector()
	src := `package main
import "reflect"
func main() {
	v := reflect.ValueOf(42)
	_ = v
}`
	fset := token.NewFileSet()
	fileAst, _ := parser.ParseFile(fset, "test.go", src, 0)
	findings := d.Detect(fileAst, fset, FileInput{Path: "test.go", Language: "go"})
	found := false
	for _, f := range findings {
		if f.RuleID == "OBFUSC002" {
			found = true
			if f.Severity != SeverityMedium {
				t.Errorf("expected medium severity, got %d", f.Severity)
			}
		}
	}
	if !found {
		t.Error("expected OBFUSC002 finding for reflection")
	}
}

func TestDetectEncodedStrings(t *testing.T) {
	d := NewObfuscationDetector()
	src := `package main
func main() {
	x := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	_ = x
}`
	fset := token.NewFileSet()
	fileAst, _ := parser.ParseFile(fset, "test.go", src, 0)
	findings := d.Detect(fileAst, fset, FileInput{Path: "test.go", Language: "go"})
	found := false
	for _, f := range findings {
		if f.RuleID == "OBFUSC004" {
			found = true
		}
	}
	if !found {
		t.Error("expected OBFUSC004 finding for long hex string")
	}
}

func TestIsHexString(t *testing.T) {
	if !isHexString(`"abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`) {
		t.Error("should detect hex string")
	}
	if isHexString(`"not hex at all just normal text here nothing special"`) {
		t.Error("should not detect normal text as hex")
	}
	if isHexString("no-quotes") {
		t.Error("should not detect without quotes")
	}
}

func TestIsStringConcat(t *testing.T) {
	src := `package main
func main() {
	_ = "a" + "b"
	_ = 1 + 2
}`
	fset := token.NewFileSet()
	fileAst, _ := parser.ParseFile(fset, "test.go", src, 0)

	var binaryExprs []*ast.BinaryExpr
	ast.Inspect(fileAst, func(n ast.Node) bool {
		if be, ok := n.(*ast.BinaryExpr); ok {
			binaryExprs = append(binaryExprs, be)
		}
		return true
	})
	if len(binaryExprs) < 2 {
		t.Fatalf("expected 2 binary exprs, got %d", len(binaryExprs))
	}
	if !isStringConcat(binaryExprs[0]) {
		t.Error("string + string should be string concat")
	}
	if isStringConcat(binaryExprs[1]) {
		t.Error("int + int should not be string concat")
	}
}

func TestHasStringLiteral(t *testing.T) {
	src := `package main
func main() {
	_ = "hello"
}`
	fset := token.NewFileSet()
	fileAst, _ := parser.ParseFile(fset, "test.go", src, 0)

	var found bool
	ast.Inspect(fileAst, func(n ast.Node) bool {
		if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING {
			if !hasStringLiteral(bl) {
				t.Error("BasicLit with STRING kind should return true")
			}
			found = true
		}
		return true
	})
	if !found {
		t.Error("no string literal found in test source")
	}
	if hasStringLiteral(nil) {
		t.Error("nil should return false")
	}
}

func TestDetectMultipleObfuscations(t *testing.T) {
	d := NewObfuscationDetector()
	src := `package main
import "reflect"
func main() {
	_ = reflect.TypeOf(42)
	x := "aaa" + "bbb"
	_ = x
}`
	fset := token.NewFileSet()
	fileAst, _ := parser.ParseFile(fset, "test.go", src, 0)
	findings := d.Detect(fileAst, fset, FileInput{Path: "test.go", Language: "go"})
	if len(findings) < 2 {
		t.Errorf("expected 2+ findings, got %d", len(findings))
	}
}

func TestGetDir(t *testing.T) {
	tests := []struct {
		path, want string
	}{
		{"/tmp/safety.log", "/tmp"},
		{"safety.log", "."},
		{"/a/b/c.log", "/a/b"},
	}
	for _, tt := range tests {
		got := getDir(tt.path)
		if got != tt.want {
			t.Errorf("getDir(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
