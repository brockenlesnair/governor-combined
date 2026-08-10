// Package protocol defines the governor internal protocol devices — the access
// control and guidance layer for multi-agent systems. It establishes tiered
// access (Steer vs Splinter) so oversight agents get full tool access while
// all agents receive baseline best-practice guidance.
package protocol

import "time"

// AgentID uniquely identifies an agent in the system.
type AgentID string

// Tier determines what devices (tools) an agent can access.
type Tier string

const (
	// TierSteer gives full access to all governor-combined devices.
	// Used by project-wide oversight agents that monitor codebase health,
	// enforce policies, and coordinate across strategies.
	TierSteer Tier = "steer"

	// TierSplinter gives access to a curated subset of devices plus
	// baseline guidance. Used by strategy agents that develop code
	// for specific trading strategies (BTC futures, weather bets, etc.).
	TierSplinter Tier = "splinter"
)

// Device represents a governor-combined tool that agents can access.
type Device struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tier        Tier     `json:"tier"`        // minimum tier required
	Category    string   `json:"category"`    // grouping: search, safety, analysis, governance
	Tags        []string `json:"tags,omitempty"`
}

// Agent represents a registered agent with its tier and capabilities.
type Agent struct {
	ID        AgentID   `json:"id"`
	Name      string    `json:"name"`
	Tier      Tier      `json:"tier"`
	Strategy  string    `json:"strategy,omitempty"`  // e.g. "btc_futures_momentum"
	RegisteredAt time.Time `json:"registered_at"`
}

// Protocol holds the device registry and tier definitions.
type Protocol struct {
	devices map[string]Device
	tiers   map[Tier][]string // tier → device names
}
