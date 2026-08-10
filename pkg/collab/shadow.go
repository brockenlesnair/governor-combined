package collab

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ShadowManager manages temporary shadow copies of files.
type ShadowManager struct {
	mu      sync.RWMutex
	shadows map[string]*ShadowDoc // composite key: "originalPath|agentID"
	config  Config
}

// NewShadowManager creates a new ShadowManager.
func NewShadowManager(cfg Config) *ShadowManager {
	return &ShadowManager{
		shadows: make(map[string]*ShadowDoc),
		config:  cfg,
	}
}

// shadowKey builds the map key for a given original path and agent.
func shadowKey(originalPath string, agentID AgentID) string {
	return string(originalPath) + "|" + string(agentID)
}

// shadowFilePath builds the on-disk path for a shadow file.
func (sm *ShadowManager) shadowFilePath(originalPath string, agentID AgentID) string {
	base := filepath.Base(originalPath)
	dir := filepath.Dir(originalPath)
	timestamp := time.Now().Format("20060102150405")
	name := fmt.Sprintf("%s.%s.%s.tmp", base, agentID, timestamp)
	return filepath.Join(sm.config.ShadowDir, dir, name)
}

// CreateShadow creates a shadow copy of the original file for the given agent.
func (sm *ShadowManager) CreateShadow(_ context.Context, originalPath string, agentID AgentID) (*ShadowDoc, error) {
	if agentID == "" {
		return nil, &CollabError{
			Code:    ErrCodeInvalidAgent,
			Message: "agent ID cannot be empty",
			Path:    originalPath,
		}
	}

	// Check if shadow already exists
	sm.mu.RLock()
	key := shadowKey(originalPath, agentID)
	if _, exists := sm.shadows[key]; exists {
		sm.mu.RUnlock()
		return nil, &CollabError{
			Code:    ErrCodeShadowExists,
			Message: "shadow already exists for this agent",
			Path:    originalPath,
			AgentID: agentID,
		}
	}
	sm.mu.RUnlock()

	// Read original content
	content, err := os.ReadFile(originalPath)
	if err != nil {
		return nil, &CollabError{
			Code:    ErrCodeFileNotFound,
			Message: "failed to read original file",
			Path:    originalPath,
			AgentID: agentID,
			Err:     err,
		}
	}

	shadowPath := sm.shadowFilePath(originalPath, agentID)

	// Create shadow directory
	if err := os.MkdirAll(filepath.Dir(shadowPath), 0o755); err != nil {
		return nil, &CollabError{
			Code:    ErrCodeFileNotFound,
			Message: "failed to create shadow directory",
			Path:    originalPath,
			AgentID: agentID,
			Err:     err,
		}
	}

	// Write shadow file
	if err := os.WriteFile(shadowPath, content, 0o644); err != nil {
		return nil, &CollabError{
			Code:    ErrCodeFileNotFound,
			Message: "failed to write shadow file",
			Path:    originalPath,
			AgentID: agentID,
			Err:     err,
		}
	}

	hash := computeHash(content)
	now := time.Now()

	doc := &ShadowDoc{
		OriginalPath: originalPath,
		ShadowPath:   shadowPath,
		AgentID:      agentID,
		CreatedAt:    now,
		ModifiedAt:    now,
		Content:       content,
		Hash:          hash,
	}

	sm.mu.Lock()
	sm.shadows[key] = doc
	sm.mu.Unlock()

	return doc, nil
}

// ReadShadow reads the content of a shadow file.
func (sm *ShadowManager) ReadShadow(_ context.Context, originalPath string, agentID AgentID) ([]byte, error) {
	sm.mu.RLock()
	key := shadowKey(originalPath, agentID)
	doc, ok := sm.shadows[key]
	sm.mu.RUnlock()

	if !ok {
		return nil, &CollabError{
			Code:    ErrCodeNotLocked,
			Message: "shadow not found",
			Path:    originalPath,
			AgentID: agentID,
		}
	}

	content, err := os.ReadFile(doc.ShadowPath)
	if err != nil {
		return nil, &CollabError{
			Code:    ErrCodeFileNotFound,
			Message: "failed to read shadow file",
			Path:    originalPath,
			AgentID: agentID,
			Err:     err,
		}
	}
	return content, nil
}

// WriteShadow writes content to the shadow file and updates metadata.
func (sm *ShadowManager) WriteShadow(_ context.Context, originalPath string, agentID AgentID, content []byte) error {
	sm.mu.Lock()
	key := shadowKey(originalPath, agentID)
	doc, ok := sm.shadows[key]
	sm.mu.Unlock()

	if !ok {
		return &CollabError{
			Code:    ErrCodeNotLocked,
			Message: "shadow not found",
			Path:    originalPath,
			AgentID: agentID,
		}
	}

	if err := os.WriteFile(doc.ShadowPath, content, 0o644); err != nil {
		return &CollabError{
			Code:    ErrCodeFileNotFound,
			Message: "failed to write shadow file",
			Path:    originalPath,
			AgentID: agentID,
			Err:     err,
		}
	}

	sm.mu.Lock()
	doc.Content = content
	doc.Hash = computeHash(content)
	doc.ModifiedAt = time.Now()
	sm.mu.Unlock()

	return nil
}

// DeleteShadow removes a shadow file and its metadata.
func (sm *ShadowManager) DeleteShadow(_ context.Context, originalPath string, agentID AgentID) error {
	sm.mu.Lock()
	key := shadowKey(originalPath, agentID)
	doc, ok := sm.shadows[key]
	if !ok {
		sm.mu.Unlock()
		return &CollabError{
			Code:    ErrCodeNotLocked,
			Message: "shadow not found",
			Path:    originalPath,
			AgentID: agentID,
		}
	}
	delete(sm.shadows, key)
	sm.mu.Unlock()

	os.Remove(doc.ShadowPath) // ignore error
	return nil
}

// ListShadows returns all shadows, optionally filtered by agent.
// If agentID is empty, returns all shadows.
func (sm *ShadowManager) ListShadows(agentID AgentID) []ShadowDoc {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var result []ShadowDoc
	for _, doc := range sm.shadows {
		if agentID == "" || doc.AgentID == agentID {
			result = append(result, *doc)
		}
	}
	return result
}

// GetShadow retrieves a specific shadow document.
func (sm *ShadowManager) GetShadow(originalPath string, agentID AgentID) (*ShadowDoc, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	key := shadowKey(originalPath, agentID)
	doc, ok := sm.shadows[key]
	if !ok {
		return nil, false
	}
	return doc, true
}

// computeHash computes the SHA-256 hex digest of content.
func computeHash(content []byte) string {
	h := sha256.Sum256(content)
	return fmt.Sprintf("%x", h)
}

// ShadowFilePathForTest returns the shadow file path for testing.
func (sm *ShadowManager) ShadowFilePathForTest(originalPath string, agentID AgentID) string {
	return sm.shadowFilePath(originalPath, agentID)
}

// shadowFilePattern returns a pattern for finding shadow files for a given original path.
func shadowFilePattern(originalPath string) string {
	base := filepath.Base(originalPath)
	dir := filepath.Dir(originalPath)
	pattern := fmt.Sprintf("%s.%s.*.tmp", base, "*")
	_ = strings.Contains(pattern, "*") // ensure pattern uses wildcard
	return filepath.Join(".governor", "shadows", dir, pattern)
}
