package docgov

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brockenlesnair/governor-combined/pkg/staleness"
)

// DocumentRegistry manages the inventory of all governance documents in a project.
type DocumentRegistry struct {
	mu           sync.RWMutex
	documents    map[string]*Document // key: path
	typeConfigs  []DocumentTypeConfig
	validatorReg *ValidatorRegistry
	stalenessChk *staleness.Checker
	projectRoot  string
}

// NewDocumentRegistry creates a new document registry.
func NewDocumentRegistry(projectRoot string, configs []DocumentTypeConfig, validatorReg *ValidatorRegistry, stalenessChk *staleness.Checker) *DocumentRegistry {
	if configs == nil {
		configs = GetDefaultDocumentTypeConfigs()
	}
	if validatorReg == nil {
		validatorReg = NewValidatorRegistry()
	}

	dr := &DocumentRegistry{
		documents:    make(map[string]*Document),
		typeConfigs:  configs,
		validatorReg: validatorReg,
		stalenessChk: stalenessChk,
		projectRoot:  projectRoot,
	}

	return dr
}

// Scan scans the project for governance documents and builds the registry.
func (dr *DocumentRegistry) Scan(ctx context.Context) error {
	dr.mu.Lock()
	defer dr.mu.Unlock()

	dr.documents = make(map[string]*Document)

	for _, config := range dr.typeConfigs {
		for _, pattern := range config.PathPatterns {
			matches, err := filepath.Glob(filepath.Join(dr.projectRoot, pattern))
			if err != nil {
				continue
			}

			for _, match := range matches {
				relPath, _ := filepath.Rel(dr.projectRoot, match)
				if dr.documents[relPath] != nil {
					continue // Already registered
				}

				doc := dr.createDocument(relPath, config)
				dr.documents[relPath] = doc
			}
		}
	}

	return nil
}

// createDocument creates a Document from a file path and type config.
func (dr *DocumentRegistry) createDocument(relPath string, config DocumentTypeConfig) *Document {
	fullPath := filepath.Join(dr.projectRoot, relPath)
	info, err := os.Stat(fullPath)
	if err != nil {
		return &Document{
			ID:        relPath,
			Type:      config.Type,
			Path:      relPath,
			Status:    DocumentStatusMissing,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Freshness: FreshnessInfo{
				Level: FreshnessLevelUnknown,
			},
			Validation: ValidationResult{
				Valid:     false,
				Errors:    []string{fmt.Sprintf("cannot stat file: %v", err)},
				CheckedAt: time.Now(),
			},
		}
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return &Document{
			ID:        relPath,
			Type:      config.Type,
			Path:      relPath,
			Status:    DocumentStatusMissing,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Freshness: FreshnessInfo{
				Level: FreshnessLevelUnknown,
			},
			Validation: ValidationResult{
				Valid:     false,
				Errors:    []string{fmt.Sprintf("cannot read file: %v", err)},
				CheckedAt: time.Now(),
			},
		}
	}

	// Extract title from frontmatter or first heading
	title := dr.extractTitle(content, relPath)

	// Calculate freshness
	freshness := dr.calculateFreshness(info.ModTime(), config)

	// Validate
	validation := dr.validatorReg.Validate(context.Background(), content, config)

	doc := &Document{
		ID:          relPath,
		Type:        config.Type,
		Path:        relPath,
		Title:       title,
		Status:      DocumentStatusDraft, // Default, would be overridden by frontmatter
		CreatedAt:   info.ModTime(),
		UpdatedAt:   info.ModTime(),
		Freshness:   freshness,
		Validation:  validation,
		Metadata:    map[string]string{"config_validator": config.Validator},
	}

	// Try to get status from frontmatter
	if fm, _, err := ParseFrontmatter(content); err == nil {
		if status, ok := getString(fm, "status"); ok {
			doc.Status = mapFrontmatterStatus(strings.ToLower(status))
		}
		if owner, ok := getString(fm, "owner"); ok {
			doc.Owner = owner
		}
		if tags, ok := getStringSlice(fm, "tags"); ok {
			doc.Tags = tags
		}
		if approvedAt, ok := getString(fm, "approved_at"); ok {
			if t, err := time.Parse(time.RFC3339, approvedAt); err == nil {
				doc.ApprovedAt = &t
			}
		}
	}

	return doc
}

func mapFrontmatterStatus(status string) DocumentStatus {
	switch status {
	case "accepted", "approved":
		return DocumentStatusApproved
	case "draft":
		return DocumentStatusDraft
	case "review":
		return DocumentStatusReview
	case "deprecated":
		return DocumentStatusDeprecated
	case "superseded":
		return DocumentStatusSuperseded
	case "proposed", "rejected":
		// ADR-specific statuses that map to review/draft
		return DocumentStatusReview
	default:
		return DocumentStatus(status)
	}
}

// extractTitle extracts the document title from frontmatter or first heading.
func (dr *DocumentRegistry) extractTitle(content []byte, fallback string) string {
	fm, body, err := ParseFrontmatter(content)
	if err == nil {
		if title, ok := getString(fm, "title"); ok && title != "" {
			return title
		}
	}

	// Try to find first markdown heading
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
		if strings.HasPrefix(line, "## ") {
			return strings.TrimSpace(line[3:])
		}
	}

	// Fallback to filename
	base := filepath.Base(fallback)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// calculateFreshness calculates the freshness info for a document.
func (dr *DocumentRegistry) calculateFreshness(modTime time.Time, config DocumentTypeConfig) FreshnessInfo {
	age := time.Since(modTime)
	freshness := FreshnessInfo{
		LastModified: modTime,
		Age:          age,
		MaxAge:       config.MaxAge,
		WarnAge:      config.WarnAge,
	}

	if config.MaxAge == 0 && config.WarnAge == 0 {
		freshness.Level = FreshnessLevelUnknown
		return freshness
	}

	if config.MaxAge > 0 && age > config.MaxAge {
		freshness.Level = FreshnessLevelStale
	} else if config.WarnAge > 0 && age > config.WarnAge {
		freshness.Level = FreshnessLevelWarning
	} else {
		freshness.Level = FreshnessLevelFresh
	}

	return freshness
}

// GetDocument returns a document by path. Accepts both absolute and relative paths.
func (dr *DocumentRegistry) GetDocument(path string) (*Document, bool) {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	if doc, ok := dr.documents[path]; ok {
		return doc, ok
	}

	if dr.projectRoot != "" {
		relPath, err := filepath.Rel(dr.projectRoot, path)
		if err == nil {
			if doc, ok := dr.documents[relPath]; ok {
				return doc, ok
			}
		}
	}

	return nil, false
}

// ListDocuments returns all documents, optionally filtered by type.
func (dr *DocumentRegistry) ListDocuments(docType DocumentType) []*Document {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	var result []*Document
	for _, doc := range dr.documents {
		if docType == "" || doc.Type == docType {
			result = append(result, doc)
		}
	}
	return result
}

// GetDocumentsByType returns documents grouped by type.
func (dr *DocumentRegistry) GetDocumentsByType() map[DocumentType][]*Document {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	result := make(map[DocumentType][]*Document)
	for _, doc := range dr.documents {
		result[doc.Type] = append(result[doc.Type], doc)
	}
	return result
}

// GetMissingRequiredTypes returns document types that are required but have no documents.
func (dr *DocumentRegistry) GetMissingRequiredTypes() []DocumentTypeConfig {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	typeCount := make(map[DocumentType]int)
	for _, doc := range dr.documents {
		typeCount[doc.Type]++
	}

	var missing []DocumentTypeConfig
	for _, config := range dr.typeConfigs {
		if config.Required && typeCount[config.Type] == 0 {
			missing = append(missing, config)
		}
	}
	return missing
}

// GetStaleDocuments returns all documents that are stale.
func (dr *DocumentRegistry) GetStaleDocuments() []*Document {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	var result []*Document
	for _, doc := range dr.documents {
		if doc.Freshness.Level == FreshnessLevelStale {
			result = append(result, doc)
		}
	}
	return result
}

// GetWarningDocuments returns all documents that are in warning state.
func (dr *DocumentRegistry) GetWarningDocuments() []*Document {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	var result []*Document
	for _, doc := range dr.documents {
		if doc.Freshness.Level == FreshnessLevelWarning {
			result = append(result, doc)
		}
	}
	return result
}

// ValidateDocument validates a specific document.
func (dr *DocumentRegistry) ValidateDocument(ctx context.Context, path string) (*Document, error) {
	dr.mu.RLock()

	doc, ok := dr.documents[path]
	if !ok && dr.projectRoot != "" {
		relPath, err := filepath.Rel(dr.projectRoot, path)
		if err == nil {
			doc, ok = dr.documents[relPath]
		}
	}
	dr.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("document not found: %s", path)
	}

	fullPath := filepath.Join(dr.projectRoot, doc.Path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, err
	}

	// Find config for this document type
	var config DocumentTypeConfig
	for _, c := range dr.typeConfigs {
		if c.Type == doc.Type {
			config = c
			break
		}
	}

	validation := dr.validatorReg.Validate(ctx, content, config)

	dr.mu.Lock()
	doc.Validation = validation
	doc.UpdatedAt = time.Now()
	dr.mu.Unlock()

	return doc, nil
}

// RefreshDocument re-scans and updates a single document.
func (dr *DocumentRegistry) RefreshDocument(ctx context.Context, path string) (*Document, error) {
	fullPath := path
	if !filepath.IsAbs(path) {
		fullPath = filepath.Join(dr.projectRoot, path)
	}
	_, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}

	relPath, _ := filepath.Rel(dr.projectRoot, fullPath)

	// Find the config for this path
	var config DocumentTypeConfig
	for _, c := range dr.typeConfigs {
		for _, pattern := range c.PathPatterns {
			matched, _ := filepath.Match(pattern, relPath)
			if matched {
				config = c
				break
			}
		}
		if config.Type != "" {
			break
		}
	}

	if config.Type == "" {
		return nil, fmt.Errorf("no config found for path: %s", path)
	}

	doc := dr.createDocument(relPath, config)

	dr.mu.Lock()
	dr.documents[relPath] = doc
	dr.mu.Unlock()

	return doc, nil
}

// RemoveDocument removes a document from the registry.
func (dr *DocumentRegistry) RemoveDocument(path string) {
	dr.mu.Lock()
	defer dr.mu.Unlock()
	delete(dr.documents, path)
}

// GetStats returns statistics about the document registry.
func (dr *DocumentRegistry) GetStats() RegistryStats {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	stats := RegistryStats{
		TotalDocuments: len(dr.documents),
		ByType:         make(map[DocumentType]int),
		ByStatus:       make(map[DocumentStatus]int),
		ByFreshness:    make(map[FreshnessLevel]int),
		RequiredMissing: len(dr.GetMissingRequiredTypes()),
	}

	for _, doc := range dr.documents {
		stats.ByType[doc.Type]++
		stats.ByStatus[doc.Status]++
		stats.ByFreshness[doc.Freshness.Level]++

		if !doc.Validation.Valid {
			stats.InvalidDocuments++
		}
	}

	return stats
}

// RegistryStats provides statistics about the document registry.
type RegistryStats struct {
	TotalDocuments    int                          `json:"total_documents"`
	ByType            map[DocumentType]int         `json:"by_type"`
	ByStatus          map[DocumentStatus]int       `json:"by_status"`
	ByFreshness       map[FreshnessLevel]int       `json:"by_freshness"`
	InvalidDocuments  int                          `json:"invalid_documents"`
	RequiredMissing   int                          `json:"required_missing"`
}