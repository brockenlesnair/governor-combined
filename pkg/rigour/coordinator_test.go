package rigour

import (
	"context"
	"log/slog"
	"testing"
)

func testCoordinator() *Coordinator {
	return NewCoordinator(slog.Default(), DefaultCoordinatorConfig())
}

func TestNewCoordinator(t *testing.T) {
	c := testCoordinator()
	if c == nil {
		t.Fatal("NewCoordinator returned nil")
	}
	if !c.overlapWarnEnabled {
		t.Error("expected overlapWarnEnabled to be true by default")
	}
	if c.driftAlpha != 0.3 {
		t.Errorf("expected driftAlpha 0.3, got %f", c.driftAlpha)
	}
}

func TestDefaultCoordinatorConfig(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	if !cfg.OverlapWarnEnabled {
		t.Error("expected OverlapWarnEnabled true")
	}
	if cfg.DriftAlpha != 0.3 {
		t.Errorf("expected DriftAlpha 0.3, got %f", cfg.DriftAlpha)
	}
	if cfg.DriftThreshold != 0.15 {
		t.Errorf("expected DriftThreshold 0.15, got %f", cfg.DriftThreshold)
	}
}

func TestCoordinator_Register(t *testing.T) {
	c := testCoordinator()
	ctx := context.Background()

	err := c.Register(ctx, "agent-1", []string{"src/*.go"})
	if err != nil {
		t.Fatal(err)
	}

	agent, ok := c.GetAgent("agent-1")
	if !ok {
		t.Fatal("agent not found after registration")
	}
	if agent.ID != "agent-1" {
		t.Errorf("expected agent ID agent-1, got %s", agent.ID)
	}
	if agent.Status != AgentActive {
		t.Errorf("expected status active, got %s", agent.Status)
	}
	if len(agent.Scope) != 1 || agent.Scope[0] != "src/*.go" {
		t.Errorf("unexpected scope: %v", agent.Scope)
	}
}

func TestCoordinator_Register_Duplicate(t *testing.T) {
	c := testCoordinator()
	ctx := context.Background()

	_ = c.Register(ctx, "agent-1", []string{"*.go"})
	err := c.Register(ctx, "agent-1", []string{"*.go"})
	if err == nil {
		t.Error("expected error when registering duplicate agent")
	}
}

func TestCoordinator_Checkpoint(t *testing.T) {
	c := testCoordinator()
	ctx := context.Background()

	_ = c.Register(ctx, "agent-1", []string{"*.go"})

	cp := &Checkpoint{
		Progress: 0.5,
		Files:    []string{"main.go", "utils.go"},
		Score:    0.85,
	}

	err := c.Checkpoint(ctx, "agent-1", cp)
	if err != nil {
		t.Fatal(err)
	}

	agent, ok := c.GetAgent("agent-1")
	if !ok {
		t.Fatal("agent not found")
	}

	if agent.Status != AgentCheckpointed {
		t.Errorf("expected status checkpointed, got %s", agent.Status)
	}
	if agent.Checkpoint == nil {
		t.Fatal("checkpoint should not be nil")
	}
	if agent.Checkpoint.Progress != 0.5 {
		t.Errorf("expected progress 0.5, got %f", agent.Checkpoint.Progress)
	}
	if agent.Checkpoint.Score != 0.85 {
		t.Errorf("expected score 0.85, got %f", agent.Checkpoint.Score)
	}
	if len(agent.Checkpoint.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(agent.Checkpoint.Files))
	}
}

func TestCoordinator_Checkpoint_UpdatesEWMA(t *testing.T) {
	c := testCoordinator()
	ctx := context.Background()

	_ = c.Register(ctx, "agent-1", []string{"*.go"})

	// First checkpoint - sets initial EWMA
	_ = c.Checkpoint(ctx, "agent-1", &Checkpoint{Progress: 0.25, Score: 0.9})

	ewma, ok := c.GetEWMA("agent-1")
	if !ok {
		t.Fatal("EWMA not found")
	}
	// First sample initializes EWMA to the score value
	if ewma != 0.9 {
		t.Errorf("expected initial EWMA 0.9, got %f", ewma)
	}

	// Second checkpoint - EWMA should update
	_ = c.Checkpoint(ctx, "agent-1", &Checkpoint{Progress: 0.5, Score: 0.7})

	ewma2, _ := c.GetEWMA("agent-1")
	// EWMA = alpha*newScore + (1-alpha)*oldEWMA = 0.3*0.7 + 0.7*0.9 = 0.21 + 0.63 = 0.84
	if ewma2 >= 0.9 || ewma2 <= 0.7 {
		t.Errorf("expected EWMA between 0.7 and 0.9, got %f", ewma2)
	}
}

func TestCoordinator_Checkpoint_NotRegistered(t *testing.T) {
	c := testCoordinator()
	ctx := context.Background()

	err := c.Checkpoint(ctx, "nonexistent", &Checkpoint{Score: 0.5})
	if err == nil {
		t.Error("expected error for unregistered agent")
	}
}

func TestCoordinator_Handoff(t *testing.T) {
	c := testCoordinator()
	ctx := context.Background()

	_ = c.Register(ctx, "agent-1", []string{"src/*.go"})
	_ = c.Register(ctx, "agent-2", []string{"test/*.go"})

	err := c.Handoff(ctx, "agent-1", "agent-2")
	if err != nil {
		t.Fatal(err)
	}

	from, _ := c.GetAgent("agent-1")
	to, _ := c.GetAgent("agent-2")

	if from.Status != AgentHandedOff {
		t.Errorf("from agent should be handed_off, got %s", from.Status)
	}
	if to.Status != AgentActive {
		t.Errorf("to agent should be active, got %s", to.Status)
	}
}

func TestCoordinator_Handoff_SourceNotRegistered(t *testing.T) {
	c := testCoordinator()
	ctx := context.Background()

	_ = c.Register(ctx, "agent-2", []string{"*.go"})
	err := c.Handoff(ctx, "nonexistent", "agent-2")
	if err == nil {
		t.Error("expected error for unregistered source")
	}
}

func TestCoordinator_Handoff_TargetNotRegistered(t *testing.T) {
	c := testCoordinator()
	ctx := context.Background()

	_ = c.Register(ctx, "agent-1", []string{"*.go"})
	err := c.Handoff(ctx, "agent-1", "nonexistent")
	if err == nil {
		t.Error("expected error for unregistered target")
	}
}

func TestCoordinator_Overlap(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.OverlapWarnEnabled = true
	c := NewCoordinator(slog.Default(), cfg)
	ctx := context.Background()

	// Register two agents with overlapping scopes
	_ = c.Register(ctx, "agent-1", []string{"src/auth"})
	_ = c.Register(ctx, "agent-2", []string{"src/auth"})

	warnings := c.GetOverlapWarnings()
	if len(warnings) == 0 {
		t.Error("expected overlap warning for overlapping scopes")
	}

	found := false
	for _, w := range warnings {
		if (w.AgentA == "agent-1" && w.AgentB == "agent-2") ||
			(w.AgentA == "agent-2" && w.AgentB == "agent-1") {
			found = true
		}
	}
	if !found {
		t.Error("expected overlap warning between agent-1 and agent-2")
	}
}

func TestCoordinator_Overlap_Disabled(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.OverlapWarnEnabled = false
	c := NewCoordinator(slog.Default(), cfg)
	ctx := context.Background()

	_ = c.Register(ctx, "agent-1", []string{"src/auth"})
	_ = c.Register(ctx, "agent-2", []string{"src/auth"})

	warnings := c.GetOverlapWarnings()
	if len(warnings) > 0 {
		t.Error("no overlap warnings expected when disabled")
	}
}

func TestCoordinator_Drift(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.DriftThreshold = 0.15
	c := NewCoordinator(slog.Default(), cfg)
	ctx := context.Background()

	_ = c.Register(ctx, "agent-1", []string{"*.go"})

	// Establish a baseline EWMA with consistent scores
	for i := 0; i < 6; i++ {
		_ = c.Checkpoint(ctx, "agent-1", &Checkpoint{Score: 0.8, Progress: float64(i) / 10.0})
	}

	// Now submit a drastically different score to trigger drift
	_ = c.Checkpoint(ctx, "agent-1", &Checkpoint{Score: 0.2, Progress: 0.6})

	alerts := c.GetDriftAlerts()
	if len(alerts) == 0 {
		t.Error("expected drift alert for gaming detection")
	}
}

func TestCoordinator_Drift_NoAlertForConsistent(t *testing.T) {
	cfg := DefaultCoordinatorConfig()
	cfg.DriftThreshold = 0.15
	c := NewCoordinator(slog.Default(), cfg)
	ctx := context.Background()

	_ = c.Register(ctx, "agent-1", []string{"*.go"})

	// Consistent scores should not trigger drift
	for i := 0; i < 10; i++ {
		_ = c.Checkpoint(ctx, "agent-1", &Checkpoint{Score: 0.8, Progress: float64(i) / 10.0})
	}

	alerts := c.GetDriftAlerts()
	if len(alerts) > 0 {
		t.Error("no drift alerts expected for consistent scores")
	}
}

func TestCoordinator_Deregister(t *testing.T) {
	c := testCoordinator()
	ctx := context.Background()

	_ = c.Register(ctx, "agent-1", []string{"*.go"})
	err := c.Deregister(ctx, "agent-1")
	if err != nil {
		t.Fatal(err)
	}

	_, ok := c.GetAgent("agent-1")
	if ok {
		t.Error("agent should not be found after deregistration")
	}
}

func TestCoordinator_Deregister_NotRegistered(t *testing.T) {
	c := testCoordinator()
	ctx := context.Background()

	err := c.Deregister(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error when deregistering nonexistent agent")
	}
}

func TestCoordinator_ListActiveAgents(t *testing.T) {
	c := testCoordinator()
	ctx := context.Background()

	_ = c.Register(ctx, "active-1", []string{"*.go"})
	_ = c.Register(ctx, "active-2", []string{"*.go"})
	_ = c.Register(ctx, "done-1", []string{"*.go"})

	// Move done-1 to done
	_ = c.Checkpoint(ctx, "done-1", &Checkpoint{Score: 1.0})

	active := c.ListActiveAgents()
	if len(active) < 2 {
		t.Errorf("expected at least 2 active agents, got %d", len(active))
	}
}

func TestCoordinator_GetEWMA_NotFound(t *testing.T) {
	c := testCoordinator()

	_, ok := c.GetEWMA("nonexistent")
	if ok {
		t.Error("expected ok=false for nonexistent agent EWMA")
	}
}

func TestPathsOverlap(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"src/auth", "src/auth", true},
		{"src/auth", "src/user", false},
		{"src/", "src/auth", true},
		{"src", "src", true},
		{"", "", true},
	}

	for _, tt := range tests {
		got := pathsOverlap(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("pathsOverlap(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestAgentStatus_String(t *testing.T) {
	tests := []struct {
		status AgentStatus
		want   string
	}{
		{AgentActive, "active"},
		{AgentCheckpointed, "checkpointed"},
		{AgentHandedOff, "handed_off"},
		{AgentDeregistered, "deregistered"},
		{AgentStatus(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.status.String()
		if got != tt.want {
			t.Errorf("AgentStatus(%d).String() = %q, want %q", int(tt.status), got, tt.want)
		}
	}
}
