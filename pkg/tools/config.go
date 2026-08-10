// Package tools implements the MCP (Model Context Protocol) tools server
// for Governor. It exposes governance analysis functions as discoverable
// tools that AI agents can invoke via JSON-RPC.
package tools

import (
	"time"
)

// ToolsConfig holds configuration for the MCP tools server.
type ToolsConfig struct {
	Port            int                    `json:"port" yaml:"port"`
	LogLevel        string                 `json:"log_level" yaml:"log_level"`
	ProjectRoot     string                 `json:"project_root" yaml:"project_root"`
	FeatureFlags    map[string]bool        `json:"feature_flags" yaml:"feature_flags"`
	GraphCachePath  string                 `json:"graph_cache_path" yaml:"graph_cache_path"`
	RebuildInterval time.Duration          `json:"rebuild_interval" yaml:"rebuild_interval"`
	StdIOMode       bool                   `json:"stdio_mode" yaml:"stdio_mode"`
	Metrics         MetricsConfig          `json:"metrics" yaml:"metrics"`
}

// MetricsConfig configures Prometheus metrics.
type MetricsConfig struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Path    string `json:"path" yaml:"path"`
}

// DefaultToolsConfig returns a ToolsConfig with sensible defaults.
func DefaultToolsConfig() *ToolsConfig {
	return &ToolsConfig{
		Port:            8080,
		LogLevel:        "info",
		ProjectRoot:     ".",
		FeatureFlags:    make(map[string]bool),
		GraphCachePath:  ".governor/graph.db",
		RebuildInterval: 5 * time.Minute,
		StdIOMode:       false,
		Metrics: MetricsConfig{
			Enabled: true,
			Path:    "/metrics",
		},
	}
}