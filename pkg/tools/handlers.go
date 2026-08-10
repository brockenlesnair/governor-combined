package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/brockenlesnair/governor-combined/pkg/adr"
	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
	"github.com/brockenlesnair/governor-combined/pkg/deadcode"
	"github.com/brockenlesnair/governor-combined/pkg/hangar"
	"github.com/brockenlesnair/governor-combined/pkg/mcp"
	"github.com/brockenlesnair/governor-combined/pkg/repo"
	"github.com/brockenlesnair/governor-combined/pkg/rigour"
	"github.com/brockenlesnair/governor-combined/pkg/safety"
	"github.com/brockenlesnair/governor-combined/pkg/sarif"
	"github.com/brockenlesnair/governor-combined/pkg/search"
	"github.com/brockenlesnair/governor-combined/pkg/untested"
)

// ToolHandlers holds all MCP tool handlers.
type ToolHandlers struct {
	graphLifecycle   *GraphLifecycle
	safetyValidator  *safety.SafetyValidator
	searcher         *search.Searcher
	untestedDetector *untested.UntestedDetector
	deadcodeDetector *deadcode.Detector
	analyzer         *callgraph.Analyzer

	// Extended components (rigour, sarif, adr, hangar, repo)
	rigourSupervisor *rigour.RigourSupervisor
	sarifDedup       *sarif.DedupPipeline
	adrClient        adr.AdrMcpClient
	hangarClient     *hangar.Client
	repoClient       *repo.Client

	logger *slog.Logger
}

// NewToolHandlers creates all MCP tool handlers.
func NewToolHandlers(gl *GraphLifecycle, logger *slog.Logger) (*ToolHandlers, error) {
	// Initialize safety validator
	safetyCfg := safety.DefaultConfig()
	safetyValidator, err := safety.NewValidator(safetyCfg)
	if err != nil {
		return nil, fmt.Errorf("create safety validator: %w", err)
	}

	th := &ToolHandlers{
		graphLifecycle:  gl,
		safetyValidator: safetyValidator,
		logger:          logger,
	}

	return th, nil
}

// UpdateGraphDependentTools updates tools that depend on the call graph.
func (th *ToolHandlers) UpdateGraphDependentTools(g *callgraph.Graph) {
	th.searcher = search.NewSearcher(g, th.logger)
	th.untestedDetector = untested.NewDetector(g, th.logger)
	th.deadcodeDetector = deadcode.NewDetector(g, th.logger)
	th.analyzer = callgraph.NewAnalyzer()
}

// SetExtendedComponents injects rigour, sarif, adr, hangar, and repo clients.
func (th *ToolHandlers) SetExtendedComponents(
	rigourSup *rigour.RigourSupervisor,
	sarifDedup *sarif.DedupPipeline,
	adrClient adr.AdrMcpClient,
	hangarClient *hangar.Client,
	repoClient *repo.Client,
) {
	th.rigourSupervisor = rigourSup
	th.sarifDedup = sarifDedup
	th.adrClient = adrClient
	th.hangarClient = hangarClient
	th.repoClient = repoClient
}

// RegisterTools registers all MCP tools with the gateway registry.
func (th *ToolHandlers) RegisterTools(reg *mcp.ToolRegistry) error {
	tools := []struct {
		def    mcp.ToolDefinition
		handler mcp.ToolHandler
	}{
		{
			def: mcp.ToolDefinition{
				Name:        "validate_code",
				Description: "Validate a source file for dangerous patterns, policy violations, and security issues",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file": map[string]any{
							"type":        "string",
							"description": "Path to the file to validate",
						},
						"content": map[string]any{
							"type":        "string",
							"description": "Optional file content (if not provided, reads from file system)",
						},
						"policy": map[string]any{
							"type":        "string",
							"description": "Policy name to use (default: 'default')",
						},
					},
					"required": []string{"file"},
				},
			},
			handler: th.handleValidateCode,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "validate_diff",
				Description: "Validate only the changed lines in a diff for safety issues",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"diff": map[string]any{
							"type":        "string",
							"description": "Unified diff content to validate",
						},
						"policy": map[string]any{
							"type":        "string",
							"description": "Policy name to use (default: 'default')",
						},
					},
					"required": []string{"diff"},
				},
			},
			handler: th.handleValidateDiff,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "search_code",
				Description: "Search for code symbols using fuzzy, regex, or type-aware queries",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "Search query string",
						},
						"type": map[string]any{
							"type":        "string",
							"description": "Search type: fuzzy, regex, callers, callees, type, exact, prefix, substring, kind",
							"enum":        []string{"fuzzy", "regex", "callers", "callees", "type", "exact", "prefix", "substring", "kind"},
						},
						"max_results": map[string]any{
							"type":        "integer",
							"description": "Maximum number of results",
							"default":     50,
						},
						"target_id": map[string]any{
							"type":        "string",
							"description": "Target function ID for caller/callee searches",
						},
						"depth": map[string]any{
							"type":        "integer",
							"description": "Traversal depth for caller/callee searches",
							"default":     3,
						},
						"param_types": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Parameter types for type-aware search",
						},
						"return_types": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Return types for type-aware search",
						},
					},
					"required": []string{"query"},
				},
			},
			handler: th.handleSearchCode,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "get_callers",
				Description: "Get all callers of a function (direct or transitive)",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"function_id": map[string]any{
							"type":        "string",
							"description": "Function ID to find callers for",
						},
						"transitive": map[string]any{
							"type":        "boolean",
							"description": "If true, return transitive callers",
							"default":     false,
						},
						"depth": map[string]any{
							"type":        "integer",
							"description": "Maximum traversal depth for transitive callers",
							"default":     3,
						},
					},
					"required": []string{"function_id"},
				},
			},
			handler: th.handleGetCallers,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "get_callees",
				Description: "Get all callees of a function (direct or transitive)",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"function_id": map[string]any{
							"type":        "string",
							"description": "Function ID to find callees for",
						},
						"transitive": map[string]any{
							"type":        "boolean",
							"description": "If true, return transitive callees",
							"default":     false,
						},
						"depth": map[string]any{
							"type":        "integer",
							"description": "Maximum traversal depth for transitive callees",
							"default":     3,
						},
					},
					"required": []string{"function_id"},
				},
			},
			handler: th.handleGetCallees,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "get_impact",
				Description: "Analyze the impact of changing a function",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"function_id": map[string]any{
							"type":        "string",
							"description": "Function ID to analyze impact for",
						},
						"depth": map[string]any{
							"type":        "integer",
							"description": "Maximum traversal depth",
							"default":     3,
						},
					},
					"required": []string{"function_id"},
				},
			},
			handler: th.handleGetImpact,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "audit_project",
				Description: "Run a full project audit: untested code, dead code, and safety issues",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"include_untested": map[string]any{
							"type":        "boolean",
							"description": "Include untested code detection",
							"default":     true,
						},
						"include_deadcode": map[string]any{
							"type":        "boolean",
							"description": "Include dead code detection",
							"default":     true,
						},
						"include_safety": map[string]any{
							"type":        "boolean",
							"description": "Include safety validation",
							"default":     true,
						},
						"min_priority": map[string]any{
							"type":        "number",
							"description": "Minimum priority threshold for untested functions",
							"default":     0.3,
						},
						"min_confidence": map[string]any{
							"type":        "number",
							"description": "Minimum confidence threshold for dead code",
							"default":     0.5,
						},
					},
				},
			},
			handler: th.handleAuditProject,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "rigour_check",
				Description: "Run DLP pre-filter scan on code text before agent processing",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text": map[string]any{
							"type":        "string",
							"description": "Code text to scan for data-leak patterns",
						},
						"file_path": map[string]any{
							"type":        "string",
							"description": "File path for context",
						},
					},
					"required": []string{"text"},
				},
			},
			handler: th.handleRigourCheck,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "rigour_state",
				Description: "Get current rigour supervisor state (idle, working, fixing, etc.)",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			handler: th.handleRigourState,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "rigour_stats",
				Description: "Get rigour brain statistics: pattern count, hard rules, avg strength",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			handler: th.handleRigourStats,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "sarif_export",
				Description: "Export findings as a SARIF 2.1.0 report",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"findings": map[string]any{
							"type":        "array",
							"description": "Findings to export (each with rule_id, message, file, line, level)",
							"items": map[string]any{
								"type": "object",
							},
						},
						"source": map[string]any{
							"type":        "string",
							"description": "Source label for the findings (e.g. 'safety', 'rigour')",
							"default":     "governor",
						},
					},
					"required": []string{"findings"},
				},
			},
			handler: th.handleSARIFExport,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "adr_create",
				Description: "Create an Architecture Decision Record from a template",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title": map[string]any{
							"type":        "string",
							"description": "ADR title",
						},
						"status": map[string]any{
							"type":        "string",
							"description": "ADR status: proposed, accepted, deprecated, superseded",
							"default":     "proposed",
						},
						"diff": map[string]any{
							"type":        "string",
							"description": "Diff or context that triggered this ADR",
						},
					},
					"required": []string{"title"},
				},
			},
			handler: th.handleADRCreate,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "adr_list",
				Description: "List existing Architecture Decision Records",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			handler: th.handleADRList,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "hangar_score",
				Description: "Get the fleet compliance scorecard from Hangar",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"page": map[string]any{
							"type":        "integer",
							"description": "Page number (0 = all pages)",
							"default":     0,
						},
						"page_size": map[string]any{
							"type":        "integer",
							"description": "Results per page",
							"default":     100,
						},
					},
				},
			},
			handler: th.handleHangarScore,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "repo_health",
				Description: "Get the repo health tier from Repo Butler",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			handler: th.handleRepoHealth,
		},
	}

	for _, t := range tools {
		if err := reg.Register(t.def, t.handler); err != nil {
			return fmt.Errorf("register tool %s: %w", t.def.Name, err)
		}
	}

	return nil
}

// RegisterToolsOnGateway registers all MCP tools on the gateway.
func (th *ToolHandlers) RegisterToolsOnGateway(gw interface {
	RegisterTool(mcp.ToolDefinition, mcp.ToolHandler) error
}) error {
	tools := []struct {
		def    mcp.ToolDefinition
		handler mcp.ToolHandler
	}{
		{
			def: mcp.ToolDefinition{
				Name:        "validate_code",
				Description: "Validate a source file for dangerous patterns, policy violations, and security issues",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file": map[string]any{
							"type":        "string",
							"description": "Path to the file to validate",
						},
						"content": map[string]any{
							"type":        "string",
							"description": "Optional file content (if not provided, reads from file system)",
						},
						"policy": map[string]any{
							"type":        "string",
							"description": "Policy name to use (default: 'default')",
						},
					},
					"required": []string{"file"},
				},
			},
			handler: th.handleValidateCode,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "validate_diff",
				Description: "Validate only the changed lines in a diff for safety issues",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"diff": map[string]any{
							"type":        "string",
							"description": "Unified diff content to validate",
						},
						"policy": map[string]any{
							"type":        "string",
							"description": "Policy name to use (default: 'default')",
						},
					},
					"required": []string{"diff"},
				},
			},
			handler: th.handleValidateDiff,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "search_code",
				Description: "Search for code symbols using fuzzy, regex, or type-aware queries",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "Search query string",
						},
						"type": map[string]any{
							"type":        "string",
							"description": "Search type: fuzzy, regex, callers, callees, type, exact, prefix, substring, kind",
							"enum":        []string{"fuzzy", "regex", "callers", "callees", "type", "exact", "prefix", "substring", "kind"},
						},
						"max_results": map[string]any{
							"type":        "integer",
							"description": "Maximum number of results",
							"default":     50,
						},
						"target_id": map[string]any{
							"type":        "string",
							"description": "Target function ID for caller/callee searches",
						},
						"depth": map[string]any{
							"type":        "integer",
							"description": "Traversal depth for caller/callee searches",
							"default":     3,
						},
						"param_types": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Parameter types for type-aware search",
						},
						"return_types": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Return types for type-aware search",
						},
					},
					"required": []string{"query"},
				},
			},
			handler: th.handleSearchCode,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "get_callers",
				Description: "Get all callers of a function (direct or transitive)",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"function_id": map[string]any{
							"type":        "string",
							"description": "Function ID to find callers for",
						},
						"transitive": map[string]any{
							"type":        "boolean",
							"description": "If true, return transitive callers",
							"default":     false,
						},
						"depth": map[string]any{
							"type":        "integer",
							"description": "Maximum traversal depth for transitive callers",
							"default":     3,
						},
					},
					"required": []string{"function_id"},
				},
			},
			handler: th.handleGetCallers,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "get_callees",
				Description: "Get all callees of a function (direct or transitive)",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"function_id": map[string]any{
							"type":        "string",
							"description": "Function ID to find callees for",
						},
						"transitive": map[string]any{
							"type":        "boolean",
							"description": "If true, return transitive callees",
							"default":     false,
						},
						"depth": map[string]any{
							"type":        "integer",
							"description": "Maximum traversal depth for transitive callees",
							"default":     3,
						},
					},
					"required": []string{"function_id"},
				},
			},
			handler: th.handleGetCallees,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "get_impact",
				Description: "Analyze the impact of changing a function",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"function_id": map[string]any{
							"type":        "string",
							"description": "Function ID to analyze impact for",
						},
						"depth": map[string]any{
							"type":        "integer",
							"description": "Maximum traversal depth",
							"default":     3,
						},
					},
					"required": []string{"function_id"},
				},
			},
			handler: th.handleGetImpact,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "audit_project",
				Description: "Run a full project audit: untested code, dead code, and safety issues",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"include_untested": map[string]any{
							"type":        "boolean",
							"description": "Include untested code detection",
							"default":     true,
						},
						"include_deadcode": map[string]any{
							"type":        "boolean",
							"description": "Include dead code detection",
							"default":     true,
						},
						"include_safety": map[string]any{
							"type":        "boolean",
							"description": "Include safety validation",
							"default":     true,
						},
						"min_priority": map[string]any{
							"type":        "number",
							"description": "Minimum priority threshold for untested functions",
							"default":     0.3,
						},
						"min_confidence": map[string]any{
							"type":        "number",
							"description": "Minimum confidence threshold for dead code",
							"default":     0.5,
						},
					},
				},
			},
			handler: th.handleAuditProject,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "rigour_check",
				Description: "Run DLP pre-filter scan on code text before agent processing",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text": map[string]any{
							"type":        "string",
							"description": "Code text to scan for data-leak patterns",
						},
						"file_path": map[string]any{
							"type":        "string",
							"description": "File path for context",
						},
					},
					"required": []string{"text"},
				},
			},
			handler: th.handleRigourCheck,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "rigour_state",
				Description: "Get current rigour supervisor state (idle, working, fixing, etc.)",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			handler: th.handleRigourState,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "rigour_stats",
				Description: "Get rigour brain statistics: pattern count, hard rules, avg strength",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			handler: th.handleRigourStats,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "sarif_export",
				Description: "Export findings as a SARIF 2.1.0 report",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"findings": map[string]any{
							"type":        "array",
							"description": "Findings to export (each with rule_id, message, file, line, level)",
							"items": map[string]any{
								"type": "object",
							},
						},
						"source": map[string]any{
							"type":        "string",
							"description": "Source label for the findings (e.g. 'safety', 'rigour')",
							"default":     "governor",
						},
					},
					"required": []string{"findings"},
				},
			},
			handler: th.handleSARIFExport,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "adr_create",
				Description: "Create an Architecture Decision Record from a template",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title": map[string]any{
							"type":        "string",
							"description": "ADR title",
						},
						"status": map[string]any{
							"type":        "string",
							"description": "ADR status: proposed, accepted, deprecated, superseded",
							"default":     "proposed",
						},
						"diff": map[string]any{
							"type":        "string",
							"description": "Diff or context that triggered this ADR",
						},
					},
					"required": []string{"title"},
				},
			},
			handler: th.handleADRCreate,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "adr_list",
				Description: "List existing Architecture Decision Records",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			handler: th.handleADRList,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "hangar_score",
				Description: "Get the fleet compliance scorecard from Hangar",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"page": map[string]any{
							"type":        "integer",
							"description": "Page number (0 = all pages)",
							"default":     0,
						},
						"page_size": map[string]any{
							"type":        "integer",
							"description": "Results per page",
							"default":     100,
						},
					},
				},
			},
			handler: th.handleHangarScore,
		},
		{
			def: mcp.ToolDefinition{
				Name:        "repo_health",
				Description: "Get the repo health tier from Repo Butler",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			handler: th.handleRepoHealth,
		},
	}

	for _, t := range tools {
		if err := gw.RegisterTool(t.def, t.handler); err != nil {
			return fmt.Errorf("register tool %s: %w", t.def.Name, err)
		}
	}

	return nil
}

// handleValidateCode validates a source file for safety issues.
func (th *ToolHandlers) handleValidateCode(ctx context.Context, args map[string]any) (map[string]any, error) {
	filePath, ok := args["file"].(string)
	if !ok || filePath == "" {
		return nil, fmt.Errorf("file parameter required")
	}

	policyName := "default"
	if p, ok := args["policy"].(string); ok && p != "" {
		policyName = p
	}

	var content string
	if c, ok := args["content"].(string); ok {
		content = c
	}

	fileInput := safety.FileInput{
		Path:     filePath,
		Content:  content,
		Language: detectLanguageFromPath(filePath),
	}

	result, err := th.safetyValidator.ValidateFile(ctx, fileInput, policyName)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"file":       filePath,
		"policy":     policyName,
		"passed":     result.Passed,
		"risk_score": result.RiskScore,
		"risk_level": result.RiskLevel,
		"findings":   result.Findings,
		"blocked":    result.Blocked,
		"reason":     result.Reason,
		"checked_files": result.CheckedFiles,
		"duration":   result.Duration,
	}, nil
}

// handleValidateDiff validates only changed lines in a diff.
func (th *ToolHandlers) handleValidateDiff(ctx context.Context, args map[string]any) (map[string]any, error) {
	diffContent, ok := args["diff"].(string)
	if !ok || diffContent == "" {
		return nil, fmt.Errorf("diff parameter required")
	}

	policyName := "default"
	if p, ok := args["policy"].(string); ok && p != "" {
		policyName = p
	}

	result, err := th.safetyValidator.ValidateDiff(ctx, diffContent, policyName)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"policy":     policyName,
		"passed":     result.Passed,
		"risk_score": result.RiskScore,
		"risk_level": result.RiskLevel,
		"findings":   result.Findings,
		"blocked":    result.Blocked,
		"reason":     result.Reason,
		"checked_files": result.CheckedFiles,
		"duration":   result.Duration,
	}, nil
}

// handleSearchCode searches for code symbols.
func (th *ToolHandlers) handleSearchCode(ctx context.Context, args map[string]any) (map[string]any, error) {
	queryStr, ok := args["query"].(string)
	if !ok || queryStr == "" {
		return nil, fmt.Errorf("query parameter required")
	}

	// Ensure graph-dependent tools are initialized
	g, err := th.graphLifecycle.GetGraph(ctx)
	if err != nil {
		return nil, fmt.Errorf("get graph: %w", err)
	}
	if th.searcher == nil {
		th.UpdateGraphDependentTools(g)
	}

	searchType := "fuzzy"
	if t, ok := args["type"].(string); ok && t != "" {
		searchType = t
	}

	maxResults := 50
	if mr, ok := args["max_results"].(float64); ok {
		maxResults = int(mr)
	}

	q := &search.Query{
		Pattern:    queryStr,
		MaxResults: maxResults,
	}

	// Handle caller/callee specific params
	if targetID, ok := args["target_id"].(string); ok && targetID != "" {
		q.CallerOf = targetID
		if searchType == "callees" {
			q.CalleeOf = targetID
			q.CallerOf = ""
		}
	}
	if paramTypes, ok := args["param_types"].([]any); ok {
		for _, pt := range paramTypes {
			if s, ok := pt.(string); ok {
				q.ParamTypes = append(q.ParamTypes, s)
			}
		}
	}
	if returnTypes, ok := args["return_types"].([]any); ok {
		for _, rt := range returnTypes {
			if s, ok := rt.(string); ok {
				q.ReturnType = s
			}
		}
	}
	if searchType == "regex" {
		q.Regex = queryStr
		q.Pattern = ""
	}

	result, err := th.searcher.Search(ctx, q)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"query":        queryStr,
		"type":         searchType,
		"results":      result.Matches,
		"total_found":  result.TotalFound,
		"duration_ms":  result.DurationMs,
	}, nil
}

// handleGetCallers returns callers of a function.
func (th *ToolHandlers) handleGetCallers(ctx context.Context, args map[string]any) (map[string]any, error) {
	functionID, ok := args["function_id"].(string)
	if !ok || functionID == "" {
		return nil, fmt.Errorf("function_id parameter required")
	}

	g, err := th.graphLifecycle.GetGraph(ctx)
	if err != nil {
		return nil, fmt.Errorf("get graph: %w", err)
	}

	transitive := false
	if t, ok := args["transitive"].(bool); ok {
		transitive = t
	}
	depth := 3
	if d, ok := args["depth"].(float64); ok {
		depth = int(d)
	}

	var callers []string
	if transitive {
		edges := g.TransitiveCallers(functionID, depth)
		seen := make(map[string]bool)
		for _, e := range edges {
			if !seen[e.From] {
				callers = append(callers, e.From)
				seen[e.From] = true
			}
		}
	} else {
		callers = g.Callers(functionID)
	}

	return map[string]any{
		"function_id": functionID,
		"transitive":  transitive,
		"depth":       depth,
		"callers":     callers,
		"count":       len(callers),
	}, nil
}

// handleGetCallees returns callees of a function.
func (th *ToolHandlers) handleGetCallees(ctx context.Context, args map[string]any) (map[string]any, error) {
	functionID, ok := args["function_id"].(string)
	if !ok || functionID == "" {
		return nil, fmt.Errorf("function_id parameter required")
	}

	g, err := th.graphLifecycle.GetGraph(ctx)
	if err != nil {
		return nil, fmt.Errorf("get graph: %w", err)
	}

	transitive := false
	if t, ok := args["transitive"].(bool); ok {
		transitive = t
	}
	depth := 3
	if d, ok := args["depth"].(float64); ok {
		depth = int(d)
	}

	var callees []string
	if transitive {
		edges := g.TransitiveCallees(functionID, depth)
		seen := make(map[string]bool)
		for _, e := range edges {
			if !seen[e.To] {
				callees = append(callees, e.To)
				seen[e.To] = true
			}
		}
	} else {
		callees = g.Callees(functionID)
	}

	return map[string]any{
		"function_id": functionID,
		"transitive":  transitive,
		"depth":       depth,
		"callees":     callees,
		"count":       len(callees),
	}, nil
}

// handleGetImpact analyzes the impact of changing a function.
func (th *ToolHandlers) handleGetImpact(ctx context.Context, args map[string]any) (map[string]any, error) {
	functionID, ok := args["function_id"].(string)
	if !ok || functionID == "" {
		return nil, fmt.Errorf("function_id parameter required")
	}

	g, err := th.graphLifecycle.GetGraph(ctx)
	if err != nil {
		return nil, fmt.Errorf("get graph: %w", err)
	}

	if th.analyzer == nil {
		th.analyzer = callgraph.NewAnalyzer()
	}

	depth := 3
	if d, ok := args["depth"].(float64); ok {
		depth = int(d)
	}

	impact := th.analyzer.AnalyzeImpact(g, functionID)

	return map[string]any{
		"function_id":       impact.Target,
		"depth":             depth,
		"direct_callers":    impact.DirectDeps,
		"transitive_callers": impact.TransDeps,
		"affected_functions": impact.Affected,
		"risk_level":        impact.RiskLevel,
		"description":       impact.Description,
	}, nil
}

// handleAuditProject runs a full project audit.
func (th *ToolHandlers) handleAuditProject(ctx context.Context, args map[string]any) (map[string]any, error) {
	g, err := th.graphLifecycle.GetGraph(ctx)
	if err != nil {
		return nil, fmt.Errorf("get graph: %w", err)
	}

	if th.untestedDetector == nil {
		th.UpdateGraphDependentTools(g)
	}

	includeUntested := true
	if iu, ok := args["include_untested"].(bool); ok {
		includeUntested = iu
	}
	includeDeadcode := true
	if idc, ok := args["include_deadcode"].(bool); ok {
		includeDeadcode = idc
	}
	includeSafety := true
	if is, ok := args["include_safety"].(bool); ok {
		includeSafety = is
	}
	minPriority := 0.3
	if mp, ok := args["min_priority"].(float64); ok {
		minPriority = mp
	}
	minConfidence := 0.5
	if mc, ok := args["min_confidence"].(float64); ok {
		minConfidence = mc
	}

	audit := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	// Untested code detection
	if includeUntested {
		cfg := &untested.Config{
			IncludeExported: true,
			MinPriority:     minPriority,
		}
		result, err := th.untestedDetector.Detect(ctx, cfg)
		if err != nil {
			audit["untested_error"] = err.Error()
		} else {
			audit["untested"] = map[string]any{
				"total_functions":  result.TotalFunctions,
				"tested_count":     result.TestedCount,
				"untested_count":   result.UntestedCount,
				"coverage_pct":     result.CoveragePct,
				"top_untested":     topUntested(result.Untested, 10),
			}
		}
	}

	// Dead code detection
	if includeDeadcode {
		cfg := &deadcode.Config{
			ExcludeExported: true,
			MinConfidence:   minConfidence,
		}
		result, err := th.deadcodeDetector.Detect(ctx, cfg)
		if err != nil {
			audit["deadcode_error"] = err.Error()
		} else {
			audit["deadcode"] = map[string]any{
				"total_entities": result.TotalEntities,
				"alive_count":    result.AliveCount,
				"dead_count":     result.DeadCount,
				"top_dead":       topDeadCode(result.DeadCode, 10),
			}
		}
	}

	// Safety audit
	if includeSafety {
		audit["safety"] = map[string]any{
			"enabled": true,
			"note":    "Run validate_code on specific files for detailed safety analysis",
		}
	}

	return audit, nil
}

// detectLanguageFromPath detects language from file extension.
func detectLanguageFromPath(filePath string) string {
	ext := strings.ToLower(filePath[strings.LastIndex(filePath, "."):])
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	default:
		return "go"
	}
}

// ── Extended tool handlers ──────────────────────────────────────────

func (th *ToolHandlers) handleRigourCheck(ctx context.Context, args map[string]any) (map[string]any, error) {
	if th.rigourSupervisor == nil {
		return nil, fmt.Errorf("rigour not configured")
	}

	text, ok := args["text"].(string)
	if !ok || text == "" {
		return nil, fmt.Errorf("text parameter required")
	}

	filePath := ""
	if fp, ok := args["file_path"].(string); ok {
		filePath = fp
	}

	result, err := th.rigourSupervisor.ProcessInput(ctx, text, filePath)
	if err != nil {
		return nil, fmt.Errorf("rigour check: %w", err)
	}

	return map[string]any{
		"blocked":                result.Blocked,
		"reasons":               result.Reasons,
		"entropy_hits":           result.EntropyHits,
		"secret_hits":            result.SecretHits,
		"pii_hits":              result.PIIHits,
		"false_positive_exemptions": result.FalsePositiveExemptions,
		"file_path":             filePath,
	}, nil
}

func (th *ToolHandlers) handleRigourState(ctx context.Context, args map[string]any) (map[string]any, error) {
	if th.rigourSupervisor == nil {
		return nil, fmt.Errorf("rigour not configured")
	}

	state := th.rigourSupervisor.GetState()
	return map[string]any{
		"state": state.String(),
	}, nil
}

func (th *ToolHandlers) handleRigourStats(ctx context.Context, args map[string]any) (map[string]any, error) {
	if th.rigourSupervisor == nil {
		return nil, fmt.Errorf("rigour not configured")
	}

	stats := th.rigourSupervisor.GetBrainStats()
	return map[string]any{
		"pattern_count":  stats.PatternCount,
		"hard_rule_count": stats.HardRuleCount,
		"avg_strength":   stats.AvgStrength,
		"max_strength":   stats.MaxStrength,
	}, nil
}

func (th *ToolHandlers) handleSARIFExport(ctx context.Context, args map[string]any) (map[string]any, error) {
	findingsRaw, ok := args["findings"].([]any)
	if !ok || len(findingsRaw) == 0 {
		return nil, fmt.Errorf("findings parameter required (non-empty array)")
	}

	source := "governor"
	if s, ok := args["source"].(string); ok && s != "" {
		source = s
	}

	var results []sarif.Result
	for _, fRaw := range findingsRaw {
		f, ok := fRaw.(map[string]any)
		if !ok {
			continue
		}

		ruleID, _ := f["rule_id"].(string)
		message, _ := f["message"].(string)
		level, _ := f["level"].(string)
		filePath, _ := f["file"].(string)
		line := 0
		if l, ok := f["line"].(float64); ok {
			line = int(l)
		}

		if level == "" {
			level = "warning"
		}

		result := sarif.Result{
			RuleID:  ruleID,
			Level:   level,
			Message: sarif.Message{Text: message},
		}
		if filePath != "" {
			result.Locations = []sarif.Location{
				{
					PhysicalLocation: &sarif.PhysicalLocation{
						ArtifactLocation: sarif.ArtifactLocation{URI: filePath},
						Region:           sarif.Region{StartLine: line},
					},
				},
			}
		}
		results = append(results, result)
	}

	report := sarif.Report{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarif.Run{
			{
				Tool: sarif.Tool{
					Driver: sarif.ToolDriver{
						Name:    source,
						Version: "1.0.0",
					},
				},
				Results: results,
			},
		},
	}

	reportJSON, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("marshal sarif report: %w", err)
	}

	return map[string]any{
		"report":   string(reportJSON),
		"findings": len(results),
		"source":   source,
	}, nil
}

func (th *ToolHandlers) handleADRCreate(ctx context.Context, args map[string]any) (map[string]any, error) {
	if th.adrClient == nil {
		return nil, fmt.Errorf("adr not configured")
	}

	title, ok := args["title"].(string)
	if !ok || title == "" {
		return nil, fmt.Errorf("title parameter required")
	}

	status := "proposed"
	if s, ok := args["status"].(string); ok && s != "" {
		status = s
	}

	diff := ""
	if d, ok := args["diff"].(string); ok {
		diff = d
	}

	result, err := th.adrClient.CallTool(ctx, "create_adr", map[string]any{
		"title":  title,
		"status": status,
		"diff":   diff,
	})
	if err != nil {
		return nil, fmt.Errorf("adr create: %w", err)
	}

	return result, nil
}

func (th *ToolHandlers) handleADRList(ctx context.Context, args map[string]any) (map[string]any, error) {
	if th.adrClient == nil {
		return nil, fmt.Errorf("adr not configured")
	}

	result, err := th.adrClient.CallTool(ctx, "list_adrs", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("adr list: %w", err)
	}

	return result, nil
}

func (th *ToolHandlers) handleHangarScore(ctx context.Context, args map[string]any) (map[string]any, error) {
	if th.hangarClient == nil {
		return nil, fmt.Errorf("hangar not configured (set base_url and api_key)")
	}

	page := 0
	if p, ok := args["page"].(float64); ok {
		page = int(p)
	}
	pageSize := 0
	if ps, ok := args["page_size"].(float64); ok {
		pageSize = int(ps)
	}

	scorecard, err := th.hangarClient.GetFleetScorecard(ctx, page, pageSize)
	if err != nil {
		return nil, fmt.Errorf("hangar score: %w", err)
	}

	return map[string]any{
		"total_repos":   scorecard.TotalCount,
		"average_score": scorecard.AverageScore,
		"fetched_at":    scorecard.FetchedAt,
		"repos":         scorecard.Repos,
	}, nil
}

func (th *ToolHandlers) handleRepoHealth(ctx context.Context, args map[string]any) (map[string]any, error) {
	if th.repoClient == nil {
		return nil, fmt.Errorf("repo-butler not configured (set server_command)")
	}

	data, _, err := th.repoClient.CallTool(ctx, repo.ToolGetHealthTier, map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("repo health: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse repo health: %w", err)
	}

	return result, nil
}

// Helper functions

func topUntested(untested []untested.UntestedFunc, limit int) []map[string]any {
	if len(untested) > limit {
		untested = untested[:limit]
	}
	result := make([]map[string]any, len(untested))
	for i, u := range untested {
		result[i] = map[string]any{
			"id":            u.ID,
			"name":          u.Name,
			"package":       u.Package,
			"file":          u.File,
			"line":          u.Line,
			"kind":          u.Kind,
			"exported":      u.Exported,
			"priority":      u.Priority,
			"caller_count":  u.CallerCount,
			"callee_count":  u.CalleeCount,
			"reason":        u.Reason,
		}
	}
	return result
}

func topDeadCode(dead []deadcode.DeadCodeCandidate, limit int) []map[string]any {
	if len(dead) > limit {
		dead = dead[:limit]
	}
	result := make([]map[string]any, len(dead))
	for i, d := range dead {
		result[i] = map[string]any{
			"id":           d.ID,
			"name":         d.Name,
			"package":      d.Package,
			"file":         d.File,
			"line":         d.Line,
			"kind":         d.Kind,
			"exported":     d.Exported,
			"confidence":   d.Confidence,
			"reason":       d.Reason,
			"dead_kind":    d.DeadKind,
		}
	}
	return result
}