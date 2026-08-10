package rigour

import (
	"context"
	"log/slog"
	"testing"
)

func testFixApplier() *FixApplier {
	return NewFixApplier(slog.Default())
}

func TestDetermineStrategy_DocGate(t *testing.T) {
	fa := testFixApplier()

	docGates := []string{
		"missing_readme",
		"missing_contributing",
		"missing_license",
		"missing_changelog",
		"missing_docs_dir",
	}

	for _, gate := range docGates {
		fp := &FixPacket{
			GateName: gate,
			Files:    []FileTarget{{Path: "README.md"}},
		}
		strategy := fa.DetermineStrategy(fp)
		if strategy != StrategyCreateFile {
			t.Errorf("gate %q: expected StrategyCreateFile, got %s", gate, strategy)
		}
	}
}

func TestDetermineStrategy_ASTGrep(t *testing.T) {
	fa := testFixApplier()

	astGrepGates := []string{
		"cyclomatic_complexity",
		"nested_callbacks",
		"code_duplication",
		"duplicated_block",
		"naming_convention",
	}

	for _, gate := range astGrepGates {
		fp := &FixPacket{
			GateName: gate,
			Files:    []FileTarget{{Path: "main.go"}},
		}
		strategy := fa.DetermineStrategy(fp)
		if strategy != StrategyASTGre {
			t.Errorf("gate %q: expected StrategyASTGre, got %s", gate, strategy)
		}
	}
}

func TestDetermineStrategy_Delegate(t *testing.T) {
	fa := testFixApplier()

	fp := &FixPacket{
		GateName:     "unknown_gate",
		Instructions: []string{"Fix the code please"},
		Files:        []FileTarget{{Path: "main.go"}},
	}
	strategy := fa.DetermineStrategy(fp)
	if strategy != StrategyDelegate {
		t.Errorf("expected StrategyDelegate, got %s", strategy)
	}
}

func TestDetermineStrategy_Skip(t *testing.T) {
	fa := testFixApplier()

	fp := &FixPacket{
		GateName: "unknown_gate",
		Files:    []FileTarget{{Path: "main.go"}},
	}
	strategy := fa.DetermineStrategy(fp)
	if strategy != StrategySkip {
		t.Errorf("expected StrategySkip, got %s", strategy)
	}
}

func TestDetermineStrategy_InstructionMatch(t *testing.T) {
	fa := testFixApplier()

	// Instruction mentioning a known gate name should trigger ASTGre
	fp := &FixPacket{
		GateName:     "custom_gate",
		Instructions: []string{"Reduce cyclomatic complexity in this function"},
	}
	strategy := fa.DetermineStrategy(fp)
	if strategy != StrategyASTGre {
		t.Errorf("expected StrategyASTGre for instruction match, got %s", strategy)
	}
}

func TestEnforceConstraints_DoNotTouch(t *testing.T) {
	fa := testFixApplier()

	fp := &FixPacket{
		GateName: "missing_readme",
		Files:    []FileTarget{{Path: "vendor/important.go"}},
		Constraints: Constraints{
			DoNotTouch: []string{"vendor/"},
		},
	}

	_, err := fa.ApplyFix(context.Background(), fp, "agent-1")
	if err == nil {
		t.Error("expected constraint violation for do_not_touch path")
	}
}

func TestEnforceConstraints_MaxFiles(t *testing.T) {
	fa := testFixApplier()

	fp := &FixPacket{
		GateName: "missing_readme",
		Files: []FileTarget{
			{Path: "a.go"},
			{Path: "b.go"},
			{Path: "c.go"},
		},
		Constraints: Constraints{
			MaxFiles: 2,
		},
	}

	_, err := fa.ApplyFix(context.Background(), fp, "agent-1")
	if err == nil {
		t.Error("expected constraint violation for exceeding max_files")
	}
}

func TestEnforceConstraints_MaxFilesNotExceeded(t *testing.T) {
	fa := testFixApplier()

	fp := &FixPacket{
		GateName: "missing_readme",
		Files: []FileTarget{
			{Path: "a.go"},
		},
		Constraints: Constraints{
			MaxFiles: 2,
		},
	}

	result, err := fa.ApplyFix(context.Background(), fp, "agent-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestApplyFix_NilPacket(t *testing.T) {
	fa := testFixApplier()

	result, err := fa.ApplyFix(context.Background(), nil, "agent-1")
	if err != nil {
		t.Fatalf("nil packet should not error: %v", err)
	}
	if result.Strategy != StrategySkip {
		t.Errorf("nil packet should use skip strategy, got %s", result.Strategy)
	}
}

func TestApplyFix_EmptyPacket(t *testing.T) {
	fa := testFixApplier()

	result, err := fa.ApplyFix(context.Background(), &FixPacket{}, "agent-1")
	if err != nil {
		t.Fatalf("empty packet should not error: %v", err)
	}
	if result.Strategy != StrategySkip {
		t.Errorf("empty packet should use skip strategy, got %s", result.Strategy)
	}
}

func TestStrategy_String(t *testing.T) {
	tests := []struct {
		strat Strategy
		want  string
	}{
		{StrategyASTGre, "ast-grep"},
		{StrategyCreateFile, "create_file"},
		{StrategyDelegate, "delegate"},
		{StrategySkip, "skip"},
		{Strategy(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.strat.String()
		if got != tt.want {
			t.Errorf("Strategy(%d).String() = %q, want %q", int(tt.strat), got, tt.want)
		}
	}
}

func TestApplyFix_Delegate(t *testing.T) {
	fa := testFixApplier()

	fp := &FixPacket{
		GateName:     "unknown_gate",
		Instructions: []string{"Fix the code"},
		Files:        []FileTarget{{Path: "main.go"}},
		Constraints:  Constraints{MaxFiles: 5},
	}

	result, err := fa.ApplyFix(context.Background(), fp, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Strategy != StrategyDelegate {
		t.Errorf("expected delegate strategy, got %s", result.Strategy)
	}
	if !result.Success {
		t.Error("delegate should report success")
	}
}

func TestGuessLanguage(t *testing.T) {
	tests := []struct {
		path string
		lang string
	}{
		{"main.go", "go"},
		{"app.ts", "typescript"},
		{"component.tsx", "typescript"},
		{"script.js", "javascript"},
		{"page.jsx", "javascript"},
		{"app.py", "python"},
		{"lib.rs", "rust"},
		{"Main.java", "java"},
		{"data.xml", "unknown"},
	}

	for _, tt := range tests {
		got := guessLanguage(tt.path)
		if got != tt.lang {
			t.Errorf("guessLanguage(%q) = %q, want %q", tt.path, got, tt.lang)
		}
	}
}
