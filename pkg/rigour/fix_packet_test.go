package rigour

import (
	"testing"
)

func TestParser_Parse(t *testing.T) {
	parser := NewParser()

	text := `Gate: cyclomatic_complexity
Severity: warning
Files: src/main.go, src/utils.go
1. Reduce cyclomatic complexity in the handleRequest function
2. Extract helper functions for nested conditionals
do_not_touch: vendor/, node_modules/
paradigm: functional
verify: go build ./...
`

	fp, err := parser.Parse(text)
	if err != nil {
		t.Fatal(err)
	}

	if fp.GateName != "cyclomatic_complexity" {
		t.Errorf("expected gate name cyclomatic_complexity, got %s", fp.GateName)
	}
	if fp.Severity != SeverityWarning {
		t.Errorf("expected severity warning, got %s", fp.Severity)
	}
	if len(fp.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(fp.Files))
	}
	if fp.Files[0].Path != "src/main.go" {
		t.Errorf("expected first file path src/main.go, got %s", fp.Files[0].Path)
	}
	if fp.Files[1].Path != "src/utils.go" {
		t.Errorf("expected second file path src/utils.go, got %s", fp.Files[1].Path)
	}
	if len(fp.Instructions) != 2 {
		t.Fatalf("expected 2 instructions, got %d", len(fp.Instructions))
	}
	if fp.Instructions[0] != "Reduce cyclomatic complexity in the handleRequest function" {
		t.Errorf("unexpected first instruction: %s", fp.Instructions[0])
	}
	if len(fp.Constraints.DoNotTouch) != 2 {
		t.Fatalf("expected 2 do_not_touch paths, got %d", len(fp.Constraints.DoNotTouch))
	}
	if fp.Constraints.DoNotTouch[0] != "vendor/" {
		t.Errorf("expected do_not_touch vendor/, got %s", fp.Constraints.DoNotTouch[0])
	}
	if fp.Constraints.Paradigm != "functional" {
		t.Errorf("expected paradigm functional, got %s", fp.Constraints.Paradigm)
	}
	if len(fp.Verification) != 1 {
		t.Fatalf("expected 1 verification command, got %d", len(fp.Verification))
	}
	if fp.Verification[0] != "go build ./..." {
		t.Errorf("expected verification 'go build ./...', got %s", fp.Verification[0])
	}
}

func TestParser_Parse_Empty(t *testing.T) {
	parser := NewParser()

	_, err := parser.Parse("")
	if err == nil {
		t.Error("expected error for empty text")
	}
}

func TestParser_Parse_NoRecognizableContent(t *testing.T) {
	parser := NewParser()

	_, err := parser.Parse("just some random garbage without any markers")
	if err == nil {
		t.Error("expected error for unrecognized content")
	}
}

func TestParser_Parse_MinimalGate(t *testing.T) {
	parser := NewParser()

	text := `Gate: missing_readme
Severity: error
1. Create a README.md file
`
	fp, err := parser.Parse(text)
	if err != nil {
		t.Fatal(err)
	}

	if fp.GateName != "missing_readme" {
		t.Errorf("expected gate name missing_readme, got %s", fp.GateName)
	}
	if fp.Severity != SeverityError {
		t.Errorf("expected severity error, got %s", fp.Severity)
	}
	if len(fp.Files) != 0 {
		t.Errorf("expected 0 files, got %d", len(fp.Files))
	}
}

func TestParser_ParseSeverity(t *testing.T) {
	tests := []struct {
		input string
		want  Severity
	}{
		{"critical", SeverityCritical},
		{"crit", SeverityCritical},
		{"CRITICAL", SeverityCritical},
		{"error", SeverityError},
		{"err", SeverityError},
		{"ERROR", SeverityError},
		{"warning", SeverityWarning},
		{"warn", SeverityWarning},
		{"WARNING", SeverityWarning},
		{"info", SeverityInfo},
		{"information", SeverityInfo},
		{"INFO", SeverityInfo},
		{"unknown", SeverityWarning}, // defaults to warning
		{"", SeverityWarning},       // defaults to warning
	}

	for _, tt := range tests {
		got := ParseSeverity(tt.input)
		if got != tt.want {
			t.Errorf("ParseSeverity(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestSeverity_String(t *testing.T) {
	tests := []struct {
		sev  Severity
		want string
	}{
		{SeverityInfo, "info"},
		{SeverityWarning, "warning"},
		{SeverityError, "error"},
		{SeverityCritical, "critical"},
		{Severity(99), "unknown(99)"},
	}

	for _, tt := range tests {
		got := tt.sev.String()
		if got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", int(tt.sev), got, tt.want)
		}
	}
}

func TestFixPacket_IsEmpty(t *testing.T) {
	tests := []struct {
		name string
		fp   FixPacket
		want bool
	}{
		{
			name: "empty",
			fp:   FixPacket{},
			want: true,
		},
		{
			name: "only_gate",
			fp:   FixPacket{GateName: "test"},
			want: false,
		},
		{
			name: "only_instructions",
			fp:   FixPacket{Instructions: []string{"do something"}},
			want: false,
		},
		{
			name: "only_files",
			fp:   FixPacket{Files: []FileTarget{{Path: "main.go"}}},
			want: false,
		},
		{
			name: "full",
			fp: FixPacket{
				GateName:     "test",
				Instructions: []string{"fix it"},
				Files:        []FileTarget{{Path: "main.go"}},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fp.IsEmpty()
			if got != tt.want {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileTarget_HasRange(t *testing.T) {
	tests := []struct {
		ft   FileTarget
		want bool
	}{
		{FileTarget{Path: "main.go"}, false},
		{FileTarget{Path: "main.go", StartLine: 10}, true},
		{FileTarget{Path: "main.go", EndLine: 20}, true},
		{FileTarget{Path: "main.go", StartLine: 10, EndLine: 20}, true},
	}

	for _, tt := range tests {
		got := tt.ft.HasRange()
		if got != tt.want {
			t.Errorf("FileTarget{StartLine:%d, EndLine:%d}.HasRange() = %v, want %v",
				tt.ft.StartLine, tt.ft.EndLine, got, tt.want)
		}
	}
}

func TestParser_Parse_MultipleFileLines(t *testing.T) {
	parser := NewParser()

	text := `Gate: naming_convention
Files: src/a.go
Files: src/b.go
`
	fp, err := parser.Parse(text)
	if err != nil {
		t.Fatal(err)
	}

	if len(fp.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(fp.Files))
	}
	if fp.Files[0].Path != "src/a.go" {
		t.Errorf("expected first file src/a.go, got %s", fp.Files[0].Path)
	}
	if fp.Files[1].Path != "src/b.go" {
		t.Errorf("expected second file src/b.go, got %s", fp.Files[1].Path)
	}
}

func TestParser_Parse_OnlyInstructions(t *testing.T) {
	parser := NewParser()

	text := `1. First instruction
2. Second instruction
3. Third instruction
`
	fp, err := parser.Parse(text)
	if err != nil {
		t.Fatal(err)
	}

	if len(fp.Instructions) != 3 {
		t.Fatalf("expected 3 instructions, got %d", len(fp.Instructions))
	}
}
