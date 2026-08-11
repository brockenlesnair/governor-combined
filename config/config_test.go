package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigReturnsNonNilWithAllFeaturesEnabled(t *testing.T) {
	cfg := DefaultConfig()
	if cfg == nil {
		t.Fatal("DefaultConfig returned nil")
	}

	f := cfg.Features
	if f.Mode != "light" {
		t.Errorf("features.mode = %q, want light", f.Mode)
	}
	if got := f.RuntimeMode(); got != RuntimeModeLight {
		t.Errorf("RuntimeMode() = %q, want %q", got, RuntimeModeLight)
	}

	tests := []struct {
		name    string
		enabled bool
	}{
		{"search", f.Search.Enabled},
		{"untested", f.Untested.Enabled},
		{"deadcode", f.Deadcode.Enabled},
		{"webhook", f.Webhook.Enabled},
		{"httpproxy", f.HTTPProxy.Enabled},
		{"memory", f.Memory.Enabled},
		{"safety", f.Safety.Enabled},
		{"callgraph", f.Callgraph.Enabled},
		{"persist", f.Persist.Enabled},
		{"gateway (tool_dispatch_use_registry)", f.Gateway.ToolDispatchUseRegistry},
	}

	for _, tt := range tests {
		if !tt.enabled {
			t.Errorf("feature %q should be enabled by default", tt.name)
		}
	}

	// Spot-check a few defaults
	if cfg.Features.Search.MaxResults != 50 {
		t.Errorf("search.max_results = %d, want 50", cfg.Features.Search.MaxResults)
	}
	if cfg.Features.Search.FuzzyThreshold != 0.3 {
		t.Errorf("search.fuzzy_threshold = %f, want 0.3", cfg.Features.Search.FuzzyThreshold)
	}
	if cfg.Features.Webhook.MaxRetries != 3 {
		t.Errorf("webhook.max_retries = %d, want 3", cfg.Features.Webhook.MaxRetries)
	}
	if cfg.Features.Gateway.Listen != ":8080" {
		t.Errorf("gateway.listen = %q, want %q", cfg.Features.Gateway.Listen, ":8080")
	}
}

func TestLoadConfigFromValidYAML(t *testing.T) {
	yaml := `
features:
  mode: full
  search:
    enabled: false
    max_results: 10
    fuzzy_threshold: 0.5
  untested:
    enabled: true
    include_exported: false
    min_priority: 0.1
  deadcode:
    enabled: false
  webhook:
    enabled: true
    timeout: 10s
    max_retries: 5
  httpproxy:
    enabled: false
  memory:
    enabled: true
    storage_path: "/tmp/test-memory.db"
  safety:
    enabled: false
  callgraph:
    enabled: true
  persist:
    enabled: true
    db_path: "/tmp/test.db"
  gateway:
    tool_dispatch_use_registry: false
    listen: ":9090"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "test-config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	// Verify overridden values
	if cfg.Features.Mode != "full" {
		t.Errorf("features.mode = %q, want full", cfg.Features.Mode)
	}
	if got := cfg.Features.RuntimeMode(); got != RuntimeModeFull {
		t.Errorf("RuntimeMode() = %q, want %q", got, RuntimeModeFull)
	}
	if prof := cfg.Features.RuntimeProfile(); !prof.EnableGraphWatcher || !prof.EnableDocGovWatcher || !prof.EnableRebuildLoop {
		t.Errorf("full profile should enable all background maintenance, got %#v", prof)
	}
	if cfg.Features.Search.Enabled {
		t.Error("search should be disabled")
	}
	if cfg.Features.Search.MaxResults != 10 {
		t.Errorf("search.max_results = %d, want 10", cfg.Features.Search.MaxResults)
	}
	if cfg.Features.Webhook.MaxRetries != 5 {
		t.Errorf("webhook.max_retries = %d, want 5", cfg.Features.Webhook.MaxRetries)
	}
	if cfg.Features.Gateway.Listen != ":9090" {
		t.Errorf("gateway.listen = %q, want %q", cfg.Features.Gateway.Listen, ":9090")
	}
	if cfg.Features.Memory.StoragePath != "/tmp/test-memory.db" {
		t.Errorf("memory.storage_path = %q, want %q", cfg.Features.Memory.StoragePath, "/tmp/test-memory.db")
	}
	// Verify non-overridden values fall back to defaults
	if cfg.Features.Deadcode.MinConfidence != 0.5 {
		t.Errorf("deadcode.min_confidence = %f, want 0.5 (default)", cfg.Features.Deadcode.MinConfidence)
	}
}

func TestLoadConfigNonexistentFileReturnsError(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/to/config.yaml")
	if err == nil {
		t.Fatal("LoadConfig should return error for nonexistent file")
	}
}

func TestLoadConfigPartialYAMLUsesDefaults(t *testing.T) {
	// Only override one feature — all others should get defaults
	yaml := `
features:
  search:
    enabled: false
`
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	// Overridden
	if cfg.Features.Search.Enabled {
		t.Error("search.enabled should be false")
	}
	if cfg.Features.Mode != "light" {
		t.Errorf("features.mode = %q, want light (default)", cfg.Features.Mode)
	}
	if got := cfg.Features.RuntimeProfile(); got.Mode != RuntimeModeLight {
		t.Errorf("RuntimeProfile().Mode = %q, want %q", got.Mode, RuntimeModeLight)
	}
	// Not overridden — should use defaults
	if !cfg.Features.Untested.Enabled {
		t.Error("untested.enabled should default to true")
	}
	if !cfg.Features.Deadcode.Enabled {
		t.Error("deadcode.enabled should default to true")
	}
	if !cfg.Features.Webhook.Enabled {
		t.Error("webhook.enabled should default to true")
	}
	if !cfg.Features.HTTPProxy.Enabled {
		t.Error("httpproxy.enabled should default to true")
	}
	if !cfg.Features.Memory.Enabled {
		t.Error("memory.enabled should default to true")
	}
	if !cfg.Features.Safety.Enabled {
		t.Error("safety.enabled should default to true")
	}
	if !cfg.Features.Callgraph.Enabled {
		t.Error("callgraph.enabled should default to true")
	}
	if !cfg.Features.Persist.Enabled {
		t.Error("persist.enabled should default to true")
	}
	if cfg.Features.Search.FuzzyThreshold != 0.3 {
		t.Errorf("search.fuzzy_threshold = %f, want 0.3 (default)", cfg.Features.Search.FuzzyThreshold)
	}
}

func TestRuntimeProfileBalanced(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Features.Mode = "balanced"

	prof := cfg.Features.RuntimeProfile()
	if prof.Mode != RuntimeModeBalanced {
		t.Fatalf("RuntimeProfile().Mode = %q, want %q", prof.Mode, RuntimeModeBalanced)
	}
	if !prof.BuildGraphOnStartup {
		t.Error("balanced mode should warm the graph on startup")
	}
	if prof.EnableGraphWatcher || prof.EnableDocGovWatcher || prof.EnableRebuildLoop {
		t.Errorf("balanced mode should avoid continuous background work, got %#v", prof)
	}
}

func TestRuntimeModeInvalidFallsBackToLight(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Features.Mode = "unknown"
	if got := cfg.Features.RuntimeMode(); got != RuntimeModeLight {
		t.Errorf("RuntimeMode() = %q, want %q", got, RuntimeModeLight)
	}
}
