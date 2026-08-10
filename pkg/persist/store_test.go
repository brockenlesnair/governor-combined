package persist

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
)

func TestSQLiteStore_Initialize(t *testing.T) {
	// Create a temporary database file
	tmpFile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("Create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	store, err := NewSQLiteStore(tmpFile.Name())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Verify tables exist
	var count int
	err = store.QueryRow(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('analysis_cache', 'callgraph_nodes', 'callgraph_edges')`).Scan(&count)
	if err != nil {
		t.Fatalf("Query tables: %v", err)
	}
	if count != 3 {
		t.Errorf("Expected 3 tables, got %d", count)
	}
}

func TestSQLiteStore_SaveAnalysisResult(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("Create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	store, err := NewSQLiteStore(tmpFile.Name())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Save analysis result
	data := []byte(`{"test": "data"}`)
	if err := store.SaveAnalysisResult(ctx, "/test/repo", "callgraph", data); err != nil {
		t.Fatalf("SaveAnalysisResult: %v", err)
	}

	// Retrieve analysis result
	retrieved, err := store.GetAnalysisResult(ctx, "/test/repo", "callgraph")
	if err != nil {
		t.Fatalf("GetAnalysisResult: %v", err)
	}

	if string(retrieved) != string(data) {
		t.Errorf("Expected %s, got %s", string(data), string(retrieved))
	}
}

func TestSQLiteStore_GetAnalysisResult_NotFound(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("Create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	store, err := NewSQLiteStore(tmpFile.Name())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	retrieved, err := store.GetAnalysisResult(ctx, "/nonexistent", "callgraph")
	if err != nil {
		t.Fatalf("GetAnalysisResult: %v", err)
	}
	if retrieved != nil {
		t.Errorf("Expected nil for nonexistent result, got %v", retrieved)
	}
}

func TestSQLiteStore_SaveLoadCallGraph(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("Create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	store, err := NewSQLiteStore(tmpFile.Name())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Create a test call graph
	g := NewCallGraph()
	nodeA := &Node{ID: "A", Name: "A", Package: "test", File: "a.go", Line: 10, Kind: "function", Exported: true}
	nodeB := &Node{ID: "B", Name: "B", Package: "test", File: "b.go", Line: 20, Kind: "function", Exported: false}
	g.AddNode(nodeA)
	g.AddNode(nodeB)
	g.AddEdge("A", "B", "direct")

	// Save call graph
	if err := store.SaveCallGraph(ctx, g); err != nil {
		t.Fatalf("SaveCallGraph: %v", err)
	}

	// Load call graph
	loaded, err := store.LoadCallGraph(ctx)
	if err != nil {
		t.Fatalf("LoadCallGraph: %v", err)
	}

	if loaded.NodeCount() != 2 {
		t.Errorf("Expected 2 nodes, got %d", loaded.NodeCount())
	}
	if loaded.EdgeCount() != 1 {
		t.Errorf("Expected 1 edge, got %d", loaded.EdgeCount())
	}

	// Verify node data
	loadedA := loaded.GetNode("A")
	if loadedA == nil {
		t.Error("Node A not found")
	} else if loadedA.Name != "A" {
		t.Errorf("Expected name A, got %s", loadedA.Name)
	}
}

func TestCallGraph_AddNode_AddEdge(t *testing.T) {
	g := NewCallGraph()

	nodeA := &Node{ID: "A", Name: "A", Package: "test"}
	nodeB := &Node{ID: "B", Name: "B", Package: "test"}

	g.AddNode(nodeA)
	g.AddNode(nodeB)
	g.AddEdge("A", "B", "direct")

	if g.NodeCount() != 2 {
		t.Errorf("Expected 2 nodes, got %d", g.NodeCount())
	}
	if g.EdgeCount() != 1 {
		t.Errorf("Expected 1 edge, got %d", g.EdgeCount())
	}

	succ := g.Successors("A")
	if len(succ) != 1 || succ[0] != "B" {
		t.Errorf("Expected successor B, got %v", succ)
	}

	pred := g.Predecessors("B")
	if len(pred) != 1 || pred[0] != "A" {
		t.Errorf("Expected predecessor A, got %v", pred)
	}
}

func TestSQLiteStore_Transaction(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("Create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	store, err := NewSQLiteStore(tmpFile.Name())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Test successful transaction
	err = store.Transaction(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO analysis_cache (id, repo_root, analysis_type, result_data) VALUES (?, ?, ?, ?)`,
			"test1", "/repo", "test", []byte("data1"))
		return err
	})
	if err != nil {
		t.Fatalf("Transaction failed: %v", err)
	}

	// Test rolled back transaction
	err = store.Transaction(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO analysis_cache (id, repo_root, analysis_type, result_data) VALUES (?, ?, ?, ?)`,
			"test2", "/repo", "test", []byte("data2"))
		if err != nil {
			return err
		}
		return fmt.Errorf("intentional error")
	})
	if err == nil {
		t.Error("Expected transaction to fail")
	}

	// Verify only first insert persisted
	var count int
	err = store.QueryRow(ctx, `SELECT COUNT(*) FROM analysis_cache WHERE repo_root = ?`, "/repo").Scan(&count)
	if err != nil {
		t.Fatalf("Query count: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 row after rollback, got %d", count)
	}
}