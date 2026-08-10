package collab

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// testConfig returns a Config suitable for testing with short timeouts.
func testConfig(tmpDir string) Config {
	return Config{
		ShadowDir:       filepath.Join(tmpDir, ".governor", "shadows"),
		LockDir:         filepath.Join(tmpDir, ".governor", "locks"),
		LockTimeout:     100 * time.Millisecond,
		LockRetryDelay:  10 * time.Millisecond,
		LockMaxRetries:  3,
		AutoMerge:       true,
		ConflictStrategy: ConflictLastWriteWins,
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ShadowDir != ".governor/shadows" {
		t.Errorf("expected ShadowDir .governor/shadows, got %s", cfg.ShadowDir)
	}
	if cfg.LockTimeout != 5*time.Minute {
		t.Errorf("expected LockTimeout 5m, got %v", cfg.LockTimeout)
	}
	if cfg.ConflictStrategy != ConflictLastWriteWins {
		t.Errorf("expected ConflictLastWriteWins, got %s", cfg.ConflictStrategy)
	}
}

// --- Lock Tests ---

func TestAcquireLock_UnlockedFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	lm := NewLockManager(cfg)
	ctx := context.Background()

	info, err := lm.AcquireLock(ctx, "test.txt", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.AgentID != "agent1" {
		t.Errorf("expected agent1, got %s", info.AgentID)
	}
	if info.Path != "test.txt" {
		t.Errorf("expected test.txt, got %s", info.Path)
	}
	if info.ExpiresAt.Before(time.Now()) {
		t.Error("expected ExpiresAt to be in the future")
	}

	// Lock file should exist on disk — just check the lock dir has a .lock file
	entries, _ := os.ReadDir(cfg.LockDir)
	lockFound := false
	for _, e := range entries {
		if e.Name() != "" && !e.IsDir() {
			lockFound = true
			break
		}
	}
	if !lockFound {
		t.Error("lock file should exist on disk")
	}
}

func TestAcquireLock_AlreadyLocked(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	lm := NewLockManager(cfg)
	ctx := context.Background()

	_, err := lm.AcquireLock(ctx, "test.txt", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Different agent tries to acquire
	_, err = lm.AcquireLock(ctx, "test.txt", "agent2")
	if err == nil {
		t.Fatal("expected error when different agent acquires locked file")
	}

	var collabErr *CollabError
	if !errors.As(err, &collabErr) {
		t.Fatalf("expected CollabError, got %T", err)
	}
	if collabErr.Code != ErrCodeLockDenied {
		t.Errorf("expected ErrCodeLockDenied, got %s", collabErr.Code)
	}
}

func TestAcquireLock_SameAgentExtends(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	lm := NewLockManager(cfg)
	ctx := context.Background()

	info1, err := lm.AcquireLock(ctx, "test.txt", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Small delay so expiry changes
	time.Sleep(10 * time.Millisecond)

	info2, err := lm.AcquireLock(ctx, "test.txt", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info2.ExpiresAt.Before(info1.ExpiresAt) {
		t.Error("expected expiry to be extended")
	}
}

func TestReleaseLock_ByHolder(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	lm := NewLockManager(cfg)
	ctx := context.Background()

	_, err := lm.AcquireLock(ctx, "test.txt", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = lm.ReleaseLock(ctx, "test.txt", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	locked, _, _ := lm.IsLocked(ctx, "test.txt")
	if locked {
		t.Error("file should be unlocked after release")
	}
}

func TestReleaseLock_NonHolder(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	lm := NewLockManager(cfg)
	ctx := context.Background()

	_, err := lm.AcquireLock(ctx, "test.txt", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = lm.ReleaseLock(ctx, "test.txt", "agent2")
	if err == nil {
		t.Fatal("expected error when non-holder releases")
	}

	var collabErr *CollabError
	if !errors.As(err, &collabErr) {
		t.Fatalf("expected CollabError, got %T", err)
	}
	if collabErr.Code != ErrCodeLockDenied {
		t.Errorf("expected ErrCodeLockDenied, got %s", collabErr.Code)
	}
}

func TestExtendLock(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	lm := NewLockManager(cfg)
	ctx := context.Background()

	_, err := lm.AcquireLock(ctx, "test.txt", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = lm.ExtendLock(ctx, "test.txt", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	locked, info, _ := lm.IsLocked(ctx, "test.txt")
	if !locked {
		t.Error("file should still be locked")
	}
	if info.ExpiresAt.Before(time.Now()) {
		t.Error("expected expiry to be extended")
	}
}

func TestCleanupExpired(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		ShadowDir:       filepath.Join(tmpDir, ".governor", "shadows"),
		LockDir:         filepath.Join(tmpDir, ".governor", "locks"),
		LockTimeout:     1 * time.Millisecond, // Very short
		LockRetryDelay:  10 * time.Millisecond,
		LockMaxRetries:  3,
		AutoMerge:       true,
		ConflictStrategy: ConflictLastWriteWins,
	}
	lm := NewLockManager(cfg)
	ctx := context.Background()

	_, err := lm.AcquireLock(ctx, "test.txt", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for lock to expire
	time.Sleep(5 * time.Millisecond)

	count := lm.CleanupExpired()
	if count != 1 {
		t.Errorf("expected 1 expired lock removed, got %d", count)
	}

	locked, _, _ := lm.IsLocked(ctx, "test.txt")
	if locked {
		t.Error("file should be unlocked after cleanup")
	}
}

func TestListLocks(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	lm := NewLockManager(cfg)
	ctx := context.Background()

	_, _ = lm.AcquireLock(ctx, "a.txt", "agent1")
	_, _ = lm.AcquireLock(ctx, "b.txt", "agent2")

	locks := lm.ListLocks()
	if len(locks) != 2 {
		t.Errorf("expected 2 locks, got %d", len(locks))
	}
}

func TestAcquireLock_EmptyAgent(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	lm := NewLockManager(cfg)
	ctx := context.Background()

	_, err := lm.AcquireLock(ctx, "test.txt", "")
	if err == nil {
		t.Fatal("expected error for empty agent ID")
	}
	var collabErr *CollabError
	if !errors.As(err, &collabErr) {
		t.Fatalf("expected CollabError, got %T", err)
	}
	if collabErr.Code != ErrCodeInvalidAgent {
		t.Errorf("expected ErrCodeInvalidAgent, got %s", collabErr.Code)
	}
}

func TestExtendLock_NotLocked(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	lm := NewLockManager(cfg)
	ctx := context.Background()

	err := lm.ExtendLock(ctx, "test.txt", "agent1")
	if err == nil {
		t.Fatal("expected error when extending unlocked file")
	}
}

// --- Shadow Tests ---

func TestCreateShadow_ExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	sm := NewShadowManager(cfg)
	ctx := context.Background()

	// Create original file
	originalPath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(originalPath, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	doc, err := sm.CreateShadow(ctx, originalPath, "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.OriginalPath != originalPath {
		t.Errorf("expected OriginalPath %s, got %s", originalPath, doc.OriginalPath)
	}
	if doc.AgentID != "agent1" {
		t.Errorf("expected agent1, got %s", doc.AgentID)
	}
	if doc.Hash == "" {
		t.Error("expected hash to be computed")
	}
	if string(doc.Content) != "hello world" {
		t.Errorf("expected content 'hello world', got %q", doc.Content)
	}
}

func TestCreateShadow_DuplicateShadow(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	sm := NewShadowManager(cfg)
	ctx := context.Background()

	originalPath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(originalPath, []byte("content"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := sm.CreateShadow(ctx, originalPath, "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second shadow for same agent
	_, err = sm.CreateShadow(ctx, originalPath, "agent1")
	if err == nil {
		t.Fatal("expected error for duplicate shadow")
	}
	var collabErr *CollabError
	if !errors.As(err, &collabErr) {
		t.Fatalf("expected CollabError, got %T", err)
	}
	if collabErr.Code != ErrCodeShadowExists {
		t.Errorf("expected ErrCodeShadowExists, got %s", collabErr.Code)
	}
}

func TestReadShadow_WriteShadow_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	sm := NewShadowManager(cfg)
	ctx := context.Background()

	originalPath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(originalPath, []byte("original"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := sm.CreateShadow(ctx, originalPath, "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Write new content
	newContent := []byte("modified content")
	err = sm.WriteShadow(ctx, originalPath, "agent1", newContent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read back
	content, err := sm.ReadShadow(ctx, originalPath, "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != "modified content" {
		t.Errorf("expected 'modified content', got %q", content)
	}

	// Verify hash updated
	doc, ok := sm.GetShadow(originalPath, "agent1")
	if !ok {
		t.Fatal("shadow not found")
	}
	if doc.Hash != computeHash(newContent) {
		t.Error("hash should be updated after write")
	}
}

func TestDeleteShadow(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	sm := NewShadowManager(cfg)
	ctx := context.Background()

	originalPath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(originalPath, []byte("content"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := sm.CreateShadow(ctx, originalPath, "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = sm.DeleteShadow(ctx, originalPath, "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, ok := sm.GetShadow(originalPath, "agent1")
	if ok {
		t.Error("shadow should be deleted")
	}
}

func TestCreateShadow_NonexistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	sm := NewShadowManager(cfg)
	ctx := context.Background()

	_, err := sm.CreateShadow(ctx, filepath.Join(tmpDir, "nope.txt"), "agent1")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	var collabErr *CollabError
	if !errors.As(err, &collabErr) {
		t.Fatalf("expected CollabError, got %T", err)
	}
	if collabErr.Code != ErrCodeFileNotFound {
		t.Errorf("expected ErrCodeFileNotFound, got %s", collabErr.Code)
	}
}

func TestListShadows_FilteredByAgent(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	sm := NewShadowManager(cfg)
	ctx := context.Background()

	// Create two files
	for _, name := range []string{"a.txt", "b.txt"} {
		p := filepath.Join(tmpDir, name)
		if err := os.WriteFile(p, []byte("content"), 0o644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	p1 := filepath.Join(tmpDir, "a.txt")
	p2 := filepath.Join(tmpDir, "b.txt")

	_, _ = sm.CreateShadow(ctx, p1, "agent1")
	_, _ = sm.CreateShadow(ctx, p2, "agent2")

	// All shadows
	all := sm.ListShadows("")
	if len(all) != 2 {
		t.Errorf("expected 2 shadows, got %d", len(all))
	}

	// Filtered by agent
	agent1Shadows := sm.ListShadows("agent1")
	if len(agent1Shadows) != 1 {
		t.Errorf("expected 1 shadow for agent1, got %d", len(agent1Shadows))
	}
	if agent1Shadows[0].AgentID != "agent1" {
		t.Errorf("expected agent1, got %s", agent1Shadows[0].AgentID)
	}
}

func TestGetShadow(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	sm := NewShadowManager(cfg)
	ctx := context.Background()

	originalPath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(originalPath, []byte("content"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := sm.CreateShadow(ctx, originalPath, "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc, ok := sm.GetShadow(originalPath, "agent1")
	if !ok {
		t.Fatal("expected shadow to be found")
	}
	if doc.OriginalPath != originalPath {
		t.Errorf("expected OriginalPath %s, got %s", originalPath, doc.OriginalPath)
	}

	// Not found
	_, ok = sm.GetShadow(originalPath, "agent999")
	if ok {
		t.Error("expected shadow not to be found for unknown agent")
	}
}

func TestReadShadow_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	sm := NewShadowManager(cfg)
	ctx := context.Background()

	_, err := sm.ReadShadow(ctx, "test.txt", "agent1")
	if err == nil {
		t.Fatal("expected error for non-existent shadow")
	}
}

func TestWriteShadow_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	sm := NewShadowManager(cfg)
	ctx := context.Background()

	err := sm.WriteShadow(ctx, "test.txt", "agent1", []byte("content"))
	if err == nil {
		t.Fatal("expected error for non-existent shadow")
	}
}

func TestDeleteShadow_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	sm := NewShadowManager(cfg)
	ctx := context.Background()

	err := sm.DeleteShadow(ctx, "test.txt", "agent1")
	if err == nil {
		t.Fatal("expected error for non-existent shadow")
	}
}

// --- Merge Tests ---

func TestMerge_NoChanges(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	cfg.ConflictStrategy = ConflictLastWriteWins
	m := NewMerger(cfg)
	ctx := context.Background()

	originalPath := filepath.Join(tmpDir, "test.txt")
	content := []byte("unchanged content")
	if err := os.WriteFile(originalPath, content, 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	shadow := &ShadowDoc{
		OriginalPath: originalPath,
		ShadowPath:   filepath.Join(tmpDir, "shadow.tmp"),
		AgentID:      "agent1",
		Content:      content, // Same as original
	}

	result, err := m.Merge(ctx, shadow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success when no changes")
	}
	if len(result.Conflicts) != 0 {
		t.Errorf("expected no conflicts, got %d", len(result.Conflicts))
	}
}

func TestMerge_NoConflicts(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	cfg.ConflictStrategy = ConflictLastWriteWins
	m := NewMerger(cfg)
	ctx := context.Background()

	originalPath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(originalPath, []byte("line1\nline2\nline3"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	shadow := &ShadowDoc{
		OriginalPath: originalPath,
		ShadowPath:   filepath.Join(tmpDir, "shadow.tmp"),
		AgentID:      "agent1",
		Content:      []byte("line1\nline2\nline4"), // Only line3 changed
	}

	result, err := m.Merge(ctx, shadow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}

	// Verify original was updated
	content, _ := os.ReadFile(originalPath)
	if string(content) != "line1\nline2\nline4" {
		t.Errorf("expected shadow content, got %q", content)
	}
}

func TestMerge_LastWriteWins(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	cfg.ConflictStrategy = ConflictLastWriteWins
	m := NewMerger(cfg)
	ctx := context.Background()

	originalPath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(originalPath, []byte("original line1\noriginal line2"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	shadow := &ShadowDoc{
		OriginalPath: originalPath,
		ShadowPath:   filepath.Join(tmpDir, "shadow.tmp"),
		AgentID:      "agent1",
		Content:      []byte("shadow line1\nshadow line2"),
	}

	result, err := m.Merge(ctx, shadow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success with last-write-wins")
	}
	if len(result.Conflicts) != 2 {
		t.Errorf("expected 2 conflicts, got %d", len(result.Conflicts))
	}

	// Shadow should win
	content, _ := os.ReadFile(originalPath)
	if string(content) != "shadow line1\nshadow line2" {
		t.Errorf("expected shadow content, got %q", content)
	}
}

func TestDetectConflicts(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	m := NewMerger(cfg)

	original := []byte("line1\nline2\nline3")
	shadow := []byte("line1\nchanged\nline3")

	conflicts := m.DetectConflicts(original, shadow)
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0].Line != 2 {
		t.Errorf("expected conflict at line 2, got %d", conflicts[0].Line)
	}
	if conflicts[0].Original != "line2" {
		t.Errorf("expected original 'line2', got %q", conflicts[0].Original)
	}
	if conflicts[0].Shadow != "changed" {
		t.Errorf("expected shadow 'changed', got %q", conflicts[0].Shadow)
	}
}

func TestDetectConflicts_NoDifferences(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	m := NewMerger(cfg)

	conflicts := m.DetectConflicts([]byte("same\ncontent"), []byte("same\ncontent"))
	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts, got %d", len(conflicts))
	}
}

func TestResolveLastWriteWins(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	m := NewMerger(cfg)

	original := []byte("original line1\noriginal line2")
	shadow := []byte("shadow line1\nshadow line2")
	conflicts := []Conflict{
		{Line: 1, Original: "original line1", Shadow: "shadow line1"},
		{Line: 2, Original: "original line2", Shadow: "shadow line2"},
	}

	result := m.ResolveLastWriteWins(original, shadow, conflicts)
	if string(result) != "shadow line1\nshadow line2" {
		t.Errorf("expected shadow to win, got %q", result)
	}
}

func TestResolveAutoMerge(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	m := NewMerger(cfg)

	original := []byte("line1\nline2\nline3")
	shadow := []byte("line1\nchanged\nline3")
	conflicts := []Conflict{
		{Line: 2, Original: "line2", Shadow: "changed"},
	}

	result := m.ResolveAutoMerge(original, shadow, conflicts)
	if string(result) != "line1\nchanged\nline3" {
		t.Errorf("expected merged result, got %q", result)
	}
}

func TestApplyToOriginal(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	m := NewMerger(cfg)
	ctx := context.Background()

	originalPath := filepath.Join(tmpDir, "new.txt")
	if err := m.ApplyToOriginal(ctx, originalPath, []byte("new content")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != "new content" {
		t.Errorf("expected 'new content', got %q", content)
	}
}

func TestApplyToOriginal_PreservesPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	m := NewMerger(cfg)
	ctx := context.Background()

	originalPath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(originalPath, []byte("content"), 0o755); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	if err := m.ApplyToOriginal(ctx, originalPath, []byte("updated")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, _ := os.Stat(originalPath)
	if info.Mode().Perm() != 0o755 {
		t.Errorf("expected permissions 0755, got %o", info.Mode().Perm())
	}
}

// --- Integration Tests ---

func TestFullWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	lm := NewLockManager(cfg)
	sm := NewShadowManager(cfg)
	m := NewMerger(cfg)
	ctx := context.Background()

	// Create original file
	originalPath := filepath.Join(tmpDir, "README.md")
	if err := os.WriteFile(originalPath, []byte("# Project\n\nOriginal content"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Step 1: Acquire lock
	lockInfo, err := lm.AcquireLock(ctx, originalPath, "agent1")
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if lockInfo.AgentID != "agent1" {
		t.Errorf("expected agent1, got %s", lockInfo.AgentID)
	}

	// Step 2: Create shadow
	shadow, err := sm.CreateShadow(ctx, originalPath, "agent1")
	if err != nil {
		t.Fatalf("create shadow: %v", err)
	}

	// Step 3: Write changes to shadow
	newContent := []byte("# Project\n\nUpdated by agent1")
	err = sm.WriteShadow(ctx, originalPath, "agent1", newContent)
	if err != nil {
		t.Fatalf("write shadow: %v", err)
	}

	// Step 4: Merge
	result, err := m.Merge(ctx, shadow)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !result.Success {
		t.Error("expected merge success")
	}

	// Verify original was updated
	content, _ := os.ReadFile(originalPath)
	if string(content) != "# Project\n\nUpdated by agent1" {
		t.Errorf("expected updated content, got %q", content)
	}

	// Step 5: Release lock
	err = lm.ReleaseLock(ctx, originalPath, "agent1")
	if err != nil {
		t.Fatalf("release lock: %v", err)
	}

	// Verify unlocked
	locked, _, _ := lm.IsLocked(ctx, originalPath)
	if locked {
		t.Error("should be unlocked")
	}

	// Cleanup shadow
	err = sm.DeleteShadow(ctx, originalPath, "agent1")
	if err != nil {
		t.Fatalf("delete shadow: %v", err)
	}
}

func TestConcurrentAgents(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	cfg.LockTimeout = 200 * time.Millisecond
	lm := NewLockManager(cfg)
	sm := NewShadowManager(cfg)
	m := NewMerger(cfg)
	ctx := context.Background()

	// Create two files
	files := map[string]string{
		"file1.txt": "content1",
		"file2.txt": "content2",
	}
	for name, content := range files {
		p := filepath.Join(tmpDir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to create %s: %v", name, err)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, 4)

	// Agent1 works on file1, Agent2 works on file2 (no conflict)
	agentWork := func(agentID, fileName, newContent string) {
		defer wg.Done()

		originalPath := filepath.Join(tmpDir, fileName)

		// Lock
		_, err := lm.AcquireLock(ctx, originalPath, AgentID(agentID))
		if err != nil {
			errs <- err
			return
		}

		// Shadow
		shadow, err := sm.CreateShadow(ctx, originalPath, AgentID(agentID))
		if err != nil {
			errs <- err
			return
		}

		// Write
		err = sm.WriteShadow(ctx, originalPath, AgentID(agentID), []byte(newContent))
		if err != nil {
			errs <- err
			return
		}

		// Merge
		_, err = m.Merge(ctx, shadow)
		if err != nil {
			errs <- err
			return
		}

		// Release
		err = lm.ReleaseLock(ctx, originalPath, AgentID(agentID))
		if err != nil {
			errs <- err
			return
		}

		// Cleanup
		_ = sm.DeleteShadow(ctx, originalPath, AgentID(agentID))
	}

	wg.Add(2)
	go agentWork("agent1", "file1.txt", "updated by agent1")
	go agentWork("agent2", "file2.txt", "updated by agent2")
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}

	// Verify both files updated
	content1, _ := os.ReadFile(filepath.Join(tmpDir, "file1.txt"))
	content2, _ := os.ReadFile(filepath.Join(tmpDir, "file2.txt"))
	if string(content1) != "updated by agent1" {
		t.Errorf("file1: expected 'updated by agent1', got %q", content1)
	}
	if string(content2) != "updated by agent2" {
		t.Errorf("file2: expected 'updated by agent2', got %q", content2)
	}
}

func TestMerge_NewFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	m := NewMerger(cfg)
	ctx := context.Background()

	newPath := filepath.Join(tmpDir, "new.txt")

	shadow := &ShadowDoc{
		OriginalPath: newPath,
		ShadowPath:   filepath.Join(tmpDir, "shadow.tmp"),
		AgentID:      "agent1",
		Content:      []byte("brand new file"),
	}

	result, err := m.Merge(ctx, shadow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success when creating new file")
	}

	content, _ := os.ReadFile(newPath)
	if string(content) != "brand new file" {
		t.Errorf("expected 'brand new file', got %q", content)
	}
}

func TestMerge_ManualConflict(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	cfg.ConflictStrategy = ConflictManual
	m := NewMerger(cfg)
	ctx := context.Background()

	originalPath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(originalPath, []byte("line1\noriginal line2"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	shadow := &ShadowDoc{
		OriginalPath: originalPath,
		ShadowPath:   filepath.Join(tmpDir, "shadow.tmp"),
		AgentID:      "agent1",
		Content:      []byte("line1\nshadow line2"),
	}

	result, err := m.Merge(ctx, shadow)
	if err == nil {
		t.Fatal("expected error with manual conflict strategy")
	}

	var collabErr *CollabError
	if !errors.As(err, &collabErr) {
		t.Fatalf("expected CollabError, got %T", err)
	}
	if collabErr.Code != ErrCodeMergeConflict {
		t.Errorf("expected ErrCodeMergeConflict, got %s", collabErr.Code)
	}
	if result.Success {
		t.Error("expected success=false")
	}
}

// --- Error Tests ---

func TestCollabError_Error(t *testing.T) {
	e := &CollabError{
		Code:    ErrCodeLockDenied,
		Message: "file is locked",
		Path:    "test.txt",
		AgentID: "agent1",
	}
	msg := e.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	// Verify it contains the code
	if !contains(msg, "LOCK_DENIED") {
		t.Error("expected error message to contain LOCK_DENIED")
	}
}

func TestCollabError_Unwrap(t *testing.T) {
	inner := os.ErrNotExist
	e := &CollabError{
		Code:    ErrCodeFileNotFound,
		Message: "not found",
		Err:     inner,
	}
	if !errors.Is(e, os.ErrNotExist) {
		t.Error("expected errors.Is to find inner error")
	}
}

func TestCollabError_ErrorNoPath(t *testing.T) {
	e := &CollabError{
		Code:    ErrCodeLockDenied,
		Message: "locked",
	}
	msg := e.Error()
	if contains(msg, "[path=") {
		t.Error("should not contain path when empty")
	}
}

// --- Shadow File Naming ---

func TestShadowFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	sm := NewShadowManager(cfg)

	path := sm.shadowFilePath("/some/dir/file.txt", "agent1")
	if filepath.Ext(path) != ".tmp" {
		t.Errorf("expected .tmp extension, got %s", filepath.Ext(path))
	}
	if !contains(path, "agent1") {
		t.Error("expected path to contain agent ID")
	}
}

// --- LoadLocksFromDisk ---

func TestLoadLocksFromDisk(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	lm := NewLockManager(cfg)
	ctx := context.Background()

	// Create a lock
	_, err := lm.AcquireLock(ctx, "test.txt", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create new manager and load from disk
	lm2 := NewLockManager(cfg)
	err = lm2.LoadLocksFromDisk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	locked, info, _ := lm2.IsLocked(ctx, "test.txt")
	if !locked {
		t.Error("expected file to be locked after loading from disk")
	}
	if info.AgentID != "agent1" {
		t.Errorf("expected agent1, got %s", info.AgentID)
	}
}

func TestLoadLocksFromDisk_NoDir(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := testConfig(tmpDir)
	cfg.LockDir = filepath.Join(tmpDir, "nonexistent")
	lm := NewLockManager(cfg)

	// Should not error when directory doesn't exist
	err := lm.LoadLocksFromDisk()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- helpers ---

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
