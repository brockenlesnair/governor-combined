// Package config provides the Governor V2.0 configuration types and YAML loading.
//
// Configuration is organized by feature (search, untested, deadcode, webhook, etc.)
// and supports environment variable overrides for sensitive values like API keys.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// GovernorConfig is the top-level configuration for Governor V2.0.
type GovernorConfig struct {
	Features FeaturesConfig `yaml:"features"`
}

// RuntimeMode controls the amount of background work Governor performs.
type RuntimeMode string

const (
	// RuntimeModeLight favors low resource usage and on-demand work.
	RuntimeModeLight RuntimeMode = "light"
	// RuntimeModeBalanced favors responsiveness without continuous watchers.
	RuntimeModeBalanced RuntimeMode = "balanced"
	// RuntimeModeFull enables continuous background maintenance.
	RuntimeModeFull RuntimeMode = "full"
)

// String returns the normalized string form of the runtime mode.
func (m RuntimeMode) String() string {
	return string(m)
}

// IsValid reports whether the mode is one of the supported runtime modes.
func (m RuntimeMode) IsValid() bool {
	switch m {
	case RuntimeModeLight, RuntimeModeBalanced, RuntimeModeFull:
		return true
	default:
		return false
	}
}

// RuntimeProfile describes the resource/behavior profile for a runtime mode.
type RuntimeProfile struct {
	Mode                RuntimeMode
	Description         string
	BuildGraphOnStartup bool
	EnableGraphWatcher  bool
	EnableDocGovWatcher bool
	EnableRebuildLoop   bool
}

// FeaturesConfig contains per-feature configuration.
type FeaturesConfig struct {
	Mode      string          `yaml:"mode"`
	Search    SearchConfig    `yaml:"search"`
	Untested  UntestedConfig  `yaml:"untested"`
	Deadcode  DeadcodeConfig  `yaml:"deadcode"`
	Webhook   WebhookConfig   `yaml:"webhook"`
	HTTPProxy HTTPProxyConfig `yaml:"httpproxy"`
	Memory    MemoryConfig    `yaml:"memory"`
	Safety    SafetyConfig    `yaml:"safety"`
	Callgraph CallgraphConfig `yaml:"callgraph"`
	Persist   PersistConfig   `yaml:"persist"`
	Gateway   GatewayConfig   `yaml:"gateway"`
	Metrics   MetricsConfig   `yaml:"metrics"`
	DocGov    DocGovConfig    `yaml:"docgov"`
}

// SearchConfig configures the search feature.
type SearchConfig struct {
	Enabled        bool    `yaml:"enabled"`
	MaxResults     int     `yaml:"max_results"`
	FuzzyThreshold float64 `yaml:"fuzzy_threshold"`
}

// UntestedConfig configures the untested-code detection feature.
type UntestedConfig struct {
	Enabled         bool    `yaml:"enabled"`
	IncludeExported bool    `yaml:"include_exported"`
	MinPriority     float64 `yaml:"min_priority"`
}

// DeadcodeConfig configures the dead-code detection feature.
type DeadcodeConfig struct {
	Enabled         bool    `yaml:"enabled"`
	ExcludeExported bool    `yaml:"exclude_exported"`
	MinConfidence   float64 `yaml:"min_confidence"`
}

// WebhookConfig configures the webhook client.
type WebhookConfig struct {
	Enabled    bool          `yaml:"enabled"`
	Timeout    time.Duration `yaml:"timeout"`
	MaxRetries int           `yaml:"max_retries"`
}

// HTTPProxyConfig configures the HTTP proxy.
type HTTPProxyConfig struct {
	Enabled bool          `yaml:"enabled"`
	Target  string        `yaml:"target"`
	Timeout time.Duration `yaml:"timeout"`
}

// MemoryConfig configures the hybrid memory/search store.
type MemoryConfig struct {
	Enabled     bool   `yaml:"enabled"`
	StoragePath string `yaml:"storage_path"`
}

// SafetyConfig configures the safety validator.
type SafetyConfig struct {
	Enabled   bool   `yaml:"enabled"`
	AuditPath string `yaml:"audit_path"`
}

// CallgraphConfig configures the call-graph builder.
type CallgraphConfig struct {
	Enabled bool `yaml:"enabled"`
}

// PersistConfig configures the SQLite persistence store.
type PersistConfig struct {
	Enabled bool   `yaml:"enabled"`
	DBPath  string `yaml:"db_path"`
}

// GatewayConfig configures the HTTP gateway.
type GatewayConfig struct {
	ToolDispatchUseRegistry bool   `yaml:"tool_dispatch_use_registry"`
	Listen                  string `yaml:"listen"`
}

// MetricsConfig configures the Prometheus metrics endpoint.
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
}

// DocGovConfig configures the document governance feature.
type DocGovConfig struct {
	Enabled            bool                `yaml:"enabled"`
	ProjectRoot        string              `yaml:"project_root"`
	CustomPathPatterns map[string][]string `yaml:"custom_path_patterns"`
	EnableWatcher      bool                `yaml:"enable_watcher"`
	WatcherInterval    string              `yaml:"watcher_interval"`
}

// DefaultConfig returns a GovernorConfig with all features enabled and sensible defaults.
func DefaultConfig() *GovernorConfig {
	return &GovernorConfig{
		Features: FeaturesConfig{
			Mode: "light",
			Search: SearchConfig{
				Enabled:        true,
				MaxResults:     50,
				FuzzyThreshold: 0.3,
			},
			Untested: UntestedConfig{
				Enabled:         true,
				IncludeExported: true,
				MinPriority:     0.3,
			},
			Deadcode: DeadcodeConfig{
				Enabled:         true,
				ExcludeExported: true,
				MinConfidence:   0.5,
			},
			Webhook: WebhookConfig{
				Enabled:    true,
				Timeout:    30 * time.Second,
				MaxRetries: 3,
			},
			HTTPProxy: HTTPProxyConfig{
				Enabled: true,
				Target:  "",
				Timeout: 30 * time.Second,
			},
			Memory: MemoryConfig{
				Enabled:     true,
				StoragePath: "./data/memory.db",
			},
			Safety: SafetyConfig{
				Enabled:   true,
				AuditPath: "./data/audit.log",
			},
			Callgraph: CallgraphConfig{
				Enabled: true,
			},
			Persist: PersistConfig{
				Enabled: true,
				DBPath:  "./data/governor.db",
			},
			Gateway: GatewayConfig{
				ToolDispatchUseRegistry: true,
				Listen:                  ":8080",
			},
			Metrics: MetricsConfig{
				Enabled: true,
				Path:    "/metrics",
			},
			DocGov: DocGovConfig{
				Enabled:            true,
				ProjectRoot:        ".",
				CustomPathPatterns: map[string][]string{},
				EnableWatcher:      true,
				WatcherInterval:    "30s",
			},
		},
	}
}

// RuntimeMode returns the normalized runtime mode, defaulting to light.
func (f FeaturesConfig) RuntimeMode() RuntimeMode {
	mode := RuntimeMode(strings.ToLower(strings.TrimSpace(f.Mode)))
	if !mode.IsValid() {
		return RuntimeModeLight
	}
	return mode
}

// RuntimeProfile returns the behavior profile for the selected runtime mode.
func (f FeaturesConfig) RuntimeProfile() RuntimeProfile {
	switch f.RuntimeMode() {
	case RuntimeModeBalanced:
		return RuntimeProfile{
			Mode:                RuntimeModeBalanced,
			Description:         "moderate startup work with no continuous background watchers",
			BuildGraphOnStartup: true,
			EnableGraphWatcher:  false,
			EnableDocGovWatcher: false,
			EnableRebuildLoop:   false,
		}
	case RuntimeModeFull:
		return RuntimeProfile{
			Mode:                RuntimeModeFull,
			Description:         "continuous background maintenance with live watchers and rebuilds",
			BuildGraphOnStartup: true,
			EnableGraphWatcher:  true,
			EnableDocGovWatcher: true,
			EnableRebuildLoop:   true,
		}
	default:
		return RuntimeProfile{
			Mode:                RuntimeModeLight,
			Description:         "lowest resource mode with on-demand graph work",
			BuildGraphOnStartup: false,
			EnableGraphWatcher:  false,
			EnableDocGovWatcher: false,
			EnableRebuildLoop:   false,
		}
	}
}

// FullModeEnabled reports whether the server should run in full background
// maintenance mode with watchers and periodic rebuilds enabled.
func (f FeaturesConfig) FullModeEnabled() bool {
	return f.RuntimeMode() == RuntimeModeFull
}

// LoadConfig reads a YAML config file at path and returns a GovernorConfig.
// Missing fields fall back to the default values for that feature.
func LoadConfig(path string) (*GovernorConfig, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", path, err)
	}

	return cfg, nil
}
