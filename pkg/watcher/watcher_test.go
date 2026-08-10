package watcher

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func tmpDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func createFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("createFile: %v", err)
	}
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_SYNC, 0o644)
	if err != nil {
		t.Fatalf("appendFile open: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("appendFile write: %v", err)
	}
}

func cfg(paths ...string) Config {
	return Config{
		Paths:        paths,
		PollInterval: 50 * time.Millisecond,
	}
}

// ─── NewWatcher ──────────────────────────────────────────────────────────────

func TestNewWatcher(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(cfg(dir))

	if w == nil {
		t.Fatal("NewWatcher returned nil")
	}
	if w.config.PollInterval != 50*time.Millisecond {
		t.Errorf("expected poll interval 50ms, got %v", w.config.PollInterval)
	}
	if w.running {
		t.Error("new watcher should not be running")
	}
	if len(w.fileStates) != 0 {
		t.Error("new watcher should have empty fileStates")
	}
}

func TestNewWatcherDefaultInterval(t *testing.T) {
	w := NewWatcher(Config{Paths: []string{"/tmp"}})
	if w.config.PollInterval != defaultPollInterval {
		t.Errorf("expected default interval %v, got %v", defaultPollInterval, w.config.PollInterval)
	}
}

// ─── Start / Stop lifecycle ──────────────────────────────────────────────────

func TestStartStopLifecycle(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(cfg(dir))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	st := w.Status()
	if !st.Running {
		t.Error("Status should report running after Start")
	}

	if err := w.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	st = w.Status()
	if st.Running {
		t.Error("Status should report not running after Stop")
	}
}

func TestStartAlreadyRunning(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(cfg(dir))

	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Stop()

	err := w.Start(ctx)
	if err == nil {
		t.Fatal("expected error on second Start")
	}
	var wErr *WatcherError
	if !isWatcherError(err, ErrCodeAlreadyRunning, &wErr) {
		t.Errorf("expected AlreadyRunning error, got %v", err)
	}
}

func TestStopNotRunning(t *testing.T) {
	w := NewWatcher(cfg("/tmp"))

	err := w.Stop()
	if err == nil {
		t.Fatal("expected error when stopping non-running watcher")
	}
	var wErr *WatcherError
	if !isWatcherError(err, ErrCodeNotRunning, &wErr) {
		t.Errorf("expected NotRunning error, got %v", err)
	}
}

func TestStartPathNotFound(t *testing.T) {
	w := NewWatcher(Config{
		Paths:        []string{"/nonexistent/path/that/does/not/exist"},
		PollInterval: 50 * time.Millisecond,
	})

	err := w.Start(context.Background())
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
	var wErr *WatcherError
	if !isWatcherError(err, ErrCodePathNotFound, &wErr) {
		t.Errorf("expected PathNotFound error, got %v", err)
	}
}

// ─── Status ──────────────────────────────────────────────────────────────────

func TestStatusShowsCorrectCounts(t *testing.T) {
	dir := tmpDir(t)
	createFile(t, filepath.Join(dir, "a.txt"), "hello")
	createFile(t, filepath.Join(dir, "b.txt"), "world")

	w := NewWatcher(cfg(dir))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Stop()

	// Wait for initial scan.
	time.Sleep(100 * time.Millisecond)

	st := w.Status()
	if st.WatchCount != 2 {
		t.Errorf("expected 2 watched files, got %d", st.WatchCount)
	}
	if st.EventCount != 2 {
		t.Errorf("expected 2 events, got %d", st.EventCount)
	}
	if len(st.Paths) != 1 || st.Paths[0] != dir {
		t.Errorf("unexpected paths: %v", st.Paths)
	}
}

// ─── File creation detection ─────────────────────────────────────────────────

func TestScanDetectsCreatedFile(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(cfg(dir))

	// Create a file.
	path := filepath.Join(dir, "new.txt")
	createFile(t, path, "content")

	events := w.scanPaths()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventType != EventCreated {
		t.Errorf("expected EventCreated, got %v", events[0].EventType)
	}
	if events[0].Path != path {
		t.Errorf("expected path %s, got %s", path, events[0].Path)
	}
	if events[0].Size != 7 {
		t.Errorf("expected size 7, got %d", events[0].Size)
	}
}

// ─── File modification detection ─────────────────────────────────────────────

func TestScanDetectsModifiedFile(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(cfg(dir))

	path := filepath.Join(dir, "mod.txt")
	createFile(t, path, "v1")

	// First scan – registers the file.
	w.scanPaths()

	// Small sleep to guarantee ModTime difference on all OSes.
	time.Sleep(10 * time.Millisecond)

	// Modify the file.
	appendFile(t, path, "-v2")

	events := w.scanPaths()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventType != EventModified {
		t.Errorf("expected EventModified, got %v", events[0].EventType)
	}
	if events[0].Size != 5 { // "v1-v2" = 5 bytes
		t.Errorf("expected size 5, got %d", events[0].Size)
	}
}

// ─── File deletion detection ─────────────────────────────────────────────────

func TestScanDetectsDeletedFile(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(cfg(dir))

	path := filepath.Join(dir, "del.txt")
	createFile(t, path, "bye")

	// Register.
	w.scanPaths()

	// Delete.
	os.Remove(path)

	events := w.scanPaths()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventType != EventDeleted {
		t.Errorf("expected EventDeleted, got %v", events[0].EventType)
	}
	if events[0].Path != path {
		t.Errorf("expected path %s, got %s", path, events[0].Path)
	}
}

// ─── GetFiles / GetFile ──────────────────────────────────────────────────────

func TestGetFiles(t *testing.T) {
	dir := tmpDir(t)
	createFile(t, filepath.Join(dir, "a.md"), "aaa")
	createFile(t, filepath.Join(dir, "b.md"), "bb")

	w := NewWatcher(cfg(dir))
	w.scanPaths()

	files := w.GetFiles()
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	sort.Strings(paths)
	expected := []string{
		filepath.Join(dir, "a.md"),
		filepath.Join(dir, "b.md"),
	}
	if paths[0] != expected[0] || paths[1] != expected[1] {
		t.Errorf("unexpected paths: %v", paths)
	}
}

func TestGetFile(t *testing.T) {
	dir := tmpDir(t)
	path := filepath.Join(dir, "single.txt")
	createFile(t, path, "data")

	w := NewWatcher(cfg(dir))
	w.scanPaths()

	f, ok := w.GetFile(path)
	if !ok {
		t.Fatal("expected to find file")
	}
	if f.Size != 4 {
		t.Errorf("expected size 4, got %d", f.Size)
	}
	if f.LastEvent != EventCreated {
		t.Errorf("expected last event Created, got %v", f.LastEvent)
	}
	if f.EventCount != 1 {
		t.Errorf("expected event count 1, got %d", f.EventCount)
	}

	// Non-existent.
	_, ok = w.GetFile("/no/such/file")
	if ok {
		t.Error("expected false for non-existent file")
	}
}

// ─── GetRecentEvents ─────────────────────────────────────────────────────────

func TestGetRecentEvents(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(cfg(dir))

	createFile(t, filepath.Join(dir, "1.txt"), "a")
	w.emitEvents(w.scanPaths())

	time.Sleep(10 * time.Millisecond)
	createFile(t, filepath.Join(dir, "2.txt"), "b")
	w.emitEvents(w.scanPaths())

	time.Sleep(10 * time.Millisecond)
	createFile(t, filepath.Join(dir, "3.txt"), "c")
	w.emitEvents(w.scanPaths())

	recent := w.GetRecentEvents(2)
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent events, got %d", len(recent))
	}
	if recent[0].EventType != EventCreated {
		t.Errorf("first recent event should be Created, got %v", recent[0].EventType)
	}

	// Get all.
	all := w.GetRecentEvents(0)
	if len(all) != 3 {
		t.Errorf("expected 3 total events, got %d", len(all))
	}

	// Request more than available.
	over := w.GetRecentEvents(100)
	if len(over) != 3 {
		t.Errorf("expected 3 when requesting 100, got %d", len(over))
	}
}

// ─── Subscribe / Unsubscribe ─────────────────────────────────────────────────

func TestSubscribeReceivesEvents(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(cfg(dir))

	sub := w.Subscribe()

	createFile(t, filepath.Join(dir, "sub.txt"), "hi")
	w.emitEvents(w.scanPaths())

	select {
	case evt := <-sub:
		if evt.EventType != EventCreated {
			t.Errorf("expected EventCreated from subscriber, got %v", evt.EventType)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("subscriber did not receive event in time")
	}

	w.Unsubscribe(sub)

	// Channel should be closed after unsubscribe.
	_, ok := <-sub
	if ok {
		t.Error("expected channel to be closed after Unsubscribe")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(cfg(dir))

	sub1 := w.Subscribe()
	sub2 := w.Subscribe()

	createFile(t, filepath.Join(dir, "multi.txt"), "x")
	w.emitEvents(w.scanPaths())

	for _, ch := range []chan FileEvent{sub1, sub2} {
		select {
		case evt := <-ch:
			if evt.EventType != EventCreated {
				t.Errorf("expected EventCreated, got %v", evt.EventType)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("subscriber did not receive event in time")
		}
	}

	w.Unsubscribe(sub1)
	w.Unsubscribe(sub2)
}

// ─── Include / Exclude patterns ──────────────────────────────────────────────

func TestIncludePatterns(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(Config{
		Paths:           []string{dir},
		PollInterval:    50 * time.Millisecond,
		IncludePatterns: []string{"*.md"},
	})

	createFile(t, filepath.Join(dir, "readme.md"), "md")
	createFile(t, filepath.Join(dir, "code.go"), "go")

	events := w.scanPaths()
	if len(events) != 1 {
		t.Fatalf("expected 1 event (only .md), got %d", len(events))
	}
	if events[0].Path != filepath.Join(dir, "readme.md") {
		t.Errorf("expected readme.md, got %s", events[0].Path)
	}
}

func TestExcludePatterns(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(Config{
		Paths:           []string{dir},
		PollInterval:    50 * time.Millisecond,
		ExcludePatterns: []string{".git"},
	})

	createFile(t, filepath.Join(dir, "src.txt"), "src")
	gitDir := filepath.Join(dir, ".git")
	os.Mkdir(gitDir, 0o755)
	createFile(t, filepath.Join(gitDir, "config"), "git config")

	events := w.scanPaths()
	for _, e := range events {
		if contains(e.Path, ".git") {
			t.Errorf("should not include .git file: %s", e.Path)
		}
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event (only src.txt), got %d", len(events))
	}
}

func TestExcludeSubstring(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(Config{
		Paths:           []string{dir},
		PollInterval:    50 * time.Millisecond,
		ExcludePatterns: []string{"vendor"},
	})

	createFile(t, filepath.Join(dir, "app.txt"), "app")
	vendorDir := filepath.Join(dir, "vendor", "lib")
	os.MkdirAll(vendorDir, 0o755)
	createFile(t, filepath.Join(vendorDir, "dep.txt"), "dep")

	events := w.scanPaths()
	if len(events) != 1 {
		t.Fatalf("expected 1 event (excluded vendor), got %d", len(events))
	}
}

// ─── MaxEvents ───────────────────────────────────────────────────────────────

func TestMaxEvents(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(Config{
		Paths:        []string{dir},
		PollInterval: 50 * time.Millisecond,
		MaxEvents:    3,
	})

	for i := 0; i < 5; i++ {
		createFile(t, filepath.Join(dir, "file.txt"), "x")
		w.scanPaths()
		// Overwrite same file to get Modified events.
		createFile(t, filepath.Join(dir, "file.txt"), "xx")
		time.Sleep(10 * time.Millisecond)
	}

	events := w.GetRecentEvents(0)
	if len(events) > 3 {
		t.Errorf("expected at most 3 events (MaxEvents=3), got %d", len(events))
	}
}

// ─── Events channel ──────────────────────────────────────────────────────────

func TestEventsChannelReceivesEvents(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(cfg(dir))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Stop()

	// Create a file so the next poll detects it.
	createFile(t, filepath.Join(dir, "ch.txt"), "data")

	select {
	case evt := <-w.Events():
		if evt.EventType != EventCreated {
			t.Errorf("expected EventCreated, got %v", evt.EventType)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Events channel did not receive event")
	}
}

// ─── Concurrent access ───────────────────────────────────────────────────────

func TestConcurrentReadsDuringScan(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(cfg(dir))

	// Pre-populate.
	createFile(t, filepath.Join(dir, "pre.txt"), "p")
	w.scanPaths()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start watcher.
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Stop()

	// Hammer from goroutines while poll runs.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			_ = w.GetFiles()
			_ = w.GetRecentEvents(1)
			_ = w.Status()
			_ = w.Subscribe()
			time.Sleep(2 * time.Millisecond)
		}
		close(done)
	}()

	// Also create files to trigger events.
	for i := 0; i < 10; i++ {
		createFile(t, filepath.Join(dir, "stress.txt"), "s")
		time.Sleep(30 * time.Millisecond)
	}

	<-done
}

// ─── WatchedFile state tracking ──────────────────────────────────────────────

func TestWatchedFileEventCountAccumulates(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(cfg(dir))

	path := filepath.Join(dir, "acc.txt")
	createFile(t, path, "v1")
	w.scanPaths()

	time.Sleep(10 * time.Millisecond)
	createFile(t, path, "v2")
	w.scanPaths()

	time.Sleep(10 * time.Millisecond)
	createFile(t, path, "v3")
	w.scanPaths()

	f, ok := w.GetFile(path)
	if !ok {
		t.Fatal("file not tracked")
	}
	if f.EventCount != 3 {
		t.Errorf("expected 3 events (1 created + 2 modified), got %d", f.EventCount)
	}
	if f.LastEvent != EventModified {
		t.Errorf("expected last event Modified, got %v", f.LastEvent)
	}
}

func TestDeletedFileRemovedFromTracking(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(cfg(dir))

	path := filepath.Join(dir, "gone.txt")
	createFile(t, path, "here")
	w.scanPaths()

	if _, ok := w.GetFile(path); !ok {
		t.Fatal("file should be tracked before deletion")
	}

	os.Remove(path)
	w.scanPaths()

	f, ok := w.GetFile(path)
	if !ok {
		t.Fatal("file should still be in map after deletion (to track deletion)")
	}
	if f.LastEvent != EventDeleted {
		t.Errorf("expected last event Deleted, got %v", f.LastEvent)
	}
}

// ─── WatcherError ────────────────────────────────────────────────────────────

func TestWatcherErrorFormat(t *testing.T) {
	e := &WatcherError{
		Code:    ErrCodeWatchFailed,
		Message: "something broke",
		Path:    "/foo/bar",
		Err:     os.ErrNotExist,
	}

	s := e.Error()
	if s == "" {
		t.Error("Error() returned empty string")
	}
	if e.Unwrap() != os.ErrNotExist {
		t.Error("Unwrap() did not return wrapped error")
	}
}

func TestWatcherErrorNoPath(t *testing.T) {
	e := newError(ErrCodeConfigInvalid, "bad config", "", nil)
	s := e.Error()
	if s == "" {
		t.Error("Error() returned empty string")
	}
}

// ─── integration: poll loop detects creation ─────────────────────────────────

func TestPollLoopDetectsNewFile(t *testing.T) {
	dir := tmpDir(t)
	w := NewWatcher(cfg(dir))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Stop()

	// Create a file after watcher starts.
	time.Sleep(100 * time.Millisecond)
	path := filepath.Join(dir, "polled.txt")
	createFile(t, path, "live")

	// Wait for poll to pick it up.
	timeout := time.After(2 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatal("poll loop did not detect new file")
		default:
			files := w.GetFiles()
			if len(files) > 0 {
				return // success
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// ─── integration: poll loop detects modification ─────────────────────────────

func TestPollLoopDetectsModification(t *testing.T) {
	dir := tmpDir(t)
	path := filepath.Join(dir, "existing.txt")
	createFile(t, path, "original")

	w := NewWatcher(cfg(dir))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Stop()

	// Wait for initial scan to register the file.
	time.Sleep(150 * time.Millisecond)

	// Modify.
	time.Sleep(10 * time.Millisecond)
	appendFile(t, path, "-changed")

	// Wait for poll.
	timeout := time.After(2 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatal("poll loop did not detect modification")
		default:
			evts := w.GetRecentEvents(0)
			for _, e := range evts {
				if e.EventType == EventModified {
					return // success
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// ─── integration: poll loop detects deletion ─────────────────────────────────

func TestPollLoopDetectsDeletion(t *testing.T) {
	dir := tmpDir(t)
	path := filepath.Join(dir, "todelete.txt")
	createFile(t, path, "bye")

	w := NewWatcher(cfg(dir))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Stop()

	// Wait for initial scan.
	time.Sleep(150 * time.Millisecond)

	// Delete.
	os.Remove(path)

	// Wait for poll.
	timeout := time.After(2 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatal("poll loop did not detect deletion")
		default:
			evts := w.GetRecentEvents(0)
			for _, e := range evts {
				if e.EventType == EventDeleted {
					return // success
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// ─── helpers (private) ───────────────────────────────────────────────────────

func isWatcherError(err error, code ErrorCode, target **WatcherError) bool {
	if err == nil {
		return false
	}
	if we, ok := err.(*WatcherError); ok {
		if target != nil {
			*target = we
		}
		return we.Code == code
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
