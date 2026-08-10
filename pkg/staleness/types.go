package staleness

import "time"

// Config holds configuration for the staleness checker.
type Config struct {
	MaxAge          time.Duration `yaml:"max_age"`          // e.g. 24h, 7d
	WarnAge         time.Duration `yaml:"warn_age"`         // e.g. 12h, 3d
	ScanPaths       []string      `yaml:"scan_paths"`       // directories to scan
	IncludePatterns []string      `yaml:"include_patterns"` // e.g. "*.md", "*.go"
	ExcludePatterns []string      `yaml:"exclude_patterns"` // e.g. "vendor/*", ".git/*"
}

// StalenessLevel represents the freshness state of a file.
type StalenessLevel string

const (
	StalenessFresh   StalenessLevel = "fresh"
	StalenessWarning StalenessLevel = "warning"
	StalenessStale   StalenessLevel = "stale"
	StalenessUnknown StalenessLevel = "unknown"
)

// FileStatus holds the staleness status of a single file.
type FileStatus struct {
	Path         string          `json:"path"`
	LastModified time.Time       `json:"last_modified"`
	Age          time.Duration   `json:"age"`
	Status       StalenessLevel  `json:"status"`
	Size         int64           `json:"size"`
	Extension    string          `json:"extension"`
}

// StalenessReport holds the aggregated results of a staleness scan.
type StalenessReport struct {
	ScannedAt  time.Time    `json:"scanned_at"`
	TotalFiles int          `json:"total_files"`
	Fresh      int          `json:"fresh"`
	Warning    int          `json:"warning"`
	Stale      int          `json:"stale"`
	Unknown    int          `json:"unknown"`
	Files      []FileStatus `json:"files"`
	DurationMs int64        `json:"duration_ms"`
}
