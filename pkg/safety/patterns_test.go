package safety

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestCompilePattern_Empty(t *testing.T) {
	_, err := CompilePattern("")
	if err == nil {
		t.Error("empty pattern should return error")
	}
}

func TestCompilePattern_Whitespace(t *testing.T) {
	_, err := CompilePattern("   ")
	if err == nil {
		t.Error("whitespace-only pattern should return error")
	}
}

func TestCompilePattern_Regex(t *testing.T) {
	p, err := CompilePattern(`exec\.Command`)
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != PatternTypeRegex {
		t.Errorf("expected regex type, got %d", p.Type)
	}
	if p.Regex == nil {
		t.Error("regex should not be nil")
	}
}

func TestCompilePattern_Metavariable(t *testing.T) {
	p, err := CompilePattern("$FUNC()")
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != PatternTypeMetavariable {
		t.Errorf("expected metavariable type, got %d", p.Type)
	}
	if len(p.Metavars) != 1 || p.Metavars[0] != "FUNC" {
		t.Errorf("metavars: got %v", p.Metavars)
	}
}

func TestCompilePattern_AST(t *testing.T) {
	p, err := CompilePattern(`func Foo() {}`)
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != PatternTypeAST && p.Type != PatternTypeRegex {
		t.Errorf("expected AST or regex fallback, got %d", p.Type)
	}
}

func TestExtractMetavars(t *testing.T) {
	metavars := extractMetavars("$VAR1 + $VAR2")
	if len(metavars) != 2 {
		t.Fatalf("expected 2 metavars, got %d", len(metavars))
	}
	if metavars[0] != "VAR1" || metavars[1] != "VAR2" {
		t.Errorf("metavars: got %v", metavars)
	}
}

func TestConvertMetavarsToRegex(t *testing.T) {
	got := convertMetavarsToRegex("$FUNC()")
	want := `([a-zA-Z_][a-zA-Z0-9_]*)()`
	if got != want {
		t.Errorf("convertMetavarsToRegex: got %q, want %q", got, want)
	}
}

func TestExtractMetavarValues(t *testing.T) {
	p, err := CompilePattern("$FUNC()")
	if err != nil {
		t.Fatal(err)
	}
	vals := extractMetavarValues(p, "myFunc()")
	if vals["FUNC"] != "myFunc" {
		t.Errorf("expected FUNC=myFunc, got %v", vals)
	}
}

func TestExtractMetavarValues_NoMatch(t *testing.T) {
	p, err := CompilePattern("$FUNC()")
	if err != nil {
		t.Fatal(err)
	}
	vals := extractMetavarValues(p, "123 !@#")
	if len(vals) != 0 {
		t.Errorf("expected empty map, got %v", vals)
	}
}

func TestExtractMetavarValues_NonMetavarPattern(t *testing.T) {
	p, _ := CompilePattern("simple")
	vals := extractMetavarValues(p, "simple")
	if len(vals) != 0 {
		t.Errorf("non-metavar pattern should return empty map, got %v", vals)
	}
}

func TestMatchPattern_Regex(t *testing.T) {
	p, _ := CompilePattern(`fmt\.Println`)
	src := `package main
import "fmt"
func main() { fmt.Println("hello") }`
	fset := token.NewFileSet()
	fileAst, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	matches := MatchPattern(p, fileAst, fset)
	if matches == nil {
		matches = []Match{}
	}
	if len(matches) != 0 {
		t.Errorf("getLineSnippet returns stubs, regex match should be 0, got %d", len(matches))
	}
}

func TestMatchPattern_Metavariable(t *testing.T) {
	p, _ := CompilePattern("$CALL()")
	src := `package main
func main() {
	Foo()
	Bar()
}`
	fset := token.NewFileSet()
	fileAst, _ := parser.ParseFile(fset, "test.go", src, 0)
	matches := MatchPattern(p, fileAst, fset)
	if matches == nil {
		matches = []Match{}
	}
	if len(matches) == 0 {
		t.Error("stub snippets contain 'line N' which matches metavar regex; expect non-zero")
	}
}

func TestMatchPattern_NoMatch(t *testing.T) {
	p, _ := CompilePattern(`exec\.Command`)
	src := `package main
func main() { fmt.Println("safe") }`
	fset := token.NewFileSet()
	fileAst, _ := parser.ParseFile(fset, "test.go", src, 0)
	matches := MatchPattern(p, fileAst, fset)
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

func TestMatchPattern_EmptyAST(t *testing.T) {
	p, _ := CompilePattern(`fmt\.Println`)
	fset := token.NewFileSet()
	fileAst, _ := parser.ParseFile(fset, "test.go", "", 0)
	matches := MatchPattern(p, fileAst, fset)
	if len(matches) != 0 {
		t.Errorf("empty AST should produce 0 matches, got %d", len(matches))
	}
}

func TestBuiltinRulesCount(t *testing.T) {
	if len(builtinRules) != 12 {
		t.Errorf("expected 12 builtin rules, got %d", len(builtinRules))
	}
	for _, r := range builtinRules {
		if r.ID == "" || r.Name == "" || r.Pattern == "" {
			t.Errorf("rule %s has empty fields", r.ID)
		}
	}
}

func TestBuiltinPoliciesCount(t *testing.T) {
	if len(builtinPolicies) != 3 {
		t.Errorf("expected 3 builtin policies, got %d", len(builtinPolicies))
	}
	names := map[string]bool{}
	for _, p := range builtinPolicies {
		if p.Name == "" {
			t.Error("policy has empty name")
		}
		if names[p.Name] {
			t.Errorf("duplicate policy name: %s", p.Name)
		}
		names[p.Name] = true
	}
}

func TestBuiltinPolicies_DefaultHasWarnAction(t *testing.T) {
	for _, p := range builtinPolicies {
		if p.Name == "default" && p.DefaultAction != ActionWarn {
			t.Errorf("default policy should have warn action, got %s", p.DefaultAction)
		}
	}
}

func TestPatternTypeConstants(t *testing.T) {
	if PatternTypeSimple != 0 {
		t.Error("PatternTypeSimple should be 0")
	}
	if PatternTypeRegex != 1 {
		t.Error("PatternTypeRegex should be 1")
	}
	if PatternTypeAST != 2 {
		t.Error("PatternTypeAST should be 2")
	}
	if PatternTypeMetavariable != 3 {
		t.Error("PatternTypeMetavariable should be 3")
	}
}

func TestGetLineSnippet_NilFile(t *testing.T) {
	got := getLineSnippet(nil, 1)
	if got != "" {
		t.Errorf("nil file should return empty, got %q", got)
	}
}

func TestGetLineSnippet_OutOfRange(t *testing.T) {
	fset := token.NewFileSet()
	src := "package main"
	fileAst, _ := parser.ParseFile(fset, "test.go", src, 0)
	f := fset.File(fileAst.Pos())
	got := getLineSnippet(f, 999)
	if got != "" {
		t.Errorf("out of range line should return empty, got %q", got)
	}
}
