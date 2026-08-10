package mcp

import (
	"context"
	"fmt"
	"sync"
)

// ToolHandler is the function signature every tool must implement.
type ToolHandler func(ctx context.Context, args map[string]any) (map[string]any, error)

// ToolDefinition describes a tool's schema for client discovery.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolRegistry holds tool handlers and their definitions.
type ToolRegistry struct {
	handlers    map[string]ToolHandler
	definitions map[string]ToolDefinition
	mu          sync.RWMutex
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		handlers:    make(map[string]ToolHandler),
		definitions: make(map[string]ToolDefinition),
	}
}

func (r *ToolRegistry) Register(def ToolDefinition, handler ToolHandler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[def.Name]; exists {
		return &ToolCollisionError{Name: def.Name}
	}
	r.handlers[def.Name] = handler
	r.definitions[def.Name] = def
	return nil
}

func (r *ToolRegistry) Execute(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	r.mu.RLock()
	handler, ok := r.handlers[name]
	r.mu.RUnlock()
	if !ok {
		return nil, &ToolNotFoundError{Name: name}
	}
	return handler(ctx, args)
}

func (r *ToolRegistry) ListDefinitions() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ToolDefinition, 0, len(r.definitions))
	for _, def := range r.definitions {
		defs = append(defs, def)
	}
	return defs
}

func (r *ToolRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.handlers[name]
	return ok
}

func (r *ToolRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.handlers)
}

// Error Types
type ToolNotFoundError struct{ Name string }

func (e *ToolNotFoundError) Error() string { return fmt.Sprintf("tool not found: %s", e.Name) }

type ToolCollisionError struct{ Name string }

func (e *ToolCollisionError) Error() string { return fmt.Sprintf("tool already registered: %s", e.Name) }

type ToolExecutionError struct{ Name string; Err error }

func (e *ToolExecutionError) Error() string { return fmt.Sprintf("tool %s failed: %v", e.Name, e.Err) }
func (e *ToolExecutionError) Unwrap() error { return e.Err }