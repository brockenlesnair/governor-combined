package docgov

import (
	"time"
)

// DocumentType represents the category of a governance document.
type DocumentType string

const (
	// Architecture Decision Records
	DocumentTypeADR DocumentType = "adr"

	// API Documentation
	DocumentTypeAPISpec     DocumentType = "api_spec"      // OpenAPI/AsyncAPI/GraphQL
	DocumentTypeAPIChangelog DocumentType = "api_changelog"

	// Architecture Documentation
	DocumentTypeArchOverview DocumentType = "arch_overview"  // C4 Context
	DocumentTypeArchContainer DocumentType = "arch_container" // C4 Container
	DocumentTypeArchComponent DocumentType = "arch_component" // C4 Component
	DocumentTypeArchCode     DocumentType = "arch_code"      // C4 Code/UML

	// Runbooks & Playbooks
	DocumentTypeRunbook        DocumentType = "runbook"
	DocumentTypeIncidentPlaybook DocumentType = "incident_playbook"
	DocumentTypeDisasterRecovery DocumentType = "disaster_recovery"

	// Data Documentation
	DocumentTypeDataDictionary DocumentType = "data_dictionary"
	DocumentTypeDataLineage    DocumentType = "data_lineage"
	DocumentTypeDataContract   DocumentType = "data_contract"

	// Security Documentation
	DocumentTypeThreatModel   DocumentType = "threat_model"   // STRIDE
	DocumentTypeSecurityPolicy DocumentType = "security_policy"

	// Compliance Documentation
	DocumentTypeComplianceEvidence DocumentType = "compliance_evidence"
	DocumentTypePolicy           DocumentType = "policy"
	DocumentTypeAuditReport      DocumentType = "audit_report"

	// Technical Specifications
	DocumentTypeRFCTechSpec DocumentType = "rfc_tech_spec"
	DocumentTypeDesignDoc   DocumentType = "design_doc"
	DocumentTypePoCDoc      DocumentType = "poc_doc"

	// Operational Documentation
	DocumentTypeSLO           DocumentType = "slo"
	DocumentTypePostmortem    DocumentType = "postmortem"
	DocumentTypeRunbookOperational DocumentType = "runbook_operational"

	// Release Documentation
	DocumentTypeChangelog     DocumentType = "changelog"
	DocumentTypeReleaseNotes  DocumentType = "release_notes"
	DocumentTypeMigrationGuide DocumentType = "migration_guide"

	// Code Documentation
	DocumentTypeCodeGuide     DocumentType = "code_guide"
	DocumentTypeArchDecision  DocumentType = "arch_decision" // Legacy alias for ADR
)

// String returns the string representation of the document type.
func (dt DocumentType) String() string {
	return string(dt)
}

// DocumentStatus represents the governance status of a document.
type DocumentStatus string

const (
	DocumentStatusDraft       DocumentStatus = "draft"
	DocumentStatusReview      DocumentStatus = "review"
	DocumentStatusApproved    DocumentStatus = "approved"
	DocumentStatusDeprecated  DocumentStatus = "deprecated"
	DocumentStatusSuperseded  DocumentStatus = "superseded"
	DocumentStatusMissing     DocumentStatus = "missing"
)

func (ds DocumentStatus) String() string {
	return string(ds)
}

// Document represents a governance document in the project.
type Document struct {
	ID          string            `json:"id"`
	Type        DocumentType      `json:"type"`
	Path        string            `json:"path"`
	Title       string            `json:"title"`
	Status      DocumentStatus    `json:"status"`
	Owner       string            `json:"owner,omitempty"`       // Team or person responsible
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	ApprovedAt  *time.Time        `json:"approved_at,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`    // Type-specific metadata
	Freshness   FreshnessInfo     `json:"freshness"`
	Validation  ValidationResult  `json:"validation"`
}

// FreshnessInfo tracks document freshness.
type FreshnessInfo struct {
	LastModified time.Time `json:"last_modified"`
	Age          time.Duration `json:"age"`
	Level        FreshnessLevel `json:"level"` // fresh, warning, stale, unknown
	MaxAge       time.Duration `json:"max_age,omitempty"`
	WarnAge      time.Duration `json:"warn_age,omitempty"`
}

// FreshnessLevel represents the freshness state.
type FreshnessLevel string

const (
	FreshnessLevelFresh   FreshnessLevel = "fresh"
	FreshnessLevelWarning FreshnessLevel = "warning"
	FreshnessLevelStale   FreshnessLevel = "stale"
	FreshnessLevelUnknown FreshnessLevel = "unknown"
)

func (fl FreshnessLevel) String() string {
	return string(fl)
}

// ValidationResult holds the result of document validation.
type ValidationResult struct {
	Valid      bool     `json:"valid"`
	Errors     []string `json:"errors,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
	Score      float64  `json:"score"` // 0.0 - 1.0
	CheckedAt  time.Time `json:"checked_at"`
	Validator  string   `json:"validator"` // Name of validator used
}

// DocumentTypeConfig holds configuration for a document type.
type DocumentTypeConfig struct {
	Type         DocumentType     `json:"type"`
	DisplayName  string           `json:"display_name"`
	Description  string           `json:"description"`
	PathPatterns []string         `json:"path_patterns"` // Glob patterns to find this type
	Required     bool             `json:"required"`      // At least one document of this type required
	MaxAge       time.Duration    `json:"max_age"`       // Staleness threshold
	WarnAge      time.Duration    `json:"warn_age"`
	Validator    string           `json:"validator"`     // Validator name
	RequiredFields []string        `json:"required_fields"` // Frontmatter fields required
	Template     string           `json:"template,omitempty"` // Template content
	Examples     []string         `json:"examples,omitempty"` // Example paths
}

// GetDefaultDocumentTypeConfigs returns the default document type configurations.
func GetDefaultDocumentTypeConfigs() []DocumentTypeConfig {
	return []DocumentTypeConfig{
		{
			Type:         DocumentTypeADR,
			DisplayName:  "Architecture Decision Record",
			Description:  "Captures architectural decisions with context, rationale, and consequences",
			PathPatterns: []string{"adr/*.md", "docs/adr/*.md", "architecture/decisions/*.md"},
			Required:     true,
			MaxAge:       90 * 24 * time.Hour,
			WarnAge:      30 * 24 * time.Hour,
			Validator:    "madr",
			RequiredFields: []string{"title", "status", "context", "decision", "consequences"},
			Template:     madrTemplate,
		},
		{
			Type:         DocumentTypeAPISpec,
			DisplayName:  "API Specification",
			Description:  "OpenAPI 3.x, AsyncAPI, or GraphQL SDL specification",
			PathPatterns: []string{"api/*.yaml", "api/*.yml", "api/*.json", "docs/api/*.yaml", "specs/*.yaml"},
			Required:     true,
			MaxAge:       30 * 24 * time.Hour,
			WarnAge:      7 * 24 * time.Hour,
			Validator:    "openapi",
			RequiredFields: []string{"openapi", "info", "paths"},
		},
		{
			Type:         DocumentTypeArchOverview,
			DisplayName:  "Architecture Overview (C4 Context)",
			Description:  "System context diagram showing the system in its environment",
			PathPatterns: []string{"docs/arch/context*.md", "architecture/context*.md", "docs/c4/context*.md"},
			Required:     true,
			MaxAge:       180 * 24 * time.Hour,
			WarnAge:      60 * 24 * time.Hour,
			Validator:    "c4_context",
			RequiredFields: []string{"title", "context"},
		},
		{
			Type:         DocumentTypeArchContainer,
			DisplayName:  "Container Diagram (C4)",
			Description:  "Applications, data stores, and technologies",
			PathPatterns: []string{"docs/arch/container*.md", "architecture/container*.md", "docs/c4/container*.md"},
			Required:     false,
			MaxAge:       180 * 24 * time.Hour,
			WarnAge:      60 * 24 * time.Hour,
			Validator:    "c4_container",
			RequiredFields: []string{"title", "containers"},
		},
		{
			Type:         DocumentTypeRunbook,
			DisplayName:  "Operational Runbook",
			Description:  "Step-by-step operational procedures",
			PathPatterns: []string{"runbooks/*.md", "docs/runbooks/*.md", "ops/runbooks/*.md"},
			Required:     true,
			MaxAge:       90 * 24 * time.Hour,
			WarnAge:      30 * 24 * time.Hour,
			Validator:    "runbook",
			RequiredFields: []string{"title", "steps", "owner", "severity"},
			Template:     runbookTemplate,
		},
		{
			Type:         DocumentTypeIncidentPlaybook,
			DisplayName:  "Incident Response Playbook",
			Description:  "Incident response procedures with roles and escalation",
			PathPatterns: []string{"runbooks/incident*.md", "docs/incident*.md", "ops/incident*.md"},
			Required:     true,
			MaxAge:       60 * 24 * time.Hour,
			WarnAge:      14 * 24 * time.Hour,
			Validator:    "incident_playbook",
			RequiredFields: []string{"title", "triage", "roles", "escalation", "communication"},
		},
		{
			Type:         DocumentTypeThreatModel,
			DisplayName:  "Threat Model (STRIDE)",
			Description:  "Security threat model with identified threats and mitigations",
			PathPatterns: []string{"security/threat-model*.md", "docs/security/threat*.md", "threat-model/*.md"},
			Required:     true,
			MaxAge:       90 * 24 * time.Hour,
			WarnAge:      30 * 24 * time.Hour,
			Validator:    "stride",
			RequiredFields: []string{"title", "system", "threats", "mitigations"},
		},
		{
			Type:         DocumentTypeDataDictionary,
			DisplayName:  "Data Dictionary",
			Description:  "Documents every field, table, relationship, and business rule",
			PathPatterns: []string{"docs/data/dictionary*.md", "data/dictionary*.md", "schemas/*.md"},
			Required:     false,
			MaxAge:       180 * 24 * time.Hour,
			WarnAge:      60 * 24 * time.Hour,
			Validator:    "data_dictionary",
			RequiredFields: []string{"title", "tables"},
		},
		{
			Type:         DocumentTypeSLO,
			DisplayName:  "Service Level Objectives",
			Description:  "SLI/SLO/SLA definitions with error budgets",
			PathPatterns: []string{"docs/slo*.md", "slo/*.md", "observability/slo*.md"},
			Required:     true,
			MaxAge:       90 * 24 * time.Hour,
			WarnAge:      30 * 24 * time.Hour,
			Validator:    "slo",
			RequiredFields: []string{"title", "slis", "slo_targets", "error_budget"},
		},
		{
			Type:         DocumentTypePostmortem,
			DisplayName:  "Blameless Postmortem",
			Description:  "Incident retrospective with root cause and action items",
			PathPatterns: []string{"postmortems/*.md", "docs/postmortems/*.md", "incidents/*.md"},
			Required:     false,
			MaxAge:       0, // Postmortems don't go stale
			WarnAge:      0,
			Validator:    "postmortem",
			RequiredFields: []string{"title", "summary", "timeline", "root_cause", "action_items"},
			Template:     postmortemTemplate,
		},
		{
			Type:         DocumentTypeChangelog,
			DisplayName:  "Changelog",
			Description:  "Human-readable changelog following Keep a Changelog format",
			PathPatterns: []string{"CHANGELOG.md", "changelog.md", "CHANGES.md", "docs/changelog.md"},
			Required:     true,
			MaxAge:       30 * 24 * time.Hour,
			WarnAge:      7 * 24 * time.Hour,
			Validator:    "changelog",
			RequiredFields: []string{"title", "versions"},
		},
		{
			Type:         DocumentTypeReleaseNotes,
			DisplayName:  "Release Notes",
			Description:  "Release notes with highlights, breaking changes, migration guide",
			PathPatterns: []string{"RELEASE*.md", "docs/release*.md", "releases/*.md"},
			Required:     false,
			MaxAge:       90 * 24 * time.Hour,
			WarnAge:      30 * 24 * time.Hour,
			Validator:    "release_notes",
			RequiredFields: []string{"title", "version", "highlights", "breaking_changes"},
		},
		{
			Type:         DocumentTypeCodeGuide,
			DisplayName:  "Code Documentation Guide",
			Description:  "Code documentation standards (GoDoc, JSDoc, RustDoc, etc.)",
			PathPatterns: []string{"docs/code-guide*.md", "CONTRIBUTING.md", "docs/contributing*.md"},
			Required:     false,
			MaxAge:       180 * 24 * time.Hour,
			WarnAge:      60 * 24 * time.Hour,
			Validator:    "code_guide",
			RequiredFields: []string{"title", "standards"},
		},
	}
}

// Templates for document types

const madrTemplate = `# {{.Title}}

**Status:** {{.Status}} (proposed | accepted | rejected | deprecated | superseded)
**Date:** {{.Date}}
**Decision Makers:** {{.DecisionMakers}}

## Context and Problem Statement

{{.Context}}

## Decision Drivers

{{.Drivers}}

## Considered Options

{{.Options}}

## Decision Outcome

**Chosen option:** {{.Decision}}

{{.Justification}}

### Positive Consequences

{{.PositiveConsequences}}

### Negative Consequences

{{.NegativeConsequences}}

## Confirmation

{{.Confirmation}}

## Links

- Related ADRs: {{.RelatedADRs}}
- Implementation: {{.ImplementationLink}}
`

const runbookTemplate = `# {{.Title}}

**Owner:** {{.Owner}}
**Severity:** {{.Severity}} (SEV-0 | SEV-1 | SEV-2 | SEV-3)
**Last Updated:** {{.Date}}
**Version:** {{.Version}}

## Purpose

{{.Purpose}}

## Prerequisites

{{.Prerequisites}}

## Steps

{{.Steps}}

## Verification

{{.Verification}}

## Rollback

{{.Rollback}}

## References

{{.References}}
`

const postmortemTemplate = `# Postmortem: {{.Title}}

**Date:** {{.Date}}
**Duration:** {{.Duration}}
**Severity:** {{.Severity}}
**Author:** {{.Author}}

## Summary

{{.Summary}}

## Timeline

{{.Timeline}}

## Root Cause

{{.RootCause}}

## Impact

{{.Impact}}

## What Went Well

{{.WhatWentWell}}

## What Went Wrong

{{.WhatWentWrong}}

## Action Items

| Action | Owner | Due Date | Status |
|--------|-------|----------|--------|
{{.ActionItems}}

## Lessons Learned

{{.LessonsLearned}}
`