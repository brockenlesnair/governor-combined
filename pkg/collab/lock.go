package collab

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LockManager manages logical file locks via JSON lock files.
type LockManager struct {
	mu     sync.RWMutex
	locks  map[string]*LockInfo // path → lock
	config Config
}

// NewLockManager creates a new LockManager with the given config.
func NewLockManager(cfg Config) *LockManager {
	return &LockManager{
		locks:  make(map[string]*LockInfo),
		config: cfg,
	}
}

// lockFilePath returns the path to the lock file for a given file path.
func (lm *LockManager) lockFilePath(path string) string {
	encoded := base64.URLEncoding.EncodeToString([]byte(path))
	return filepath.Join(lm.config.LockDir, encoded+".lock")
}

// AcquireLock attempts to acquire a lock on the given file for the given agent.
// If the file is already locked by another agent and the lock hasn't expired, it returns an error.
// If the same agent already holds the lock, it extends the expiry.
func (lm *LockManager) AcquireLock(_ context.Context, path string, agentID AgentID) (*LockInfo, error) {
	if agentID == "" {
		return nil, &CollabError{
			Code:    ErrCodeInvalidAgent,
			Message: "agent ID cannot be empty",
			Path:    path,
		}
	}

	lm.mu.Lock()
	defer lm.mu.Unlock()

	now := time.Now()

	// Check existing in-memory lock
	if existing, ok := lm.locks[path]; ok {
		if existing.AgentID == agentID {
			// Same agent — extend
			existing.ExpiresAt = now.Add(lm.config.LockTimeout)
			lm.writeLockFile(existing)
			return existing, nil
		}
		// Different agent — check expiry
		if now.Before(existing.ExpiresAt) {
			return nil, &CollabError{
				Code:    ErrCodeLockDenied,
				Message: "file is locked by another agent",
				Path:    path,
				AgentID: existing.AgentID,
			}
		}
		// Lock expired — allow takeover
	}

	lockInfo := &LockInfo{
		Path:       path,
		AgentID:    agentID,
		AcquiredAt: now,
		ExpiresAt:  now.Add(lm.config.LockTimeout),
	}

	lm.locks[path] = lockInfo
	if err := lm.writeLockFile(lockInfo); err != nil {
		// Rollback in-memory
		delete(lm.locks, path)
		return nil, err
	}

	return lockInfo, nil
}

// ReleaseLock releases a lock held by the given agent.
func (lm *LockManager) ReleaseLock(_ context.Context, path string, agentID AgentID) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	existing, ok := lm.locks[path]
	if !ok {
		return &CollabError{
			Code:    ErrCodeNotLocked,
			Message: "file is not locked",
			Path:    path,
		}
	}

	if existing.AgentID != agentID {
		return &CollabError{
			Code:    ErrCodeLockDenied,
			Message: "only the lock holder can release the lock",
			Path:    path,
			AgentID: agentID,
		}
	}

	delete(lm.locks, path)
	lm.removeLockFile(path)
	return nil
}

// ExtendLock extends the expiry of a lock held by the given agent.
func (lm *LockManager) ExtendLock(_ context.Context, path string, agentID AgentID) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	existing, ok := lm.locks[path]
	if !ok {
		return &CollabError{
			Code:    ErrCodeNotLocked,
			Message: "file is not locked",
			Path:    path,
		}
	}

	if existing.AgentID != agentID {
		return &CollabError{
			Code:    ErrCodeLockDenied,
			Message: "only the lock holder can extend the lock",
			Path:    path,
			AgentID: agentID,
		}
	}

	existing.ExpiresAt = time.Now().Add(lm.config.LockTimeout)
	lm.writeLockFile(existing)
	return nil
}

// IsLocked checks if a file is currently locked.
func (lm *LockManager) IsLocked(_ context.Context, path string) (bool, *LockInfo, error) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	existing, ok := lm.locks[path]
	if !ok {
		return false, nil, nil
	}

	// Check expiry
	if time.Now().After(existing.ExpiresAt) {
		return false, nil, nil
	}

	return true, existing, nil
}

// CleanupExpired removes all expired locks and returns the count removed.
func (lm *LockManager) CleanupExpired() int {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	now := time.Now()
	count := 0
	for path, info := range lm.locks {
		if now.After(info.ExpiresAt) {
			delete(lm.locks, path)
			lm.removeLockFile(path)
			count++
		}
	}
	return count
}

// ListLocks returns all active (non-expired) locks.
func (lm *LockManager) ListLocks() []LockInfo {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	now := time.Now()
	var result []LockInfo
	for _, info := range lm.locks {
		if now.Before(info.ExpiresAt) {
			result = append(result, *info)
		}
	}
	return result
}

// writeLockFile persists a lock to disk as JSON.
func (lm *LockManager) writeLockFile(info *LockInfo) error {
	if err := os.MkdirAll(lm.config.LockDir, 0o755); err != nil {
		return &CollabError{
			Code:    ErrCodeLockDenied,
			Message: "failed to create lock directory",
			Path:    info.Path,
			Err:     err,
		}
	}

	data, err := json.Marshal(info)
	if err != nil {
		return &CollabError{
			Code:    ErrCodeLockDenied,
			Message: "failed to serialize lock info",
			Path:    info.Path,
			Err:     err,
		}
	}

	lockFile := lm.lockFilePath(info.Path)
	if err := os.WriteFile(lockFile, data, 0o644); err != nil {
		return &CollabError{
			Code:    ErrCodeLockDenied,
			Message: "failed to write lock file",
			Path:    info.Path,
			Err:     err,
		}
	}
	return nil
}

// removeLockFile removes the lock file from disk.
func (lm *LockManager) removeLockFile(path string) {
	lockFile := lm.lockFilePath(path)
	os.Remove(lockFile) // ignore error — file may not exist
}

// LoadLocksFromDisk loads existing lock files into the in-memory map.
// Useful for recovery after a restart.
func (lm *LockManager) LoadLocksFromDisk() error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	entries, err := os.ReadDir(lm.config.LockDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		lockFile := filepath.Join(lm.config.LockDir, entry.Name())
		data, err := os.ReadFile(lockFile)
		if err != nil {
			continue
		}
		var info LockInfo
		if err := json.Unmarshal(data, &info); err != nil {
			continue
		}
		if time.Now().Before(info.ExpiresAt) {
			lm.locks[info.Path] = &info
		} else {
			os.Remove(lockFile)
		}
	}
	return nil
}
