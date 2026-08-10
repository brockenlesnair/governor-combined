package deadcode

// Config holds configuration for dead code detection.
type Config struct {
	ExcludeExported bool     `json:"exclude_exported"` // skip exported funcs (default: true)
	ExcludeTests    bool     `json:"exclude_tests"`    // skip test funcs (default: true)
	BuildTags       []string `json:"build_tags"`
	MinConfidence   float64  `json:"min_confidence"` // minimum confidence (0.0-1.0)
}

// DeadCodeCandidate represents a piece of potentially dead code.
type DeadCodeCandidate struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Package     string   `json:"package"`
	File        string   `json:"file"`
	Line        int      `json:"line"`
	Kind        string   `json:"kind"`        // "function", "method", "type"
	Exported    bool     `json:"exported"`
	Confidence  float64  `json:"confidence"`  // 0.0-1.0
	Reason      string   `json:"reason"`
	EntryPoints []string `json:"entry_points"`
	Reachable   bool     `json:"reachable"`
	DeadKind    string   `json:"dead_kind"` // "function", "method", "type", "const"
}

// DetectionResult holds the results of dead code detection.
type DetectionResult struct {
	TotalEntities int                 `json:"total_entities"`
	AliveCount    int                 `json:"alive_count"`
	DeadCount     int                 `json:"dead_count"`
	DeadCode      []DeadCodeCandidate `json:"dead_code"`
	DurationMs    int64               `json:"duration_ms"`
}
