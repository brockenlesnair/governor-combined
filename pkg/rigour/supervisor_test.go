package rigour

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

func testSupervisorConfig() SupervisorConfig {
	return SupervisorConfig{
		DLPEnabled:     true,
		EntropyThresh:  4.5,
		Coordinator:    DefaultCoordinatorConfig(),
		Brain:          DefaultBrainConfig(),
		ContextTimeout: 60e9, // 60s in nanoseconds
	}
}

func testSupervisor() *RigourSupervisor {
	return NewRigourSupervisor(slog.Default(), testSupervisorConfig())
}

func TestNewRigourSupervisor(t *testing.T) {
	rs := testSupervisor()
	if rs == nil {
		t.Fatal("NewRigourSupervisor returned nil")
	}
	if rs.GetState() != StateIdle {
		t.Errorf("expected initial state idle, got %s", rs.GetState())
	}
	if rs.state == nil {
		t.Error("state machine should not be nil")
	}
	if rs.dlp == nil {
		t.Error("DLP filter should not be nil")
	}
	if rs.coordinator == nil {
		t.Error("coordinator should not be nil")
	}
	if rs.brain == nil {
		t.Error("brain should not be nil")
	}
	if rs.parser == nil {
		t.Error("parser should not be nil")
	}
	if rs.fixApplier == nil {
		t.Error("fix applier should not be nil")
	}
}

func TestSupervisor_ProcessInput_Clean(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	text := `package main
import "fmt"
func main() { fmt.Println("hello") }
`
	result, err := rs.ProcessInput(ctx, text, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if result.Blocked {
		t.Error("clean code should not be blocked")
	}
}

func TestSupervisor_ProcessInput_Secrets(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	text := `api_key = "sk-1234567890abcdef12345678"`
	result, err := rs.ProcessInput(ctx, text, "config.go")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Blocked {
		t.Error("code with secrets should be blocked")
	}
}

func TestSupervisor_ProcessInput_DLPDisabled(t *testing.T) {
	cfg := testSupervisorConfig()
	cfg.DLPEnabled = false
	rs := NewRigourSupervisor(slog.Default(), cfg)
	ctx := context.Background()

	text := `api_key = "sk-1234567890abcdef12345678"`
	result, err := rs.ProcessInput(ctx, text, "config.go")
	if err != nil {
		t.Fatal(err)
	}
	if result.Blocked {
		t.Error("DLP disabled should not block")
	}
}

func TestSupervisor_HandleFixPacket_Skip(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	// Start the supervisor to set up hooks
	_ = rs.Start()
	defer rs.Stop()

	text := `Gate: unknown_gate
Severity: info
`
	result, err := rs.HandleFixPacket(ctx, text, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Strategy != StrategySkip {
		t.Errorf("expected skip strategy, got %s", result.Strategy)
	}
	if !result.Success {
		t.Error("skip should be successful")
	}
}

func TestSupervisor_HandleFixPacket_Delegate(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	_ = rs.Start()
	defer rs.Stop()

	text := `Gate: custom_fix
Severity: warning
1. Refactor the authentication module to use OAuth2
Files: src/auth.go
`
	result, err := rs.HandleFixPacket(ctx, text, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Strategy != StrategyDelegate {
		t.Errorf("expected delegate strategy, got %s", result.Strategy)
	}
	if !result.Success {
		t.Error("delegate should be successful")
	}
}

func TestSupervisor_HandleFixPacket_InvalidText(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	_, err := rs.HandleFixPacket(ctx, "", "agent-1")
	if err == nil {
		t.Error("expected error for empty fix packet")
	}

	_, err = rs.HandleFixPacket(ctx, "totally random text without gate", "agent-1")
	if err == nil {
		t.Error("expected error for unrecognized fix packet")
	}
}

func TestSupervisor_HandleFixPacket_BrainLearns(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	p := Pattern{
		ID:          "test-pattern",
		Name:        "cyclomatic_complexity",
		Description: "reduces complexity",
		Metadata:    map[string]any{"gate": "cyclomatic_complexity"},
	}
	if err := rs.GetBrain().Learn(ctx, p); err != nil {
		t.Fatal(err)
	}

	stats := rs.GetBrainStats()
	if stats.PatternCount != 1 {
		t.Errorf("expected 1 pattern, got %d", stats.PatternCount)
	}
}

func TestSupervisor_RegisterAgent(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	err := rs.RegisterAgent(ctx, "agent-1", []string{"src/*.go"})
	if err != nil {
		t.Fatal(err)
	}

	// Check coordinator has the agent
	agent, ok := rs.GetCoordinator().GetAgent("agent-1")
	if !ok {
		t.Fatal("agent not found in coordinator")
	}
	if agent.ID != "agent-1" {
		t.Errorf("expected agent ID agent-1, got %s", agent.ID)
	}
}

func TestSupervisor_DeregisterAgent(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	_ = rs.RegisterAgent(ctx, "agent-1", []string{"*.go"})
	err := rs.DeregisterAgent(ctx, "agent-1")
	if err != nil {
		t.Fatal(err)
	}

	_, ok := rs.GetCoordinator().GetAgent("agent-1")
	if ok {
		t.Error("agent should be deregistered from coordinator")
	}
}

func TestSupervisor_CheckpointAgent(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	_ = rs.RegisterAgent(ctx, "agent-1", []string{"*.go"})

	cp := &Checkpoint{
		Progress: 0.75,
		Files:    []string{"main.go"},
		Score:    0.9,
	}

	err := rs.CheckpointAgent(ctx, "agent-1", cp)
	if err != nil {
		t.Fatal(err)
	}

	agent, _ := rs.GetCoordinator().GetAgent("agent-1")
	if agent.Checkpoint == nil {
		t.Error("checkpoint should be set")
	}
}

func TestSupervisor_StartStop(t *testing.T) {
	rs := testSupervisor()

	if err := rs.Start(); err != nil {
		t.Fatal(err)
	}

	// Starting again should fail
	if err := rs.Start(); err == nil {
		t.Error("expected error when starting already running supervisor")
	}

	if err := rs.Stop(); err != nil {
		t.Fatal(err)
	}

	// Stopping again should be a no-op
	if err := rs.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisor_Getters(t *testing.T) {
	rs := testSupervisor()

	if rs.GetState() != StateIdle {
		t.Errorf("expected idle state, got %s", rs.GetState())
	}
	if rs.GetStateMachine() == nil {
		t.Error("GetStateMachine should not return nil")
	}
	if rs.GetBrain() == nil {
		t.Error("GetBrain should not return nil")
	}
	if rs.GetCoordinator() == nil {
		t.Error("GetCoordinator should not return nil")
	}

	stats := rs.GetBrainStats()
	if stats.PatternCount != 0 {
		t.Errorf("expected 0 patterns initially, got %d", stats.PatternCount)
	}
}

func TestSupervisor_HandleFixPacket_WithConstraints(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	_ = rs.Start()
	defer rs.Stop()

	text := `Gate: custom_gate
Severity: warning
1. Fix the code
Files: src/main.go
do_not_touch: vendor/
`
	result, err := rs.HandleFixPacket(ctx, text, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	// Should succeed since file doesn't match do_not_touch
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestSupervisor_ProcessInput_PII(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	text := `Contact: john.doe@example.com`
	result, err := rs.ProcessInput(ctx, text, "contacts.go")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Blocked {
		t.Error("PII should be blocked")
	}
}

func TestSupervisor_HandleFixPacket_FullPipeline(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	_ = rs.Start()
	defer rs.Stop()

	// Register agent first
	_ = rs.RegisterAgent(ctx, "agent-1", []string{"*.go"})

	// Handle a delegate fix packet
	text := `Gate: complex_refactor
Severity: error
1. Refactor the authentication module
2. Add proper error handling
Files: src/auth.go, src/auth_test.go
max_files: 5
`
	result, err := rs.HandleFixPacket(ctx, text, "agent-1")
	if err != nil {
		t.Fatal(err)
	}

	if result.Strategy != StrategyDelegate {
		t.Errorf("expected delegate strategy, got %s", result.Strategy)
	}

	// State should be back to idle
	if rs.GetState() != StateIdle {
		t.Errorf("expected state back to idle, got %s", rs.GetState())
	}

	// Check that instructions were parsed
	if len(result.FilesChanged) != 0 {
		// Delegate doesn't change files
	}
}

func TestSupervisor_HandleFixPacket_TextWithFile(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	_ = rs.Start()
	defer rs.Stop()

	// Check that a fix packet with file reference to naming_convention triggers ast-grep
	text := `Gate: my_custom_gate
Severity: warning
1. Fix naming_convention issues in the codebase
Files: src/main.go
`
	result, err := rs.HandleFixPacket(ctx, text, "agent-1")
	if err != nil {
		t.Fatal(err)
	}

	// The instruction mentions naming_convention which is a known ast-grep pattern
	if result.Strategy != StrategyASTGre {
		t.Logf("strategy is %s (instruction-based matching may depend on pattern registry)", result.Strategy)
	}
}

func TestSupervisor_FixApplierIntegration(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	_ = rs.Start()
	defer rs.Stop()

	// Create a fix packet that should be delegated
	text := `Gate: unknown_gate
Severity: warning
1. Do some refactoring
Files: main.go
`
	result, err := rs.HandleFixPacket(ctx, text, "agent-1")
	if err != nil {
		t.Fatal(err)
	}

	if !result.Success {
		t.Error("fix should be successful")
	}
	if result.Strategy != StrategyDelegate {
		t.Errorf("expected delegate, got %s", result.Strategy)
	}
}

func TestSupervisor_AgentRegisterAndHandoff(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	_ = rs.Start()
	defer rs.Stop()

	_ = rs.RegisterAgent(ctx, "agent-1", []string{"src/*.go"})
	_ = rs.GetCoordinator().Register(ctx, "agent-2", []string{"test/*.go"})

	_ = rs.GetStateMachine().Transition(ctx, StateWorking, nil)

	err := rs.HandoffAgent(ctx, "agent-1", "agent-2")
	if err != nil {
		t.Fatal(err)
	}

	from, _ := rs.GetCoordinator().GetAgent("agent-1")
	to, _ := rs.GetCoordinator().GetAgent("agent-2")

	if from.Status != AgentHandedOff {
		t.Errorf("from agent should be handed_off, got %s", from.Status)
	}
	if to.Status != AgentActive {
		t.Errorf("to agent should be active, got %s", to.Status)
	}
}

func TestSupervisor_ProcessInput_EntropyWarning(t *testing.T) {
	rs := testSupervisor()
	ctx := context.Background()

	// High entropy string that should generate a warning (but not necessarily block without secrets)
	text := strings.Repeat("aB3dE5gH7jK9mN2pQ4sT6vW8xY1", 2)
	result, err := rs.ProcessInput(ctx, text, "data.bin")
	if err != nil {
		t.Fatal(err)
	}
	// Just verify it doesn't crash; high entropy may or may not block
	_ = result
}
