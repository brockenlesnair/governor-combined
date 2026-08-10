package rigour

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"
)

// AgentStatus represents the lifecycle state of a registered agent.
type AgentStatus int

const (
	AgentActive    AgentStatus = iota
	AgentCheckpointed
	AgentHandedOff
	AgentDeregistered
)

// String returns the agent status label.
func (s AgentStatus) String() string {
	switch s {
	case AgentActive:
		return "active"
	case AgentCheckpointed:
		return "checkpointed"
	case AgentHandedOff:
		return "handed_off"
	case AgentDeregistered:
		return "deregistered"
	default:
		return "unknown"
	}
}

// RegisteredAgent tracks a working agent in the coordination layer.
type RegisteredAgent struct {
	ID           string
	Scope        []string // file path patterns this agent owns
	Status       AgentStatus
	Checkpoint   *Checkpoint
	RegisteredAt time.Time
	LastActivity time.Time
}

// Checkpoint captures agent progress for resumability.
type Checkpoint struct {
	Progress   float64            // 0.0 - 1.0
	Files      []string           // files touched
	Score      float64            // quality score at checkpoint
	Timestamp  time.Time
	Metadata   map[string]any
}

// OverlapWarning is emitted when two agents touch overlapping file scopes.
type OverlapWarning struct {
	AgentA   string
	AgentB   string
	Files    []string // overlapping files
	Timestamp time.Time
}

// DriftAlert is emitted when EWMA detects score gaming.
type DriftAlert struct {
	AgentID   string
	Score     float64
	EWMA      float64
	Drift     float64 // score - ewma
	Timestamp time.Time
}

// Coordinator manages multi-agent registration, checkpointing, handoff,
// scope overlap detection, and quality score drift detection.
type Coordinator struct {
	mu              sync.RWMutex
	agents          map[string]*RegisteredAgent
	overlapWarnings []OverlapWarning
	driftAlerts     []DriftAlert
	ewmaState       map[string]*ewmaTracker // per-agent EWMA tracking
	logger          *slog.Logger

	// Configuration
	overlapWarnEnabled bool
	driftAlpha         float64 // EWMA smoothing factor (0-1)
	driftThreshold     float64 // drift beyond this triggers alert
}

// ewmaTracker holds per-agent EWMA state.
type ewmaTracker struct {
	value    float64
	init     bool
	samples  int
}

// CoordinatorConfig holds configuration for the Coordinator.
type CoordinatorConfig struct {
	OverlapWarnEnabled bool
	DriftAlpha         float64 // EWMA smoothing factor
	DriftThreshold     float64
}

// DefaultCoordinatorConfig returns sensible defaults.
func DefaultCoordinatorConfig() CoordinatorConfig {
	return CoordinatorConfig{
		OverlapWarnEnabled: true,
		DriftAlpha:         0.3,
		DriftThreshold:     0.15,
	}
}

// NewCoordinator creates a new multi-agent coordinator.
func NewCoordinator(logger *slog.Logger, cfg CoordinatorConfig) *Coordinator {
	return &Coordinator{
		agents:             make(map[string]*RegisteredAgent),
		ewmaState:          make(map[string]*ewmaTracker),
		overlapWarnings:    make([]OverlapWarning, 0, 64),
		driftAlerts:        make([]DriftAlert, 0, 64),
		logger:             logger.With("component", "coordinator"),
		overlapWarnEnabled: cfg.OverlapWarnEnabled,
		driftAlpha:         cfg.DriftAlpha,
		driftThreshold:     cfg.DriftThreshold,
	}
}

// Register adds a new agent to the coordinator.
func (c *Coordinator) Register(ctx context.Context, id string, scope []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.agents[id]; exists {
		return fmt.Errorf("agent already registered: %s", id)
	}

	agent := &RegisteredAgent{
		ID:           id,
		Scope:        scope,
		Status:       AgentActive,
		RegisteredAt: time.Now(),
		LastActivity: time.Now(),
	}

	c.agents[id] = agent
	c.ewmaState[id] = &ewmaTracker{}

	c.logger.Info("agent registered",
		"id", id,
		"scope", scope,
	)

	// Check for scope overlaps with existing agents
	if c.overlapWarnEnabled {
		c.checkOverlaps(agent)
	}

	return nil
}

// Checkpoint records progress for an agent.
func (c *Coordinator) Checkpoint(ctx context.Context, agentID string, cp *Checkpoint) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	agent, ok := c.agents[agentID]
	if !ok {
		return fmt.Errorf("agent not registered: %s", agentID)
	}

	cp.Timestamp = time.Now()
	agent.Checkpoint = cp
	agent.Status = AgentCheckpointed
	agent.LastActivity = time.Now()

	// Update EWMA for drift detection
	c.updateEWMA(agentID, cp.Score)

	c.logger.Info("agent checkpointed",
		"id", agentID,
		"progress", cp.Progress,
		"score", cp.Score,
		"files", len(cp.Files),
	)

	return nil
}

// Handoff transfers responsibility from one agent to another.
func (c *Coordinator) Handoff(ctx context.Context, fromID, toID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	from, ok := c.agents[fromID]
	if !ok {
		return fmt.Errorf("source agent not registered: %s", fromID)
	}

	to, ok := c.agents[toID]
	if !ok {
		return fmt.Errorf("target agent not registered: %s", toID)
	}

	from.Status = AgentHandedOff
	to.Status = AgentActive
	to.LastActivity = time.Now()

	c.logger.Info("agent handoff",
		"from", fromID,
		"to", toID,
	)

	return nil
}

// Deregister removes an agent from the coordinator.
func (c *Coordinator) Deregister(ctx context.Context, agentID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	agent, ok := c.agents[agentID]
	if !ok {
		return fmt.Errorf("agent not registered: %s", agentID)
	}

	agent.Status = AgentDeregistered
	delete(c.agents, agentID)
	delete(c.ewmaState, agentID)

	c.logger.Info("agent deregistered", "id", agentID)
	return nil
}

// GetAgent returns the current state of a registered agent.
func (c *Coordinator) GetAgent(agentID string) (*RegisteredAgent, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	agent, ok := c.agents[agentID]
	return agent, ok
}

// ListActiveAgents returns all agents in active or checkpointed status.
func (c *Coordinator) ListActiveAgents() []*RegisteredAgent {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var active []*RegisteredAgent
	for _, agent := range c.agents {
		if agent.Status == AgentActive || agent.Status == AgentCheckpointed {
			active = append(active, agent)
		}
	}
	return active
}

// checkOverlaps compares a new agent's scope against existing agents.
func (c *Coordinator) checkOverlaps(newAgent *RegisteredAgent) {
	for _, existing := range c.agents {
		if existing.ID == newAgent.ID {
			continue
		}
		if existing.Status == AgentDeregistered {
			continue
		}

		overlapFiles := findScopeOverlap(existing.Scope, newAgent.Scope)
		if len(overlapFiles) > 0 {
			warning := OverlapWarning{
				AgentA:     existing.ID,
				AgentB:     newAgent.ID,
				Files:      overlapFiles,
				Timestamp:  time.Now(),
			}
			c.overlapWarnings = append(c.overlapWarnings, warning)

			c.logger.Warn("scope overlap detected",
				"agent_a", existing.ID,
				"agent_b", newAgent.ID,
				"overlapping", overlapFiles,
			)
		}
	}
}

// findScopeOverlap identifies common path prefixes between two scope sets.
func findScopeOverlap(scopeA, scopeB []string) []string {
	var overlap []string
	for _, a := range scopeA {
		for _, b := range scopeB {
			if pathsOverlap(a, b) {
				key := a + "|" + b
				overlap = append(overlap, key)
			}
		}
	}
	return overlap
}

// pathsOverlap checks if two glob-like scope patterns could match the same paths.
func pathsOverlap(a, b string) bool {
	// Normalize: strip wildcards for prefix comparison
	cleanA := strings.TrimRight(a, "*")
	cleanB := strings.TrimRight(b, "*")

	if cleanA == "" || cleanB == "" {
		return true // wildcard root overlaps with everything
	}

	return strings.HasPrefix(cleanA, cleanB) || strings.HasPrefix(cleanB, cleanA)
}

// updateEWMA updates the exponentially weighted moving average for score tracking.
func (c *Coordinator) updateEWMA(agentID string, newScore float64) {
	tracker, ok := c.ewmaState[agentID]
	if !ok {
		return
	}

	if !tracker.init {
		tracker.value = newScore
		tracker.init = true
		tracker.samples = 1
		return
	}

	tracker.samples++
	tracker.value = c.driftAlpha*newScore + (1-c.driftAlpha)*tracker.value

	// Check for drift (gaming detection)
	drift := math.Abs(newScore - tracker.value)
	if drift > c.driftThreshold && tracker.samples > 5 {
		alert := DriftAlert{
			AgentID:   agentID,
			Score:     newScore,
			EWMA:      tracker.value,
			Drift:     drift,
			Timestamp: time.Now(),
		}
		c.driftAlerts = append(c.driftAlerts, alert)

		c.logger.Warn("score drift detected",
			"agent", agentID,
			"score", newScore,
			"ewma", tracker.value,
			"drift", drift,
		)
	}
}

// GetOverlapWarnings returns all recorded overlap warnings.
func (c *Coordinator) GetOverlapWarnings() []OverlapWarning {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]OverlapWarning, len(c.overlapWarnings))
	copy(out, c.overlapWarnings)
	return out
}

// GetDriftAlerts returns all recorded drift alerts.
func (c *Coordinator) GetDriftAlerts() []DriftAlert {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]DriftAlert, len(c.driftAlerts))
	copy(out, c.driftAlerts)
	return out
}

// GetEWMA returns the current EWMA value for an agent.
func (c *Coordinator) GetEWMA(agentID string) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tracker, ok := c.ewmaState[agentID]
	if !ok {
		return 0, false
	}
	return tracker.value, true
}
