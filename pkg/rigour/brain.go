package rigour

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"
)

// Pattern represents a learned code pattern with strength tracking.
type Pattern struct {
	ID          string
	Name        string
	Description string
	Strength    float64    // 0.0 - 1.0, grows with reinforcement
	Decay       float64    // decay rate per cycle
	LastSeen    time.Time
	CreatedAt   time.Time
	Metadata    map[string]any
}

// HardRule is a non-negotiable constraint derived from patterns
// that have exceeded the strength threshold.
type HardRule struct {
	PatternID string
	Rule      string
	Threshold float64 // strength threshold that made it hard
	CreatedAt time.Time
}

// Brain provides pattern queries backed by @rigour-labs/core.
// Patterns represent recurring code structures that the system learns
// over time. Strong patterns (> 0.9 strength) become hard rules.
type Brain struct {
	mu          sync.RWMutex
	patterns    map[string]*Pattern
	hardRules   map[string]*HardRule
	logger      *slog.Logger
	hardThresh  float64 // strength threshold for promoting to hard rule
	decayRate   float64 // default decay rate per cycle
	pruneThresh float64 // patterns below this strength are pruned
}

// BrainConfig configures the brain's learning parameters.
type BrainConfig struct {
	HardThreshold  float64 // default: 0.9
	DecayRate      float64 // default: 0.02
	PruneThreshold float64 // default: 0.1
}

// DefaultBrainConfig returns sensible defaults.
func DefaultBrainConfig() BrainConfig {
	return BrainConfig{
		HardThreshold:  0.9,
		DecayRate:      0.02,
		PruneThreshold: 0.1,
	}
}

// NewBrain creates a Brain with the given configuration.
func NewBrain(logger *slog.Logger, cfg BrainConfig) *Brain {
	return &Brain{
		patterns:    make(map[string]*Pattern),
		hardRules:   make(map[string]*HardRule),
		logger:      logger.With("component", "brain"),
		hardThresh:  cfg.HardThreshold,
		decayRate:   cfg.DecayRate,
		pruneThresh: cfg.PruneThreshold,
	}
}

// GetStrongPatterns returns all patterns with strength above the threshold.
// This is the getStrongPatterns query from @rigour-labs/core.
func (b *Brain) GetStrongPatterns(ctx context.Context, threshold float64) ([]Pattern, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var strong []Pattern
	for _, p := range b.patterns {
		if p.Strength >= threshold {
			strong = append(strong, *p)
		}
	}

	b.logger.Debug("getStrongPatterns",
		"threshold", threshold,
		"count", len(strong),
	)

	return strong, nil
}

// GetHardRules returns all rules that have been promoted from strong patterns.
// This is the getHardRules query from @rigour-labs/core.
func (b *Brain) GetHardRules(ctx context.Context) ([]HardRule, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	rules := make([]HardRule, 0, len(b.hardRules))
	for _, r := range b.hardRules {
		rules = append(rules, *r)
	}

	b.logger.Debug("getHardRules", "count", len(rules))
	return rules, nil
}

// Learn records or reinforces a pattern observation.
// If the pattern exists, its strength grows; otherwise a new pattern is created.
// Patterns that exceed the hard threshold are promoted to hard rules.
func (b *Brain) Learn(ctx context.Context, pattern Pattern) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()

	if existing, ok := b.patterns[pattern.ID]; ok {
		// Reinforce existing pattern: grow strength multiplicatively
		existing.Strength = b.growStrength(existing.Strength)
		existing.LastSeen = now

		b.logger.Debug("pattern reinforced",
			"id", pattern.ID,
			"strength", existing.Strength,
		)

		// Check for hard rule promotion
		if existing.Strength >= b.hardThresh {
			b.promoteToHardRule(existing)
		}

		return nil
	}

	// New pattern
	p := &pattern
	if p.Strength == 0 {
		p.Strength = 0.1 // initial strength for first observation
	}
	p.CreatedAt = now
	p.LastSeen = now
	if p.Decay == 0 {
		p.Decay = b.decayRate
	}

	b.patterns[p.ID] = p

	b.logger.Info("pattern learned",
		"id", p.ID,
		"name", p.Name,
		"strength", p.Strength,
	)

	return nil
}

// Decay reduces strength of all patterns by their decay rate.
// Patterns that fall below the prune threshold are removed.
// This should be called periodically (e.g., once per rigour cycle).
func (b *Brain) Decay(ctx context.Context) (pruned int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	pruned = 0
	now := time.Now()

	for id, p := range b.patterns {
		// Time-weighted decay: more time since last seen = more decay
		hoursSinceLastSeen := now.Sub(p.LastSeen).Hours()
		timeFactor := 1.0 + (hoursSinceLastSeen / 24.0) // increases over days

		p.Strength -= p.Decay * timeFactor
		if p.Strength < 0 {
			p.Strength = 0
		}

		// Prune weak patterns
		if p.Strength < b.pruneThresh {
			delete(b.patterns, id)
			pruned++

			b.logger.Debug("pattern pruned",
				"id", id,
				"strength", p.Strength,
			)
		}
	}

	b.logger.Info("decay cycle", "pruned", pruned, "remaining", len(b.patterns))
	return pruned, nil
}

// growStrength applies reinforcement growth. Uses a diminishing returns
// curve so patterns approach but don't trivially reach 1.0.
func (b *Brain) growStrength(current float64) float64 {
	// Logarithmic growth: harder to strengthen at higher values
	growth := 0.1 * (1.0 - current)
	newStrength := current + growth
	if newStrength > 1.0 {
		newStrength = 1.0
	}
	return math.Round(newStrength*1000) / 1000 // 3 decimal places
}

// promoteToHardRule creates a hard rule from a strong pattern.
func (b *Brain) promoteToHardRule(p *Pattern) {
	if _, exists := b.hardRules[p.ID]; exists {
		// Already a hard rule, update it
		b.hardRules[p.ID].Rule = p.Description
		b.hardRules[p.ID].Threshold = p.Strength
		return
	}

	b.hardRules[p.ID] = &HardRule{
		PatternID: p.ID,
		Rule:      p.Description,
		Threshold: p.Strength,
		CreatedAt: time.Now(),
	}

	b.logger.Info("pattern promoted to hard rule",
		"id", p.ID,
		"name", p.Name,
		"strength", p.Strength,
	)
}

// GetPattern returns a specific pattern by ID.
func (b *Brain) GetPattern(id string) (*Pattern, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	p, ok := b.patterns[id]
	if !ok {
		return nil, false
	}
	cp := *p
	return &cp, true
}

// ListPatterns returns all patterns (for diagnostics).
func (b *Brain) ListPatterns() []Pattern {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]Pattern, 0, len(b.patterns))
	for _, p := range b.patterns {
		out = append(out, *p)
	}
	return out
}

// Stats returns brain statistics.
type BrainStats struct {
	PatternCount int
	HardRuleCount int
	AvgStrength  float64
	MaxStrength  float64
}

// GetStats returns aggregate statistics about the brain.
func (b *Brain) GetStats() BrainStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	stats := BrainStats{
		PatternCount:  len(b.patterns),
		HardRuleCount: len(b.hardRules),
	}

	if len(b.patterns) > 0 {
		total := 0.0
		for _, p := range b.patterns {
			total += p.Strength
			if p.Strength > stats.MaxStrength {
				stats.MaxStrength = p.Strength
			}
		}
		stats.AvgStrength = total / float64(len(b.patterns))
	}

	return stats
}

// IsHardRule checks if a pattern ID has been promoted to a hard rule.
func (b *Brain) IsHardRule(patternID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.hardRules[patternID]
	return ok
}

// MatchRules returns hard rules relevant to the given file paths.
// Matches rules based on file extensions and patterns in the rule description.
func (b *Brain) MatchRules(ctx context.Context, filePaths []string) []HardRule {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var matched []HardRule
	for _, r := range b.hardRules {
		for _, path := range filePaths {
			if ruleMatchesPath(r.Rule, path) {
				matched = append(matched, *r)
				break
			}
		}
	}

	return matched
}

// ruleMatchesPath checks if a hard rule is relevant to a given file path.
// Uses file extension matching and keyword extraction from rule description.
func ruleMatchesPath(rule, path string) bool {
	// Extract file extension
	ext := ""
	if extIdx := strings.LastIndexByte(path, '.'); extIdx >= 0 {
		ext = path[extIdx:]
	}

	// Check if rule mentions this file extension
	if ext != "" && strings.Contains(strings.ToLower(rule), ext) {
		return true
	}

	// Extract meaningful keywords from rule (words > 3 chars)
	words := strings.Fields(rule)
	for _, word := range words {
		if len(word) > 3 && strings.Contains(strings.ToLower(path), strings.ToLower(word)) {
			return true
		}
	}

	return false
}
