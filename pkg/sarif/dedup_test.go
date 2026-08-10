package sarif

import (
	"log/slog"
	"testing"
	"time"
)

func TestGenerateDedupFingerprint(t *testing.T) {
	// Same inputs should produce same fingerprint
	fp1 := GenerateDedupFingerprint("rule-1", "main.go", 42)
	fp2 := GenerateDedupFingerprint("rule-1", "main.go", 42)

	if fp1 != fp2 {
		t.Errorf("same inputs should produce same fingerprint: %s != %s", fp1, fp2)
	}

	// Different rule should produce different fingerprint
	fp3 := GenerateDedupFingerprint("rule-2", "main.go", 42)
	if fp1 == fp3 {
		t.Error("different rule IDs should produce different fingerprints")
	}

	// Different file should produce different fingerprint
	fp4 := GenerateDedupFingerprint("rule-1", "other.go", 42)
	if fp1 == fp4 {
		t.Error("different file paths should produce different fingerprints")
	}

	// Different line should produce different fingerprint
	fp5 := GenerateDedupFingerprint("rule-1", "main.go", 99)
	if fp1 == fp5 {
		t.Error("different lines should produce different fingerprints")
	}
}

func TestFingerprintDedup_Deduplicate(t *testing.T) {
	dedup := NewFingerprintDedup(DedupConfig{}, slog.Default())

	findings := []Finding{
		{RuleID: "rule-1", File: "main.go", Line: 10, Fingerprint: "fp-1"},
		{RuleID: "rule-1", File: "main.go", Line: 10, Fingerprint: "fp-1"},
		{RuleID: "rule-1", File: "main.go", Line: 10, Fingerprint: "fp-1"},
		{RuleID: "rule-2", File: "utils.go", Line: 20, Fingerprint: "fp-2"},
	}

	result := dedup.Deduplicate(findings)

	if result.Original != 4 {
		t.Errorf("expected 4 original findings, got %d", result.Original)
	}
	if result.Deduped != 2 {
		t.Errorf("expected 2 deduped findings, got %d", result.Deduped)
	}
	if result.Removed != 2 {
		t.Errorf("expected 2 removed findings, got %d", result.Removed)
	}
	if result.Ratio != 0.5 {
		t.Errorf("expected ratio 0.5, got %f", result.Ratio)
	}
}

func TestFingerprintDedup_Deduplicate_Empty(t *testing.T) {
	dedup := NewFingerprintDedup(DedupConfig{}, slog.Default())

	result := dedup.Deduplicate([]Finding{})
	if result.Original != 0 {
		t.Errorf("expected 0 original findings for empty input, got %d", result.Original)
	}
}

func TestFingerprintDedup_Deduplicate_NoDuplicates(t *testing.T) {
	dedup := NewFingerprintDedup(DedupConfig{}, slog.Default())

	findings := []Finding{
		{RuleID: "rule-1", File: "main.go", Line: 10, Fingerprint: "fp-1"},
		{RuleID: "rule-2", File: "utils.go", Line: 20, Fingerprint: "fp-2"},
	}

	result := dedup.Deduplicate(findings)

	if result.Original != 2 {
		t.Errorf("expected 2 original, got %d", result.Original)
	}
	if result.Deduped != 2 {
		t.Errorf("expected 2 deduped, got %d", result.Deduped)
	}
	if result.Removed != 0 {
		t.Errorf("expected 0 removed, got %d", result.Removed)
	}
}

func TestFingerprintDedup_GeneratesFingerprintIfMissing(t *testing.T) {
	dedup := NewFingerprintDedup(DedupConfig{}, slog.Default())

	// Findings with no fingerprint should get one auto-generated
	findings := []Finding{
		{RuleID: "rule-1", File: "main.go", Line: 10},
		{RuleID: "rule-1", File: "main.go", Line: 10},
	}

	result := dedup.Deduplicate(findings)

	if result.Deduped != 1 {
		t.Errorf("expected 1 deduped (auto-generated fingerprints match), got %d", result.Deduped)
	}
}

func TestFingerprintDedup_KeptContainsOnePerGroup(t *testing.T) {
	dedup := NewFingerprintDedup(DedupConfig{}, slog.Default())

	findings := []Finding{
		{RuleID: "rule-1", File: "main.go", Line: 10, Fingerprint: "fp-1"},
		{RuleID: "rule-1", File: "main.go", Line: 10, Fingerprint: "fp-1"},
	}

	result := dedup.Deduplicate(findings)

	if len(result.Kept) != 1 {
		t.Errorf("expected 1 kept finding, got %d", len(result.Kept))
	}
}

func TestFingerprintDedup_DuplicatesRecorded(t *testing.T) {
	dedup := NewFingerprintDedup(DedupConfig{}, slog.Default())

	findings := []Finding{
		{RuleID: "rule-1", File: "main.go", Line: 10, Fingerprint: "fp-1"},
		{RuleID: "rule-1", File: "main.go", Line: 10, Fingerprint: "fp-1"},
		{RuleID: "rule-1", File: "main.go", Line: 10, Fingerprint: "fp-1"},
	}

	result := dedup.Deduplicate(findings)

	if len(result.Duplicates) != 2 {
		t.Errorf("expected 2 duplicate records, got %d", len(result.Duplicates))
	}

	for _, d := range result.Duplicates {
		if d.Reason != "fingerprint" {
			t.Errorf("expected reason 'fingerprint', got %s", d.Reason)
		}
		if d.Similarity != 1.0 {
			t.Errorf("expected similarity 1.0, got %f", d.Similarity)
		}
	}
}

func TestTimeWindowDedup_Deduplicate(t *testing.T) {
	dedup := NewTimeWindowDedup(DedupConfig{TimeWindow: 1 * time.Hour}, slog.Default())

	now := time.Now()
	findings := []Finding{
		{RuleID: "rule-1", File: "main.go", Line: 10, Timestamp: now},
		{RuleID: "rule-1", File: "main.go", Line: 10, Timestamp: now.Add(30 * time.Minute)},
		{RuleID: "rule-1", File: "main.go", Line: 10, Timestamp: now.Add(2 * time.Hour)},
	}

	result := dedup.Deduplicate(findings, now.Add(3*time.Hour))

	if result.Original != 3 {
		t.Errorf("expected 3 original, got %d", result.Original)
	}
	// The first two are within the window, the third is outside
	if result.Deduped >= 3 {
		t.Errorf("expected some deduplication, got %d kept", result.Deduped)
	}
}

func TestTimeWindowDedup_Deduplicate_Empty(t *testing.T) {
	dedup := NewTimeWindowDedup(DedupConfig{}, slog.Default())

	result := dedup.Deduplicate([]Finding{}, time.Now())
	if result.Original != 0 {
		t.Errorf("expected 0 original, got %d", result.Original)
	}
}

func TestTimeWindowDedup_Deduplicate_SingleFinding(t *testing.T) {
	dedup := NewTimeWindowDedup(DedupConfig{}, slog.Default())

	findings := []Finding{
		{RuleID: "rule-1", File: "main.go", Line: 10, Timestamp: time.Now()},
	}

	result := dedup.Deduplicate(findings, time.Now())

	if result.Deduped != 1 {
		t.Errorf("expected 1 kept for single finding, got %d", result.Deduped)
	}
}

func TestDedupConfig_ApplyDefaults(t *testing.T) {
	cfg := DedupConfig{}
	cfg.applyDefaults()

	if cfg.TimeWindow != 24*time.Hour {
		t.Errorf("expected default TimeWindow 24h, got %v", cfg.TimeWindow)
	}
	if cfg.TargetReduction != 0.3 {
		t.Errorf("expected default TargetReduction 0.3, got %f", cfg.TargetReduction)
	}
}

func TestDedupConfig_ApplyDefaults_PreservesValues(t *testing.T) {
	cfg := DedupConfig{
		TimeWindow:      1 * time.Hour,
		TargetReduction: 0.5,
	}
	cfg.applyDefaults()

	if cfg.TimeWindow != 1*time.Hour {
		t.Errorf("expected preserved TimeWindow 1h, got %v", cfg.TimeWindow)
	}
	if cfg.TargetReduction != 0.5 {
		t.Errorf("expected preserved TargetReduction 0.5, got %f", cfg.TargetReduction)
	}
}

func TestNewFingerprintDedup(t *testing.T) {
	dedup := NewFingerprintDedup(DedupConfig{}, slog.Default())
	if dedup == nil {
		t.Fatal("NewFingerprintDedup returned nil")
	}
}

func TestNewTimeWindowDedup(t *testing.T) {
	dedup := NewTimeWindowDedup(DedupConfig{}, slog.Default())
	if dedup == nil {
		t.Fatal("NewTimeWindowDedup returned nil")
	}
}

func TestNewCrossToolDedup(t *testing.T) {
	dedup := NewCrossToolDedup(DedupConfig{}, slog.Default())
	if dedup == nil {
		t.Fatal("NewCrossToolDedup returned nil")
	}
}

func TestCrossToolDedup_Disabled(t *testing.T) {
	dedup := NewCrossToolDedup(DedupConfig{CrossTool: false}, slog.Default())

	findings := []Finding{
		{RuleID: "rule-1", File: "main.go", Line: 10},
		{RuleID: "rule-1", File: "main.go", Line: 10},
	}

	result := dedup.Deduplicate(findings)

	// When cross-tool is disabled, all findings should be kept
	if result.Deduped != 2 {
		t.Errorf("expected 2 kept when disabled, got %d", result.Deduped)
	}
}

func TestNewDedupPipeline(t *testing.T) {
	pipeline := NewDedupPipeline(DedupConfig{}, slog.Default())
	if pipeline == nil {
		t.Fatal("NewDedupPipeline returned nil")
	}
}

func TestDedupPipeline_Deduplicate(t *testing.T) {
	pipeline := NewDedupPipeline(DedupConfig{}, slog.Default())

	findings := []Finding{
		{RuleID: "rule-1", File: "main.go", Line: 10, Fingerprint: "fp-1"},
		{RuleID: "rule-1", File: "main.go", Line: 10, Fingerprint: "fp-1"},
		{RuleID: "rule-2", File: "utils.go", Line: 20, Fingerprint: "fp-2"},
	}

	result := pipeline.Deduplicate(findings, time.Now())

	if result.Original != 3 {
		t.Errorf("expected 3 original, got %d", result.Original)
	}
	if result.Deduped >= 3 {
		t.Error("expected some deduplication")
	}
	if result.Removed <= 0 {
		t.Error("expected some removals")
	}
}

func TestDedupPipeline_Deduplicate_Empty(t *testing.T) {
	pipeline := NewDedupPipeline(DedupConfig{}, slog.Default())

	result := pipeline.Deduplicate([]Finding{}, time.Now())
	if result.Original != 0 {
		t.Errorf("expected 0 original, got %d", result.Original)
	}
}

func TestDedupPipeline_Deduplicate_AllUnique(t *testing.T) {
	pipeline := NewDedupPipeline(DedupConfig{}, slog.Default())

	findings := []Finding{
		{RuleID: "rule-1", File: "main.go", Line: 10, Fingerprint: "fp-1"},
		{RuleID: "rule-2", File: "utils.go", Line: 20, Fingerprint: "fp-2"},
	}

	result := pipeline.Deduplicate(findings, time.Now())

	if result.Deduped != 2 {
		t.Errorf("expected 2 kept for unique findings, got %d", result.Deduped)
	}
	if result.Removed != 0 {
		t.Errorf("expected 0 removed for unique findings, got %d", result.Removed)
	}
}
