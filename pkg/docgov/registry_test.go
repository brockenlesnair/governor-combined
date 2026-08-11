package docgov

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/brockenlesnair/governor-combined/pkg/staleness"
)

func TestDocumentType_String(t *testing.T) {
	tests := []struct {
		dt     DocumentType
		expect string
	}{
		{DocumentTypeADR, "adr"},
		{DocumentTypeAPISpec, "api_spec"},
		{DocumentTypeRunbook, "runbook"},
		{DocumentTypePostmortem, "postmortem"},
	}

	for _, tc := range tests {
		if tc.dt.String() != tc.expect {
			t.Errorf("DocumentType(%s).String() = %s, want %s", tc.dt, tc.dt.String(), tc.expect)
		}
	}
}

func TestFreshnessLevel_String(t *testing.T) {
	tests := []struct {
		fl     FreshnessLevel
		expect string
	}{
		{FreshnessLevelFresh, "fresh"},
		{FreshnessLevelWarning, "warning"},
		{FreshnessLevelStale, "stale"},
		{FreshnessLevelUnknown, "unknown"},
	}

	for _, tc := range tests {
		if tc.fl.String() != tc.expect {
			t.Errorf("FreshnessLevel(%s).String() = %s, want %s", tc.fl, tc.fl.String(), tc.expect)
		}
	}
}

func TestDocumentStatus_String(t *testing.T) {
	tests := []struct {
		ds     DocumentStatus
		expect string
	}{
		{DocumentStatusDraft, "draft"},
		{DocumentStatusReview, "review"},
		{DocumentStatusApproved, "approved"},
		{DocumentStatusDeprecated, "deprecated"},
		{DocumentStatusSuperseded, "superseded"},
		{DocumentStatusMissing, "missing"},
	}

	for _, tc := range tests {
		if tc.ds.String() != tc.expect {
			t.Errorf("DocumentStatus(%s).String() = %s, want %s", tc.ds, tc.ds.String(), tc.expect)
		}
	}
}

func TestValidatorRegistry_Builtins(t *testing.T) {
	vr := NewValidatorRegistry()

	expectedValidators := []string{
		"madr", "openapi", "c4_context", "c4_container", "c4_component",
		"runbook", "runbook_operational", "disaster_recovery", "incident_playbook", "stride",
		"data_dictionary", "slo", "postmortem",
		"changelog", "api_changelog", "migration_guide", "release_notes", "code_guide",
	}

	for _, name := range expectedValidators {
		if _, ok := vr.Get(name); !ok {
			t.Errorf("missing validator: %s", name)
		}
	}
}

func TestADRValidator_ValidMADR(t *testing.T) {
	v := NewADRValidator()
	content := []byte(`---
title: "Test ADR"
status: accepted
context: "We need to decide"
decision: "We will use X"
consequences: "Things will happen"
---

# Context

This is the context.

## Decision

We decided.

## Consequences

Good things happen.
`)

	config := DocumentTypeConfig{
		Validator:      "madr",
		RequiredFields: []string{"title", "status", "context", "decision", "consequences"},
	}

	result := v.Validate(context.Background(), content, config)
	if !result.Valid {
		t.Errorf("valid ADR should pass, got errors: %v", result.Errors)
	}
	if result.Score < 0.8 {
		t.Errorf("score too low: %f", result.Score)
	}
}

func TestADRValidator_InvalidStatus(t *testing.T) {
	v := NewADRValidator()
	content := []byte(`---
title: "Test ADR"
status: invalid_status
context: "context"
decision: "decision"
consequences: "consequences"
---

# Context
Context here.

## Decision
Decision here.

## Consequences
Consequences here.
`)

	config := DocumentTypeConfig{
		Validator:      "madr",
		RequiredFields: []string{"title", "status", "context", "decision", "consequences"},
	}

	result := v.Validate(context.Background(), content, config)
	if len(result.Warnings) == 0 {
		t.Error("expected warning for invalid status")
	}
}

func TestOpenAPIValidator_ValidSpec(t *testing.T) {
	v := NewOpenAPIValidator()
	content := []byte(`openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"
paths:
  /test:
    get:
      responses:
        '200':
          description: OK
`)

	config := DocumentTypeConfig{
		Validator:      "openapi",
		RequiredFields: []string{"openapi", "info", "paths"},
	}

	result := v.Validate(context.Background(), content, config)
	if !result.Valid {
		t.Errorf("valid OpenAPI spec should pass, got errors: %v", result.Errors)
	}
}

func TestRunbookValidator_ValidRunbook(t *testing.T) {
	v := NewRunbookValidator()
	content := []byte(`---
title: "Deploy Service"
severity: SEV-2
owner: "platform-team"
---

# Prerequisites

- Kubernetes cluster

## Steps

1. Apply manifests
2. Verify deployment

## Verification

Check pods are running

## Rollback

Rollback deployment
`)

	config := DocumentTypeConfig{
		Validator:      "runbook",
		RequiredFields: []string{"title", "owner", "severity"},
	}

	result := v.Validate(context.Background(), content, config)
	if !result.Valid {
		t.Errorf("valid runbook should pass, got errors: %v", result.Errors)
	}
}

func TestNewValidators_Validate(t *testing.T) {
	tests := []struct {
		name      string
		validator DocumentValidator
		content   []byte
		config    DocumentTypeConfig
	}{
		{
			name:      "c4 component",
			validator: NewC4ComponentValidator(),
			content: []byte(`---
title: "Component View"
components: ["api", "worker"]
---

# Components

This component diagram shows the service, its responsibilities, interface, and dependencies.
`),
			config: DocumentTypeConfig{Validator: "c4_component", RequiredFields: []string{"title", "components"}},
		},
		{
			name:      "runbook operational",
			validator: NewRunbookOperationalValidator(),
			content: []byte(`---
title: "Rotate Certificates"
severity: SEV-3
owner: "platform"
---

# Steps

1. Check expiry
2. Renew certificate

## Monitoring

Watch alerts and logs.

## Verification

Confirm the service is healthy.

## Rollback

Revert to the previous certificate if needed.
`),
			config: DocumentTypeConfig{Validator: "runbook_operational", RequiredFields: []string{"title", "owner", "severity"}},
		},
		{
			name:      "disaster recovery",
			validator: NewDisasterRecoveryValidator(),
			content: []byte(`---
title: "Primary Region Loss"
owner: "platform"
---

# Recovery

Backup data, restore services, and fail over to the secondary region.

## RTO

15 minutes.

## RPO

5 minutes.
`),
			config: DocumentTypeConfig{Validator: "disaster_recovery", RequiredFields: []string{"title", "owner"}},
		},
		{
			name:      "api changelog",
			validator: NewAPIChangelogValidator(),
			content: []byte(`---
title: "API Changes"
versions: ["1.0.0"]
---

## [1.0.0] - 2026-08-11

### Added

- New endpoint.
`),
			config: DocumentTypeConfig{Validator: "api_changelog", RequiredFields: []string{"title", "versions"}},
		},
		{
			name:      "migration guide",
			validator: NewMigrationGuideValidator(),
			content: []byte(`---
title: "Upgrade to v2"
from_version: "1.0.0"
to_version: "2.0.0"
---

## Prerequisites

Review breaking changes.

## Upgrade Steps

Follow the steps carefully.

## Verification

Confirm the system starts.
`),
			config: DocumentTypeConfig{Validator: "migration_guide", RequiredFields: []string{"title", "from_version", "to_version"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.validator.Validate(context.Background(), tc.content, tc.config)
			if !result.Valid {
				t.Fatalf("expected %s validator to accept sample content: %v", tc.name, result.Errors)
			}
		})
	}
}

func TestRegistry_Scan(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "docgov-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test documents
	adrDir := filepath.Join(tmpDir, "adr")
	if err := os.MkdirAll(adrDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	adrContent := `---
title: "Use PostgreSQL"
status: accepted
context: "Need a database"
decision: "Use PostgreSQL"
consequences: "Better performance"
---
`
	if err := os.WriteFile(filepath.Join(adrDir, "001-use-postgres.md"), []byte(adrContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	runbookDir := filepath.Join(tmpDir, "runbooks")
	if err := os.MkdirAll(runbookDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	runbookContent := `---
title: "Deploy App"
severity: SEV-2
owner: "team-a"
---

# Steps

1. Deploy
`
	if err := os.WriteFile(filepath.Join(runbookDir, "deploy.md"), []byte(runbookContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	stalenessChk := staleness.NewChecker(staleness.Config{
		ScanPaths:       []string{tmpDir},
		IncludePatterns: []string{"*.md"},
	})

	validatorReg := NewValidatorRegistry()
	registry := NewDocumentRegistry(tmpDir, nil, validatorReg, stalenessChk)

	ctx := context.Background()
	if err := registry.Scan(ctx); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	docs := registry.ListDocuments("")
	if len(docs) != 2 {
		t.Errorf("expected 2 documents, got %d", len(docs))
	}

	// Check ADR
	adrDoc, ok := registry.GetDocument("adr/001-use-postgres.md")
	if !ok {
		t.Error("ADR document not found")
	} else {
		if adrDoc.Type != DocumentTypeADR {
			t.Errorf("expected ADR type, got %s", adrDoc.Type)
		}
		if !adrDoc.Validation.Valid {
			t.Errorf("ADR should be valid: %v", adrDoc.Validation.Errors)
		}
	}

	// Check runbook
	runbookDoc, ok := registry.GetDocument("runbooks/deploy.md")
	if !ok {
		t.Error("Runbook document not found")
	} else {
		if runbookDoc.Type != DocumentTypeRunbook {
			t.Errorf("expected Runbook type, got %s", runbookDoc.Type)
		}
	}

	// Check missing required
	missing := registry.GetMissingRequiredTypes()
	if len(missing) == 0 {
		t.Error("expected missing required types (api_spec, arch_overview, etc.)")
	}
}

func TestRegistry_ValidateDocument(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "docgov-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	adrDir := filepath.Join(tmpDir, "adr")
	if err := os.MkdirAll(adrDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	adrContent := `---
title: "Test ADR"
status: accepted
context: "context"
decision: "decision"
consequences: "consequences"
---
`
	if err := os.WriteFile(filepath.Join(adrDir, "test.md"), []byte(adrContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	stalenessChk := staleness.NewChecker(staleness.Config{
		ScanPaths: []string{tmpDir},
	})

	validatorReg := NewValidatorRegistry()
	registry := NewDocumentRegistry(tmpDir, nil, validatorReg, stalenessChk)

	ctx := context.Background()
	if err := registry.Scan(ctx); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	doc, err := registry.ValidateDocument(ctx, "adr/test.md")
	if err != nil {
		t.Fatalf("ValidateDocument failed: %v", err)
	}

	if !doc.Validation.Valid {
		t.Errorf("document should be valid: %v", doc.Validation.Errors)
	}
}

func TestDocumentRegistry_RefreshDocument(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "docgov-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	adrDir := filepath.Join(tmpDir, "adr")
	if err := os.MkdirAll(adrDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	adrContent := `---
title: "Original"
status: draft
context: "context"
decision: "decision"
consequences: "consequences"
---
`
	if err := os.WriteFile(filepath.Join(adrDir, "test.md"), []byte(adrContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	stalenessChk := staleness.NewChecker(staleness.Config{
		ScanPaths: []string{tmpDir},
	})

	validatorReg := NewValidatorRegistry()
	registry := NewDocumentRegistry(tmpDir, nil, validatorReg, stalenessChk)

	ctx := context.Background()
	if err := registry.Scan(ctx); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Update the file
	updatedContent := `---
title: "Updated Title"
status: accepted
context: "context"
decision: "decision"
consequences: "consequences"
---
`
	if err := os.WriteFile(filepath.Join(adrDir, "test.md"), []byte(updatedContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Refresh
	doc, err := registry.RefreshDocument(ctx, "adr/test.md")
	if err != nil {
		t.Fatalf("RefreshDocument failed: %v", err)
	}

	if doc.Title != "Updated Title" {
		t.Errorf("title not updated: %s", doc.Title)
	}
	if doc.Status != DocumentStatusApproved {
		t.Errorf("status not updated: %s", doc.Status)
	}
}

func TestDocumentRegistry_Stats(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "docgov-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	adrDir := filepath.Join(tmpDir, "adr")
	if err := os.MkdirAll(adrDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	adrContent := `---
title: "Test ADR"
status: accepted
context: "context"
decision: "decision"
consequences: "consequences"
---
`
	if err := os.WriteFile(filepath.Join(adrDir, "test.md"), []byte(adrContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	stalenessChk := staleness.NewChecker(staleness.Config{
		ScanPaths: []string{tmpDir},
	})

	validatorReg := NewValidatorRegistry()
	registry := NewDocumentRegistry(tmpDir, nil, validatorReg, stalenessChk)

	ctx := context.Background()
	if err := registry.Scan(ctx); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	stats := registry.GetStats()
	if stats.TotalDocuments != 1 {
		t.Errorf("expected 1 total document, got %d", stats.TotalDocuments)
	}
	if stats.ByType[DocumentTypeADR] != 1 {
		t.Errorf("expected 1 ADR, got %d", stats.ByType[DocumentTypeADR])
	}
	if stats.ByStatus[DocumentStatusApproved] != 1 {
		t.Errorf("expected 1 approved, got %d", stats.ByStatus[DocumentStatusApproved])
	}
	if stats.ByFreshness[FreshnessLevelFresh] != 1 {
		t.Errorf("expected 1 fresh, got %d", stats.ByFreshness[FreshnessLevelFresh])
	}
	if stats.InvalidDocuments != 0 {
		t.Errorf("expected 0 invalid, got %d", stats.InvalidDocuments)
	}
}
