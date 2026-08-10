package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// ─── Council Personas ─────────────────────────────────────────────────

// Persona identifies a council review persona.
type Persona string

const (
	PersonaSecurity       Persona = "security"
	PersonaPerformance    Persona = "performance"
	PersonaReliability    Persona = "reliability"
	PersonaMaintainability Persona = "maintainability"
	PersonaCompliance     Persona = "compliance"
)

// AllPersonas returns all 5 council personas.
func AllPersonas() []Persona {
	return []Persona{
		PersonaSecurity,
		PersonaPerformance,
		PersonaReliability,
		PersonaMaintainability,
		PersonaCompliance,
	}
}

// ─── Council Findings ─────────────────────────────────────────────────

// FindingSeverity is the severity of a council finding.
type FindingSeverity string

const (
	SeverityCritical FindingSeverity = "critical"
	SeverityHigh     FindingSeverity = "high"
	SeverityMedium   FindingSeverity = "medium"
	SeverityLow      FindingSeverity = "low"
	SeverityInfo     FindingSeverity = "info"
)

// FindingCategory identifies the type of finding.
type FindingCategory string

const (
	CategoryStandardsGap FindingCategory = "standards-gap"
	CategoryBug          FindingCategory = "bug"
	CategorySmell        FindingCategory = "smell"
	CategoryVulnerability FindingCategory = "vulnerability"
	CategoryPerformance  FindingCategory = "performance"
	CategoryCompliance   FindingCategory = "compliance"
)

// CouncilFinding is a single finding from a council persona.
type CouncilFinding struct {
	ID          string            `json:"id"`
	Persona    Persona           `json:"persona"`
	Category   FindingCategory   `json:"category"`
	Severity   FindingSeverity   `json:"severity"`
	Title      string            `json:"title"`
	Message    string            `json:"message"`
	File       string            `json:"file,omitempty"`
	Line       int               `json:"line,omitempty"`
	RuleID     string            `json:"rule_id,omitempty"`
	Remediation string           `json:"remediation,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
}

// CouncilReport is the aggregated output of a council review.
type CouncilReport struct {
	Mode      ReviewMode       `json:"mode"`
	Personas  []Persona        `json:"personas"`
	Findings  []CouncilFinding `json:"findings"`
	Total     int              `json:"total"`
	Critical  int              `json:"critical"`
	High      int              `json:"high"`
	Medium    int              `json:"medium"`
	Low       int              `json:"low"`
	Info      int              `json:"info"`
	PassedAt  time.Time        `json:"passed_at"`
	Duration  Duration         `json:"duration"`
}

// ReviewMode identifies the review depth.
type ReviewMode string

const (
	ModeQuick ReviewMode = "quick"
	ModeFull  ReviewMode = "full"
)

// ─── Council Review ───────────────────────────────────────────────────

// CouncilReviewRequest is the arguments for a council review.
type CouncilReviewRequest struct {
	// Mode determines the review depth.
	Mode ReviewMode `json:"mode"`

	// Personas to run (empty = all 5).
	Personas []Persona `json:"personas,omitempty"`

	// RepoPath is the repository to review.
	RepoPath string `json:"repo_path,omitempty"`

	// FocusFiles limits the review to specific files.
	FocusFiles []string `json:"focus_files,omitempty"`
}

// RunCouncilReview executes a council review with the specified personas and mode.
// Quick mode runs a subset of checks; full mode runs all checks.
func (c *Client) RunCouncilReview(ctx context.Context, req CouncilReviewRequest) (*CouncilReport, error) {
	if req.Mode == "" {
		req.Mode = ModeQuick
	}
	if len(req.Personas) == 0 {
		req.Personas = AllPersonas()
	}

	start := time.Now()

	c.logger.Info("starting council review",
		"mode", req.Mode,
		"personas", req.Personas,
		"repo", req.RepoPath,
	)

	// Validate personas
	for _, p := range req.Personas {
		switch p {
		case PersonaSecurity, PersonaPerformance, PersonaReliability, PersonaMaintainability, PersonaCompliance:
			// valid
		default:
			return nil, fmt.Errorf("unknown persona: %s", p)
		}
	}

	var allFindings []CouncilFinding

	// Run each persona in sequence (they may depend on each other)
	for _, persona := range req.Personas {
		findings, err := c.runPersonaReview(ctx, persona, req.Mode, req.RepoPath, req.FocusFiles)
		if err != nil {
			c.logger.Error("persona review failed",
				"persona", persona,
				"err", err,
			)
			// Continue with other personas on failure
			continue
		}
		allFindings = append(allFindings, findings...)
	}

	// Build the report
	report := &CouncilReport{
		Mode:     req.Mode,
		Personas: req.Personas,
		Findings: allFindings,
		Total:    len(allFindings),
		PassedAt: time.Now(),
		Duration: Duration{time.Since(start)},
	}

	// Count by severity
	for _, f := range allFindings {
		switch f.Severity {
		case SeverityCritical:
			report.Critical++
		case SeverityHigh:
			report.High++
		case SeverityMedium:
			report.Medium++
		case SeverityLow:
			report.Low++
		case SeverityInfo:
			report.Info++
		}
	}

	c.logger.Info("council review complete",
		"mode", req.Mode,
		"total", report.Total,
		"critical", report.Critical,
		"high", report.High,
		"medium", report.Medium,
		"low", report.Low,
		"info", report.Info,
		"duration", report.Duration.Duration,
	)

	return report, nil
}

// runPersonaReview runs the review for a single persona.
func (c *Client) runPersonaReview(ctx context.Context, persona Persona, mode ReviewMode, repoPath string, focusFiles []string) ([]CouncilFinding, error) {
	c.logger.Debug("running persona review",
		"persona", persona,
		"mode", mode,
	)

	args := map[string]any{
		"persona": string(persona),
		"mode":    string(mode),
	}
	if repoPath != "" {
		args["repo_path"] = repoPath
	}
	if len(focusFiles) > 0 {
		args["focus_files"] = focusFiles
	}

	rawResult, err := c.CallToolRaw(ctx, ToolAuditCompliance, args)
	if err != nil {
		return nil, fmt.Errorf("run persona %s: %w", persona, err)
	}

	var findings []CouncilFinding
	if err := json.Unmarshal(rawResult, &findings); err != nil {
		return nil, fmt.Errorf("parse persona findings: %w", err)
	}

	return findings, nil
}

// ─── Council → ADR Bridge ─────────────────────────────────────────────

// ADRBridge converts standards-gap findings into ADR creation requests.
type ADRBridge struct {
	logger *slog.Logger
}

// NewADRBridge creates a new bridge for council→ADR conversion.
func NewADRBridge(logger *slog.Logger) *ADRBridge {
	return &ADRBridge{logger: logger}
}

// StandardsGapFinding represents a finding that should become an ADR.
type StandardsGapFinding struct {
	Finding    CouncilFinding `json:"finding"`
	ADRTitle   string         `json:"adr_title"`
	ADRSummary string         `json:"adr_summary"`
	ADRContext string         `json:"adr_context"`
	Proposed   string         `json:"proposed"`
}

// FilterStandardsGaps extracts findings that represent standards gaps.
func FilterStandardsGaps(findings []CouncilFinding) []StandardsGapFinding {
	var gaps []StandardsGapFinding

	for _, f := range findings {
		if f.Category == CategoryStandardsGap {
			gap := StandardsGapFinding{
				Finding: f,
				ADRTitle: fmt.Sprintf("ADR: %s", f.Title),
				ADRSummary: f.Message,
				ADRContext: fmt.Sprintf("Detected by %s persona during %s review", f.Persona, "governance"),
				Proposed: f.Remediation,
			}
			gaps = append(gaps, gap)
		}
	}

	return gaps
}

// CreateADRFromFinding creates an ADR via the AdrMcp for a standards-gap finding.
func (c *Client) CreateADRFromFinding(ctx context.Context, gap StandardsGapFinding) (json.RawMessage, error) {
	c.logger.Info("creating ADR from council finding",
		"finding_id", gap.Finding.ID,
		"persona", gap.Finding.Persona,
		"title", gap.ADRTitle,
	)

	args := map[string]any{
		"title":   gap.ADRTitle,
		"summary": gap.ADRSummary,
		"context": gap.ADRContext,
		"proposed": gap.Proposed,
		"status":  "proposed",
		"metadata": map[string]string{
			"source":     "council",
			"persona":    string(gap.Finding.Persona),
			"finding_id": gap.Finding.ID,
			"severity":   string(gap.Finding.Severity),
			"rule_id":    gap.Finding.RuleID,
		},
	}

	// Call the ADR MCP's create_adr tool through the gateway.
	// The gateway routes this to the adr server.
	rawResult, err := c.CallToolRaw(ctx, ToolCreateADR, args)
	if err != nil {
		return nil, fmt.Errorf("create adr from finding %s: %w", gap.Finding.ID, err)
	}

	c.logger.Info("ADR created from council finding",
		"finding_id", gap.Finding.ID,
	)

	return rawResult, nil
}

// BridgeFindingsToADR processes a council report and creates ADRs for all standards-gap findings.
func (c *Client) BridgeFindingsToADR(ctx context.Context, report *CouncilReport) (int, error) {
	gaps := FilterStandardsGaps(report.Findings)
	if len(gaps) == 0 {
		c.logger.Info("no standards-gap findings to bridge to ADRs")
		return 0, nil
	}

	c.logger.Info("bridging findings to ADRs",
		"gaps", len(gaps),
	)

	successCount := 0
	for _, gap := range gaps {
		_, err := c.CreateADRFromFinding(ctx, gap)
		if err != nil {
			c.logger.Error("failed to create ADR from finding",
				"finding_id", gap.Finding.ID,
				"err", err,
			)
			continue
		}
		successCount++
	}

	c.logger.Info("ADR bridging complete",
		"total_gaps", len(gaps),
		"created", successCount,
		"failed", len(gaps)-successCount,
	)

	return successCount, nil
}
