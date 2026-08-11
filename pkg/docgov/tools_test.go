package docgov

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/brockenlesnair/governor-combined/pkg/staleness"
)

func TestDocGovTools_SuggestDocument_OpinionatedRecommendations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "docgov-suggest-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mustWrite := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) failed: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) failed: %v", path, err)
		}
	}

	mustWrite(filepath.Join(tmpDir, "adr", "0001-use-postgresql.md"), `---
title: "Use PostgreSQL"
status: accepted
context: "We need durable storage"
decision: "Use PostgreSQL"
consequences: "We gain relational consistency"
---

# Context
We need a database.

## Decision
Use PostgreSQL.

## Consequences
Better durability.
`)

	mustWrite(filepath.Join(tmpDir, "api", "openapi.yaml"), `openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"
paths:
  /health:
    get:
      responses:
        "200":
          description: OK
`)

	mustWrite(filepath.Join(tmpDir, "docs", "arch", "context.md"), `---
title: "System Context"
context: "External users depend on the system"
---

# System Context
External users interact with the system boundary.
`)

	mustWrite(filepath.Join(tmpDir, "docs", "incident-response.md"), `---
title: "Incident Response"
triage: "First response"
roles: ["incident commander"]
escalation: "Escalate quickly"
communication: "Status updates"
---

# Triage
Triage and diagnose the incident.

## Roles
Roles are assigned.

## Escalation
Escalate when needed.

## Communication
Communicate with stakeholders.
`)

	mustWrite(filepath.Join(tmpDir, "docs", "slo.md"), `---
title: "Platform SLOs"
slis: ["availability"]
slo_targets: ["99.9%"]
error_budget: "0.1%"
---

# SLI
Track availability.

# SLO
Define service objectives.

# Error Budget
Track error budget and burn rate.
`)

	mustWrite(filepath.Join(tmpDir, "docs", "security", "threat-model.md"), `---
title: "Threat Model"
system: "governor-combined"
threats: ["spoofing", "tampering"]
mitigations: ["mfa", "integrity checks"]
---

# Threats
Spoofing, tampering, repudiation, information disclosure, denial of service, and elevation of privilege are considered.
`)

	mustWrite(filepath.Join(tmpDir, "runbooks", "deploy.md"), `---
title: "Deploy Service"
owner: "platform"
severity: "SEV-2"
---

# Prerequisites
Cluster access.

## Steps
1. Deploy the service.

## Verification
Check the rollout.

## Rollback
Revert to the previous release.
`)

	mustWrite(filepath.Join(tmpDir, "CHANGELOG.md"), `# Changelog

## [1.0.0] - 2026-08-11
### Added
- Initial release
`)

	mustWrite(filepath.Join(tmpDir, "main.go"), `package main

func main() {}
`)

	stalenessChk := staleness.NewChecker(staleness.Config{
		ScanPaths: []string{tmpDir},
	})

	registry := NewDocumentRegistry(tmpDir, nil, NewValidatorRegistry(), stalenessChk)
	if err := registry.Scan(context.Background()); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if missing := registry.GetMissingRequiredTypes(); len(missing) != 0 {
		t.Fatalf("expected no missing required types, got %d", len(missing))
	}

	recommended := registry.GetRecommendedTypes()
	if len(recommended) == 0 {
		t.Fatal("expected at least one optional recommendation")
	}

	wantTypes := map[DocumentType]bool{
		DocumentTypeArchContainer: false,
		DocumentTypePostmortem:    false,
		DocumentTypeCodeGuide:     false,
		DocumentTypeReleaseNotes:  false,
	}
	for _, rec := range recommended {
		if _, ok := wantTypes[rec.Type]; ok {
			wantTypes[rec.Type] = true
		}
		if rec.Priority <= 0 {
			t.Fatalf("recommendation %s had non-positive priority", rec.Type)
		}
		if rec.Reason == "" {
			t.Fatalf("recommendation %s had no reason", rec.Type)
		}
	}

	for docType, seen := range wantTypes {
		if !seen {
			t.Errorf("expected recommendation for %s", docType)
		}
	}

	tools := NewDocGovTools(registry)
	result, err := tools.handleSuggestDocument(context.Background(), nil)
	if err != nil {
		t.Fatalf("handleSuggestDocument failed: %v", err)
	}

	if got, ok := result["required_count"].(int); !ok || got != 0 {
		t.Fatalf("expected required_count=0, got %#v", result["required_count"])
	}
	if got, ok := result["recommended_count"].(int); !ok || got == 0 {
		t.Fatalf("expected recommended_count > 0, got %#v", result["recommended_count"])
	}
	if got, ok := result["count"].(int); !ok || got == 0 {
		t.Fatalf("expected count > 0, got %#v", result["count"])
	}
}
