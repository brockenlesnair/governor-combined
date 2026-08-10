package sarif

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMapToSARIF(t *testing.T) {
	reportJSON := `{
		"$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		"version": "2.1.0",
		"runs": [{
			"tool": {
				"driver": {
					"name": "test-linter",
					"version": "1.0.0"
				}
			},
			"results": [{
				"ruleId": "test-rule",
				"level": "error",
				"message": { "text": "Something is wrong" },
				"locations": [{
					"physicalLocation": {
						"artifactLocation": { "uri": "src/main.go" },
						"region": { "startLine": 42, "startColumn": 5 }
					}
				}]
			}]
		}]
	}`

	report, err := ParseReport([]byte(reportJSON))
	if err != nil {
		t.Fatal(err)
	}

	findings := MapResults(report, "test-source")

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}

	f := findings[0]
	if f.RuleID != "test-rule" {
		t.Errorf("expected rule ID test-rule, got %s", f.RuleID)
	}
	if f.Level != "error" {
		t.Errorf("expected level error, got %s", f.Level)
	}
	if f.Severity != SeverityError {
		t.Errorf("expected severity error, got %s", f.Severity)
	}
	if f.Message != "Something is wrong" {
		t.Errorf("expected message 'Something is wrong', got %s", f.Message)
	}
	if f.File != "src/main.go" {
		t.Errorf("expected file src/main.go, got %s", f.File)
	}
	if f.Line != 42 {
		t.Errorf("expected line 42, got %d", f.Line)
	}
	if f.Column != 5 {
		t.Errorf("expected column 5, got %d", f.Column)
	}
	if f.Tool != "test-linter" {
		t.Errorf("expected tool test-linter, got %s", f.Tool)
	}
	if f.ToolVersion != "1.0.0" {
		t.Errorf("expected tool version 1.0.0, got %s", f.ToolVersion)
	}
	if f.Source != "test-source" {
		t.Errorf("expected source test-source, got %s", f.Source)
	}
	if f.Fingerprint == "" {
		t.Error("expected non-empty fingerprint")
	}
}

func TestMapToSARIF_MultipleResults(t *testing.T) {
	reportJSON := `{
		"$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		"version": "2.1.0",
		"runs": [{
			"tool": {
				"driver": {
					"name": "multi-linter",
					"version": "2.0.0"
				}
			},
			"results": [
				{
					"ruleId": "rule-1",
					"level": "error",
					"message": { "text": "Error 1" },
					"locations": [{
						"physicalLocation": {
							"artifactLocation": { "uri": "main.go" },
							"region": { "startLine": 10 }
						}
					}]
				},
				{
					"ruleId": "rule-2",
					"level": "warning",
					"message": { "text": "Warning 1" },
					"locations": [{
						"physicalLocation": {
							"artifactLocation": { "uri": "utils.go" },
							"region": { "startLine": 25, "startColumn": 3, "endLine": 25, "endColumn": 20 }
						}
					}]
				},
				{
					"ruleId": "rule-3",
					"level": "note",
					"message": { "text": "Note 1" }
				}
			]
		}]
	}`

	report, err := ParseReport([]byte(reportJSON))
	if err != nil {
		t.Fatal(err)
	}

	findings := MapResults(report, "test")

	if len(findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(findings))
	}

	// Check second finding has column and end info
	f2 := findings[1]
	if f2.Column != 3 {
		t.Errorf("expected column 3, got %d", f2.Column)
	}
	if f2.EndLine != 25 {
		t.Errorf("expected endLine 25, got %d", f2.EndLine)
	}
	if f2.EndColumn != 20 {
		t.Errorf("expected endColumn 20, got %d", f2.EndColumn)
	}

	// Third finding has no location
	f3 := findings[2]
	if f3.File != "" {
		t.Errorf("expected empty file for finding without location, got %s", f3.File)
	}
}

func TestMapToSARIF_WithFingerprint(t *testing.T) {
	reportJSON := `{
		"$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		"version": "2.1.0",
		"runs": [{
			"tool": {
				"driver": {
					"name": "fingerprint-tool",
					"version": "1.0"
				}
			},
			"results": [{
				"ruleId": "test-rule",
				"level": "warning",
				"message": { "text": "has fingerprint" },
				"fingerprints": {
					"sha256/vulnerability": "abc123"
				}
			}]
		}]
	}`

	report, err := ParseReport([]byte(reportJSON))
	if err != nil {
		t.Fatal(err)
	}

	findings := MapResults(report, "test")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}

	if findings[0].Fingerprint != "abc123" {
		t.Errorf("expected fingerprint abc123, got %s", findings[0].Fingerprint)
	}
}

func TestMapToSARIF_WithTimestamps(t *testing.T) {
	reportJSON := `{
		"$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		"version": "2.1.0",
		"runs": [{
			"tool": {
				"driver": {
					"name": "timestamp-tool",
					"version": "1.0"
				}
			},
			"timestamps": {
				"startTime": "2024-01-15T10:00:00Z",
				"endTime": "2024-01-15T10:05:00Z"
			},
			"results": [{
				"ruleId": "test-rule",
				"level": "warning",
				"message": { "text": "timed finding" }
			}]
		}]
	}`

	report, err := ParseReport([]byte(reportJSON))
	if err != nil {
		t.Fatal(err)
	}

	findings := MapResults(report, "test")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}

	expected := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	if !findings[0].Timestamp.Equal(expected) {
		t.Errorf("expected timestamp %v, got %v", expected, findings[0].Timestamp)
	}
}

func TestMapToSARIF_EmptyReport(t *testing.T) {
	reportJSON := `{
		"$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		"version": "2.1.0",
		"runs": []
	}`

	report, err := ParseReport([]byte(reportJSON))
	if err != nil {
		t.Fatal(err)
	}

	findings := MapResults(report, "test")
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for empty report, got %d", len(findings))
	}
}

func TestGenerateFingerprint(t *testing.T) {
	// Deterministic
	fp1 := GenerateFingerprint("rule-1", "main.go", 42)
	fp2 := GenerateFingerprint("rule-1", "main.go", 42)
	if fp1 != fp2 {
		t.Errorf("same inputs should produce same fingerprint: %s != %s", fp1, fp2)
	}

	// Different inputs produce different fingerprints
	fp3 := GenerateFingerprint("rule-2", "main.go", 42)
	if fp1 == fp3 {
		t.Error("different rules should produce different fingerprints")
	}

	fp4 := GenerateFingerprint("rule-1", "other.go", 42)
	if fp1 == fp4 {
		t.Error("different files should produce different fingerprints")
	}

	fp5 := GenerateFingerprint("rule-1", "main.go", 99)
	if fp1 == fp5 {
		t.Error("different lines should produce different fingerprints")
	}
}

func TestNormalizeRuleID(t *testing.T) {
	tests := []struct {
		input    string
		prefix   RulePrefix
		category string
		rule     string
	}{
		{
			"hangar.dependabot.vulnerable-dependency",
			PrefixDependabot,
			"vulnerable-dependency",
			"",
		},
		{
			"rigour.ast.complexity.high-cognitive-load",
			PrefixASTComplexity,
			"complexity",
			"high-cognitive-load",
		},
		{
			"adr.conflict.duplicate",
			PrefixADRConflict,
			"",
			"duplicate",
		},
		{
			"rb.standards-gap.missing-codeowners",
			PrefixStandardsGap,
			"standards-gap",
			"missing-codeowners",
		},
		{
			"unknown.rule.id",
			PrefixSARIF,
			"",
			"unknown.rule.id",
		},
	}

	for _, tt := range tests {
		norm := NormalizeRuleID(tt.input)
		if norm.Full != tt.input {
			t.Errorf("Full should be %s, got %s", tt.input, norm.Full)
		}
		if norm.Prefix != tt.prefix {
			t.Errorf("input %s: expected prefix %s, got %s", tt.input, tt.prefix, norm.Prefix)
		}
	}
}

func TestMapSeverity(t *testing.T) {
	tests := []struct {
		input string
		want  Severity
	}{
		{"error", SeverityError},
		{"Error", SeverityError},
		{"warning", SeverityWarning},
		{"Warning", SeverityWarning},
		{"note", SeverityNote},
		{"Note", SeverityNote},
		{"unknown", SeverityNone},
		{"", SeverityNone},
	}

	for _, tt := range tests {
		got := MapSeverity(tt.input)
		if got != tt.want {
			t.Errorf("MapSeverity(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSeverityWeight(t *testing.T) {
	tests := []struct {
		sev  Severity
		want int
	}{
		{SeverityError, 4},
		{SeverityWarning, 3},
		{SeverityNote, 2},
		{SeverityNone, 0},
		{Severity("unknown"), 0},
	}

	for _, tt := range tests {
		got := SeverityWeight(tt.sev)
		if got != tt.want {
			t.Errorf("SeverityWeight(%q) = %d, want %d", tt.sev, got, tt.want)
		}
	}
}

func TestParseReport_InvalidJSON(t *testing.T) {
	_, err := ParseReport([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseReport_WrongVersion(t *testing.T) {
	reportJSON := `{
		"$schema": "test",
		"version": "1.0.0",
		"runs": []
	}`

	_, err := ParseReport([]byte(reportJSON))
	if err == nil {
		t.Error("expected error for wrong SARIF version")
	}
}

func TestSummarizeReport(t *testing.T) {
	findings := []Finding{
		{RuleID: "rule-1", Severity: SeverityError, Tool: "linter-1", Normalized: NormalizedRuleID{Prefix: PrefixSARIF, Rule: "rule-1"}, File: "main.go"},
		{RuleID: "rule-1", Severity: SeverityError, Tool: "linter-1", Normalized: NormalizedRuleID{Prefix: PrefixSARIF, Rule: "rule-1"}, File: "main.go"},
		{RuleID: "rule-2", Severity: SeverityWarning, Tool: "linter-2", Normalized: NormalizedRuleID{Prefix: PrefixSARIF, Rule: "rule-2"}, File: "utils.go"},
	}

	summary := SummarizeReport(findings)

	if summary.TotalFindings != 3 {
		t.Errorf("expected 3 total findings, got %d", summary.TotalFindings)
	}
	if summary.BySeverity[SeverityError] != 2 {
		t.Errorf("expected 2 error findings, got %d", summary.BySeverity[SeverityError])
	}
	if summary.BySeverity[SeverityWarning] != 1 {
		t.Errorf("expected 1 warning finding, got %d", summary.BySeverity[SeverityWarning])
	}
	if summary.FilesAffected != 2 {
		t.Errorf("expected 2 files affected, got %d", summary.FilesAffected)
	}
}

func TestSummarizeReport_Empty(t *testing.T) {
	summary := SummarizeReport([]Finding{})
	if summary.TotalFindings != 0 {
		t.Errorf("expected 0 total findings, got %d", summary.TotalFindings)
	}
}

func TestMapToSARIF_UsesEndTime(t *testing.T) {
	reportJSON := `{
		"$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		"version": "2.1.0",
		"runs": [{
			"tool": {
				"driver": {
					"name": "end-time-tool",
					"version": "1.0"
				}
			},
			"timestamps": {
				"endTime": "2024-06-01T12:00:00Z"
			},
			"results": [{
				"ruleId": "test-rule",
				"level": "warning",
				"message": { "text": "end time test" }
			}]
		}]
	}`

	report, err := ParseReport([]byte(reportJSON))
	if err != nil {
		t.Fatal(err)
	}

	findings := MapResults(report, "test")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}

	expected := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	if !findings[0].Timestamp.Equal(expected) {
		t.Errorf("expected endTime %v, got %v", expected, findings[0].Timestamp)
	}
}

func TestFinding_JSON(t *testing.T) {
	f := Finding{
		ID:       "finding-1",
		RuleID:   "rule-1",
		Level:    "error",
		Severity: SeverityError,
		Message:  "test message",
		File:     "main.go",
		Line:     42,
		Tool:     "test-tool",
	}

	data, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}

	var f2 Finding
	if err := json.Unmarshal(data, &f2); err != nil {
		t.Fatal(err)
	}

	if f2.RuleID != "rule-1" {
		t.Errorf("expected rule-1, got %s", f2.RuleID)
	}
	if f2.Line != 42 {
		t.Errorf("expected line 42, got %d", f2.Line)
	}
}
