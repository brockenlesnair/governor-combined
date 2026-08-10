package sarif

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

// ─── Edge Cases ───────────────────────────────────────────────────────

// EdgeCaseConfig controls how edge cases are handled.
type EdgeCaseConfig struct {
	// StalenessThresholdHours is the max age before blocking.
	StalenessThresholdHours float64 `yaml:"staleness_threshold_hours"`

	// StalenessMode controls behavior when data is stale.
	StalenessMode string `yaml:"staleness_mode"` // "block", "warn", "ignore"

	// MissingLocationStrategy controls how missing file locations are handled.
	MissingLocationStrategy string `yaml:"missing_location_strategy"` // "extract", "skip", "unknown"

	// RuleIDCollisionStrategy controls how rule ID collisions are resolved.
	RuleIDCollisionStrategy string `yaml:"rule_id_collision_strategy"` // "prefix", "suffix", "reject"

	// PhaseDispatchTimeout is the timeout for phase dispatch.
	PhaseDispatchTimeout time.Duration `yaml:"phase_dispatch_timeout"`
}

func (c *EdgeCaseConfig) applyDefaults() {
	if c.StalenessThresholdHours == 0 {
		c.StalenessThresholdHours = 24.0
	}
	if c.StalenessMode == "" {
		c.StalenessMode = "warn"
	}
	if c.MissingLocationStrategy == "" {
		c.MissingLocationStrategy = "extract"
	}
	if c.RuleIDCollisionStrategy == "" {
		c.RuleIDCollisionStrategy = "prefix"
	}
	if c.PhaseDispatchTimeout == 0 {
		c.PhaseDispatchTimeout = 30 * time.Second
	}
}

// ─── Staleness Edge Cases ─────────────────────────────────────────────

// StalenessCheck evaluates data staleness and applies the configured strategy.
type StalenessCheck struct {
	config EdgeCaseConfig
	logger *slog.Logger
}

// NewStalenessCheck creates a new staleness checker.
func NewStalenessCheck(config EdgeCaseConfig, logger *slog.Logger) *StalenessCheck {
	config.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &StalenessCheck{
		config: config,
		logger: logger.With("component", "staleness-check"),
	}
}

// StalenessResult holds the result of a staleness evaluation.
type StalenessResult struct {
	IsStale       bool    `json:"is_stale"`
	DataAgeHours  float64 `json:"data_age_hours"`
	ThresholdHours float64 `json:"threshold_hours"`
	Action        string  `json:"action"` // "allow", "warn", "block"
	Message       string  `json:"message"`
}

// Evaluate checks if data is stale and returns the appropriate action.
func (s *StalenessCheck) Evaluate(dataAgeHours float64) StalenessResult {
	result := StalenessResult{
		DataAgeHours:   dataAgeHours,
		ThresholdHours: s.config.StalenessThresholdHours,
	}

	if dataAgeHours <= s.config.StalenessThresholdHours {
		result.Action = "allow"
		result.Message = fmt.Sprintf("data is fresh (%.1fh old, threshold %.1fh)", dataAgeHours, s.config.StalenessThresholdHours)
		return result
	}

	result.IsStale = true

	switch s.config.StalenessMode {
	case "block":
		result.Action = "block"
		result.Message = fmt.Sprintf("data is stale (%.1fh old, threshold %.1fh), blocking", dataAgeHours, s.config.StalenessThresholdHours)
		s.logger.Error("stale data blocked",
			"data_age_hours", dataAgeHours,
			"threshold_hours", s.config.StalenessThresholdHours,
		)
	case "warn":
		result.Action = "warn"
		result.Message = fmt.Sprintf("data is stale (%.1fh old, threshold %.1fh), proceeding with warning", dataAgeHours, s.config.StalenessThresholdHours)
		s.logger.Warn("stale data warning",
			"data_age_hours", dataAgeHours,
			"threshold_hours", s.config.StalenessThresholdHours,
		)
	case "ignore":
		result.Action = "allow"
		result.Message = fmt.Sprintf("data is stale (%.1fh old, threshold %.1fh), ignoring staleness", dataAgeHours, s.config.StalenessThresholdHours)
	default:
		result.Action = "warn"
		result.Message = fmt.Sprintf("data is stale (%.1fh old, threshold %.1fh), defaulting to warn", dataAgeHours, s.config.StalenessThresholdHours)
	}

	return result
}

// ─── Missing File Locations ───────────────────────────────────────────

// LocationExtractor handles missing file location edge cases.
type LocationExtractor struct {
	config EdgeCaseConfig
	logger *slog.Logger
}

// NewLocationExtractor creates a new location extractor.
func NewLocationExtractor(config EdgeCaseConfig, logger *slog.Logger) *LocationExtractor {
	config.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &LocationExtractor{
		config: config,
		logger: logger.With("component", "location-extractor"),
	}
}

// ExtractedLocation holds the extracted location information.
type ExtractedLocation struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Source string `json:"source"` // "sarif", "metadata", "description", "unknown"
}

// ExtractLocation attempts to extract file location from various sources.
func (e *LocationExtractor) ExtractLocation(finding Finding, metadata map[string]string) *ExtractedLocation {
	// Source 1: Direct SARIF location
	if finding.File != "" {
		return &ExtractedLocation{
			File:   finding.File,
			Line:   finding.Line,
			Source: "sarif",
		}
	}

	// Source 2: Metadata fields
	if metadata != nil {
		if file, ok := metadata["file_path"]; ok && file != "" {
			line := 0
			if l, ok := metadata["line"]; ok {
				fmt.Sscanf(l, "%d", &line)
			}
			return &ExtractedLocation{
				File:   file,
				Line:   line,
				Source: "metadata",
			}
		}
	}

	// Source 3: Extract from description/message
	if loc := e.extractFromDescription(finding.Message); loc != nil {
		return loc
	}

	// Strategy-based fallback
	switch e.config.MissingLocationStrategy {
	case "extract":
		// Already tried extraction above
		return &ExtractedLocation{Source: "unknown"}
	case "skip":
		return nil
	case "unknown":
		return &ExtractedLocation{Source: "unknown"}
	default:
		return &ExtractedLocation{Source: "unknown"}
	}
}

// extractFromDescription tries to extract file location from a description string.
func (e *LocationExtractor) extractFromDescription(description string) *ExtractedLocation {
	// Pattern: "file.go:42" or "file.go:42:10"
	re := regexp.MustCompile(`([^\s:]+\.\w+):(\d+)(?::(\d+))?`)
	matches := re.FindStringSubmatch(description)
	if len(matches) >= 3 {
		line := 0
		fmt.Sscanf(matches[2], "%d", &line)
		return &ExtractedLocation{
			File:   matches[1],
			Line:   line,
			Source: "description",
		}
	}

	return nil
}

// ─── Rule ID Collisions ───────────────────────────────────────────────

// CollisionResolver handles rule ID collision resolution.
type CollisionResolver struct {
	config EdgeCaseConfig
	logger *slog.Logger
}

// NewCollisionResolver creates a new collision resolver.
func NewCollisionResolver(config EdgeCaseConfig, logger *slog.Logger) *CollisionResolver {
	config.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &CollisionResolver{
		config: config,
		logger: logger.With("component", "collision-resolver"),
	}
}

// CollisionResult holds the result of collision resolution.
type CollisionResult struct {
	ResolvedID string `json:"resolved_id"`
	OriginalID string `json:"original_id"`
	Strategy   string `json:"strategy"`
	Conflict   bool   `json:"conflict"`
}

// ResolveCollision resolves a rule ID collision using the configured strategy.
func (r *CollisionResolver) ResolveCollision(ruleID string, existingIDs map[string]bool) CollisionResult {
	if !existingIDs[ruleID] {
		return CollisionResult{
			ResolvedID: ruleID,
			OriginalID: ruleID,
			Strategy:   "none",
			Conflict:   false,
		}
	}

	result := CollisionResult{
		OriginalID: ruleID,
		Conflict:   true,
	}

	switch r.config.RuleIDCollisionStrategy {
	case "prefix":
		// Add tool prefix to disambiguate
		parts := strings.SplitN(ruleID, ".", 2)
		if len(parts) == 2 {
			result.ResolvedID = fmt.Sprintf("%s.dup.%s", parts[0], parts[1])
		} else {
			result.ResolvedID = fmt.Sprintf("unknown.dup.%s", ruleID)
		}
		result.Strategy = "prefix"

	case "suffix":
		// Add numeric suffix
		suffix := 1
		for {
			candidate := fmt.Sprintf("%s.%d", ruleID, suffix)
			if !existingIDs[candidate] {
				result.ResolvedID = candidate
				break
			}
			suffix++
		}
		result.Strategy = "suffix"

	case "reject":
		// Reject the duplicate
		result.ResolvedID = ""
		result.Strategy = "reject"

	default:
		result.ResolvedID = ruleID
		result.Strategy = "none"
	}

	r.logger.Warn("rule ID collision resolved",
		"original", ruleID,
		"resolved", result.ResolvedID,
		"strategy", result.Strategy,
	)

	return result
}

// ─── Phase Dispatch Timeout ───────────────────────────────────────────

// PhaseDispatcher handles phase dispatch with timeout and fallback.
type PhaseDispatcher struct {
	config EdgeCaseConfig
	logger *slog.Logger
}

// NewPhaseDispatcher creates a new phase dispatcher.
func NewPhaseDispatcher(config EdgeCaseConfig, logger *slog.Logger) *PhaseDispatcher {
	config.applyDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &PhaseDispatcher{
		config: config,
		logger: logger.With("component", "phase-dispatcher"),
	}
}

// DispatchResult holds the result of a phase dispatch.
type DispatchResult struct {
	Phase      string        `json:"phase"`
	Status     string        `json:"status"` // "dispatched", "timeout", "fallback"
	Duration   time.Duration `json:"duration"`
	Fallback   string        `json:"fallback,omitempty"`
	Message    string        `json:"message"`
}

// DispatchPhase dispatches a phase with timeout handling and fallback suggestions.
func (d *PhaseDispatcher) DispatchPhase(phase string, dispatchFunc func() error) DispatchResult {
	start := time.Now()

	done := make(chan error, 1)
	go func() {
		done <- dispatchFunc()
	}()

	select {
	case err := <-done:
		elapsed := time.Since(start)
		if err != nil {
			d.logger.Error("phase dispatch failed",
				"phase", phase,
				"err", err,
				"duration", elapsed,
			)
			return DispatchResult{
				Phase:    phase,
				Status:   "fallback",
				Duration: elapsed,
				Fallback: d.suggestFallback(phase, err),
				Message:  fmt.Sprintf("dispatch failed: %v", err),
			}
		}
		return DispatchResult{
			Phase:    phase,
			Status:   "dispatched",
			Duration: elapsed,
			Message:  "dispatch successful",
		}

	case <-time.After(d.config.PhaseDispatchTimeout):
		elapsed := time.Since(start)
		d.logger.Warn("phase dispatch timeout",
			"phase", phase,
			"timeout", d.config.PhaseDispatchTimeout,
		)
		return DispatchResult{
			Phase:    phase,
			Status:   "timeout",
			Duration: elapsed,
			Fallback: d.suggestFallbackForTimeout(phase),
			Message:  fmt.Sprintf("dispatch timed out after %v", d.config.PhaseDispatchTimeout),
		}
	}
}

// suggestFallback suggests a fallback strategy based on the error.
func (d *PhaseDispatcher) suggestFallback(phase string, err error) string {
	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "connection"):
		return "retry with backoff"
	case strings.Contains(errStr, "permission"):
		return "check credentials"
	case strings.Contains(errStr, "not found"):
		return "verify repository exists"
	default:
		return "use quick phase"
	}
}

// suggestFallbackForTimeout suggests a fallback strategy for timeout.
func (d *PhaseDispatcher) suggestFallbackForTimeout(phase string) string {
	switch phase {
	case "full":
		return "try quick phase"
	case "refresh":
		return "try quick phase"
	case "quick":
		return "check server health"
	default:
		return "retry with longer timeout"
	}
}
