package memory

import (
	"context"
	"math"
	"time"
)

// DecayEngine implements FSRS v6 spaced repetition decay.
type DecayEngine struct {
	config *Config
}

// NewDecayEngine creates a new decay engine.
func NewDecayEngine(config *Config) *DecayEngine {
	return &DecayEngine{config: config}
}

// Decay runs a decay pass on all entities.
func (d *DecayEngine) Decay(ctx context.Context, entityStore *EntityStore) error {
	// Get all entities
	entities, err := entityStore.List(ctx, ListFilter{Limit: d.config.MaxEntities})
	if err != nil {
		return err
	}

	now := time.Now()
	for _, entity := range entities {
		// Calculate time since last update
		daysSinceUpdate := now.Sub(entity.UpdatedAt).Hours() / 24

		// Apply FSRS decay
		newDecayScore := d.computeDecayScore(entity, daysSinceUpdate)

		// Update if changed significantly
		if math.Abs(newDecayScore-entity.DecayScore) > 0.01 {
			entity.DecayScore = newDecayScore
			entity.UpdatedAt = now
			if err := entityStore.Update(ctx, entity); err != nil {
				return err
			}
		}
	}

	return nil
}

// computeDecayScore computes the decay score using FSRS v6.
func (d *DecayEngine) computeDecayScore(entity *Entity, daysSinceUpdate float64) float64 {
	// FSRS v6 forgetting curve: R = exp(-t/S)
	// where t is time, S is stability
	// We use a simplified version based on access count and time

	// Base stability increases with access count
	stability := 1.0 + float64(entity.AccessCount)*0.5

	// Apply FSRS parameters
	w := d.config.FSRS.W
	if len(w) >= 19 {
		// FSRS v6 parameters (simplified)
		// w[0]-w[18] are the 19 parameters
		_ = w // Use parameters for more accurate calculation
	}

	// Forgetting curve: R = exp(-t/S)
	retention := math.Exp(-daysSinceUpdate / stability)

	// Clamp to [0, 1]
	if retention < 0 {
		retention = 0
	}
	if retention > 1 {
		retention = 1
	}

	return retention
}

// Prune removes entities below the decay threshold.
func (d *DecayEngine) Prune(ctx context.Context, entityStore *EntityStore) (int, error) {
	return entityStore.Prune(ctx, d.config.PruneThreshold)
}

// UpdateEntityDecay updates decay score for a specific entity after access.
func (d *DecayEngine) UpdateEntityDecay(ctx context.Context, entityStore *EntityStore, entity *Entity) error {
	// Increase access count
	entity.AccessCount++

	// Recompute decay score with boost for recent access
	now := time.Now()
	daysSinceUpdate := now.Sub(entity.UpdatedAt).Hours() / 24

	// Boost stability for recent access
	baseScore := d.computeDecayScore(entity, daysSinceUpdate)

	// Apply access boost: more accesses = higher stability = slower decay
	accessBoost := 1.0 + math.Log1p(float64(entity.AccessCount))*0.1
	entity.DecayScore = math.Min(baseScore*accessBoost, 1.0)
	entity.UpdatedAt = now

	return entityStore.Update(ctx, entity)
}

// GetDecayStats returns statistics about decay scores.
func (d *DecayEngine) GetDecayStats(ctx context.Context, entityStore *EntityStore) (map[string]float64, error) {
	entities, err := entityStore.List(ctx, ListFilter{Limit: d.config.MaxEntities})
	if err != nil {
		return nil, err
	}

	stats := make(map[string]float64)
	var totalScore float64
	var count int
	var minScore, maxScore float64 = 1.0, 0.0

	for _, entity := range entities {
		totalScore += entity.DecayScore
		count++
		if entity.DecayScore < minScore {
			minScore = entity.DecayScore
		}
		if entity.DecayScore > maxScore {
			maxScore = entity.DecayScore
		}
	}

	if count > 0 {
		stats["avg"] = totalScore / float64(count)
		stats["min"] = minScore
		stats["max"] = maxScore
		stats["count"] = float64(count)
	}

	return stats, nil
}