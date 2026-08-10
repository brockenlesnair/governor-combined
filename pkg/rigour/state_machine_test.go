package rigour

import (
	"context"
	"log/slog"
	"testing"
)

func testStateMachine() *StateMachine {
	return NewStateMachine(slog.Default())
}

func TestNewStateMachine(t *testing.T) {
	sm := testStateMachine()
	if sm == nil {
		t.Fatal("NewStateMachine returned nil")
	}
	if sm.Current() != StateIdle {
		t.Errorf("expected initial state idle, got %s", sm.Current())
	}
}

func TestStateMachine_Transition_Valid(t *testing.T) {
	sm := testStateMachine()
	ctx := context.Background()

	// idle -> working
	if err := sm.Transition(ctx, StateWorking, nil); err != nil {
		t.Fatalf("idle->working should succeed: %v", err)
	}
	if sm.Current() != StateWorking {
		t.Errorf("expected state working, got %s", sm.Current())
	}

	// working -> fixing
	if err := sm.Transition(ctx, StateFixing, nil); err != nil {
		t.Fatalf("working->fixing should succeed: %v", err)
	}
	if sm.Current() != StateFixing {
		t.Errorf("expected state fixing, got %s", sm.Current())
	}

	// fixing -> done
	if err := sm.Transition(ctx, StateDone, nil); err != nil {
		t.Fatalf("fixing->done should succeed: %v", err)
	}
	if sm.Current() != StateDone {
		t.Errorf("expected state done, got %s", sm.Current())
	}
}

func TestStateMachine_Transition_Valid_AllPaths(t *testing.T) {
	validPaths := []struct {
		name  string
		steps []State
	}{
		{"idle-working-done", []State{StateWorking, StateDone}},
		{"idle-working-idle", []State{StateWorking, StateIdle}},
		{"idle-working-fixing-working", []State{StateWorking, StateFixing, StateWorking}},
		{"idle-working-fixing-done", []State{StateWorking, StateFixing, StateDone}},
		{"idle-working-handoff-idle", []State{StateWorking, StateHandoff, StateIdle}},
		{"idle-working-handoff-working", []State{StateWorking, StateHandoff, StateWorking}},
	}

	for _, path := range validPaths {
		sm := testStateMachine()
		ctx := context.Background()
		for _, step := range path.steps {
			if err := sm.Transition(ctx, step, nil); err != nil {
				t.Errorf("%s: unexpected error on transition to %s: %v", path.name, step, err)
			}
		}
	}
}

func TestStateMachine_Transition_Invalid(t *testing.T) {
	sm := testStateMachine()
	ctx := context.Background()

	// idle -> fixing is invalid
	if err := sm.Transition(ctx, StateFixing, nil); err == nil {
		t.Error("idle->fixing should fail")
	}

	// idle -> handoff is invalid
	if err := sm.Transition(ctx, StateHandoff, nil); err == nil {
		t.Error("idle->handoff should fail")
	}

	// idle -> done is invalid
	if err := sm.Transition(ctx, StateDone, nil); err == nil {
		t.Error("idle->done should fail")
	}

	// State should remain idle
	if sm.Current() != StateIdle {
		t.Errorf("state should remain idle after invalid transitions, got %s", sm.Current())
	}
}

func TestStateMachine_Transition_Invalid_DoneToFixing(t *testing.T) {
	sm := testStateMachine()
	ctx := context.Background()

	// Go to done
	_ = sm.Transition(ctx, StateWorking, nil)
	_ = sm.Transition(ctx, StateDone, nil)

	// done -> fixing is invalid
	if err := sm.Transition(ctx, StateFixing, nil); err == nil {
		t.Error("done->fixing should fail")
	}
}

func TestStateMachine_CanTransition(t *testing.T) {
	sm := testStateMachine()

	// idle: can only go to working
	if !sm.CanTransition(StateWorking) {
		t.Error("idle should be able to transition to working")
	}
	if sm.CanTransition(StateFixing) {
		t.Error("idle should not be able to transition to fixing")
	}
	if sm.CanTransition(StateDone) {
		t.Error("idle should not be able to transition to done")
	}
	if sm.CanTransition(StateIdle) {
		t.Error("idle should not be able to transition to idle")
	}

	// Move to working
	ctx := context.Background()
	_ = sm.Transition(ctx, StateWorking, nil)

	// working: can go to fixing, handoff, done, idle
	if !sm.CanTransition(StateFixing) {
		t.Error("working should be able to transition to fixing")
	}
	if !sm.CanTransition(StateHandoff) {
		t.Error("working should be able to transition to handoff")
	}
	if !sm.CanTransition(StateDone) {
		t.Error("working should be able to transition to done")
	}
	if !sm.CanTransition(StateIdle) {
		t.Error("working should be able to transition to idle")
	}
	if sm.CanTransition(StateWorking) {
		t.Error("working should not be able to transition to working")
	}
}

func TestStateMachine_Hooks(t *testing.T) {
	sm := testStateMachine()
	ctx := context.Background()

	var hookFrom, hookTo State
	hookFired := false

	sm.AddHook(func(ctx context.Context, from, to State, meta map[string]any) {
		hookFrom = from
		hookTo = to
		hookFired = true
	})

	_ = sm.Transition(ctx, StateWorking, nil)

	if !hookFired {
		t.Error("transition hook should have fired")
	}
	if hookFrom != StateIdle {
		t.Errorf("hook from should be idle, got %s", hookFrom)
	}
	if hookTo != StateWorking {
		t.Errorf("hook to should be working, got %s", hookTo)
	}
}

func TestStateMachine_Hooks_Multiple(t *testing.T) {
	sm := testStateMachine()
	ctx := context.Background()

	count := 0
	sm.AddHook(func(ctx context.Context, from, to State, meta map[string]any) {
		count++
	})
	sm.AddHook(func(ctx context.Context, from, to State, meta map[string]any) {
		count++
	})

	_ = sm.Transition(ctx, StateWorking, nil)

	if count != 2 {
		t.Errorf("expected 2 hooks fired, got %d", count)
	}
}

func TestStateMachine_History(t *testing.T) {
	sm := testStateMachine()
	ctx := context.Background()

	_ = sm.Transition(ctx, StateWorking, map[string]any{"gate": "test"})
	_ = sm.Transition(ctx, StateFixing, map[string]any{"strategy": "ast-grep"})
	_ = sm.Transition(ctx, StateDone, nil)

	history := sm.GetTransitions()
	if len(history) != 3 {
		t.Fatalf("expected 3 transitions in history, got %d", len(history))
	}

	if history[0].From != StateIdle || history[0].To != StateWorking {
		t.Errorf("first transition: expected idle->working, got %s->%s", history[0].From, history[0].To)
	}
	if history[1].From != StateWorking || history[1].To != StateFixing {
		t.Errorf("second transition: expected working->fixing, got %s->%s", history[1].From, history[1].To)
	}
	if history[2].From != StateFixing || history[2].To != StateDone {
		t.Errorf("third transition: expected fixing->done, got %s->%s", history[2].From, history[2].To)
	}

	// Check metadata
	if history[0].Meta["gate"] != "test" {
		t.Error("expected metadata gate=test in first transition")
	}
}

func TestStateMachine_TransitionUnsafe(t *testing.T) {
	sm := testStateMachine()
	ctx := context.Background()

	// Force an unsafe transition from idle to fixing (invalid normally)
	sm.TransitionUnsafe(ctx, StateFixing, map[string]any{"reason": "recovery"})
	if sm.Current() != StateFixing {
		t.Errorf("expected state fixing after unsafe transition, got %s", sm.Current())
	}

	// Should still be recorded in history
	history := sm.GetTransitions()
	if len(history) != 1 {
		t.Errorf("expected 1 transition in history, got %d", len(history))
	}
}

func TestState_String(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateIdle, "idle"},
		{StateWorking, "working"},
		{StateFixing, "fixing"},
		{StateHandoff, "handoff"},
		{StateDone, "done"},
		{State(99), "unknown(99)"},
	}

	for _, tt := range tests {
		got := tt.state.String()
		if got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", int(tt.state), got, tt.want)
		}
	}
}

func TestParseState(t *testing.T) {
	tests := []struct {
		input   string
		want    State
		wantErr bool
	}{
		{"idle", StateIdle, false},
		{"working", StateWorking, false},
		{"fixing", StateFixing, false},
		{"handoff", StateHandoff, false},
		{"done", StateDone, false},
		{"unknown", StateIdle, true},
	}

	for _, tt := range tests {
		got, err := ParseState(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseState(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
		if got != tt.want {
			t.Errorf("ParseState(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestStateMachine_RegisterAgent(t *testing.T) {
	sm := testStateMachine()

	agent := &AgentRegistration{
		ID:    "agent-1",
		Scope: []string{"src/*.go"},
	}

	if err := sm.RegisterAgent(agent); err != nil {
		t.Fatal(err)
	}

	got := sm.GetAgent()
	if got == nil {
		t.Fatal("GetAgent returned nil after registration")
	}
	if got.ID != "agent-1" {
		t.Errorf("expected agent ID agent-1, got %s", got.ID)
	}
}

func TestStateMachine_RegisterAgent_Duplicate(t *testing.T) {
	sm := testStateMachine()

	_ = sm.RegisterAgent(&AgentRegistration{ID: "agent-1", Scope: []string{"*.go"}})
	err := sm.RegisterAgent(&AgentRegistration{ID: "agent-2", Scope: []string{"*.go"}})
	if err == nil {
		t.Error("expected error when registering second agent")
	}
}

func TestStateMachine_DeregisterAgent(t *testing.T) {
	sm := testStateMachine()

	_ = sm.RegisterAgent(&AgentRegistration{ID: "agent-1"})
	sm.DeregisterAgent()

	if sm.GetAgent() != nil {
		t.Error("GetAgent should return nil after deregistration")
	}
}
