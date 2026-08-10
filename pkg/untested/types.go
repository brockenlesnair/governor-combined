// Package untested detects functions and methods lacking test coverage.
//
// It analyzes package structure to find exported and unexported functions
// without corresponding test files, prioritizing by risk factors like
// complexity and public API surface.
package untested

// Config controls the behavior of the untested detector.
type Config struct {
	IncludeExported bool     `json:"include_exported"`
	IncludeMethods  bool     `json:"include_methods"`
	MinPriority     float64  `json:"min_priority"`
	BuildTags       []string `json:"build_tags"`
}

// UntestedFunc represents a function with no test coverage.
type UntestedFunc struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Package     string  `json:"package"`
	File        string  `json:"file"`
	Line        int     `json:"line"`
	Kind        string  `json:"kind"`       // "function", "method"
	Exported    bool    `json:"exported"`
	Priority    float64 `json:"priority"`    // risk score 0.0-1.0
	CallerCount int     `json:"caller_count"`
	CalleeCount int     `json:"callee_count"`
	Reason      string  `json:"reason"`
	HasTestFile bool    `json:"has_test_file"`
	CoveragePct float64 `json:"coverage_pct"`
}

// DetectionResult is the output of the untested detection pass.
type DetectionResult struct {
	TotalFunctions int            `json:"total_functions"`
	TestedCount    int            `json:"tested_count"`
	UntestedCount  int            `json:"untested_count"`
	CoveragePct    float64        `json:"coverage_pct"`
	Untested       []UntestedFunc `json:"untested"`
	DurationMs     int64          `json:"duration_ms"`
}
