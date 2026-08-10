package persist

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store is the interface for the persistence layer.
type Store interface {
	// Initialize sets up the database schema.
	Initialize(ctx context.Context) error

	// Close closes the database connection.
	Close() error

	// GetDB returns the underlying database connection (for advanced use).
	GetDB() *sql.DB

	// Transaction executes a function within a database transaction.
	Transaction(ctx context.Context, fn func(*sql.Tx) error) error

	// Exec executes a query without returning rows.
	Exec(ctx context.Context, query string, args ...any) (sql.Result, error)

	// Query executes a query returning rows.
	Query(ctx context.Context, query string, args ...any) (*sql.Rows, error)

	// QueryRow executes a query returning a single row.
	QueryRow(ctx context.Context, query string, args ...any) *sql.Row
}

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite store.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(1) // SQLite only supports one writer
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	return &SQLiteStore{db: db}, nil
}

// Initialize sets up the database schema.
func (s *SQLiteStore) Initialize(ctx context.Context) error {
	schema := `
	-- Analysis results cache
	CREATE TABLE IF NOT EXISTS analysis_cache (
		id TEXT PRIMARY KEY,
		repo_root TEXT NOT NULL,
		analysis_type TEXT NOT NULL,
		result_data BLOB NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(repo_root, analysis_type)
	);

	-- Call graph nodes
	CREATE TABLE IF NOT EXISTS callgraph_nodes (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		package TEXT NOT NULL,
		file TEXT NOT NULL,
		line INTEGER NOT NULL,
		kind TEXT NOT NULL,
		receiver TEXT,
		exported BOOLEAN NOT NULL,
		fan_in INTEGER DEFAULT 0,
		fan_out INTEGER DEFAULT 0,
		in_cycle BOOLEAN DEFAULT FALSE,
		is_leaf BOOLEAN DEFAULT FALSE,
		is_root BOOLEAN DEFAULT FALSE
	);

	-- Call graph edges
	CREATE TABLE IF NOT EXISTS callgraph_edges (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		from_node TEXT NOT NULL,
		to_node TEXT NOT NULL,
		call_type TEXT NOT NULL,
		FOREIGN KEY (from_node) REFERENCES callgraph_nodes(id),
		FOREIGN KEY (to_node) REFERENCES callgraph_nodes(id)
	);

	-- Index for efficient lookups
	CREATE INDEX IF NOT EXISTS idx_callgraph_nodes_package ON callgraph_nodes(package);
	CREATE INDEX IF NOT EXISTS idx_callgraph_edges_from ON callgraph_edges(from_node);
	CREATE INDEX IF NOT EXISTS idx_callgraph_edges_to ON callgraph_edges(to_node);
	CREATE INDEX IF NOT EXISTS idx_analysis_cache_repo_type ON analysis_cache(repo_root, analysis_type);
	`

	_, err := s.db.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("initialize schema: %w", err)
	}

	return nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// GetDB returns the underlying database connection.
func (s *SQLiteStore) GetDB() *sql.DB {
	return s.db
}

// Transaction executes a function within a database transaction.
func (s *SQLiteStore) Transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("rollback failed: %v (original error: %w)", rbErr, err)
		}
		return err
	}

	return tx.Commit()
}

// Exec executes a query without returning rows.
func (s *SQLiteStore) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, query, args...)
}

// Query executes a query returning rows.
func (s *SQLiteStore) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, query, args...)
}

// QueryRow executes a query returning a single row.
func (s *SQLiteStore) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, query, args...)
}

// SaveAnalysisResult saves an analysis result to the cache.
func (s *SQLiteStore) SaveAnalysisResult(ctx context.Context, repoRoot, analysisType string, data []byte) error {
	query := `
		INSERT INTO analysis_cache (id, repo_root, analysis_type, result_data, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(repo_root, analysis_type) DO UPDATE SET
			result_data = excluded.result_data,
			created_at = excluded.created_at
	`
	id := fmt.Sprintf("%s:%s", repoRoot, analysisType)
	_, err := s.Exec(ctx, query, id, repoRoot, analysisType, data, time.Now())
	return err
}

// GetAnalysisResult retrieves an analysis result from the cache.
func (s *SQLiteStore) GetAnalysisResult(ctx context.Context, repoRoot, analysisType string) ([]byte, error) {
	query := `SELECT result_data FROM analysis_cache WHERE repo_root = ? AND analysis_type = ?`
	var data []byte
	err := s.QueryRow(ctx, query, repoRoot, analysisType).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return data, err
}

// SaveCallGraph saves a call graph to the database.
func (s *SQLiteStore) SaveCallGraph(ctx context.Context, g *CallGraph) error {
	return s.Transaction(ctx, func(tx *sql.Tx) error {
		// Clear existing data for this repo (simplified - in reality you'd use repo identifier)
		if _, err := tx.ExecContext(ctx, `DELETE FROM callgraph_edges`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM callgraph_nodes`); err != nil {
			return err
		}

		// Insert nodes
		nodeStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO callgraph_nodes (id, name, package, file, line, kind, receiver, exported, fan_in, fan_out, in_cycle, is_leaf, is_root)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer nodeStmt.Close()

		for _, node := range g.Nodes {
			_, err := nodeStmt.ExecContext(ctx,
				node.ID, node.Name, node.Package, node.File, node.Line,
				node.Kind, node.Receiver, node.Exported,
				node.FanIn, node.FanOut, node.InCycle, node.IsLeaf, node.IsRoot,
			)
			if err != nil {
				return err
			}
		}

		// Insert edges
		edgeStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO callgraph_edges (from_node, to_node, call_type)
			VALUES (?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer edgeStmt.Close()

		for _, edge := range g.Edges {
			_, err := edgeStmt.ExecContext(ctx, edge.From, edge.To, edge.CallType)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// LoadCallGraph loads a call graph from the database.
func (s *SQLiteStore) LoadCallGraph(ctx context.Context) (*CallGraph, error) {
	g := NewCallGraph()

	// Load nodes
	rows, err := s.Query(ctx, `SELECT id, name, package, file, line, kind, receiver, exported, fan_in, fan_out, in_cycle, is_leaf, is_root FROM callgraph_nodes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var node Node
		err := rows.Scan(&node.ID, &node.Name, &node.Package, &node.File, &node.Line,
			&node.Kind, &node.Receiver, &node.Exported,
			&node.FanIn, &node.FanOut, &node.InCycle, &node.IsLeaf, &node.IsRoot)
		if err != nil {
			return nil, err
		}
		g.AddNode(&node)
	}

	// Load edges
	rows, err = s.Query(ctx, `SELECT from_node, to_node, call_type FROM callgraph_edges`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var edge Edge
		err := rows.Scan(&edge.From, &edge.To, &edge.CallType)
		if err != nil {
			return nil, err
		}
		g.AddEdge(edge.From, edge.To, edge.CallType)
	}

	return g, nil
}