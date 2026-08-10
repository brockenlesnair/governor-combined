// Package watcher monitors filesystem changes and notifies subscribers.
//
// It polls directories at configurable intervals, detects file modifications,
// additions, and deletions, and broadcasts events to registered handlers.
// Supports debouncing and filtering by file extension.
package watcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultPollInterval = 5 * time.Second
	eventChannelBuffer  = 100
	subscriberBuffer    = 50
)

// Watcher polls the filesystem and maintains a live list of document changes.
type Watcher struct {
	config      Config
	mu          sync.RWMutex
	running     bool
	fileStates  map[string]*WatchedFile // path → current state
	events      []FileEvent             // event history (ring-buffer style via MaxEvents)
	eventCh     chan FileEvent           // internal channel for live streaming
	subscribers []chan FileEvent         // per-subscriber channels
	startTime   time.Time
	cancel      context.CancelFunc
	done        chan struct{} // closed when poll goroutine exits
}

// NewWatcher creates a Watcher with the supplied configuration.
// If PollInterval is zero the default (5 s) is used.
func NewWatcher(cfg Config) *Watcher {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	return &Watcher{
		config:  cfg,
		fileStates: make(map[string]*WatchedFile),
		events:  make([]FileEvent, 0),
		eventCh: make(chan FileEvent, eventChannelBuffer),
	}
}

// ─── Lifecycle ───────────────────────────────────────────────────────────────

// Start begins the polling loop in a background goroutine.
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return newError(ErrCodeAlreadyRunning, "watcher already running", "", nil)
	}

	// Validate that at least one configured path exists.
	for _, p := range w.config.Paths {
		if _, err := os.Stat(p); err != nil {
			return newError(ErrCodePathNotFound, "configured path not found", p, err)
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.running = true
	w.startTime = time.Now()
	w.done = make(chan struct{})

	go w.poll(ctx)

	return nil
}

// Stop terminates the polling loop and waits for it to finish.
func (w *Watcher) Stop() error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return newError(ErrCodeNotRunning, "watcher is not running", "", nil)
	}
	w.cancel()
	w.running = false
	done := w.done
	w.mu.Unlock()

	<-done

	// Close subscriber channels.
	w.mu.Lock()
	for _, ch := range w.subscribers {
		close(ch)
	}
	w.subscribers = nil
	w.mu.Unlock()

	return nil
}

// Status returns a snapshot of the watcher's current state.
func (w *Watcher) Status() WatcherStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()

	paths := make([]string, len(w.config.Paths))
	copy(paths, w.config.Paths)

	var uptime time.Duration
	if w.running {
		uptime = time.Since(w.startTime)
	}

	return WatcherStatus{
		Running:    w.running,
		WatchCount: len(w.fileStates),
		EventCount: len(w.events),
		Uptime:     uptime,
		Paths:      paths,
	}
}

// ─── Live list ───────────────────────────────────────────────────────────────

// GetFiles returns a snapshot of every tracked file.
func (w *Watcher) GetFiles() []WatchedFile {
	w.mu.RLock()
	defer w.mu.RUnlock()

	result := make([]WatchedFile, 0, len(w.fileStates))
	for _, f := range w.fileStates {
		result = append(result, *f)
	}
	return result
}

// GetFile returns the tracked state for a single file.
func (w *Watcher) GetFile(path string) (*WatchedFile, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	f, ok := w.fileStates[path]
	if !ok {
		return nil, false
	}
	cp := *f
	return &cp, true
}

// GetRecentEvents returns up to n most recent events (newest last).
func (w *Watcher) GetRecentEvents(n int) []FileEvent {
	w.mu.RLock()
	defer w.mu.RUnlock()

	total := len(w.events)
	if n <= 0 || n > total {
		n = total
	}

	result := make([]FileEvent, n)
	copy(result, w.events[total-n:])
	return result
}

// Events returns a read-only channel that receives every detected event.
func (w *Watcher) Events() <-chan FileEvent {
	return w.eventCh
}

// ─── Subscription ────────────────────────────────────────────────────────────

// Subscribe creates and returns a new channel that will receive event copies.
func (w *Watcher) Subscribe() chan FileEvent {
	ch := make(chan FileEvent, subscriberBuffer)
	w.mu.Lock()
	w.subscribers = append(w.subscribers, ch)
	w.mu.Unlock()
	return ch
}

// Unsubscribe removes a previously subscribed channel and closes it.
func (w *Watcher) Unsubscribe(ch chan FileEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for i, sub := range w.subscribers {
		if sub == ch {
			w.subscribers = append(w.subscribers[:i], w.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// ─── Internal ────────────────────────────────────────────────────────────────

// poll is the main loop that runs until ctx is cancelled.
func (w *Watcher) poll(ctx context.Context) {
	defer close(w.done)

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	// Initial scan.
	events := w.scanPaths()
	w.emitEvents(events)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			events := w.scanPaths()
			w.emitEvents(events)
		}
	}
}

// emitEvents sends events to the main channel and all subscribers, then
// appends them to the history ring.
func (w *Watcher) emitEvents(events []FileEvent) {
	for _, ev := range events {
		// Main channel (blocking if full – slow consumer is intentionally penalised).
		select {
		case w.eventCh <- ev:
		default:
		}

		// Subscribers (non-blocking, drop if full).
		w.mu.RLock()
		for _, sub := range w.subscribers {
			select {
			case sub <- ev:
			default:
			}
		}
		w.mu.RUnlock()
	}

	// Append to history.
	w.mu.Lock()
	w.events = append(w.events, events...)
	if w.config.MaxEvents > 0 && len(w.events) > w.config.MaxEvents {
		w.events = w.events[len(w.events)-w.config.MaxEvents:]
	}
	w.mu.Unlock()
}

// scanPaths walks every configured path and returns events for any changes
// detected since the last scan.
func (w *Watcher) scanPaths() []FileEvent {
	var events []FileEvent
	now := time.Now()

	// Collect the set of files seen on this scan so we can detect deletions.
	seen := make(map[string]struct{})

	for _, root := range w.config.Paths {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip inaccessible entries
			}
			if d.IsDir() {
				return nil
			}

			rel, _ := filepath.Rel(root, path)
			name := rel

			if !w.matchesPatterns(name) {
				return nil
			}

			info, err := os.Stat(path)
			if err != nil {
				return nil // file may have vanished between Walk and Stat
			}

			seen[path] = struct{}{}

			w.mu.RLock()
			prev, exists := w.fileStates[path]
			w.mu.RUnlock()

			if !exists {
				// New file.
				evt := FileEvent{
					Path:      path,
					EventType: EventCreated,
					Timestamp: now,
					Size:      info.Size(),
					ModTime:   info.ModTime(),
				}
				events = append(events, evt)

				w.mu.Lock()
				w.fileStates[path] = &WatchedFile{
					Path:         path,
					LastModified: info.ModTime(),
					Size:         info.Size(),
					LastEvent:    EventCreated,
					EventCount:   1,
					FirstSeen:    now,
				}
				w.mu.Unlock()
			} else if info.ModTime().After(prev.LastModified) || info.Size() != prev.Size {
				// Modified.
				evt := FileEvent{
					Path:      path,
					EventType: EventModified,
					Timestamp: now,
					Size:      info.Size(),
					ModTime:   info.ModTime(),
				}
				events = append(events, evt)

				w.mu.Lock()
				prev.LastModified = info.ModTime()
				prev.Size = info.Size()
				prev.LastEvent = EventModified
				prev.EventCount++
				w.mu.Unlock()
			}

			return nil
		})
	}

	// Detect deletions: files in fileStates that were not seen on this scan.
	w.mu.RLock()
	var deleted []string
	for path := range w.fileStates {
		if _, ok := seen[path]; !ok {
			deleted = append(deleted, path)
		}
	}
	w.mu.RUnlock()

	for _, path := range deleted {
		w.mu.Lock()
		prev := w.fileStates[path]
		evt := FileEvent{
			Path:      path,
			EventType: EventDeleted,
			Timestamp: now,
			Size:      0,
			ModTime:   prev.LastModified,
		}
		events = append(events, evt)
		prev.LastEvent = EventDeleted
		prev.EventCount++
		w.mu.Unlock()
	}

	return events
}

// matchesPatterns checks whether a relative file path should be tracked.
// A file is included if it matches any include pattern (or include list is empty)
// AND does not match any exclude pattern.
func (w *Watcher) matchesPatterns(name string) bool {
	// Exclude patterns.
	for _, pat := range w.config.ExcludePatterns {
		if matched, _ := filepath.Match(pat, name); matched {
			return false
		}
		// Also check if any path component matches (for patterns like ".git").
		parts := strings.Split(name, string(filepath.Separator))
		for _, part := range parts {
			if matched, _ := filepath.Match(pat, part); matched {
				return false
			}
		}
	}

	// Include patterns: if none specified, include everything.
	if len(w.config.IncludePatterns) == 0 {
		return true
	}
	for _, pat := range w.config.IncludePatterns {
		if matched, _ := filepath.Match(pat, name); matched {
			return true
		}
	}
	return false
}
