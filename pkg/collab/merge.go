package collab

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
)

// Merger handles merging shadow documents into originals.
type Merger struct {
	config Config
}

// NewMerger creates a new Merger.
func NewMerger(cfg Config) *Merger {
	return &Merger{config: cfg}
}

// Merge merges a shadow document into the original file.
func (m *Merger) Merge(_ context.Context, shadow *ShadowDoc) (*MergeResult, error) {
	result := &MergeResult{
		MergedPath: shadow.OriginalPath,
		ShadowPath: shadow.ShadowPath,
		Strategy:   m.config.ConflictStrategy,
	}

	// Read original content (may not exist)
	original, err := os.ReadFile(shadow.OriginalPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, &CollabError{
			Code:    ErrCodeFileNotFound,
			Message: "failed to read original file",
			Path:    shadow.OriginalPath,
			AgentID: shadow.AgentID,
			Err:     err,
		}
	}

	// If original doesn't exist, shadow becomes the original
	if os.IsNotExist(err) {
		if err := m.ApplyToOriginal(context.Background(), shadow.OriginalPath, shadow.Content); err != nil {
			return nil, err
		}
		result.Success = true
		return result, nil
	}

	// No changes — skip merge
	if bytes.Equal(original, shadow.Content) {
		result.Success = true
		return result, nil
	}

	// Detect conflicts
	conflicts := m.DetectConflicts(original, shadow.Content)
	if len(conflicts) == 0 {
		// No conflicts — apply shadow directly
		if err := m.ApplyToOriginal(context.Background(), shadow.OriginalPath, shadow.Content); err != nil {
			return nil, err
		}
		result.Success = true
		return result, nil
	}

	// Resolve conflicts based on strategy
	var merged []byte
	switch m.config.ConflictStrategy {
	case ConflictLastWriteWins:
		merged = m.ResolveLastWriteWins(original, shadow.Content, conflicts)
	case ConflictAutoMerge:
		merged = m.ResolveAutoMerge(original, shadow.Content, conflicts)
	case ConflictManual:
		result.Conflicts = conflicts
		result.Success = false
		return result, &CollabError{
			Code:    ErrCodeMergeConflict,
			Message: "merge conflicts require manual resolution",
			Path:    shadow.OriginalPath,
			AgentID: shadow.AgentID,
		}
	default:
		merged = m.ResolveLastWriteWins(original, shadow.Content, conflicts)
	}

	if err := m.ApplyToOriginal(context.Background(), shadow.OriginalPath, merged); err != nil {
		return nil, err
	}

	result.Conflicts = conflicts
	result.Success = true
	return result, nil
}

// DetectConflicts performs line-by-line comparison and returns conflicts.
// A conflict is detected when both original and shadow have changed the same line.
func (m *Merger) DetectConflicts(original, shadow []byte) []Conflict {
	origLines := strings.Split(string(original), "\n")
	shadowLines := strings.Split(string(shadow), "\n")

	var conflicts []Conflict
	maxLines := len(origLines)
	if len(shadowLines) > maxLines {
		maxLines = len(shadowLines)
	}

	for i := 0; i < maxLines; i++ {
		var origLine, shadowLine string
		if i < len(origLines) {
			origLine = origLines[i]
		}
		if i < len(shadowLines) {
			shadowLine = shadowLines[i]
		}

		// Both sides have content and they differ
		if origLine != shadowLine {
			// Check if this is truly a conflict (both sides changed from some baseline)
			// For simplicity: any difference is a potential conflict
			conflicts = append(conflicts, Conflict{
				Path:     "",
				Line:     i + 1,
				Original: origLine,
				Shadow:   shadowLine,
			})
		}
	}

	return conflicts
}

// ResolveLastWriteWins resolves conflicts by letting the shadow win.
func (m *Merger) ResolveLastWriteWins(_ /* original */, shadow []byte, _ []Conflict) []byte {
	// Shadow always wins in last-write-wins
	return shadow
}

// ResolveAutoMerge attempts to merge non-conflicting lines and lets shadow win on conflicts.
func (m *Merger) ResolveAutoMerge(original, shadow []byte, conflicts []Conflict) []byte {
	origLines := strings.Split(string(original), "\n")
	shadowLines := strings.Split(string(shadow), "\n")

	// Build a set of conflict line numbers (1-indexed)
	conflictLines := make(map[int]bool)
	for _, c := range conflicts {
		conflictLines[c.Line] = true
	}

	maxLines := len(origLines)
	if len(shadowLines) > maxLines {
		maxLines = len(shadowLines)
	}

	var result []string
	for i := 0; i < maxLines; i++ {
		lineNum := i + 1
		var origLine, shadowLine string
		if i < len(origLines) {
			origLine = origLines[i]
		}
		if i < len(shadowLines) {
			shadowLine = shadowLines[i]
		}

		if conflictLines[lineNum] {
			// For auto-merge: try to combine, otherwise shadow wins
			merged := autoMergeLine(origLine, shadowLine)
			result = append(result, merged)
		} else if i < len(shadowLines) {
			result = append(result, shadowLine)
		} else {
			result = append(result, origLine)
		}
	}

	return []byte(strings.Join(result, "\n"))
}

// autoMergeLine attempts to merge a single line from original and shadow.
// If both are non-empty and different, shadow wins.
func autoMergeLine(orig, shadow string) string {
	if orig == shadow {
		return orig
	}
	if orig == "" {
		return shadow
	}
	if shadow == "" {
		return orig
	}
	// Both changed — shadow wins
	return shadow
}

// ApplyToOriginal writes content to the original file, preserving permissions if the file exists.
func (m *Merger) ApplyToOriginal(_ context.Context, originalPath string, content []byte) error {
	// Ensure directory exists
	dir := filepath.Dir(originalPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &CollabError{
			Code:    ErrCodeFileNotFound,
			Message: "failed to create directory",
			Path:    originalPath,
			Err:     err,
		}
	}

	// Check existing permissions
	var perm os.FileMode = 0o644
	if info, err := os.Stat(originalPath); err == nil {
		perm = info.Mode().Perm()
	}

	if err := os.WriteFile(originalPath, content, perm); err != nil {
		return &CollabError{
			Code:    ErrCodeFileNotFound,
			Message: "failed to write merged file",
			Path:    originalPath,
			Err:     err,
		}
	}

	return nil
}
