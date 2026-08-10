package watcher

import "time"

// EventType describes the kind of file system change detected.
type EventType string

const (
	EventCreated  EventType = "created"
	EventModified EventType = "modified"
	EventDeleted  EventType = "deleted"
	EventRenamed  EventType = "renamed"
)

// FileEvent represents a single detected change to a file.
type FileEvent struct {
	Path      string    `json:"path"`
	EventType EventType `json:"event_type"`
	Timestamp time.Time `json:"timestamp"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"mod_time"`
}

// WatchedFile tracks the known state of a file being monitored.
type WatchedFile struct {
	Path         string    `json:"path"`
	LastModified time.Time `json:"last_modified"`
	Size         int64     `json:"size"`
	LastEvent    EventType `json:"last_event"`
	EventCount   int       `json:"event_count"`
	FirstSeen    time.Time `json:"first_seen"`
}

// Config controls the behaviour of a Watcher.
type Config struct {
	Paths           []string      `yaml:"paths"`            // directories to watch
	PollInterval    time.Duration `yaml:"poll_interval"`    // default 5s
	IncludePatterns []string      `yaml:"include_patterns"` // e.g. "*.md"
	ExcludePatterns []string      `yaml:"exclude_patterns"` // e.g. ".git/*"
	MaxEvents       int           `yaml:"max_events"`       // max events to retain (0 = unlimited)
}

// WatcherStatus is a snapshot of the watcher's current state.
type WatcherStatus struct {
	Running    bool          `json:"running"`
	WatchCount int           `json:"watch_count"`
	EventCount int           `json:"event_count"`
	Uptime     time.Duration `json:"uptime"`
	Paths      []string      `json:"paths"`
}
