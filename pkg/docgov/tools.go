package docgov

import (
	"context"
	"fmt"
	"time"

	"github.com/brockenlesnair/governor-combined/pkg/mcp"
)

// DocGovTools holds the MCP tool handlers for document governance.
type DocGovTools struct {
	registry *DocumentRegistry
}

// NewDocGovTools creates new document governance MCP tools.
func NewDocGovTools(registry *DocumentRegistry) *DocGovTools {
	return &DocGovTools{
		registry: registry,
	}
}

// RegisterTools registers all document governance MCP tools.
func (dt *DocGovTools) RegisterTools(reg *mcp.ToolRegistry) error {
	tools := []struct {
		def    mcp.ToolDefinition
		handler mcp.ToolHandler
	}{
		{
			def: mcp.ToolDefinition{
				Name:        "list_documents",
				Description: "List all governance documents, optionally filtered by type",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"type": map[string]any{
							"type":        "string",
							"description": "Filter by document type (adr, api_spec, runbook, etc.)",
						},
						"status": map[string]any{
							"type":        "string",
							"description": "Filter by status (draft, review, approved, deprecated, superseded, missing)",
						},
						"freshness": map[string]any{
							"type":        "string",
							"description": "Filter by freshness (fresh, warning, stale, unknown)",
						},
					},
				},
			},
			handler: dt.handleListDocuments,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "get_document",
				Description: "Get detailed information about a specific document",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Path to the document",
						},
					},
					"required": []string{"path"},
				},
			},
			handler: dt.handleGetDocument,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "validate_document",
				Description: "Validate a document against its type's schema and rules",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Path to the document to validate",
						},
					},
					"required": []string{"path"},
				},
			},
			handler: dt.handleValidateDocument,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "check_freshness",
				Description: "Check freshness of all documents or a specific document",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Optional path to check specific document",
						},
					},
				},
			},
			handler: dt.handleCheckFreshness,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "suggest_document",
				Description: "Suggest document types that should exist based on project analysis",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{},
				},
			},
			handler: dt.handleSuggestDocument,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "get_document_stats",
				Description: "Get statistics about the document registry",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{},
				},
			},
			handler: dt.handleGetStats,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "get_missing_required",
				Description: "Get list of required document types that are missing",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{},
				},
			},
			handler: dt.handleGetMissingRequired,
		},
	}

	for _, t := range tools {
		if err := reg.Register(t.def, t.handler); err != nil {
			return fmt.Errorf("register tool %s: %w", t.def.Name, err)
		}
	}

	return nil
}

// handleListDocuments lists all documents with optional filters.
func (dt *DocGovTools) handleListDocuments(ctx context.Context, args map[string]any) (map[string]any, error) {
	var docType DocumentType
	if t, ok := args["type"].(string); ok && t != "" {
		docType = DocumentType(t)
	}

	documents := dt.registry.ListDocuments(docType)

	// Apply status filter
	if statusStr, ok := args["status"].(string); ok && statusStr != "" {
		status := DocumentStatus(statusStr)
		filtered := make([]*Document, 0)
		for _, doc := range documents {
			if doc.Status == status {
				filtered = append(filtered, doc)
			}
		}
		documents = filtered
	}

	// Apply freshness filter
	if freshnessStr, ok := args["freshness"].(string); ok && freshnessStr != "" {
		freshness := FreshnessLevel(freshnessStr)
		filtered := make([]*Document, 0)
		for _, doc := range documents {
			if doc.Freshness.Level == freshness {
				filtered = append(filtered, doc)
			}
		}
		documents = filtered
	}

	result := make([]map[string]any, len(documents))
	for i, doc := range documents {
		result[i] = map[string]any{
			"path":        doc.Path,
			"type":        doc.Type.String(),
			"title":       doc.Title,
			"status":      doc.Status.String(),
			"freshness":   doc.Freshness.Level.String(),
			"age_days":    int(doc.Freshness.Age.Hours() / 24),
			"valid":       doc.Validation.Valid,
			"owner":       doc.Owner,
			"updated_at":  doc.UpdatedAt.Format(time.RFC3339),
		}
	}

	return map[string]any{
		"documents": result,
		"count":     len(result),
	}, nil
}

// handleGetDocument returns detailed information about a document.
func (dt *DocGovTools) handleGetDocument(ctx context.Context, args map[string]any) (map[string]any, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("path parameter required")
	}

	doc, ok := dt.registry.GetDocument(path)
	if !ok {
		return nil, fmt.Errorf("document not found: %s", path)
	}

	return map[string]any{
		"id":           doc.ID,
		"path":         doc.Path,
		"type":         doc.Type.String(),
		"title":        doc.Title,
		"status":       doc.Status.String(),
		"owner":        doc.Owner,
		"tags":         doc.Tags,
		"created_at":   doc.CreatedAt.Format(time.RFC3339),
		"updated_at":   doc.UpdatedAt.Format(time.RFC3339),
		"approved_at":  doc.ApprovedAt,
		"freshness": map[string]any{
			"level":      doc.Freshness.Level.String(),
			"age_days":   int(doc.Freshness.Age.Hours() / 24),
			"max_age_days": int(doc.Freshness.MaxAge.Hours() / 24),
			"warn_age_days": int(doc.Freshness.WarnAge.Hours() / 24),
			"last_modified": doc.Freshness.LastModified.Format(time.RFC3339),
		},
		"validation": map[string]any{
			"valid":      doc.Validation.Valid,
			"score":      doc.Validation.Score,
			"errors":     doc.Validation.Errors,
			"warnings":   doc.Validation.Warnings,
			"checked_at": doc.Validation.CheckedAt.Format(time.RFC3339),
			"validator":  doc.Validation.Validator,
		},
		"metadata": doc.Metadata,
	}, nil
}

// handleValidateDocument validates a specific document.
func (dt *DocGovTools) handleValidateDocument(ctx context.Context, args map[string]any) (map[string]any, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("path parameter required")
	}

	doc, err := dt.registry.ValidateDocument(ctx, path)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"path":        doc.Path,
		"type":        doc.Type.String(),
		"valid":       doc.Validation.Valid,
		"score":       doc.Validation.Score,
		"errors":      doc.Validation.Errors,
		"warnings":    doc.Validation.Warnings,
		"checked_at":  doc.Validation.CheckedAt.Format(time.RFC3339),
		"validator":   doc.Validation.Validator,
	}, nil
}

// handleCheckFreshness checks freshness of documents.
func (dt *DocGovTools) handleCheckFreshness(ctx context.Context, args map[string]any) (map[string]any, error) {
	path, ok := args["path"].(string)
	if ok && path != "" {
		// Check specific document
		doc, ok := dt.registry.GetDocument(path)
		if !ok {
			return nil, fmt.Errorf("document not found: %s", path)
		}

		return map[string]any{
			"path":       doc.Path,
			"level":      doc.Freshness.Level.String(),
			"age_days":   int(doc.Freshness.Age.Hours() / 24),
			"max_age":    int(doc.Freshness.MaxAge.Hours() / 24),
			"warn_age":   int(doc.Freshness.WarnAge.Hours() / 24),
			"last_modified": doc.Freshness.LastModified.Format(time.RFC3339),
		}, nil
	}

	// Return all freshness info
	stale := dt.registry.GetStaleDocuments()
	warning := dt.registry.GetWarningDocuments()
	all := dt.registry.ListDocuments("")

	freshCount := 0
	for _, doc := range all {
		if doc.Freshness.Level == FreshnessLevelFresh {
			freshCount++
		}
	}

	return map[string]any{
		"summary": map[string]any{
			"total":    len(all),
			"fresh":    freshCount,
			"warning":  len(warning),
			"stale":    len(stale),
			"unknown":  len(all) - freshCount - len(warning) - len(stale),
		},
		"stale_documents":   dt.documentsToSummary(stale),
		"warning_documents": dt.documentsToSummary(warning),
	}, nil
}

func (dt *DocGovTools) documentsToSummary(docs []*Document) []map[string]any {
	result := make([]map[string]any, len(docs))
	for i, doc := range docs {
		result[i] = map[string]any{
			"path":        doc.Path,
			"title":       doc.Title,
			"type":        doc.Type.String(),
			"age_days":    int(doc.Freshness.Age.Hours() / 24),
			"level":       doc.Freshness.Level.String(),
		}
	}
	return result
}

// handleSuggestDocument suggests missing required documents.
func (dt *DocGovTools) handleSuggestDocument(ctx context.Context, args map[string]any) (map[string]any, error) {
	missing := dt.registry.GetMissingRequiredTypes()

	suggestions := make([]map[string]any, len(missing))
	for i, config := range missing {
		suggestions[i] = map[string]any{
			"type":           config.Type.String(),
			"display_name":   config.DisplayName,
			"description":    config.Description,
			"required":       config.Required,
			"path_patterns":  config.PathPatterns,
			"template_available": config.Template != "",
			"validator":      config.Validator,
		}
	}

	return map[string]any{
		"missing_required": suggestions,
		"count":            len(suggestions),
	}, nil
}

// handleGetStats returns registry statistics.
func (dt *DocGovTools) handleGetStats(ctx context.Context, args map[string]any) (map[string]any, error) {
	stats := dt.registry.GetStats()

	byType := make(map[string]int)
	for k, v := range stats.ByType {
		byType[k.String()] = v
	}

	byStatus := make(map[string]int)
	for k, v := range stats.ByStatus {
		byStatus[k.String()] = v
	}

	byFreshness := make(map[string]int)
	for k, v := range stats.ByFreshness {
		byFreshness[k.String()] = v
	}

	return map[string]any{
		"total_documents":     stats.TotalDocuments,
		"by_type":             byType,
		"by_status":           byStatus,
		"by_freshness":        byFreshness,
		"invalid_documents":   stats.InvalidDocuments,
		"required_missing":    stats.RequiredMissing,
	}, nil
}

// handleGetMissingRequired returns missing required document types.
func (dt *DocGovTools) handleGetMissingRequired(ctx context.Context, args map[string]any) (map[string]any, error) {
	missing := dt.registry.GetMissingRequiredTypes()

	result := make([]map[string]any, len(missing))
	for i, config := range missing {
		result[i] = map[string]any{
			"type":           config.Type.String(),
			"display_name":   config.DisplayName,
			"description":    config.Description,
			"path_patterns":  config.PathPatterns,
			"validator":      config.Validator,
			"max_age_days":   int(config.MaxAge.Hours() / 24),
			"warn_age_days":  int(config.WarnAge.Hours() / 24),
			"template_available": config.Template != "",
		}
	}

	return map[string]any{
		"missing": result,
		"count":   len(result),
	}, nil
}