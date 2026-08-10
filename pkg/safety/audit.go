package safety

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// AuditLog provides tamper-evident audit logging.
type AuditLog struct {
	file     *os.File
	mu       sync.Mutex
	prevHash string
}

// NewAuditLog creates a new audit log.
func NewAuditLog(path string) (*AuditLog, error) {
	if err := os.MkdirAll(getDir(path), 0755); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &AuditLog{
		file: file,
	}, nil
}

// Log writes an audit entry.
func (a *AuditLog) Log(req *ValidationRequest, result *ValidationResult) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	entry := AuditEntry{
		ID:        fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Request:   req,
		Result:    result,
		PrevHash:  a.prevHash,
	}

	// Compute hash
	data, _ := json.Marshal(entry)
	hash := sha256.Sum256(append([]byte(a.prevHash), data...))
	entry.Hash = fmt.Sprintf("%x", hash)
	a.prevHash = entry.Hash

	// Write to file
	if _, err := a.file.Write(append(data, '\n')); err != nil {
		return err
	}

	return nil
}

// GetEntries retrieves audit entries.
func (a *AuditLog) GetEntries(filter AuditFilter) ([]AuditEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// For now, return empty - would need to read and parse the file
	return []AuditEntry{}, nil
}

// VerifyChain verifies the integrity of the audit log chain.
func (a *AuditLog) VerifyChain() (bool, string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Read all entries
	if _, err := a.file.Seek(0, 0); err != nil {
		return false, "seek failed"
	}

	dec := json.NewDecoder(a.file)
	var prevHash string
	count := 0

	for dec.More() {
		var entry AuditEntry
		if err := dec.Decode(&entry); err != nil {
			return false, fmt.Sprintf("decode failed at entry %d: %v", count, err)
		}

		// Verify chain
		if entry.PrevHash != prevHash {
			return false, fmt.Sprintf("chain broken at entry %d", count)
		}

		// Verify hash
		entryCopy := entry
		entryCopy.Hash = ""
		entryCopy.PrevHash = ""
		data, _ := json.Marshal(entryCopy)
		hash := sha256.Sum256(append([]byte(prevHash), data...))
		computedHash := fmt.Sprintf("%x", hash)

		if computedHash != entry.Hash {
			return false, fmt.Sprintf("hash mismatch at entry %d", count)
		}

		prevHash = entry.Hash
		count++
	}

	return true, "chain valid"
}

// Close closes the audit log.
func (a *AuditLog) Close() error {
	if a.file != nil {
		return a.file.Close()
	}
	return nil
}

// getDir extracts the directory from a file path.
func getDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}