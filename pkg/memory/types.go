package memory

import (
	"time"
)

// EntityType classifies what a memory represents.
type EntityType string

const (
	EntityDecision   EntityType = "decision"
	EntityLesson     EntityType = "lesson"
	EntityFact       EntityType = "fact"
	EntityPreference EntityType = "preference"
	EntityPerson     EntityType = "person"
	EntityProject    EntityType = "project"
	EntityBug        EntityType = "bug"
	EntityPattern    EntityType = "pattern"
	EntityContext    EntityType = "context"
)

// Entity is the fundamental unit of memory.
type Entity struct {
	ID          string            `json:"id"`
	Type        EntityType        `json:"type"`
	Content     string            `json:"content"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Project     string            `json:"project,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	AccessCount int               `json:"access_count"`
	DecayScore  float64           `json:"decay_score"` // 0.0 = forgotten, 1.0 = fresh
	Embedding   []float32         `json:"-"`           // 384-dim float32, not serialized
}

// Relationship connects two entities with a typed edge.
type Relationship struct {
	ID        string    `json:"id"`
	SourceID  string    `json:"source_id"`
	TargetID  string    `json:"target_id"`
	Kind      string    `json:"kind"`       // "depends_on", "decided_by", "related_to", etc.
	Weight    float64   `json:"weight"`     // 0.0-1.0
	CreatedAt time.Time `json:"created_at"`
}

// SearchQuery controls hybrid retrieval.
type SearchQuery struct {
	Text       string      `json:"text"`
	Types      []EntityType `json:"types,omitempty"`
	Projects   []string    `json:"projects,omitempty"`
	Tags       []string    `json:"tags,omitempty"`
	Limit      int         `json:"limit"`
	MinScore   float64     `json:"min_score"`    // abstention floor
	MaxHops    int         `json:"max_hops"`     // spreading activation depth
}

// SearchResult is a single retrieval hit.
type SearchResult struct {
	Entity Entity  `json:"entity"`
	Score  float64 `json:"score"`
	Source string  `json:"source"`     // "vector", "fts", "graph", "hybrid"
	Path   []string `json:"path,omitempty"` // graph traversal path
}

// ListFilter filters entity listings.
type ListFilter struct {
	Types    []EntityType
	Projects []string
	Tags     []string
	Limit    int
	Offset   int
}

// MemoryStats provides statistics about the memory system.
type MemoryStats struct {
	TotalEntities     int            `json:"total_entities"`
	EntitiesByType    map[string]int `json:"entities_by_type"`
	TotalRelationships int           `json:"total_relationships"`
	AvgDecayScore     float64        `json:"avg_decay_score"`
	OldestEntity      *time.Time     `json:"oldest_entity,omitempty"`
	NewestEntity      *time.Time     `json:"newest_entity,omitempty"`
	StorageSizeBytes  int64          `json:"storage_size_bytes"`
}

// MCPToolDef defines the MCP tool for the memory system.
type MCPToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}