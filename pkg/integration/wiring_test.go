package integration

import (
	"io"
	"log/slog"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNew_Governor(t *testing.T) {
	logger := testLogger()
	cfg := DefaultConfig()

	g, err := New(logger, cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer g.Stop()

	if g == nil {
		t.Fatal("expected non-nil Governor")
	}
	if g.Logger == nil {
		t.Error("expected non-nil Logger")
	}
}

func TestNew_NilConfig(t *testing.T) {
	logger := testLogger()

	g, err := New(logger, nil)
	if err != nil {
		t.Fatalf("New(nil config) failed: %v", err)
	}
	defer g.Stop()

	if g == nil {
		t.Fatal("expected non-nil Governor with nil config")
	}
}

func TestNew_SafetyInitialized(t *testing.T) {
	logger := testLogger()
	cfg := DefaultConfig()

	g, err := New(logger, cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer g.Stop()

	if g.Safety == nil {
		t.Error("expected Safety to be initialized")
	}
}

func TestNew_RigourInitialized(t *testing.T) {
	logger := testLogger()
	cfg := DefaultConfig()

	g, err := New(logger, cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer g.Stop()

	if g.Rigour == nil {
		t.Error("expected Rigour to be initialized")
	}
}

func TestNew_SARIFInitialized(t *testing.T) {
	logger := testLogger()
	cfg := DefaultConfig()

	g, err := New(logger, cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer g.Stop()

	if g.SARIF == nil {
		t.Error("expected SARIF to be initialized")
	}
}

func TestNew_ADRInitialized(t *testing.T) {
	logger := testLogger()
	cfg := DefaultConfig()

	g, err := New(logger, cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer g.Stop()

	if g.ADRClient == nil {
		t.Error("expected ADRClient to be initialized")
	}
	if g.ADRHook == nil {
		t.Error("expected ADRHook to be initialized")
	}
	if g.ADRCmd == nil {
		t.Error("expected ADRCmd to be initialized")
	}
}

func TestNew_HangarNotInitializedWithoutConfig(t *testing.T) {
	logger := testLogger()
	cfg := DefaultConfig()

	g, err := New(logger, cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer g.Stop()

	// Hangar requires BaseURL to be set; DefaultConfig leaves it empty
	if g.Hangar != nil {
		t.Error("expected Hangar to be nil when BaseURL is empty")
	}
}

func TestNew_RepoNotInitializedWithoutConfig(t *testing.T) {
	logger := testLogger()
	cfg := DefaultConfig()

	g, err := New(logger, cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer g.Stop()

	// Repo requires ServerCommand to be set; DefaultConfig leaves it empty
	if g.Repo != nil {
		t.Error("expected Repo to be nil when ServerCommand is empty")
	}
}

func TestDefaultConfig_Values(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("expected non-nil Config from DefaultConfig()")
	}
	if cfg.Safety == nil {
		t.Error("expected non-nil Safety config")
	}
	if cfg.ADR == nil {
		t.Error("expected non-nil ADR config")
	}
	if cfg.ADR.AdrRoot != "docs/adr" {
		t.Errorf("expected ADR root 'docs/adr', got %q", cfg.ADR.AdrRoot)
	}
	if cfg.ADR.RepoRoot != "." {
		t.Errorf("expected ADR repo root '.', got %q", cfg.ADR.RepoRoot)
	}
}
