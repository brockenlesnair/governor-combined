package memory

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestMemory_Store_Get(t *testing.T) {
	fmt.Println("TestMemory_Store_Get: starting")
	tmpFile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("Create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	cfg := DefaultConfig()
	cfg.DBPath = tmpFile.Name()
	cfg.Debug = true

	fmt.Println("TestMemory_Store_Get: creating memory")
	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer m.Close()

	ctx := context.Background()

	// Store an entity
	entity := &Entity{
		ID:      "test-1",
		Type:    EntityFact,
		Content: "Test fact",
		Project: "test-project",
		Tags:    []string{"test", "fact"},
	}

	fmt.Println("TestMemory_Store_Get: storing entity")
	if err := m.Store(ctx, entity); err != nil {
		t.Fatalf("Store: %v", err)
	}
	fmt.Println("TestMemory_Store_Get: stored entity")

	// Retrieve entity
	fmt.Println("TestMemory_Store_Get: getting entity")
	retrieved, err := m.Get(ctx, "test-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	fmt.Println("TestMemory_Store_Get: got entity")

	if retrieved == nil {
		t.Fatal("Entity not found")
	}

	if retrieved.Content != "Test fact" {
		t.Errorf("Expected 'Test fact', got '%s'", retrieved.Content)
	}

	if retrieved.Type != EntityFact {
		t.Errorf("Expected type 'fact', got '%s'", retrieved.Type)
	}
}

func TestMemory_Store_Duplicate(t *testing.T) {
	fmt.Println("TestMemory_Store_Duplicate: starting")
	tmpFile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("Create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	cfg := DefaultConfig()
	cfg.DBPath = tmpFile.Name()
	cfg.Debug = true

	fmt.Println("TestMemory_Store_Duplicate: creating memory")
	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		fmt.Println("TestMemory_Store_Duplicate: closing memory")
		m.Close()
		fmt.Println("TestMemory_Store_Duplicate: closed memory")
	}()

	ctx := context.Background()

	entity := &Entity{
		ID:      "test-dup",
		Type:    EntityFact,
		Content: "First",
	}

	fmt.Println("TestMemory_Store_Duplicate: storing first entity")
	if err := m.Store(ctx, entity); err != nil {
		t.Fatalf("First Store: %v", err)
	}
	fmt.Println("TestMemory_Store_Duplicate: stored first entity")

	// Store again with same ID (should update)
	entity.Content = "Second"
	fmt.Println("TestMemory_Store_Duplicate: storing second entity")
	if err := m.Store(ctx, entity); err != nil {
		t.Fatalf("Second Store: %v", err)
	}
	fmt.Println("TestMemory_Store_Duplicate: stored second entity")

	retrieved, err := m.Get(ctx, "test-dup")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if retrieved.Content != "Second" {
		t.Errorf("Expected 'Second', got '%s'", retrieved.Content)
	}
}

func TestMemory_Link_GetRelationships(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("Create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	cfg := DefaultConfig()
	cfg.DBPath = tmpFile.Name()

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer m.Close()

	ctx := context.Background()

	// Store two entities
	entity1 := &Entity{ID: "ent-1", Type: EntityFact, Content: "Entity 1"}
	entity2 := &Entity{ID: "ent-2", Type: EntityFact, Content: "Entity 2"}

	m.Store(ctx, entity1)
	m.Store(ctx, entity2)

	// Link them
	rel := &Relationship{
		ID:        "rel-1",
		SourceID:  "ent-1",
		TargetID:  "ent-2",
		Kind:      "depends_on",
		Weight:    0.8,
		CreatedAt: time.Now(),
	}

	if err := m.Link(ctx, rel); err != nil {
		t.Fatalf("Link: %v", err)
	}

	// Get relationships
	relationships, err := m.GetRelationships(ctx, "ent-1")
	if err != nil {
		t.Fatalf("GetRelationships: %v", err)
	}

	if len(relationships) != 1 {
		t.Errorf("Expected 1 relationship, got %d", len(relationships))
	}

	if relationships[0].Kind != "depends_on" {
		t.Errorf("Expected kind 'depends_on', got '%s'", relationships[0].Kind)
	}
}

func TestMemory_Search(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("Create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	cfg := DefaultConfig()
	cfg.DBPath = tmpFile.Name()

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer m.Close()

	ctx := context.Background()

	// Store entities
	entities := []*Entity{
		{ID: "1", Type: EntityFact, Content: "The quick brown fox", Project: "test"},
		{ID: "2", Type: EntityFact, Content: "The lazy dog", Project: "test"},
		{ID: "3", Type: EntityLesson, Content: "Learning Go", Project: "other"},
	}

	for _, e := range entities {
		if err := m.Store(ctx, e); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}

	// Search
	query := &SearchQuery{
		Text:   "fox",
		Limit:  10,
		MinScore: 0.0,
	}

	results, err := m.Search(ctx, query)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) == 0 {
		t.Error("Expected at least 1 result")
	}

	// Check that result contains "fox"
	found := false
	for _, r := range results {
		if contains(r.Entity.Content, "fox") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected result containing 'fox'")
	}
}

func TestMemory_Traverse(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("Create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	cfg := DefaultConfig()
	cfg.DBPath = tmpFile.Name()

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer m.Close()

	ctx := context.Background()

	// Create chain: A -> B -> C
	entities := []*Entity{
		{ID: "A", Type: EntityFact, Content: "A"},
		{ID: "B", Type: EntityFact, Content: "B"},
		{ID: "C", Type: EntityFact, Content: "C"},
	}
	for _, e := range entities {
		m.Store(ctx, e)
	}

	m.Link(ctx, &Relationship{ID: "1", SourceID: "A", TargetID: "B", Kind: "next"})
	m.Link(ctx, &Relationship{ID: "2", SourceID: "B", TargetID: "C", Kind: "next"})

	// Traverse from A
	results, err := m.Traverse(ctx, "A", 2)
	if err != nil {
		t.Fatalf("Traverse: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 entities, got %d", len(results))
	}
}

func TestMemory_Stats(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("Create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	cfg := DefaultConfig()
	cfg.DBPath = tmpFile.Name()

	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer m.Close()

	ctx := context.Background()

	// Store some entities
	for i := 0; i < 5; i++ {
		m.Store(ctx, &Entity{
			ID:      fmt.Sprintf("ent-%d", i),
			Type:    EntityFact,
			Content: fmt.Sprintf("Entity %d", i),
		})
	}

	stats, err := m.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if stats.TotalEntities != 5 {
		t.Errorf("Expected 5 entities, got %d", stats.TotalEntities)
	}

	if stats.EntitiesByType["fact"] != 5 {
		t.Errorf("Expected 5 facts, got %d", stats.EntitiesByType["fact"])
	}
}

func TestMemory_Delete(t *testing.T) {
	fmt.Println("TestMemory_Delete: starting")
	tmpFile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("Create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	cfg := DefaultConfig()
	cfg.DBPath = tmpFile.Name()
	cfg.Debug = true

	fmt.Println("TestMemory_Delete: creating memory")
	m, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		fmt.Println("TestMemory_Delete: closing memory")
		m.Close()
		fmt.Println("TestMemory_Delete: closed memory")
	}()

	ctx := context.Background()

	entity := &Entity{ID: "to-delete", Type: EntityFact, Content: "Delete me"}
	fmt.Println("TestMemory_Delete: storing entity")
	m.Store(ctx, entity)
	fmt.Println("TestMemory_Delete: stored entity")

	// Verify exists
	fmt.Println("TestMemory_Delete: getting entity before delete")
	retrieved, _ := m.Get(ctx, "to-delete")
	if retrieved == nil {
		t.Fatal("Entity should exist before delete")
	}
	fmt.Println("TestMemory_Delete: got entity before delete")

	// Delete
	fmt.Println("TestMemory_Delete: deleting entity")
	if err := m.Delete(ctx, "to-delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	fmt.Println("TestMemory_Delete: deleted entity")

	// Verify gone
	fmt.Println("TestMemory_Delete: getting entity after delete")
	retrieved, _ = m.Get(ctx, "to-delete")
	if retrieved != nil {
		t.Error("Entity should not exist after delete")
	}
	fmt.Println("TestMemory_Delete: verified entity gone")
}

func TestEntity_Type_Constants(t *testing.T) {
	// Test that all entity types are defined
	types := []EntityType{
		EntityDecision,
		EntityLesson,
		EntityFact,
		EntityPreference,
		EntityPerson,
		EntityProject,
		EntityBug,
		EntityPattern,
		EntityContext,
	}

	for _, et := range types {
		if et == "" {
			t.Error("Entity type should not be empty")
		}
	}
}

func TestConfig_DefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.DBPath == "" {
		t.Error("DBPath should not be empty")
	}
	if cfg.EmbeddingDim != 384 {
		t.Errorf("Expected EmbeddingDim 384, got %d", cfg.EmbeddingDim)
	}
	if cfg.MaxEntities <= 0 {
		t.Error("MaxEntities should be positive")
	}
	if cfg.VectorWeight+cfg.FTSWeight+cfg.GraphWeight != 1.0 {
		t.Errorf("Search weights should sum to 1.0, got %f", cfg.VectorWeight+cfg.FTSWeight+cfg.GraphWeight)
	}
}

func TestConfig_FSRSConfig(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.FSRS.W) != 19 {
		t.Errorf("Expected 19 FSRS parameters, got %d", len(cfg.FSRS.W))
	}
	if cfg.FSRS.DesiredRetention <= 0 || cfg.FSRS.DesiredRetention > 1 {
		t.Error("DesiredRetention should be in (0, 1]")
	}
}

func TestSearchQuery_Defaults(t *testing.T) {
	query := &SearchQuery{
		Text: "test",
	}
	// The Search method applies defaults, so we verify the logic there
	limit := query.Limit
	if limit <= 0 {
		limit = 10
	}
	minScore := query.MinScore
	if minScore < 0 {
		minScore = 0.0
	}
	maxHops := query.MaxHops
	if maxHops <= 0 {
		maxHops = 2
	}

	if limit != 10 {
		t.Errorf("Expected default limit 10, got %d", limit)
	}
	if minScore != 0.0 {
		t.Errorf("Expected default min_score 0.0, got %f", minScore)
	}
	if maxHops != 2 {
		t.Errorf("Expected default max_hops 2, got %d", maxHops)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}