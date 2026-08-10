package safety

import (
	"testing"
)

func TestParseDiff_SingleFile(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
 
+import "fmt"
 func main() {}`
	files, err := ParseDiff(diff)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Path != "main.go" {
		t.Errorf("path: got %q", files[0].Path)
	}
	if files[0].Language != "go" {
		t.Errorf("language: got %q", files[0].Language)
	}
	if files[0].Content == "" {
		t.Error("content should not be empty")
	}
}

func TestParseDiff_MultipleFiles(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1,2 @@\n+line1\ndiff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n@@ -1 +1,2 @@\n+line2"
	files, err := ParseDiff(diff)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
}

func TestParseDiff_NewFile(t *testing.T) {
	diff := `diff --git a/new.go b/new.go
--- /dev/null
+++ b/new.go
@@ -0,0 +1,3 @@
+package main
+func main() {}
+`
	files, err := ParseDiff(diff)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].OldContent != "" {
		t.Errorf("new file should have empty old content")
	}
}

func TestParseDiff_Empty(t *testing.T) {
	files, err := ParseDiff("")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("empty diff should return 0 files, got %d", len(files))
	}
}

func TestParseDiff_DeletedFile(t *testing.T) {
	diff := `diff --git a/old.go b/old.go
--- a/old.go
+++ /dev/null
@@ -1 +0,0 @@
-old line`
	files, err := ParseDiff(diff)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Content != "" {
		t.Errorf("deleted file should have empty new content")
	}
}

func TestParseDiff_ContextLines(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,3 @@
 package main
 
-func old() {}
+func new() {}
 func main() {}`
	files, err := ParseDiff(diff)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	content := files[0].Content
	oldContent := files[0].OldContent
	if content == "" || oldContent == "" {
		t.Error("context lines should populate both content and old_content")
	}
}

func TestParseHunkHeader(t *testing.T) {
	oldStart, oldCount, newStart, newCount := parseHunkHeader("@@ -10,5 +15,7 @@")
	if oldStart != 10 || oldCount != 5 || newStart != 15 || newCount != 7 {
		t.Errorf("got %d,%d,%d,%d", oldStart, oldCount, newStart, newCount)
	}
}

func TestParseHunkHeader_SingleLine(t *testing.T) {
	oldStart, oldCount, newStart, newCount := parseHunkHeader("@@ -1 +1 @@")
	if oldStart != 1 || oldCount != 0 || newStart != 1 || newCount != 0 {
		t.Errorf("got %d,%d,%d,%d", oldStart, oldCount, newStart, newCount)
	}
}

func TestParseHunkHeader_Invalid(t *testing.T) {
	oldStart, oldCount, newStart, newCount := parseHunkHeader("@@ bad @@")
	if oldStart != 0 || oldCount != 0 || newStart != 0 || newCount != 0 {
		t.Errorf("invalid header should return zeros, got %d,%d,%d,%d", oldStart, oldCount, newStart, newCount)
	}
}

func TestTrimDiffPrefix(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"a/main.go", "main.go"},
		{"b/main.go", "main.go"},
		{"main.go", "main.go"},
		{"a/b/c.go", "b/c.go"},
	}
	for _, tt := range tests {
		got := trimDiffPrefix(tt.input)
		if got != tt.want {
			t.Errorf("trimDiffPrefix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path, want string
	}{
		{"main.go", "go"},
		{"app.py", "python"},
		{"index.js", "javascript"},
		{"app.ts", "typescript"},
		{"readme.md", "unknown"},
		{"x", "unknown"},
	}
	for _, tt := range tests {
		got := detectLanguage(tt.path)
		if got != tt.want {
			t.Errorf("detectLanguage(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
