package repo

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client, err := NewClient(Config{
		ServerCommand: "echo",
		ServerArgs:    []string{"hello"},
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.config.ServerCommand != "echo" {
		t.Errorf("expected server command 'echo', got %s", client.config.ServerCommand)
	}
}

func TestNewClient_MissingServerCommand(t *testing.T) {
	_, err := NewClient(Config{}, slog.Default())
	if err == nil {
		t.Error("expected error for missing server command")
	}
}

func TestNewClient_InvalidFramingMode(t *testing.T) {
	_, err := NewClient(Config{
		ServerCommand: "echo",
		FramingMode:   "invalid",
	}, slog.Default())
	if err == nil {
		t.Error("expected error for invalid framing mode")
	}
}

func TestNewClient_Defaults(t *testing.T) {
	client, err := NewClient(Config{
		ServerCommand: "echo",
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if client.config.FramingMode != "newline" {
		t.Errorf("expected default framing mode 'newline', got %s", client.config.FramingMode)
	}
	if client.config.StalenessThresholdHours != 24.0 {
		t.Errorf("expected default staleness threshold 24.0, got %f", client.config.StalenessThresholdHours)
	}
	if client.config.RequestTimeout != 60*time.Second {
		t.Errorf("expected default request timeout 60s, got %v", client.config.RequestTimeout)
	}
	if client.config.InitTimeout != 30*time.Second {
		t.Errorf("expected default init timeout 30s, got %v", client.config.InitTimeout)
	}
}

func TestAllTools(t *testing.T) {
	tools := AllTools()
	if len(tools) != 12 {
		t.Errorf("expected 12 tools, got %d", len(tools))
	}

	toolSet := make(map[ToolName]bool)
	for _, tool := range tools {
		toolSet[tool] = true
	}

	expected := []ToolName{
		ToolTriggerRefresh, ToolGetStaleness, ToolCheckConflicts,
		ToolListDependencies, ToolGetHealthTier, ToolGetCampaignStatus,
		ToolQueryPortfolio, ToolGetRepoMetadata, ToolListPRs,
		ToolGetCodeowners, ToolCheckBranchProtection, ToolAuditCompliance,
	}

	for _, name := range expected {
		if !toolSet[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestAllResources(t *testing.T) {
	resources := AllResources()
	if len(resources) != 3 {
		t.Errorf("expected 3 resources, got %d", len(resources))
	}

	expected := map[ResourceURI]bool{
		ResourceCurrentRepo:   false,
		ResourceChangeHistory: false,
		ResourcePortfolio:     false,
	}

	for _, r := range resources {
		if _, ok := expected[r]; !ok {
			t.Errorf("unexpected resource: %s", r)
		}
		expected[r] = true
	}

	for r, found := range expected {
		if !found {
			t.Errorf("missing resource: %s", r)
		}
	}
}

func TestFingerprint(t *testing.T) {
	fp1 := Fingerprint("rule1", "file.go", 10)
	fp2 := Fingerprint("rule1", "file.go", 10)
	fp3 := Fingerprint("rule1", "file.go", 20)
	fp4 := Fingerprint("rule2", "file.go", 10)
	fp5 := Fingerprint("rule1", "other.go", 10)

	if fp1 != fp2 {
		t.Error("same inputs should produce same fingerprint")
	}
	if fp1 == fp3 {
		t.Error("different line should produce different fingerprint")
	}
	if fp1 == fp4 {
		t.Error("different rule should produce different fingerprint")
	}
	if fp1 == fp5 {
		t.Error("different file should produce different fingerprint")
	}

	// SHA256 hex is 64 chars
	if len(fp1) != 64 {
		t.Errorf("expected 64 char fingerprint, got %d", len(fp1))
	}
}

func TestStalenessEnvelope_IsStale(t *testing.T) {
	tests := []struct {
		name      string
		envelope  StalenessEnvelope
		threshold float64
		expected  bool
	}{
		{"fresh", StalenessEnvelope{DataAgeHours: 1.0}, 24.0, false},
		{"stale", StalenessEnvelope{DataAgeHours: 25.0}, 24.0, true},
		{"at threshold", StalenessEnvelope{DataAgeHours: 24.0}, 24.0, false},
		{"zero threshold", StalenessEnvelope{DataAgeHours: 0.1}, 0.0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.envelope.IsStale(tt.threshold); got != tt.expected {
				t.Errorf("IsStale(%f) = %v, want %v", tt.threshold, got, tt.expected)
			}
		})
	}
}

func TestExtractStaleness_WithEnvelope(t *testing.T) {
	content := []textContent{
		{
			Type: "text",
			Text: `{"staleness": {"data_age_hours": 1.5, "commits_behind_main": 3}, "data": {"key": "value"}}`,
		},
	}

	envelope, data, err := extractStaleness(content)
	if err != nil {
		t.Fatal(err)
	}

	if envelope.DataAgeHours != 1.5 {
		t.Errorf("expected data_age_hours 1.5, got %f", envelope.DataAgeHours)
	}
	if envelope.CommitsBehindMain != 3 {
		t.Errorf("expected commits_behind_main 3, got %d", envelope.CommitsBehindMain)
	}
	if string(data) != `{"key": "value"}` {
		t.Errorf("unexpected data: %s", string(data))
	}
}

func TestExtractStaleness_NoEnvelope(t *testing.T) {
	content := []textContent{
		{
			Type: "text",
			Text: `{"key": "value"}`,
		},
	}

	envelope, data, err := extractStaleness(content)
	if err != nil {
		t.Fatal(err)
	}

	if envelope.DataAgeHours != 0 {
		t.Errorf("expected default data_age_hours 0, got %f", envelope.DataAgeHours)
	}
	// JSON parses as a wrapper with no "data" field, so Data is nil
	if data != nil {
		t.Errorf("expected nil data for non-envelope JSON, got: %s", string(data))
	}
}

func TestExtractStaleness_EmptyContent(t *testing.T) {
	_, _, err := extractStaleness([]textContent{})
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestStalenessModeName(t *testing.T) {
	tests := []struct {
		mode     StalenessMode
		expected string
	}{
		{StalenessStrict, "strict"},
		{StalenessWarn, "warn"},
		{StalenessIgnore, "ignore"},
		{StalenessMode(99), "unknown"},
	}

	for _, tt := range tests {
		if got := stalenessModeName(tt.mode); got != tt.expected {
			t.Errorf("stalenessModeName(%d) = %q, want %q", tt.mode, got, tt.expected)
		}
	}
}

func TestDuration_MarshalJSON(t *testing.T) {
	d := Duration{5 * time.Second}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}

	// Should be 5000 milliseconds
	if string(data) != "5000" {
		t.Errorf("expected 5000, got %s", string(data))
	}
}

func TestDuration_UnmarshalJSON(t *testing.T) {
	var d Duration
	err := json.Unmarshal([]byte("3000"), &d)
	if err != nil {
		t.Fatal(err)
	}

	if d.Duration != 3*time.Second {
		t.Errorf("expected 3s, got %v", d.Duration)
	}
}

func TestDuration_RoundTrip(t *testing.T) {
	original := Duration{1500 * time.Millisecond}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Duration
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Duration != original.Duration {
		t.Errorf("round trip failed: %v != %v", decoded.Duration, original.Duration)
	}
}

func TestDefaultPollOptions(t *testing.T) {
	opts := DefaultPollOptions()

	if opts.Interval != 5*time.Second {
		t.Errorf("expected 5s interval, got %v", opts.Interval)
	}
	if opts.MaxWait != 5*time.Minute {
		t.Errorf("expected 5m max wait, got %v", opts.MaxWait)
	}
}

func TestValidateStaleness_Fresh(t *testing.T) {
	client, err := NewClient(Config{
		ServerCommand:           "echo",
		StalenessMode:           StalenessStrict,
		StalenessThresholdHours: 24.0,
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	envelope := &StalenessEnvelope{DataAgeHours: 1.0}
	if err := client.validateStaleness(envelope); err != nil {
		t.Errorf("fresh data should pass strict validation: %v", err)
	}
}

func TestValidateStaleness_StrictRejects(t *testing.T) {
	client, err := NewClient(Config{
		ServerCommand:           "echo",
		StalenessMode:           StalenessStrict,
		StalenessThresholdHours: 24.0,
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	envelope := &StalenessEnvelope{DataAgeHours: 48.0, CommitsBehindMain: 10}
	if err := client.validateStaleness(envelope); err == nil {
		t.Error("stale data should be rejected in strict mode")
	}
}

func TestValidateStaleness_WarnAccepts(t *testing.T) {
	client, err := NewClient(Config{
		ServerCommand:           "echo",
		StalenessMode:           StalenessWarn,
		StalenessThresholdHours: 24.0,
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	envelope := &StalenessEnvelope{DataAgeHours: 48.0}
	if err := client.validateStaleness(envelope); err != nil {
		t.Errorf("stale data should be accepted in warn mode: %v", err)
	}
}

func TestValidateStaleness_IgnoreAccepts(t *testing.T) {
	client, err := NewClient(Config{
		ServerCommand:           "echo",
		StalenessMode:           StalenessIgnore,
		StalenessThresholdHours: 24.0,
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	envelope := &StalenessEnvelope{DataAgeHours: 48.0}
	if err := client.validateStaleness(envelope); err != nil {
		t.Errorf("stale data should be accepted in ignore mode: %v", err)
	}
}
