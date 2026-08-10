package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// EntityStore handles entity persistence.
type EntityStore struct {
	db *sql.DB
}

// NewEntityStore creates a new entity store.
func NewEntityStore(db *sql.DB) *EntityStore {
	return &EntityStore{db: db}
}

// Initialize creates the entity tables.
func (s *EntityStore) Initialize(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS entities (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		content TEXT NOT NULL,
		metadata TEXT, -- JSON
		project TEXT,
		tags TEXT, -- JSON array
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		access_count INTEGER DEFAULT 0,
		decay_score REAL DEFAULT 1.0,
		embedding BLOB -- 384-dim 8-bit quantized
	);

	CREATE INDEX IF NOT EXISTS idx_entities_type ON entities(type);
	CREATE INDEX IF NOT EXISTS idx_entities_project ON entities(project);
	CREATE INDEX IF NOT EXISTS idx_entities_decay ON entities(decay_score);
	CREATE INDEX IF NOT EXISTS idx_entities_updated ON entities(updated_at);
	`

	_, err := s.db.ExecContext(ctx, schema)
	return err
}

// Store saves an entity.
func (s *EntityStore) Store(ctx context.Context, entity *Entity) error {
	metadataJSON, _ := json.Marshal(entity.Metadata)
	tagsJSON, _ := json.Marshal(entity.Tags)

	// Serialize embedding to bytes
	embeddingBytes := serializeEmbedding(entity.Embedding)

	query := `
		INSERT INTO entities (id, type, content, metadata, project, tags, created_at, updated_at, access_count, decay_score, embedding)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			content = excluded.content,
			metadata = excluded.metadata,
			project = excluded.project,
			tags = excluded.tags,
			updated_at = excluded.updated_at,
			access_count = excluded.access_count,
			decay_score = excluded.decay_score,
			embedding = excluded.embedding
	`

	_, err := s.db.ExecContext(ctx, query,
		entity.ID, entity.Type, entity.Content,
		string(metadataJSON), entity.Project, string(tagsJSON),
		entity.CreatedAt, entity.UpdatedAt,
		entity.AccessCount, entity.DecayScore, embeddingBytes,
	)
	return err
}

// serializeEmbedding converts []float32 to []byte
func serializeEmbedding(embedding []float32) []byte {
	if len(embedding) == 0 {
		return nil
	}
	buf := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// deserializeEmbedding converts []byte to []float32
func deserializeEmbedding(data []byte) []float32 {
	if len(data) == 0 {
		return nil
	}
	if len(data)%4 != 0 {
		return nil
	}
	embedding := make([]float32, len(data)/4)
	for i := 0; i < len(embedding); i++ {
		bits := binary.LittleEndian.Uint32(data[i*4:])
		embedding[i] = math.Float32frombits(bits)
	}
	return embedding
}

// Get retrieves an entity by ID.
func (s *EntityStore) Get(ctx context.Context, id string) (*Entity, error) {
	query := `
		SELECT id, type, content, metadata, project, tags, created_at, updated_at, access_count, decay_score, embedding
		FROM entities WHERE id = ?
	`

	row := s.db.QueryRowContext(ctx, query, id)
	return s.scanEntity(row)
}

// Update updates an entity.
func (s *EntityStore) Update(ctx context.Context, entity *Entity) error {
	return s.Store(ctx, entity)
}

// Delete deletes an entity.
func (s *EntityStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM entities WHERE id = ?`, id)
	return err
}

// List lists entities with filtering.
func (s *EntityStore) List(ctx context.Context, filter ListFilter) ([]*Entity, error) {
	var conditions []string
	var args []any

	if len(filter.Types) > 0 {
		placeholders := strings.Repeat("?,", len(filter.Types))
		conditions = append(conditions, "type IN ("+placeholders[:len(placeholders)-1]+")")
		for _, t := range filter.Types {
			args = append(args, t)
		}
	}

	if len(filter.Projects) > 0 {
		placeholders := strings.Repeat("?,", len(filter.Projects))
		conditions = append(conditions, "project IN ("+placeholders[:len(placeholders)-1]+")")
		for _, p := range filter.Projects {
			args = append(args, p)
		}
	}

	if len(filter.Tags) > 0 {
		for _, tag := range filter.Tags {
			conditions = append(conditions, "tags LIKE ?")
			args = append(args, "%"+tag+"%")
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := filter.Offset

	query := fmt.Sprintf(`
		SELECT id, type, content, metadata, project, tags, created_at, updated_at, access_count, decay_score, embedding
		FROM entities %s
		ORDER BY updated_at DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entities []*Entity
	for rows.Next() {
		entity, err := s.scanEntity(rows)
		if err != nil {
			return nil, err
		}
		entities = append(entities, entity)
	}

	return entities, nil
}

// SearchFTS performs full-text search on entities.
func (s *EntityStore) SearchFTS(ctx context.Context, query string, limit int) ([]*Entity, error) {
	// Using SQLite FTS5 virtual table would be better, but for now use LIKE
	searchQuery := `
		SELECT id, type, content, metadata, project, tags, created_at, updated_at, access_count, decay_score, embedding
		FROM entities
		WHERE content LIKE ?
		ORDER BY decay_score DESC, updated_at DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, searchQuery, "%"+query+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entities []*Entity
	for rows.Next() {
		entity, err := s.scanEntity(rows)
		if err != nil {
			return nil, err
		}
		entities = append(entities, entity)
	}

	return entities, nil
}

// GetByProject gets entities by project.
func (s *EntityStore) GetByProject(ctx context.Context, project string, limit int) ([]*Entity, error) {
	query := `
		SELECT id, type, content, metadata, project, tags, created_at, updated_at, access_count, decay_score, embedding
		FROM entities
		WHERE project = ?
		ORDER BY decay_score DESC, updated_at DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entities []*Entity
	for rows.Next() {
		entity, err := s.scanEntity(rows)
		if err != nil {
			return nil, err
		}
		entities = append(entities, entity)
	}

	return entities, nil
}

// UpdateAccessCount increments the access count and updates decay score.
func (s *EntityStore) UpdateAccessCount(ctx context.Context, id string, decayScore float64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE entities SET access_count = access_count + 1, decay_score = ?, updated_at = ?
		WHERE id = ?
	`, decayScore, time.Now(), id)
	return err
}

// Prune removes entities below the decay threshold.
func (s *EntityStore) Prune(ctx context.Context, threshold float64) (int, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM entities WHERE decay_score < ?`, threshold)
	if err != nil {
		return 0, err
	}
	count, _ := result.RowsAffected()
	return int(count), nil
}

// Count returns the total number of entities.
func (s *EntityStore) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entities`).Scan(&count)
	return count, err
}

func (s *EntityStore) scanEntity(scanner interface {
	Scan(dest ...any) error
}) (*Entity, error) {
	var entity Entity
	var metadataJSON, tagsJSON string
	var embeddingBytes []byte

	err := scanner.Scan(
		&entity.ID, &entity.Type, &entity.Content,
		&metadataJSON, &entity.Project, &tagsJSON,
		&entity.CreatedAt, &entity.UpdatedAt,
		&entity.AccessCount, &entity.DecayScore, &embeddingBytes,
	)
	if err != nil {
		return nil, err
	}

	if metadataJSON != "" {
		json.Unmarshal([]byte(metadataJSON), &entity.Metadata)
	}
	if tagsJSON != "" {
		json.Unmarshal([]byte(tagsJSON), &entity.Tags)
	}
	entity.Embedding = deserializeEmbedding(embeddingBytes)

	return &entity, nil
}

// RelationshipStore handles relationship persistence.
type RelationshipStore struct {
	db *sql.DB
}

// NewRelationshipStore creates a new relationship store.
func NewRelationshipStore(db *sql.DB) *RelationshipStore {
	return &RelationshipStore{db: db}
}

// Initialize creates the relationship tables.
func (s *RelationshipStore) Initialize(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS relationships (
		id TEXT PRIMARY KEY,
		source_id TEXT NOT NULL,
		target_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		weight REAL DEFAULT 1.0,
		created_at DATETIME NOT NULL,
		FOREIGN KEY (source_id) REFERENCES entities(id),
		FOREIGN KEY (target_id) REFERENCES entities(id)
	);

	CREATE INDEX IF NOT EXISTS idx_relationships_source ON relationships(source_id);
	CREATE INDEX IF NOT EXISTS idx_relationships_target ON relationships(target_id);
	CREATE INDEX IF NOT EXISTS idx_relationships_kind ON relationships(kind);
	`

	_, err := s.db.ExecContext(ctx, schema)
	return err
}

// Link creates a relationship.
func (s *RelationshipStore) Link(ctx context.Context, rel *Relationship) error {
	query := `
		INSERT INTO relationships (id, source_id, target_id, kind, weight, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			kind = excluded.kind,
			weight = excluded.weight
	`

	_, err := s.db.ExecContext(ctx, query,
		rel.ID, rel.SourceID, rel.TargetID, rel.Kind, rel.Weight, rel.CreatedAt,
	)
	return err
}

// Unlink removes a relationship.
func (s *RelationshipStore) Unlink(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM relationships WHERE id = ?`, id)
	return err
}

// GetRelationships gets all relationships for an entity.
func (s *RelationshipStore) GetRelationships(ctx context.Context, entityID string) ([]*Relationship, error) {
	query := `
		SELECT id, source_id, target_id, kind, weight, created_at
		FROM relationships
		WHERE source_id = ? OR target_id = ?
	`

	rows, err := s.db.QueryContext(ctx, query, entityID, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var relationships []*Relationship
	for rows.Next() {
		var rel Relationship
		err := rows.Scan(&rel.ID, &rel.SourceID, &rel.TargetID, &rel.Kind, &rel.Weight, &rel.CreatedAt)
		if err != nil {
			return nil, err
		}
		relationships = append(relationships, &rel)
	}

	return relationships, nil
}

// GetOutgoing gets outgoing relationships.
func (s *RelationshipStore) GetOutgoing(ctx context.Context, entityID string) ([]*Relationship, error) {
	query := `
		SELECT id, source_id, target_id, kind, weight, created_at
		FROM relationships
		WHERE source_id = ?
	`

	rows, err := s.db.QueryContext(ctx, query, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var relationships []*Relationship
	for rows.Next() {
		var rel Relationship
		err := rows.Scan(&rel.ID, &rel.SourceID, &rel.TargetID, &rel.Kind, &rel.Weight, &rel.CreatedAt)
		if err != nil {
			return nil, err
		}
		relationships = append(relationships, &rel)
	}

	return relationships, nil
}

// GetIncoming gets incoming relationships.
func (s *RelationshipStore) GetIncoming(ctx context.Context, entityID string) ([]*Relationship, error) {
	query := `
		SELECT id, source_id, target_id, kind, weight, created_at
		FROM relationships
		WHERE target_id = ?
	`

	rows, err := s.db.QueryContext(ctx, query, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var relationships []*Relationship
	for rows.Next() {
		var rel Relationship
		err := rows.Scan(&rel.ID, &rel.SourceID, &rel.TargetID, &rel.Kind, &rel.Weight, &rel.CreatedAt)
		if err != nil {
			return nil, err
		}
		relationships = append(relationships, &rel)
	}

	return relationships, nil
}