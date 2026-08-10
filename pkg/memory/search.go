package memory

import (
	"context"
	"math"
	"sort"
)

// SearchEngine implements the OMEGA hybrid retrieval pipeline.
type SearchEngine struct {
	entityStore       *EntityStore
	relationshipStore *RelationshipStore
	config            *Config
	vectorIndex       *VectorIndex
}

// NewSearchEngine creates a new search engine.
func NewSearchEngine(entityStore *EntityStore, relationshipStore *RelationshipStore, config *Config) *SearchEngine {
	return &SearchEngine{
		entityStore:       entityStore,
		relationshipStore: relationshipStore,
		config:            config,
		vectorIndex:       NewVectorIndex(config.EmbeddingDim),
	}
}

// Search performs hybrid retrieval: vector + FTS + graph traversal.
func (e *SearchEngine) Search(ctx context.Context, query *SearchQuery) ([]*SearchResult, error) {
	if query.Limit <= 0 {
		query.Limit = 10
	}
	if query.MinScore < 0 {
		query.MinScore = 0.0
	}
	if query.MaxHops < 0 {
		query.MaxHops = 2
	}

	var allResults []*SearchResult

	// 1. Vector similarity search
	if query.Text != "" && e.vectorIndex != nil {
		vectorResults, err := e.vectorSearch(ctx, query)
		if err == nil {
			for _, r := range vectorResults {
				r.Source = "vector"
				allResults = append(allResults, r)
			}
		}
	}

	// 2. Full-text search
	if query.Text != "" {
		ftsResults, err := e.ftsSearch(ctx, query)
		if err == nil {
			for _, r := range ftsResults {
				r.Source = "fts"
				allResults = append(allResults, r)
			}
		}
	}

	// 3. Graph traversal (spreading activation)
	if query.MaxHops > 0 {
		graphResults, err := e.graphSearch(ctx, query)
		if err == nil {
			for _, r := range graphResults {
				r.Source = "graph"
				allResults = append(allResults, r)
			}
		}
	}

	// 4. Hybrid fusion and deduplication
	fused := e.fuseResults(allResults, query)

	// 5. Apply minimum score threshold
	filtered := make([]*SearchResult, 0)
	for _, r := range fused {
		if r.Score >= query.MinScore {
			filtered = append(filtered, r)
		}
	}

	// 6. Sort by score descending
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Score > filtered[j].Score
	})

	// 7. Limit results
	if len(filtered) > query.Limit {
		filtered = filtered[:query.Limit]
	}

	return filtered, nil
}

// vectorSearch performs vector similarity search.
func (e *SearchEngine) vectorSearch(ctx context.Context, query *SearchQuery) ([]*SearchResult, error) {
	if e.vectorIndex == nil {
		return nil, nil
	}

	// Generate embedding for query
	queryEmbedding, err := e.generateEmbedding(ctx, query.Text)
	if err != nil {
		return nil, err
	}

	// Search vector index
	candidates := e.vectorIndex.Search(queryEmbedding, query.Limit*3)

	var results []*SearchResult
	for _, candidate := range candidates {
		entity, err := e.entityStore.Get(ctx, candidate.ID)
		if err != nil || entity == nil {
			continue
		}

		// Filter by type, project, tags
		if !e.matchesFilters(entity, query) {
			continue
		}

		// Compute cosine similarity
		score := cosineSimilarity(queryEmbedding, entity.Embedding)
		if score < query.MinScore {
			continue
		}

		results = append(results, &SearchResult{
			Entity: *entity,
			Score:  score * e.config.VectorWeight,
			Source: "vector",
		})
	}

	return results, nil
}

// ftsSearch performs full-text search.
func (e *SearchEngine) ftsSearch(ctx context.Context, query *SearchQuery) ([]*SearchResult, error) {
	entities, err := e.entityStore.SearchFTS(ctx, query.Text, query.Limit*3)
	if err != nil {
		return nil, err
	}

	var results []*SearchResult
	for _, entity := range entities {
		if !e.matchesFilters(entity, query) {
			continue
		}

		// Simple BM25-like scoring based on term frequency
		score := e.computeFTSScore(entity.Content, query.Text)
		score = score * e.config.FTSWeight

		results = append(results, &SearchResult{
			Entity: *entity,
			Score:  score,
			Source: "fts",
		})
	}

	return results, nil
}

// graphSearch performs graph traversal (spreading activation).
func (e *SearchEngine) graphSearch(ctx context.Context, query *SearchQuery) ([]*SearchResult, error) {
	// Find seed entities from vector/FTS results
	seedEntities := e.findSeedEntities(ctx, query)
	if len(seedEntities) == 0 {
		return nil, nil
	}

	visited := make(map[string]bool)
	var results []*SearchResult

	for _, seed := range seedEntities {
		if visited[seed.ID] {
			continue
		}
		visited[seed.ID] = true

		// Spreading activation
		spread := e.spreadActivation(ctx, seed, query.MaxHops, visited, query)
		results = append(results, spread...)
	}

	return results, nil
}

// spreadActivation performs spreading activation from a seed entity.
func (e *SearchEngine) spreadActivation(ctx context.Context, seed *Entity, maxHops int, visited map[string]bool, query *SearchQuery) []*SearchResult {
	var results []*SearchResult
	currentLevel := map[string]float64{seed.ID: 1.0}

	for hop := 0; hop < maxHops; hop++ {
		nextLevel := make(map[string]float64)

		for entityID, activation := range currentLevel {
			if activation < 0.1 { // Threshold
				continue
			}

			// Get outgoing relationships
			relationships, err := e.relationshipStore.GetOutgoing(ctx, entityID)
			if err != nil {
				continue
			}

			for _, rel := range relationships {
				if visited[rel.TargetID] {
					continue
				}

				// Compute activation spread
				spreadActivation := activation * rel.Weight * 0.8 // Decay factor
				if spreadActivation > nextLevel[rel.TargetID] {
					nextLevel[rel.TargetID] = spreadActivation
				}
			}
		}

		// Fetch entities for next level
		for entityID, activation := range nextLevel {
			if visited[entityID] {
				continue
			}
			visited[entityID] = true

			entity, err := e.entityStore.Get(ctx, entityID)
			if err != nil || entity == nil {
				continue
			}

			if !e.matchesFilters(entity, query) {
				continue
			}

			score := activation * e.config.GraphWeight
			results = append(results, &SearchResult{
				Entity: *entity,
				Score:  score,
				Source: "graph",
				Path:   []string{seed.ID, entityID},
			})
		}

		currentLevel = nextLevel
		if len(currentLevel) == 0 {
			break
		}
	}

	return results
}

// findSeedEntities finds initial entities for graph traversal.
func (e *SearchEngine) findSeedEntities(ctx context.Context, query *SearchQuery) []*Entity {
	// Use vector search results as seeds
	vectorResults, _ := e.vectorSearch(ctx, query)
	var seeds []*Entity
	for _, r := range vectorResults {
		seeds = append(seeds, &r.Entity)
		if len(seeds) >= 5 {
			break
		}
	}
	return seeds
}

// matchesFilters checks if an entity matches the query filters.
func (e *SearchEngine) matchesFilters(entity *Entity, query *SearchQuery) bool {
	if len(query.Types) > 0 {
		match := false
		for _, t := range query.Types {
			if entity.Type == t {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	if len(query.Projects) > 0 {
		match := false
		for _, p := range query.Projects {
			if entity.Project == p {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	if len(query.Tags) > 0 {
		for _, qtag := range query.Tags {
			match := false
			for _, etag := range entity.Tags {
				if etag == qtag {
					match = true
					break
				}
			}
			if !match {
				return false
			}
		}
	}

	return true
}

// computeFTSScore computes a simple FTS score.
func (e *SearchEngine) computeFTSScore(content, query string) float64 {
	// Simple term frequency scoring
	contentLower := content
	queryLower := query

	count := 0
	pos := 0
	for {
		idx := findSubstring(contentLower[pos:], queryLower)
		if idx == -1 {
			break
		}
		count++
		pos += idx + len(queryLower)
	}

	if count == 0 {
		return 0
	}

	// Normalize by content length
	score := float64(count) / float64(len(contentLower)/100+1)
	return math.Min(score, 1.0)
}

// fuseResults combines results from multiple sources.
func (e *SearchEngine) fuseResults(results []*SearchResult, query *SearchQuery) []*SearchResult {
	// Deduplicate by entity ID, keeping highest score
	scoreMap := make(map[string]*SearchResult)

	for _, r := range results {
		existing, ok := scoreMap[r.Entity.ID]
		if !ok || r.Score > existing.Score {
			scoreMap[r.Entity.ID] = r
		}
	}

	fused := make([]*SearchResult, 0, len(scoreMap))
	for _, r := range scoreMap {
		fused = append(fused, r)
	}

	return fused
}

// generateEmbedding generates an embedding for text.
// This is a placeholder - real implementation uses ONNX Runtime.
func (e *SearchEngine) generateEmbedding(ctx context.Context, text string) ([]float32, error) {
	// Placeholder: return random embedding for testing
	embedding := make([]float32, e.config.EmbeddingDim)
	for i := range embedding {
		embedding[i] = float32(i) / float32(e.config.EmbeddingDim)
	}
	return embedding, nil
}

// cosineSimilarity computes cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// findSubstring finds a substring (simple implementation).
func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// VectorIndex is a simple in-memory vector index.
type VectorIndex struct {
	dim       int
	vectors   map[string][]float32
	entityIDs []string
}

// NewVectorIndex creates a new vector index.
func NewVectorIndex(dim int) *VectorIndex {
	return &VectorIndex{
		dim:       dim,
		vectors:   make(map[string][]float32),
		entityIDs: make([]string, 0),
	}
}

// Add adds a vector to the index.
func (v *VectorIndex) Add(entityID string, vector []float32) {
	v.vectors[entityID] = vector
	v.entityIDs = append(v.entityIDs, entityID)
}

// Search searches for similar vectors.
func (v *VectorIndex) Search(query []float32, k int) []VectorCandidate {
	type scoredEntity struct {
		id    string
		score float64
	}

	var scored []scoredEntity
	for _, id := range v.entityIDs {
		vec := v.vectors[id]
		score := cosineSimilarity(query, vec)
		scored = append(scored, scoredEntity{id: id, score: score})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	var candidates []VectorCandidate
	for i := 0; i < min(k, len(scored)); i++ {
		candidates = append(candidates, VectorCandidate{
			ID:    scored[i].id,
			Score: scored[i].score,
		})
	}

	return candidates
}

// VectorCandidate is a candidate from vector search.
type VectorCandidate struct {
	ID    string
	Score float64
}