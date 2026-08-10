package treesitter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want Language
	}{
		{"main.py", LangPython},
		{"app.py", LangPython},
		{"lib.rs", LangRust},
		{"main.rs", LangRust},
		{"index.ts", LangTypeScript},
		{"component.tsx", LangTypeScript},
		{"index.js", LangJavaScript},
		{"app.jsx", LangJavaScript},
		{"readme.md", LangUnknown},
		{"data.json", LangUnknown},
		{"", LangUnknown},
		{"/path/to/file.py", LangPython},
		{"/path/to/file.PY", LangPython}, // case-insensitive
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := DetectLanguage(tt.path)
			if got != tt.want {
				t.Errorf("DetectLanguage(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestNewParser(t *testing.T) {
	p := NewParser()
	if p == nil {
		t.Fatal("NewParser() returned nil")
	}
	if len(p.grammars) == 0 {
		t.Fatal("NewParser() created parser with no grammars")
	}

	// Verify all expected languages are registered
	expectedLangs := []Language{LangPython, LangRust, LangTypeScript, LangJavaScript}
	for _, lang := range expectedLangs {
		if _, ok := p.grammars[lang]; !ok {
			t.Errorf("NewParser() missing grammar for %s", lang)
		}
	}
}

func TestPythonParse(t *testing.T) {
	pythonCode := `
def greet(name):
    print(name)
    return name

async def fetch_data(url):
    result = await fetch(url)
    return result

def compute(x, y):
    result = add(x, y)
    return result

class Calculator:
    def add(self, a, b):
        return a + b

    def subtract(self, a, b):
        result = self.add(a, b)
        return a - result
`
	tmpDir := t.TempDir()
	pyFile := filepath.Join(tmpDir, "test_module.py")
	if err := os.WriteFile(pyFile, []byte(pythonCode), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	p := NewParser()
	pkgInfo := &callgraph.PackageInfo{
		Name:  "test_module",
		Path:  tmpDir,
		Files: []string{pyFile},
	}

	ctx := context.Background()
	nodes, edges, err := p.ParseFile(ctx, pyFile, pkgInfo)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	// Verify we found function definitions
	if len(nodes) < 4 {
		t.Fatalf("expected at least 4 function nodes, got %d", len(nodes))
	}

	// Check function names
	funcNames := make(map[string]bool)
	for _, n := range nodes {
		funcNames[n.Name] = true
		if n.File != pyFile {
			t.Errorf("node %s has wrong file: got %s, want %s", n.Name, n.File, pyFile)
		}
		if n.Package != tmpDir {
			t.Errorf("node %s has wrong package: got %s, want %s", n.Name, n.Package, tmpDir)
		}
		if n.Line <= 0 {
			t.Errorf("node %s has invalid line: %d", n.Name, n.Line)
		}
	}

	// Check that specific functions are found
	for _, name := range []string{"greet", "fetch_data", "compute"} {
		if !funcNames[name] {
			t.Errorf("expected function %q not found in nodes", name)
		}
	}

	// Check exported status: Python functions without underscore prefix are exported
	for _, n := range nodes {
		if n.Name == "greet" || n.Name == "fetch_data" || n.Name == "compute" {
			if !n.Exported {
				t.Errorf("function %s should be exported (no underscore prefix)", n.Name)
			}
		}
	}

	// Verify edges connect function definitions to callees
	if len(edges) == 0 {
		t.Error("expected at least one edge from function calls")
	}

	// Verify edge properties
	for _, e := range edges {
		if e.From == "" || e.To == "" {
			t.Errorf("edge has empty from/to: %+v", e)
		}
		if e.CallType != "direct" {
			t.Errorf("edge has unexpected call type: %s", e.CallType)
		}
	}

	// Verify the compute function calls add
	foundComputeToAdd := false
	for _, e := range edges {
		if e.From == nodeID(tmpDir, "test_module.py", "compute") && e.To == "add" {
			foundComputeToAdd = true
			break
		}
	}
	if !foundComputeToAdd {
		t.Error("expected edge from compute() to add()")
	}
}

func TestRustParse(t *testing.T) {
	rustCode := `
pub fn greet(name: &str) -> String {
    let msg = format!("Hello, {}", name);
    println!("{}", msg);
    msg
}

fn helper(x: i32) -> i32 {
    let result = add(x, 1);
    result
}

fn add(a: i32, b: i32) -> i32 {
    a + b
}

struct Calculator;

impl Calculator {
    pub fn add(&self, a: i32, b: i32) -> i32 {
        a + b
    }

    fn internal(&self, x: i32) -> i32 {
        let r = self.add(x, 0);
        r
    }
}
`
	tmpDir := t.TempDir()
	rsFile := filepath.Join(tmpDir, "lib.rs")
	if err := os.WriteFile(rsFile, []byte(rustCode), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	p := NewParser()
	pkgInfo := &callgraph.PackageInfo{
		Name:  "lib",
		Path:  tmpDir,
		Files: []string{rsFile},
	}

	ctx := context.Background()
	nodes, edges, err := p.ParseFile(ctx, rsFile, pkgInfo)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	// Verify function definitions
	if len(nodes) < 4 {
		t.Fatalf("expected at least 4 function nodes, got %d", len(nodes))
	}

	funcNames := make(map[string]bool)
	for _, n := range nodes {
		funcNames[n.Name] = true
	}

	for _, name := range []string{"greet", "helper", "add"} {
		if !funcNames[name] {
			t.Errorf("expected function %q not found in nodes", name)
		}
	}

	// Check exported status: greet starts with "pub" keyword
	for _, n := range nodes {
		if n.Name == "greet" {
			if !n.Exported {
				t.Error("pub fn greet should be marked as exported")
			}
		}
	}

	// Verify edges
	if len(edges) == 0 {
		t.Error("expected at least one edge from function calls")
	}

	// Verify helper calls add
	foundHelperToAdd := false
	for _, e := range edges {
		if e.From == nodeID(tmpDir, "lib.rs", "helper") && e.To == "add" {
			foundHelperToAdd = true
			break
		}
	}
	if !foundHelperToAdd {
		t.Error("expected edge from helper() to add()")
	}
}

func TestTypeScriptParse(t *testing.T) {
	tsCode := `
function greet(name: string): string {
    const msg = formatMessage(name);
    console.log(msg);
    return msg;
}

const fetch = async (url: string): Promise<Response> => {
    const response = await doFetch(url);
    return response;
};

function compute(x: number, y: number): number {
    const result = add(x, y);
    return result;
}

function add(a: number, b: number): number {
    return a + b;
}

class Calculator {
    add(a: number, b: number): number {
        return a + b;
    }

    compute(x: number): number {
        const r = this.add(x, 0);
        return r;
    }
}
`
	tmpDir := t.TempDir()
	tsFile := filepath.Join(tmpDir, "utils.ts")
	if err := os.WriteFile(tsFile, []byte(tsCode), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	p := NewParser()
	pkgInfo := &callgraph.PackageInfo{
		Name:  "utils",
		Path:  tmpDir,
		Files: []string{tsFile},
	}

	ctx := context.Background()
	nodes, edges, err := p.ParseFile(ctx, tsFile, pkgInfo)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	// Verify function definitions
	if len(nodes) < 4 {
		t.Fatalf("expected at least 4 function nodes, got %d (found: %d)", 4, len(nodes))
	}

	funcNames := make(map[string]bool)
	for _, n := range nodes {
		funcNames[n.Name] = true
		if n.File != tsFile {
			t.Errorf("node %s has wrong file: got %s, want %s", n.Name, n.File, tsFile)
		}
	}

	for _, name := range []string{"greet", "compute", "add"} {
		if !funcNames[name] {
			t.Errorf("expected function %q not found in nodes", name)
		}
	}

	// Without `export` keyword, functions are not exported in TypeScript
	for _, n := range nodes {
		if n.Exported {
			t.Errorf("function %s should not be exported (no export keyword)", n.Name)
		}
	}

	// Verify edges
	if len(edges) == 0 {
		t.Error("expected at least one edge from function calls")
	}

	// Verify compute calls add
	foundComputeToAdd := false
	for _, e := range edges {
		if e.From == nodeID(tmpDir, "utils.ts", "compute") && e.To == "add" {
			foundComputeToAdd = true
			break
		}
	}
	if !foundComputeToAdd {
		t.Error("expected edge from compute() to add()")
	}
}

func TestParseFileUnknown(t *testing.T) {
	tmpDir := t.TempDir()
	mdFile := filepath.Join(tmpDir, "readme.md")
	if err := os.WriteFile(mdFile, []byte("# Hello"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	p := NewParser()
	pkgInfo := &callgraph.PackageInfo{
		Name:  "readme",
		Path:  tmpDir,
		Files: []string{mdFile},
	}

	ctx := context.Background()
	_, _, err := p.ParseFile(ctx, mdFile, pkgInfo)
	if err == nil {
		t.Fatal("ParseFile() should return error for unknown language")
	}
}

func TestParsePackages(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a structure: tmpDir/pkg1/a.py, tmpDir/pkg1/b.py, tmpDir/pkg2/c.rs
	pkg1 := filepath.Join(tmpDir, "pkg1")
	pkg2 := filepath.Join(tmpDir, "pkg2")
	if err := os.MkdirAll(pkg1, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(pkg2, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	pyContent := []byte("def hello(): pass")
	rsContent := []byte("fn hello() {}")

	if err := os.WriteFile(filepath.Join(pkg1, "a.py"), pyContent, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkg1, "b.py"), pyContent, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkg2, "c.rs"), rsContent, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := NewParser()
	pkgs, err := p.ParsePackages(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("ParsePackages() error = %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs))
	}

	// Verify package details
	for _, pkg := range pkgs {
		switch pkg.Name {
		case "pkg1":
			if len(pkg.Files) != 2 {
				t.Errorf("pkg1 should have 2 files, got %d", len(pkg.Files))
			}
		case "pkg2":
			if len(pkg.Files) != 1 {
				t.Errorf("pkg2 should have 1 file, got %d", len(pkg.Files))
			}
		default:
			t.Errorf("unexpected package name: %s", pkg.Name)
		}
	}
}

func TestParsePackagesNotADir(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "file.py")
	if err := os.WriteFile(filePath, []byte("def pass"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := NewParser()
	_, err := p.ParsePackages(context.Background(), filePath)
	if err == nil {
		t.Fatal("ParsePackages() should return error for non-directory path")
	}
}

func TestParseFileReadError(t *testing.T) {
	tmpDir := t.TempDir()
	p := NewParser()
	pkgInfo := &callgraph.PackageInfo{
		Name: "test",
		Path: tmpDir,
	}

	ctx := context.Background()
	_, _, err := p.ParseFile(ctx, "/nonexistent/file.py", pkgInfo)
	if err == nil {
		t.Fatal("ParseFile() should return error for nonexistent file")
	}
}
