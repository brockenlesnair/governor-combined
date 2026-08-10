// Package collab provides multi-agent collaboration primitives for concurrent
// file editing. It implements distributed file locking with conflict detection,
// shadow document management, and merge strategies (last-write-wins, manual, auto-merge).
package collab

import "time"

// AgentID uniquely identifies an agent working on files.
type AgentID string

// Config controls collaboration behavior.
type Config struct {
	ShadowDir        string           `yaml:"shadow_dir"`        // where to store temp docs (default: ".governor/shadows")
	LockDir          string           `yaml:"lock_dir"`          // where to store lock files (default: ".governor/locks")
	LockTimeout      time.Duration    `yaml:"lock_timeout"`      // max lock hold time (default: 5m)
	LockRetryDelay   time.Duration    `yaml:"lock_retry_delay"`  // retry delay (default: 100ms)
	LockMaxRetries   int              `yaml:"lock_max_retries"`  // max retries (default: 30)
	AutoMerge        bool             `yaml:"auto_merge"`        // auto-merge if no conflicts (default: true)
	ConflictStrategy ConflictStrategy `yaml:"conflict_strategy"` // last_write_wins, manual, auto_merge
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		ShadowDir:        ".governor/shadows",
		LockDir:          ".governor/locks",
		LockTimeout:      5 * time.Minute,
		LockRetryDelay:   100 * time.Millisecond,
		LockMaxRetries:   30,
		AutoMerge:        true,
		ConflictStrategy: ConflictLastWriteWins,
	}
}

// ConflictStrategy determines how merge conflicts are resolved.
type ConflictStrategy string

const (
	ConflictLastWriteWins ConflictStrategy = "last_write_wins"
	ConflictManual       ConflictStrategy = "manual"
	ConflictAutoMerge    ConflictStrategy = "auto_merge"
)

// LockInfo represents a file lock held by an agent.
type LockInfo struct {
	Path       string    `json:"path"`
	AgentID    AgentID   `json:"agent_id"`
	AcquiredAt time.Time `json:"acquired_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	ShadowPath string    `json:"shadow_path"`
}

// ShadowDoc represents a temporary copy of a file being edited by an agent.
type ShadowDoc struct {
	OriginalPath string    `json:"original_path"`
	ShadowPath   string    `json:"shadow_path"`
	AgentID      AgentID   `json:"agent_id"`
	CreatedAt    time.Time `json:"created_at"`
	ModifiedAt   time.Time `json:"modified_at"`
	Content      []byte    `json:"content"`
	Hash         string    `json:"hash"` // SHA-256 of content
}

// MergeResult represents the result of merging a shadow into the original.
type MergeResult struct {
	Success    bool             `json:"success"`
	Conflicts  []Conflict       `json:"conflicts"`
	MergedPath string           `json:"merged_path"`
	ShadowPath string           `json:"shadow_path"`
	Strategy   ConflictStrategy `json:"strategy"`
}

// Conflict represents a single conflict between original and shadow content.
type Conflict struct {
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Original   string `json:"original"`
	Shadow     string `json:"shadow"`
	Resolution string `json:"resolution,omitempty"`
}

// FileStatus represents the collaboration status of a file.
type FileStatus struct {
	Path        string  `json:"path"`
	Locked      bool    `json:"locked"`
	LockedBy    AgentID `json:"locked_by,omitempty"`
	HasShadow   bool    `json:"has_shadow"`
	ShadowAgent AgentID `json:"shadow_agent,omitempty"`
}
