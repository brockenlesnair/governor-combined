package sarif

import (
	"fmt"
	"log/slog"
	"testing"
	"time"
)

func TestStalenessCheck_FreshData(t *testing.T) {
	sc := NewStalenessCheck(EdgeCaseConfig{}, slog.Default())

	result := sc.Evaluate(12.0)

	if result.IsStale {
		t.Error("12h data should not be stale with 24h threshold")
	}
	if result.Action != "allow" {
		t.Errorf("expected action 'allow', got %s", result.Action)
	}
}

func TestStalenessCheck_StaleData_Warn(t *testing.T) {
	sc := NewStalenessCheck(EdgeCaseConfig{}, slog.Default())

	result := sc.Evaluate(48.0)

	if !result.IsStale {
		t.Error("48h data should be stale with 24h threshold")
	}
	if result.Action != "warn" {
		t.Errorf("expected action 'warn', got %s", result.Action)
	}
}

func TestStalenessCheck_StaleData_Block(t *testing.T) {
	sc := NewStalenessCheck(EdgeCaseConfig{StalenessMode: "block"}, slog.Default())

	result := sc.Evaluate(48.0)

	if !result.IsStale {
		t.Error("48h data should be stale")
	}
	if result.Action != "block" {
		t.Errorf("expected action 'block', got %s", result.Action)
	}
}

func TestStalenessCheck_StaleData_Ignore(t *testing.T) {
	sc := NewStalenessCheck(EdgeCaseConfig{StalenessMode: "ignore"}, slog.Default())

	result := sc.Evaluate(48.0)

	if !result.IsStale {
		t.Error("48h data should be stale")
	}
	if result.Action != "allow" {
		t.Errorf("expected action 'allow' (ignore mode), got %s", result.Action)
	}
}

func TestStalenessCheck_ExactThreshold(t *testing.T) {
	sc := NewStalenessCheck(EdgeCaseConfig{}, slog.Default())

	// Exactly at threshold should not be stale
	result := sc.Evaluate(24.0)
	if result.IsStale {
		t.Error("data at exact threshold should not be stale")
	}
}

func TestStalenessCheck_AboveThreshold(t *testing.T) {
	sc := NewStalenessCheck(EdgeCaseConfig{}, slog.Default())

	result := sc.Evaluate(25.0)
	if !result.IsStale {
		t.Error("data above threshold should be stale")
	}
}

func TestStalenessCheck_CustomThreshold(t *testing.T) {
	sc := NewStalenessCheck(EdgeCaseConfig{StalenessThresholdHours: 12}, slog.Default())

	result := sc.Evaluate(13.0)
	if !result.IsStale {
		t.Error("data above custom threshold should be stale")
	}

	result2 := sc.Evaluate(11.0)
	if result2.IsStale {
		t.Error("data below custom threshold should not be stale")
	}
}

func TestStalenessCheck_ResultMessage(t *testing.T) {
	sc := NewStalenessCheck(EdgeCaseConfig{}, slog.Default())

	result := sc.Evaluate(48.0)
	if result.Message == "" {
		t.Error("expected non-empty message")
	}

	result2 := sc.Evaluate(12.0)
	if result2.Message == "" {
		t.Error("expected non-empty message for fresh data")
	}
}

func TestLocationExtractor_FromSARIF(t *testing.T) {
	le := NewLocationExtractor(EdgeCaseConfig{}, slog.Default())

	finding := Finding{
		File: "src/main.go",
		Line: 42,
	}

	loc := le.ExtractLocation(finding, nil)
	if loc == nil {
		t.Fatal("expected non-nil location")
	}
	if loc.File != "src/main.go" {
		t.Errorf("expected file src/main.go, got %s", loc.File)
	}
	if loc.Line != 42 {
		t.Errorf("expected line 42, got %d", loc.Line)
	}
	if loc.Source != "sarif" {
		t.Errorf("expected source sarif, got %s", loc.Source)
	}
}

func TestLocationExtractor_FromMetadata(t *testing.T) {
	le := NewLocationExtractor(EdgeCaseConfig{}, slog.Default())

	finding := Finding{} // No file set
	metadata := map[string]string{
		"file_path": "src/utils.go",
		"line":      "100",
	}

	loc := le.ExtractLocation(finding, metadata)
	if loc == nil {
		t.Fatal("expected non-nil location from metadata")
	}
	if loc.File != "src/utils.go" {
		t.Errorf("expected file src/utils.go, got %s", loc.File)
	}
	if loc.Line != 100 {
		t.Errorf("expected line 100, got %d", loc.Line)
	}
	if loc.Source != "metadata" {
		t.Errorf("expected source metadata, got %s", loc.Source)
	}
}

func TestLocationExtractor_FromDescription(t *testing.T) {
	le := NewLocationExtractor(EdgeCaseConfig{}, slog.Default())

	finding := Finding{
		Message: "Error in main.go:42:10 - something went wrong",
	}

	loc := le.ExtractLocation(finding, nil)
	if loc == nil {
		t.Fatal("expected non-nil location from description")
	}
	if loc.File != "main.go" {
		t.Errorf("expected file main.go, got %s", loc.File)
	}
	if loc.Line != 42 {
		t.Errorf("expected line 42, got %d", loc.Line)
	}
	if loc.Source != "description" {
		t.Errorf("expected source description, got %s", loc.Source)
	}
}

func TestLocationExtractor_NoLocation(t *testing.T) {
	le := NewLocationExtractor(EdgeCaseConfig{}, slog.Default())

	finding := Finding{
		Message: "Something went wrong without file info",
	}

	loc := le.ExtractLocation(finding, nil)
	// Should return unknown source or nil depending on strategy
	if loc != nil && loc.Source == "description" {
		t.Error("should not extract from description when no file pattern matches")
	}
}

func TestLocationExtractor_SkipStrategy(t *testing.T) {
	le := NewLocationExtractor(EdgeCaseConfig{MissingLocationStrategy: "skip"}, slog.Default())

	finding := Finding{
		Message: "Error in unknown format",
	}

	loc := le.ExtractLocation(finding, nil)
	if loc != nil {
		t.Error("skip strategy should return nil")
	}
}

func TestLocationExtractor_MetadataWithoutLine(t *testing.T) {
	le := NewLocationExtractor(EdgeCaseConfig{}, slog.Default())

	finding := Finding{}
	metadata := map[string]string{
		"file_path": "src/app.go",
	}

	loc := le.ExtractLocation(finding, metadata)
	if loc == nil {
		t.Fatal("expected non-nil location")
	}
	if loc.Line != 0 {
		t.Errorf("expected line 0 when not in metadata, got %d", loc.Line)
	}
}

func TestNewStalenessCheck(t *testing.T) {
	sc := NewStalenessCheck(EdgeCaseConfig{}, slog.Default())
	if sc == nil {
		t.Fatal("NewStalenessCheck returned nil")
	}
}

func TestNewLocationExtractor(t *testing.T) {
	le := NewLocationExtractor(EdgeCaseConfig{}, slog.Default())
	if le == nil {
		t.Fatal("NewLocationExtractor returned nil")
	}
}

func TestNewCollisionResolver(t *testing.T) {
	cr := NewCollisionResolver(EdgeCaseConfig{}, slog.Default())
	if cr == nil {
		t.Fatal("NewCollisionResolver returned nil")
	}
}

func TestNewPhaseDispatcher(t *testing.T) {
	pd := NewPhaseDispatcher(EdgeCaseConfig{}, slog.Default())
	if pd == nil {
		t.Fatal("NewPhaseDispatcher returned nil")
	}
}

func TestEdgeCaseConfig_ApplyDefaults(t *testing.T) {
	cfg := EdgeCaseConfig{}
	cfg.applyDefaults()

	if cfg.StalenessThresholdHours != 24.0 {
		t.Errorf("expected default StalenessThresholdHours 24.0, got %f", cfg.StalenessThresholdHours)
	}
	if cfg.StalenessMode != "warn" {
		t.Errorf("expected default StalenessMode warn, got %s", cfg.StalenessMode)
	}
	if cfg.MissingLocationStrategy != "extract" {
		t.Errorf("expected default MissingLocationStrategy extract, got %s", cfg.MissingLocationStrategy)
	}
	if cfg.RuleIDCollisionStrategy != "prefix" {
		t.Errorf("expected default RuleIDCollisionStrategy prefix, got %s", cfg.RuleIDCollisionStrategy)
	}
	if cfg.PhaseDispatchTimeout != 30*time.Second {
		t.Errorf("expected default PhaseDispatchTimeout 30s, got %v", cfg.PhaseDispatchTimeout)
	}
}

func TestCollisionResolver_NoConflict(t *testing.T) {
	cr := NewCollisionResolver(EdgeCaseConfig{}, slog.Default())

	result := cr.ResolveCollision("rule-1", map[string]bool{})

	if result.Conflict {
		t.Error("should not detect conflict when rule doesn't exist")
	}
	if result.ResolvedID != "rule-1" {
		t.Errorf("expected resolved ID rule-1, got %s", result.ResolvedID)
	}
}

func TestCollisionResolver_Prefix(t *testing.T) {
	cr := NewCollisionResolver(EdgeCaseConfig{RuleIDCollisionStrategy: "prefix"}, slog.Default())

	existing := map[string]bool{"mytool.rule-1": true}
	result := cr.ResolveCollision("mytool.rule-1", existing)

	if !result.Conflict {
		t.Error("should detect conflict")
	}
	if result.Strategy != "prefix" {
		t.Errorf("expected strategy prefix, got %s", result.Strategy)
	}
}

func TestCollisionResolver_Suffix(t *testing.T) {
	cr := NewCollisionResolver(EdgeCaseConfig{RuleIDCollisionStrategy: "suffix"}, slog.Default())

	existing := map[string]bool{"rule-1": true, "rule-1.1": true}
	result := cr.ResolveCollision("rule-1", existing)

	if !result.Conflict {
		t.Error("should detect conflict")
	}
	if result.Strategy != "suffix" {
		t.Errorf("expected strategy suffix, got %s", result.Strategy)
	}
	if result.ResolvedID != "rule-1.2" {
		t.Errorf("expected resolved ID rule-1.2, got %s", result.ResolvedID)
	}
}

func TestCollisionResolver_Reject(t *testing.T) {
	cr := NewCollisionResolver(EdgeCaseConfig{RuleIDCollisionStrategy: "reject"}, slog.Default())

	existing := map[string]bool{"rule-1": true}
	result := cr.ResolveCollision("rule-1", existing)

	if !result.Conflict {
		t.Error("should detect conflict")
	}
	if result.Strategy != "reject" {
		t.Errorf("expected strategy reject, got %s", result.Strategy)
	}
	if result.ResolvedID != "" {
		t.Errorf("expected empty resolved ID for reject, got %s", result.ResolvedID)
	}
}

func TestPhaseDispatcher_DispatchPhase_Success(t *testing.T) {
	pd := NewPhaseDispatcher(EdgeCaseConfig{PhaseDispatchTimeout: 5 * time.Second}, slog.Default())

	result := pd.DispatchPhase("quick", func() error {
		return nil
	})

	if result.Status != "dispatched" {
		t.Errorf("expected status dispatched, got %s", result.Status)
	}
	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

func TestPhaseDispatcher_DispatchPhase_Error(t *testing.T) {
	pd := NewPhaseDispatcher(EdgeCaseConfig{PhaseDispatchTimeout: 5 * time.Second}, slog.Default())

	result := pd.DispatchPhase("full", func() error {
		return fmt.Errorf("connection refused")
	})

	if result.Status != "fallback" {
		t.Errorf("expected status fallback, got %s", result.Status)
	}
	if result.Fallback == "" {
		t.Error("expected non-empty fallback suggestion")
	}
}

func TestPhaseDispatcher_DispatchPhase_Timeout(t *testing.T) {
	pd := NewPhaseDispatcher(EdgeCaseConfig{PhaseDispatchTimeout: 50 * time.Millisecond}, slog.Default())

	result := pd.DispatchPhase("full", func() error {
		time.Sleep(200 * time.Millisecond)
		return nil
	})

	if result.Status != "timeout" {
		t.Errorf("expected status timeout, got %s", result.Status)
	}
	if result.Fallback == "" {
		t.Error("expected non-empty fallback for timeout")
	}
}

func TestCollisionResolver_Prefix_NoDot(t *testing.T) {
	cr := NewCollisionResolver(EdgeCaseConfig{RuleIDCollisionStrategy: "prefix"}, slog.Default())

	existing := map[string]bool{"nodothrule": true}
	result := cr.ResolveCollision("nodothrule", existing)

	if !result.Conflict {
		t.Error("should detect conflict")
	}
	// No dot in rule ID, so it gets "unknown.dup." prefix
	if result.ResolvedID != "unknown.dup.nodothrule" {
		t.Errorf("expected unknown.dup.nodothrule, got %s", result.ResolvedID)
	}
}
