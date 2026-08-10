package rigour

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// State represents the rigour supervisor lifecycle state.
type State int

const (
	StateIdle    State = iota // Waiting for work
	StateWorking              // Agent processing task
	StateFixing               // Applying fix packet
	StateHandoff              // Transferring to another agent
	StateDone                 // Task completed
)

// String returns the human-readable name for a state.
func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateWorking:
		return "working"
	case StateFixing:
		return "fixing"
	case StateHandoff:
		return "handoff"
	case StateDone:
		return "done"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// ParseState converts a string name to a State.
func ParseState(s string) (State, error) {
	switch s {
	case "idle":
		return StateIdle, nil
	case "working":
		return StateWorking, nil
	case "fixing":
		return StateFixing, nil
	case "handoff":
		return StateHandoff, nil
	case "done":
		return StateDone, nil
	default:
		return StateIdle, fmt.Errorf("unknown state: %s", s)
	}
}

// validTransitions defines which state transitions are allowed.
// Source -> set of valid destinations.
var validTransitions = map[State]map[State]bool{
	StateIdle:    {StateWorking: true},
	StateWorking: {StateFixing: true, StateHandoff: true, StateDone: true, StateIdle: true},
	StateFixing:  {StateWorking: true, StateDone: true, StateHandoff: true},
	StateHandoff: {StateIdle: true, StateWorking: true},
	StateDone:    {StateIdle: true, StateWorking: true},
}

// TransitionHook is called after a successful state transition.
type TransitionHook func(ctx context.Context, from, to State, meta map[string]any)

// AgentRegistration tracks an agent registered with the supervisor.
type AgentRegistration struct {
	ID        string
	Scope     []string // task scope patterns
	RegisteredAt time.Time
	LastSeen  time.Time
	Metadata  map[string]any
}

// StateMachine manages the rigour supervisor lifecycle states.
type StateMachine struct {
	mu          sync.RWMutex
	current     State
	agent       *AgentRegistration
	hooks       []TransitionHook
	logger      *slog.Logger
	transitions []TransitionRecord
	maxHistory  int
}

// TransitionRecord captures a completed state transition for observability.
type TransitionRecord struct {
	From      State
	To        State
	Timestamp time.Time
	Meta      map[string]any
}

// NewStateMachine creates a new StateMachine in idle state.
func NewStateMachine(logger *slog.Logger) *StateMachine {
	return &StateMachine{
		current:     StateIdle,
		logger:      logger.With("component", "state_machine"),
		transitions: make([]TransitionRecord, 0, 64),
		maxHistory:  256,
	}
}

// Current returns the current state (thread-safe).
func (sm *StateMachine) Current() State {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.current
}

// AddHook registers a transition hook for observability.
func (sm *StateMachine) AddHook(hook TransitionHook) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.hooks = append(sm.hooks, hook)
}

// Transition attempts a state transition. Returns error if invalid.
// Hooks are called after the state is updated; hook failures are logged but
// do not roll back the transition.
func (sm *StateMachine) Transition(ctx context.Context, to State, meta map[string]any) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	from := sm.current
	valid := validTransitions[from]
	if valid == nil || !valid[to] {
		return fmt.Errorf("invalid transition: %s -> %s", from, to)
	}

	sm.current = to

	// Record transition history
	if len(sm.transitions) >= sm.maxHistory {
		sm.transitions = sm.transitions[1:]
	}
	sm.transitions = append(sm.transitions, TransitionRecord{
		From:      from,
		To:        to,
		Timestamp: time.Now(),
		Meta:      meta,
	})

	sm.logger.Info("state transition",
		"from", from.String(),
		"to", to.String(),
		slog.Any("meta", meta),
	)

	// Fire hooks outside lock scope would be ideal, but we keep it simple
	// since hooks should be fast and non-blocking.
	for _, hook := range sm.hooks {
		hook(ctx, from, to, meta)
	}

	return nil
}

// TransitionUnsafe forces a transition without validation. Use for recovery paths only.
func (sm *StateMachine) TransitionUnsafe(ctx context.Context, to State, meta map[string]any) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	from := sm.current
	sm.current = to

	sm.logger.Warn("unsafe state transition",
		"from", from.String(),
		"to", to.String(),
		slog.Any("meta", meta),
	)

	if len(sm.transitions) >= sm.maxHistory {
		sm.transitions = sm.transitions[1:]
	}
	sm.transitions = append(sm.transitions, TransitionRecord{
		From:      from,
		To:        to,
		Timestamp: time.Now(),
		Meta:      meta,
	})

	for _, hook := range sm.hooks {
		hook(ctx, from, to, meta)
	}
}

// CanTransition checks if a transition is valid without performing it.
func (sm *StateMachine) CanTransition(to State) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	valid := validTransitions[sm.current]
	return valid != nil && valid[to]
}

// RegisterAgent associates an agent with the supervisor.
func (sm *StateMachine) RegisterAgent(agent *AgentRegistration) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.agent != nil {
		return fmt.Errorf("agent already registered: %s", sm.agent.ID)
	}

	agent.RegisteredAt = time.Now()
	agent.LastSeen = time.Now()
	sm.agent = agent

	sm.logger.Info("agent registered",
		"id", agent.ID,
		"scope", agent.Scope,
	)
	return nil
}

// DeregisterAgent removes the current agent.
func (sm *StateMachine) DeregisterAgent() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.agent != nil {
		sm.logger.Info("agent deregistered", "id", sm.agent.ID)
		sm.agent = nil
	}
}

// GetAgent returns the current agent registration.
func (sm *StateMachine) GetAgent() *AgentRegistration {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.agent
}

// TouchAgent updates the agent's last-seen timestamp.
func (sm *StateMachine) TouchAgent() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.agent != nil {
		sm.agent.LastSeen = time.Now()
	}
}

// GetTransitions returns a copy of the transition history.
func (sm *StateMachine) GetTransitions() []TransitionRecord {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	out := make([]TransitionRecord, len(sm.transitions))
	copy(out, sm.transitions)
	return out
}
