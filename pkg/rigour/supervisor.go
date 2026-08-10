package rigour

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// SupervisorConfig holds configuration for the Rigour Supervisor.
type SupervisorConfig struct {
	// DLP configuration
	DLPEnabled    bool
	EntropyThresh float64
	TestFixtures  []string // false-positive paths

	// Coordinator configuration
	Coordinator CoordinatorConfig

	// Brain configuration
	Brain BrainConfig

	// Timing
	CheckInterval time.Duration // how often to run rigour checks
	ContextTimeout time.Duration // timeout for individual operations
}

// DefaultSupervisorConfig returns sensible defaults.
func DefaultSupervisorConfig() SupervisorConfig {
	return SupervisorConfig{
		DLPEnabled:     true,
		EntropyThresh:  4.5,
		Coordinator:    DefaultCoordinatorConfig(),
		Brain:          DefaultBrainConfig(),
		CheckInterval:  30 * time.Second,
		ContextTimeout: 60 * time.Second,
	}
}

// RigourSupervisor is the main orchestrator that integrates all components
// of the rigour governance layer.
type RigourSupervisor struct {
	state       *StateMachine
	dlp         *DLPFilter
	coordinator *Coordinator
	brain       *Brain
	parser      *Parser
	fixApplier  *FixApplier
	logger      *slog.Logger
	config      SupervisorConfig
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	running     bool
}

// NewRigourSupervisor creates a fully wired RigourSupervisor.
func NewRigourSupervisor(logger *slog.Logger, cfg SupervisorConfig) *RigourSupervisor {
	ctx, cancel := context.WithCancel(context.Background())

	l := logger.With("component", "rigour")

	return &RigourSupervisor{
		state:       NewStateMachine(l),
		dlp:         NewDLPFilter(l),
		coordinator: NewCoordinator(l, cfg.Coordinator),
		brain:       NewBrain(l, cfg.Brain),
		parser:      NewParser(),
		fixApplier:  NewFixApplier(l),
		logger:      l,
		config:      cfg,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start begins the supervisor's background operations.
func (rs *RigourSupervisor) Start() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.running {
		return fmt.Errorf("supervisor already running")
	}

	rs.running = true
	rs.logger.Info("rigour supervisor starting")

	// Configure DLP false positives from config
	for _, fixture := range rs.config.TestFixtures {
		rs.dlp.AddFalsePositive(fixture)
	}
	if rs.config.EntropyThresh > 0 {
		rs.dlp.SetEntropyThreshold(rs.config.EntropyThresh)
	}

	// Register observability hook on the state machine
	rs.state.AddHook(func(ctx context.Context, from, to State, meta map[string]any) {
		rs.logger.Info("supervisor state change",
			"from", from.String(),
			"to", to.String(),
			slog.Any("meta", meta),
		)
	})

	return nil
}

// Stop gracefully shuts down the supervisor.
func (rs *RigourSupervisor) Stop() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if !rs.running {
		return nil
	}

	rs.logger.Info("rigour supervisor stopping")
	rs.cancel()
	rs.wg.Wait()
	rs.running = false

	return nil
}

// ProcessInput runs the DLP pre-filter on input text before agent processing.
func (rs *RigourSupervisor) ProcessInput(ctx context.Context, text string, filePath string) (*DLPResult, error) {
	if !rs.config.DLPEnabled {
		return &DLPResult{}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, rs.config.ContextTimeout)
	defer cancel()

	result := rs.dlp.Check(ctx, text, filePath)
	return result, nil
}

// HandleFixPacket processes a rigour fix packet through the full pipeline:
// 1. Parse the TEXT format
// 2. Check brain for hard rules
// 3. Determine and apply fix strategy
// 4. Coordinate with agents
func (rs *RigourSupervisor) HandleFixPacket(ctx context.Context, rawText string, agentID string) (*FixResult, error) {
	ctx, cancel := context.WithTimeout(ctx, rs.config.ContextTimeout)
	defer cancel()

	// Step 1: Parse the fix packet
	fp, err := rs.parser.Parse(rawText)
	if err != nil {
		return nil, fmt.Errorf("parse fix packet: %w", err)
	}

	rs.logger.Info("fix packet parsed",
		"gate", fp.GateName,
		"severity", fp.Severity,
		"files", len(fp.Files),
		"instructions", len(fp.Instructions),
	)

	// Step 2: Transition to working state
	if err := rs.state.Transition(ctx, StateWorking, map[string]any{
		"gate": fp.GateName,
		"agent": agentID,
	}); err != nil {
		rs.logger.Warn("state transition failed", "err", err)
	}

	// Step 3: Check brain for relevant hard rules
	filePaths := make([]string, len(fp.Files))
	for i, f := range fp.Files {
		filePaths[i] = f.Path
	}
	hardRules := rs.brain.MatchRules(ctx, filePaths)
	if len(hardRules) > 0 {
		rs.logger.Info("hard rules matched",
			"count", len(hardRules),
			"gate", fp.GateName,
		)
	}

	// Step 4: Transition to fixing state
	if err := rs.state.Transition(ctx, StateFixing, map[string]any{
		"gate": fp.GateName,
		"strategy": rs.fixApplier.DetermineStrategy(fp).String(),
	}); err != nil {
		rs.logger.Warn("state transition to fixing failed", "err", err)
	}

	// Step 5: Apply the fix
	result, err := rs.fixApplier.ApplyFix(ctx, fp, agentID)
	if err != nil {
		// Transition back to working on error
		_ = rs.state.Transition(ctx, StateWorking, map[string]any{
			"error": err.Error(),
		})
		return nil, fmt.Errorf("apply fix: %w", err)
	}

	// Step 6: Record pattern learning
	if result.Success && result.Strategy == StrategyASTGre {
		_ = rs.brain.Learn(ctx, Pattern{
			ID:          fp.GateName,
			Name:        fp.GateName,
			Description: fmt.Sprintf("ast-grep pattern for %s", fp.GateName),
		})
	}

	// Step 7: Transition to done
	_ = rs.state.Transition(ctx, StateDone, map[string]any{
		"gate":    fp.GateName,
		"success": result.Success,
		"files":   len(result.FilesChanged),
	})

	// Step 8: Return to idle for next fix packet
	_ = rs.state.Transition(ctx, StateIdle, map[string]any{
		"completed_gate": fp.GateName,
		"success":        result.Success,
	})

	rs.logger.Info("fix packet handled",
		"gate", fp.GateName,
		"strategy", result.Strategy,
		"success", result.Success,
		"files_changed", len(result.FilesChanged),
	)

	return result, nil
}

// HandoffAgent transfers work from one agent to another.
func (rs *RigourSupervisor) HandoffAgent(ctx context.Context, fromAgent, toAgent string) error {
	ctx, cancel := context.WithTimeout(ctx, rs.config.ContextTimeout)
	defer cancel()

	if err := rs.state.Transition(ctx, StateHandoff, map[string]any{
		"from": fromAgent,
		"to":   toAgent,
	}); err != nil {
		return fmt.Errorf("state transition: %w", err)
	}

	if err := rs.coordinator.Handoff(ctx, fromAgent, toAgent); err != nil {
		return fmt.Errorf("coordinator handoff: %w", err)
	}

	// Return to idle after handoff
	_ = rs.state.Transition(ctx, StateIdle, nil)

	return nil
}

// RegisterAgent registers an agent with both the state machine and coordinator.
func (rs *RigourSupervisor) RegisterAgent(ctx context.Context, id string, scope []string) error {
	if err := rs.state.RegisterAgent(&AgentRegistration{
		ID:    id,
		Scope: scope,
	}); err != nil {
		return fmt.Errorf("state machine register: %w", err)
	}

	if err := rs.coordinator.Register(ctx, id, scope); err != nil {
		// Rollback state machine registration
		rs.state.DeregisterAgent()
		return fmt.Errorf("coordinator register: %w", err)
	}

	return nil
}

// DeregisterAgent removes an agent from both systems.
func (rs *RigourSupervisor) DeregisterAgent(ctx context.Context, agentID string) error {
	rs.state.DeregisterAgent()
	return rs.coordinator.Deregister(ctx, agentID)
}

// CheckpointAgent records progress through the coordinator.
func (rs *RigourSupervisor) CheckpointAgent(ctx context.Context, agentID string, cp *Checkpoint) error {
	return rs.coordinator.Checkpoint(ctx, agentID, cp)
}

// GetState returns the current supervisor state.
func (rs *RigourSupervisor) GetState() State {
	return rs.state.Current()
}

// GetStateMachine returns the underlying state machine for advanced operations.
func (rs *RigourSupervisor) GetStateMachine() *StateMachine {
	return rs.state
}

// GetBrainStats returns brain pattern statistics.
func (rs *RigourSupervisor) GetBrainStats() BrainStats {
	return rs.brain.GetStats()
}

// GetCoordinator returns the underlying coordinator for direct access.
func (rs *RigourSupervisor) GetCoordinator() *Coordinator {
	return rs.coordinator
}

// GetBrain returns the underlying brain for direct access.
func (rs *RigourSupervisor) GetBrain() *Brain {
	return rs.brain
}
