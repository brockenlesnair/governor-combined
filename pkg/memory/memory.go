// Package memory implements a persistent knowledge graph with semantic search.
//
// It stores entities (concepts, decisions, patterns) and relationships between
// them in SQLite, with vector embeddings for similarity search. Includes automatic
// decay of stale knowledge and configurable retention policies.
package memory

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Memory is the top-level API for the memory system.
type Memory struct {
	config            *Config
	db                *sql.DB
	entityStore       *EntityStore
	relationshipStore *RelationshipStore
	searchEngine      *SearchEngine
	decayEngine       *DecayEngine
	embeddingEngine   *EmbeddingEngine
	mu                sync.RWMutex
	closed            bool
	decayDone         chan struct{} // signals decayLoop to stop
}

// New creates a new memory system.
func New(cfg *Config) (*Memory, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Open database
	db, err := sql.Open("sqlite", cfg.DBPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	m := &Memory{
		config: cfg,
		db:     db,
	}

	// Initialize stores
	m.entityStore = NewEntityStore(db)
	m.relationshipStore = NewRelationshipStore(db)

	// Initialize tables
	ctx := context.Background()
	if err := m.entityStore.Initialize(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize entity store: %w", err)
	}
	if err := m.relationshipStore.Initialize(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize relationship store: %w", err)
	}

	// Initialize engines
	m.embeddingEngine = NewEmbeddingEngine(cfg)
	m.searchEngine = NewSearchEngine(m.entityStore, m.relationshipStore, cfg)
	m.decayEngine = NewDecayEngine(cfg)
	m.decayDone = make(chan struct{})

	// Start decay ticker
	go m.decayLoop()

	return m, nil
}

// decayLoop runs periodic decay passes.
func (m *Memory) decayLoop() {
	ticker := time.NewTicker(m.config.DecayInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.mu.RLock()
			closed := m.closed
			m.mu.RUnlock()
			if closed {
				return
			}
			ctx := context.Background()
			if err := m.decayEngine.Decay(ctx, m.entityStore); err != nil {
				if m.config.Debug {
					fmt.Printf("decay pass failed: %v\n", err)
				}
			}
		case <-m.decayDone:
			if m.config.Debug {
				fmt.Printf("decayLoop: received shutdown signal\n")
			}
			return
		}
	}
}

// Store stores an entity.
func (m *Memory) Store(ctx context.Context, entity *Entity) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return fmt.Errorf("memory system closed")
	}

	// Generate embedding if not present
	if len(entity.Embedding) == 0 && m.embeddingEngine != nil {
		embedding, err := m.embeddingEngine.GenerateEmbedding(ctx, entity.Content)
		if err == nil {
			entity.Embedding = embedding
			// Add to vector index
			m.searchEngine.vectorIndex.Add(entity.ID, dequantizeEmbedding(embedding))
		}
	}

	// Set timestamps
	now := time.Now()
	if entity.CreatedAt.IsZero() {
		entity.CreatedAt = now
	}
	entity.UpdatedAt = now
	entity.DecayScore = 1.0 // Fresh memory

	// Store entity
	if err := m.entityStore.Store(ctx, entity); err != nil {
		return err
	}

	// Add to vector index
	if len(entity.Embedding) > 0 {
		m.searchEngine.vectorIndex.Add(entity.ID, dequantizeEmbedding(entity.Embedding))
	}

	return nil
}

// Get retrieves an entity by ID.
func (m *Memory) Get(ctx context.Context, id string) (*Entity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, fmt.Errorf("memory system closed")
	}

	entity, err := m.entityStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update access count and decay
	if entity != nil {
		m.decayEngine.UpdateEntityDecay(ctx, m.entityStore, entity)
	}

	return entity, nil
}

// Update updates an entity.
func (m *Memory) Update(ctx context.Context, entity *Entity) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return fmt.Errorf("memory system closed")
	}

	entity.UpdatedAt = time.Now()
	return m.entityStore.Update(ctx, entity)
}

// Delete deletes an entity.
func (m *Memory) Delete(ctx context.Context, id string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return fmt.Errorf("memory system closed")
	}

	// Delete relationships first
	relationships, _ := m.relationshipStore.GetRelationships(ctx, id)
	for _, rel := range relationships {
		m.relationshipStore.Unlink(ctx, rel.ID)
	}

	return m.entityStore.Delete(ctx, id)
}

// List lists entities with filtering.
func (m *Memory) List(ctx context.Context, filter ListFilter) ([]*Entity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, fmt.Errorf("memory system closed")
	}

	return m.entityStore.List(ctx, filter)
}

// Link creates a relationship between entities.
func (m *Memory) Link(ctx context.Context, rel *Relationship) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return fmt.Errorf("memory system closed")
	}

	if rel.CreatedAt.IsZero() {
		rel.CreatedAt = time.Now()
	}

	return m.relationshipStore.Link(ctx, rel)
}

// Unlink removes a relationship.
func (m *Memory) Unlink(ctx context.Context, id string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return fmt.Errorf("memory system closed")
	}

	return m.relationshipStore.Unlink(ctx, id)
}

// GetRelationships gets all relationships for an entity.
func (m *Memory) GetRelationships(ctx context.Context, entityID string) ([]*Relationship, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, fmt.Errorf("memory system closed")
	}

	return m.relationshipStore.GetRelationships(ctx, entityID)
}

// Traverse performs graph traversal from a starting entity.
func (m *Memory) Traverse(ctx context.Context, startID string, maxHops int) ([]*Entity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, fmt.Errorf("memory system closed")
	}

	visited := make(map[string]bool)
	var results []*Entity

	var dfs func(string, int)
	dfs = func(id string, hops int) {
		if hops > maxHops || visited[id] {
			return
		}
		visited[id] = true

		entity, err := m.entityStore.Get(ctx, id)
		if err != nil || entity == nil {
			return
		}
		results = append(results, entity)

		relationships, _ := m.relationshipStore.GetOutgoing(ctx, id)
		for _, rel := range relationships {
			dfs(rel.TargetID, hops+1)
		}
	}

	dfs(startID, 0)
	return results, nil
}

// Search performs hybrid search.
func (m *Memory) Search(ctx context.Context, query *SearchQuery) ([]*SearchResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, fmt.Errorf("memory system closed")
	}

	return m.searchEngine.Search(ctx, query)
}

// Decay runs a decay pass.
func (m *Memory) Decay(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return fmt.Errorf("memory system closed")
	}

	return m.decayEngine.Decay(ctx, m.entityStore)
}

// Prune removes forgotten entities.
func (m *Memory) Prune(ctx context.Context, threshold float64) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return 0, fmt.Errorf("memory system closed")
	}

	if threshold <= 0 {
		threshold = m.config.PruneThreshold
	}

	return m.decayEngine.Prune(ctx, m.entityStore)
}

// Stats returns memory system statistics.
func (m *Memory) Stats(ctx context.Context) (*MemoryStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, fmt.Errorf("memory system closed")
	}

	count, err := m.entityStore.Count(ctx)
	if err != nil {
		return nil, err
	}

	entities, err := m.entityStore.List(ctx, ListFilter{Limit: count})
	if err != nil {
		return nil, err
	}

	stats := &MemoryStats{
		TotalEntities: count,
		EntitiesByType: make(map[string]int),
	}

	var totalDecay float64
	var oldest, newest *time.Time

	for _, entity := range entities {
		stats.EntitiesByType[string(entity.Type)]++
		totalDecay += entity.DecayScore

		if oldest == nil || entity.CreatedAt.Before(*oldest) {
			oldest = &entity.CreatedAt
		}
		if newest == nil || entity.CreatedAt.After(*newest) {
			newest = &entity.CreatedAt
		}
	}

	if count > 0 {
		stats.AvgDecayScore = totalDecay / float64(count)
	}
	stats.OldestEntity = oldest
	stats.NewestEntity = newest

	return stats, nil
}

// Close closes the memory system.
func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}

	m.closed = true

	// Signal decay loop to stop
	close(m.decayDone)

	if m.embeddingEngine != nil {
		m.embeddingEngine.Close()
	}

	return m.db.Close()
}

// ToolDefinition returns the MCP tool definition.
func (m *Memory) ToolDefinition() MCPToolDef {
	return MCPToolDef{
		Name:        "memory",
		Description: "Store, retrieve, and search memories with hybrid vector/FTS/graph retrieval",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type": "string",
					"enum": []string{"store", "get", "search", "link", "traverse", "stats"},
				},
				"entity": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":          map[string]any{"type": "string"},
						"type":        map[string]any{"type": "string"},
						"content":     map[string]any{"type": "string"},
						"metadata":    map[string]any{"type": "object"},
						"project":     map[string]any{"type": "string"},
						"tags":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
				},
				"query": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text":      map[string]any{"type": "string"},
						"types":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"projects":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"tags":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"limit":     map[string]any{"type": "integer"},
						"min_score": map[string]any{"type": "number"},
						"max_hops":  map[string]any{"type": "integer"},
					},
				},
				"relationship": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":         map[string]any{"type": "string"},
						"source_id":  map[string]any{"type": "string"},
						"target_id":  map[string]any{"type": "string"},
						"kind":       map[string]any{"type": "string"},
						"weight":     map[string]any{"type": "number"},
					},
				},
			},
		},
	}
}

// HandleToolCall handles MCP tool calls.
func (m *Memory) HandleToolCall(ctx context.Context, input []byte) ([]byte, error) {
	// This would parse the input and route to appropriate method
	// Simplified for now
	return []byte(`{"result": "not implemented"}`), nil
}