package sarif

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"
)

// ─── Deduplication Engine ─────────────────────────────────────────────

// DedupConfig controls the deduplication behavior.
type DedupConfig struct {
	// TimeWindow is the window for time-based dedup (default: 24h).
	TimeWindow time.Duration `yaml:"time_window"`

	// CrossTool enables dedup across different tools for the same rule.
	CrossTool bool `yaml:"cross_tool"`

	// TargetReduction is the target dedup ratio (0.0 - 1.0).
	// 0.3 means target 30% reduction.
	TargetReduction float64 `yaml:"target_reduction"`
}

func (c *DedupConfig) applyDefaults() {
	if c.TimeWindow == 0 {
		c.TimeWindow = 24 * time.Hour
	}
	if c.TargetReduction == 0 {
		c.TargetReduction = 0.3
	}
}

// DedupResult holds the deduplication results.
type DedupResult struct {
	Original  int            `json:"original"`
	Deduped   int            `json:"deduped"`
	Removed   int            `json:"removed"`
	Ratio     float64        `json:"ratio"` // removal ratio
	Kept      []Finding      `json:"kept"`
	Duplicates []Duplicate   `json:"duplicates"`
	ByFingerprint map[string]int `json:"by_fingerprint"`
}

// Duplicate records a finding that was deduplicated.
type Duplicate struct {
	Original   Finding `json:"original"`
	Duplicate  Finding `json:"duplicate"`
	Reason     string  `json:"reason"` // "fingerprint", "time_window", "cross_tool"
	Similarity float64 `json:"similarity"`
}

// ─── Fingerprint-Based Dedup ──────────────────────────────────────────

// FingerprintDedup performs fingerprint-based deduplication.
// It groups findings by SHA256(rule_id + file_path + line) and keeps only one per group.
type FingerprintDedup struct {
	config DedupConfig
	logger *slog.Logger
}

// NewFingerprintDedup creates a new fingerprint deduplication engine.
func NewFingerprintDedup(config DedupConfig, logger *slog.Logger) *FingerprintDedup {
	config.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &FingerprintDedup{
		config: config,
		logger: logger.With("component", "fingerprint-dedup"),
	}
}

// Deduplicate removes duplicate findings based on fingerprints.
func (d *FingerprintDedup) Deduplicate(findings []Finding) DedupResult {
	if len(findings) == 0 {
		return DedupResult{}
	}

	// Group by fingerprint
	groups := make(map[string][]Finding)
	for _, f := range findings {
		fp := f.Fingerprint
		if fp == "" {
			fp = GenerateFingerprint(f.RuleID, f.File, f.Line)
		}
		groups[fp] = append(groups[fp], f)
	}

	result := DedupResult{
		Original:      len(findings),
		ByFingerprint: make(map[string]int),
	}

	// Keep one from each group
	for fp, group := range groups {
		result.ByFingerprint[fp] = len(group)
		result.Kept = append(result.Kept, group[0])

		// Record duplicates
		for i := 1; i < len(group); i++ {
			result.Duplicates = append(result.Duplicates, Duplicate{
				Original:   group[0],
				Duplicate:  group[i],
				Reason:     "fingerprint",
				Similarity: 1.0,
			})
		}
	}

	result.Deduped = len(result.Kept)
	result.Removed = result.Original - result.Deduped
	if result.Original > 0 {
		result.Ratio = float64(result.Removed) / float64(result.Original)
	}

	d.logger.Info("fingerprint dedup complete",
		"original", result.Original,
		"deduped", result.Deduped,
		"removed", result.Removed,
		"ratio", fmt.Sprintf("%.1f%%", result.Ratio*100),
	)

	return result
}

// ─── Time-Window Dedup ────────────────────────────────────────────────

// TimeWindowDedup performs time-based deduplication.
// It removes findings that appear within the time window for the same rule+file+line.
type TimeWindowDedup struct {
	config DedupConfig
	logger *slog.Logger
}

// NewTimeWindowDedup creates a new time-window deduplication engine.
func NewTimeWindowDedup(config DedupConfig, logger *slog.Logger) *TimeWindowDedup {
	config.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &TimeWindowDedup{
		config: config,
		logger: logger.With("component", "time-window-dedup"),
	}
}

// Deduplicate removes findings that appear within the time window.
func (d *TimeWindowDedup) Deduplicate(findings []Finding, reportTime time.Time) DedupResult {
	if len(findings) == 0 {
		return DedupResult{}
	}

	// Group by normalized key (rule_id + file + line)
	groups := make(map[string][]Finding)
	for _, f := range findings {
		key := fmt.Sprintf("%s:%s:%d", f.RuleID, f.File, f.Line)
		groups[key] = append(groups[key], f)
	}

	result := DedupResult{
		Original:      len(findings),
		ByFingerprint: make(map[string]int),
	}

	for _, group := range groups {
		if len(group) == 1 {
			result.Kept = append(result.Kept, group[0])
			continue
		}

		// Sort by time and keep the most recent within the window
		kept := group[0]
		lastWasElse := false
		for i := 1; i < len(group); i++ {
			// Use finding timestamps for time-window comparison
			timeDiff := group[i].Timestamp.Sub(kept.Timestamp)
			if timeDiff >= 0 && timeDiff <= d.config.TimeWindow {
				result.Duplicates = append(result.Duplicates, Duplicate{
					Original:   kept,
					Duplicate:  group[i],
					Reason:     "time_window",
					Similarity: 0.9,
				})
				lastWasElse = false
			} else {
				result.Kept = append(result.Kept, group[i])
				kept = group[i]
				lastWasElse = true
			}
		}
		if !lastWasElse {
			result.Kept = append(result.Kept, kept)
		}
	}

	result.Deduped = len(result.Kept)
	result.Removed = result.Original - result.Deduped
	if result.Original > 0 {
		result.Ratio = float64(result.Removed) / float64(result.Original)
	}

	d.logger.Info("time-window dedup complete",
		"original", result.Original,
		"deduped", result.Deduped,
		"removed", result.Removed,
		"ratio", fmt.Sprintf("%.1f%%", result.Ratio*100),
	)

	return result
}

// ─── Cross-Tool Dedup ────────────────────────────────────────────────

// CrossToolDedup deduplicates findings across different tools that detect the same issue.
type CrossToolDedup struct {
	config DedupConfig
	logger *slog.Logger
}

// NewCrossToolDedup creates a new cross-tool deduplication engine.
func NewCrossToolDedup(config DedupConfig, logger *slog.Logger) *CrossToolDedup {
	config.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &CrossToolDedup{
		config: config,
		logger: logger.With("component", "cross-tool-dedup"),
	}
}

// Deduplicate removes duplicate findings across different tools.
// When multiple tools report the same rule for the same file+line, keep the one
// from the more trusted tool (based on prefix priority).
func (d *CrossToolDedup) Deduplicate(findings []Finding) DedupResult {
	if !d.config.CrossTool {
		return DedupResult{
			Original: len(findings),
			Deduped:  len(findings),
			Kept:     findings,
		}
	}

	// Group by normalized rule + file + line (including tool prefix to avoid cross-tool merging)
	groups := make(map[string][]Finding)
	for _, f := range findings {
		key := fmt.Sprintf("%s:%s:%s:%d", f.Normalized.Prefix, f.Normalized.Rule, f.File, f.Line)
		groups[key] = append(groups[key], f)
	}

	result := DedupResult{
		Original:      len(findings),
		ByFingerprint: make(map[string]int),
	}

	// Tool trust priority (lower = more trusted)
	trustPriority := map[RulePrefix]int{
		PrefixDependabot:    1,
		PrefixASTComplexity: 2,
		PrefixADRConflict:   3,
		PrefixStandardsGap:  4,
		PrefixSARIF:         5,
	}

	for _, group := range groups {
		if len(group) == 1 {
			result.Kept = append(result.Kept, group[0])
			continue
		}

		// Find the most trusted tool's finding
		best := group[0]
		bestPriority := trustPriority[best.Normalized.Prefix]
		for _, f := range group[1:] {
			p := trustPriority[f.Normalized.Prefix]
			if p < bestPriority {
				best = f
				bestPriority = p
			}
		}

		result.Kept = append(result.Kept, best)

		// Record duplicates
		for _, f := range group {
			if f.Fingerprint != best.Fingerprint {
				result.Duplicates = append(result.Duplicates, Duplicate{
					Original:   best,
					Duplicate:  f,
					Reason:     "cross_tool",
					Similarity: 0.85,
				})
			}
		}
	}

	result.Deduped = len(result.Kept)
	result.Removed = result.Original - result.Deduped
	if result.Original > 0 {
		result.Ratio = float64(result.Removed) / float64(result.Original)
	}

	d.logger.Info("cross-tool dedup complete",
		"original", result.Original,
		"deduped", result.Deduped,
		"removed", result.Removed,
		"ratio", fmt.Sprintf("%.1f%%", result.Ratio*100),
	)

	return result
}

// ─── Composite Deduplication Pipeline ─────────────────────────────────

// DedupPipeline chains multiple deduplication strategies.
type DedupPipeline struct {
	fingerprint *FingerprintDedup
	timeWindow  *TimeWindowDedup
	crossTool   *CrossToolDedup
	config      DedupConfig
	logger      *slog.Logger
}

// NewDedupPipeline creates a deduplication pipeline with all strategies.
func NewDedupPipeline(config DedupConfig, logger *slog.Logger) *DedupPipeline {
	config.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &DedupPipeline{
		fingerprint: NewFingerprintDedup(config, logger),
		timeWindow:  NewTimeWindowDedup(config, logger),
		crossTool:   NewCrossToolDedup(config, logger),
		config:      config,
		logger:      logger.With("component", "dedup-pipeline"),
	}
}

// Deduplicate runs all deduplication strategies in sequence.
func (p *DedupPipeline) Deduplicate(findings []Finding, reportTime time.Time) DedupResult {
	if len(findings) == 0 {
		return DedupResult{}
	}

	startCount := len(findings)

	// Stage 1: Cross-tool dedup (before fingerprint to merge tool-specific findings)
	crossResult := p.crossTool.Deduplicate(findings)

	// Stage 2: Fingerprint dedup
	fingerResult := p.fingerprint.Deduplicate(crossResult.Kept)

	// Stage 3: Time-window dedup
	timeResult := p.timeWindow.Deduplicate(fingerResult.Kept, reportTime)

	// Merge all duplicates
	allDuplicates := make([]Duplicate, 0)
	allDuplicates = append(allDuplicates, crossResult.Duplicates...)
	allDuplicates = append(allDuplicates, fingerResult.Duplicates...)
	allDuplicates = append(allDuplicates, timeResult.Duplicates...)

	result := DedupResult{
		Original:      startCount,
		Deduped:       len(timeResult.Kept),
		Removed:       startCount - len(timeResult.Kept),
		Kept:          timeResult.Kept,
		Duplicates:    allDuplicates,
		ByFingerprint: fingerResult.ByFingerprint,
	}

	if result.Original > 0 {
		result.Ratio = float64(result.Removed) / float64(result.Original)
	}

	p.logger.Info("dedup pipeline complete",
		"original", result.Original,
		"deduped", result.Deduped,
		"removed", result.Removed,
		"ratio", fmt.Sprintf("%.1f%%", result.Ratio*100),
		"target_reduction", fmt.Sprintf("%.1f%%", p.config.TargetReduction*100),
		"target_met", result.Ratio >= p.config.TargetReduction,
	)

	return result
}

// GenerateDedupFingerprint creates a SHA256 fingerprint for a finding.
func GenerateDedupFingerprint(ruleID, filePath string, line int) string {
	h := sha256.New()
	h.Write([]byte(ruleID))
	h.Write([]byte{0})
	h.Write([]byte(filePath))
	h.Write([]byte{0})
	h.Write([]byte(fmt.Sprintf("%d", line)))
	return fmt.Sprintf("%x", h.Sum(nil))
}
