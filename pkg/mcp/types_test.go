package mcp

import (
	"context"
	"testing"
)

func TestToolRegistry_RegisterAndExecute(t *testing.T) {
	r := NewToolRegistry()

	handler := func(ctx context.Context, args map[string]any) (map[string]any, error) {
		return map[string]any{"result": "ok"}, nil
	}

	def := ToolDefinition{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]any{"type": "object"},
	}

	if err := r.Register(def, handler); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	result, err := r.Execute(context.Background(), "test_tool", nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result["result"] != "ok" {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestToolRegistry_RegisterDuplicate(t *testing.T) {
	r := NewToolRegistry()

	handler := func(ctx context.Context, args map[string]any) (map[string]any, error) {
		return nil, nil
	}

	def := ToolDefinition{
		Name:        "dup_tool",
		Description: "A test tool",
		InputSchema: map[string]any{"type": "object"},
	}

	if err := r.Register(def, handler); err != nil {
		t.Fatalf("First register failed: %v", err)
	}

	if err := r.Register(def, handler); err == nil {
		t.Fatal("expected collision error on duplicate register")
	}
}

func TestToolRegistry_ExecuteNotFound(t *testing.T) {
	r := NewToolRegistry()

	_, err := r.Execute(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected ToolNotFoundError")
	}

	var notFound *ToolNotFoundError
	if !errorsAs(err, &notFound) {
		t.Errorf("expected *ToolNotFoundError, got %T", err)
	}
}

func TestToolRegistry_ListDefinitions(t *testing.T) {
	r := NewToolRegistry()

	handler := func(ctx context.Context, args map[string]any) (map[string]any, error) {
		return nil, nil
	}

	defs := []ToolDefinition{
		{Name: "tool_a", Description: "Tool A", InputSchema: map[string]any{}},
		{Name: "tool_b", Description: "Tool B", InputSchema: map[string]any{}},
	}

	for _, def := range defs {
		if err := r.Register(def, handler); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
	}

	list := r.ListDefinitions()
	if len(list) != 2 {
		t.Errorf("expected 2 definitions, got %d", len(list))
	}

	names := make(map[string]bool)
	for _, def := range list {
		names[def.Name] = true
	}
	if !names["tool_a"] || !names["tool_b"] {
		t.Errorf("missing expected tools: %v", names)
	}
}

func TestToolRegistry_HasAndCount(t *testing.T) {
	r := NewToolRegistry()

	if r.Count() != 0 {
		t.Errorf("expected 0, got %d", r.Count())
	}
	if r.Has("any") {
		t.Error("expected false for empty registry")
	}

	handler := func(ctx context.Context, args map[string]any) (map[string]any, error) { return nil, nil }
	if err := r.Register(ToolDefinition{Name: "t1", InputSchema: map[string]any{}}, handler); err != nil {
		t.Fatal(err)
	}

	if r.Count() != 1 {
		t.Errorf("expected 1, got %d", r.Count())
	}
	if !r.Has("t1") {
		t.Error("expected Has('t1') = true")
	}
	if r.Has("t2") {
		t.Error("expected Has('t2') = false")
	}
}

func TestToolNotFoundError_Error(t *testing.T) {
	err := &ToolNotFoundError{Name: "mytool"}
	if err.Error() != "tool not found: mytool" {
		t.Errorf("unexpected error string: %s", err.Error())
	}
}

func TestToolCollisionError_Error(t *testing.T) {
	err := &ToolCollisionError{Name: "mytool"}
	if err.Error() != "tool already registered: mytool" {
		t.Errorf("unexpected error string: %s", err.Error())
	}
}

func TestToolExecutionError_ErrorAndUnwrap(t *testing.T) {
	base := &ToolNotFoundError{Name: "inner"}
	err := &ToolExecutionError{Name: "outer", Err: base}

	expected := "tool outer failed: tool not found: inner"
	if err.Error() != expected {
		t.Errorf("unexpected error string: %s", err.Error())
	}

	if err.Unwrap() != base {
		t.Error("Unwrap should return wrapped error")
	}
}

func errorsAs(err error, target any) bool {
	// Simple errorsAs for testing
	for err != nil {
		if target != nil {
			if t, ok := target.(**ToolNotFoundError); ok {
				if e, ok := err.(*ToolNotFoundError); ok {
					*t = e
					return true
				}
			}
			if t, ok := target.(**ToolCollisionError); ok {
				if e, ok := err.(*ToolCollisionError); ok {
					*t = e
					return true
				}
			}
			if t, ok := target.(**ToolExecutionError); ok {
				if e, ok := err.(*ToolExecutionError); ok {
					*t = e
					return true
				}
			}
		}
		err = unwrap(err)
	}
	return false
}

func unwrap(err error) error {
	if e, ok := err.(interface{ Unwrap() error }); ok {
		return e.Unwrap()
	}
	return nil
}