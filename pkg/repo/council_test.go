package repo

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestAllPersonas(t *testing.T) {
	personas := AllPersonas()
	if len(personas) != 5 {
		t.Errorf("expected 5 personas, got %d", len(personas))
	}

	expected := map[Persona]bool{
		PersonaSecurity:        false,
		PersonaPerformance:     false,
		PersonaReliability:     false,
		PersonaMaintainability: false,
		PersonaCompliance:      false,
	}

	for _, p := range personas {
		if _, ok := expected[p]; !ok {
			t.Errorf("unexpected persona: %s", p)
		}
		expected[p] = true
	}

	for p, found := range expected {
		if !found {
			t.Errorf("missing persona: %s", p)
		}
	}
}

func TestFilterStandardsGaps(t *testing.T) {
	findings := []CouncilFinding{
		{
			ID:          "f1",
			Persona:     PersonaSecurity,
			Category:    CategoryStandardsGap,
			Severity:    SeverityHigh,
			Title:       "Missing rate limiting",
			Message:     "API endpoints lack rate limiting",
			Remediation: "Add rate limiting middleware",
		},
		{
			ID:       "f2",
			Persona:  PersonaPerformance,
			Category: CategoryBug,
			Severity: SeverityMedium,
			Title:    "N+1 query",
			Message:  "Database query in loop",
		},
		{
			ID:          "f3",
			Persona:     PersonaCompliance,
			Category:    CategoryStandardsGap,
			Severity:    SeverityCritical,
			Title:       "No audit logging",
			Message:     "Critical operations are not audited",
			Remediation: "Implement audit logging for all write operations",
		},
	}

	gaps := FilterStandardsGaps(findings)
	if len(gaps) != 2 {
		t.Fatalf("expected 2 standards gaps, got %d", len(gaps))
	}

	if gaps[0].Finding.ID != "f1" {
		t.Errorf("expected first gap from f1, got %s", gaps[0].Finding.ID)
	}
	if gaps[0].ADRTitle != "ADR: Missing rate limiting" {
		t.Errorf("unexpected ADR title: %s", gaps[0].ADRTitle)
	}
	if gaps[0].ADRSummary != "API endpoints lack rate limiting" {
		t.Errorf("unexpected ADR summary: %s", gaps[0].ADRSummary)
	}
	if gaps[0].Proposed != "Add rate limiting middleware" {
		t.Errorf("unexpected proposed: %s", gaps[0].Proposed)
	}
	if !strings.Contains(gaps[0].ADRContext, "security") {
		t.Errorf("expected context to mention security persona, got: %s", gaps[0].ADRContext)
	}
}

func TestFilterStandardsGaps_NoGaps(t *testing.T) {
	findings := []CouncilFinding{
		{
			ID:       "f1",
			Category: CategoryBug,
			Title:    "some bug",
		},
	}

	gaps := FilterStandardsGaps(findings)
	if len(gaps) != 0 {
		t.Errorf("expected 0 gaps, got %d", len(gaps))
	}
}

func TestFilterStandardsGaps_Empty(t *testing.T) {
	gaps := FilterStandardsGaps(nil)
	if len(gaps) != 0 {
		t.Errorf("expected 0 gaps for nil findings, got %d", len(gaps))
	}
}

func TestNewADRBridge(t *testing.T) {
	bridge := NewADRBridge(slog.Default())
	if bridge == nil {
		t.Fatal("expected non-nil bridge")
	}
	if bridge.logger == nil {
		t.Error("logger should not be nil")
	}
}

func TestCouncilFinding_Creation(t *testing.T) {
	now := time.Now()
	finding := CouncilFinding{
		ID:          "finding-1",
		Persona:     PersonaSecurity,
		Category:    CategoryVulnerability,
		Severity:    SeverityCritical,
		Title:       "SQL Injection",
		Message:     "User input used in SQL query without sanitization",
		File:        "pkg/db/query.go",
		Line:        42,
		RuleID:      "SEC001",
		Remediation: "Use parameterized queries",
		Metadata:    map[string]string{"cwe": "CWE-89"},
		CreatedAt:   now,
	}

	if finding.ID != "finding-1" {
		t.Errorf("expected ID 'finding-1', got %s", finding.ID)
	}
	if finding.Persona != PersonaSecurity {
		t.Errorf("expected persona security, got %s", finding.Persona)
	}
	if finding.Category != CategoryVulnerability {
		t.Errorf("expected category vulnerability, got %s", finding.Category)
	}
	if finding.Severity != SeverityCritical {
		t.Errorf("expected severity critical, got %s", finding.Severity)
	}
	if finding.Line != 42 {
		t.Errorf("expected line 42, got %d", finding.Line)
	}
	if finding.Metadata["cwe"] != "CWE-89" {
		t.Errorf("expected CWE-89 metadata, got %s", finding.Metadata["cwe"])
	}
}

func TestCouncilReport_SeverityCounts(t *testing.T) {
	report := &CouncilReport{
		Mode:     ModeFull,
		Personas: AllPersonas(),
		Findings: []CouncilFinding{
			{Severity: SeverityCritical},
			{Severity: SeverityCritical},
			{Severity: SeverityHigh},
			{Severity: SeverityHigh},
			{Severity: SeverityMedium},
			{Severity: SeverityLow},
			{Severity: SeverityInfo},
		},
	}

	// Count severities
	for _, f := range report.Findings {
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
	report.Total = len(report.Findings)

	if report.Total != 7 {
		t.Errorf("expected 7 total, got %d", report.Total)
	}
	if report.Critical != 2 {
		t.Errorf("expected 2 critical, got %d", report.Critical)
	}
	if report.High != 2 {
		t.Errorf("expected 2 high, got %d", report.High)
	}
	if report.Medium != 1 {
		t.Errorf("expected 1 medium, got %d", report.Medium)
	}
	if report.Low != 1 {
		t.Errorf("expected 1 low, got %d", report.Low)
	}
	if report.Info != 1 {
		t.Errorf("expected 1 info, got %d", report.Info)
	}
}

func TestFindingSeverity_Constants(t *testing.T) {
	severities := []FindingSeverity{
		SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow, SeverityInfo,
	}

	if len(severities) != 5 {
		t.Errorf("expected 5 severities, got %d", len(severities))
	}

	severitySet := make(map[FindingSeverity]bool)
	for _, s := range severities {
		severitySet[s] = true
	}

	for _, expected := range []string{"critical", "high", "medium", "low", "info"} {
		if !severitySet[FindingSeverity(expected)] {
			t.Errorf("missing severity: %s", expected)
		}
	}
}

func TestFindingCategory_Constants(t *testing.T) {
	categories := []FindingCategory{
		CategoryStandardsGap, CategoryBug, CategorySmell,
		CategoryVulnerability, CategoryPerformance, CategoryCompliance,
	}

	if len(categories) != 6 {
		t.Errorf("expected 6 categories, got %d", len(categories))
	}
}

func TestReviewMode_Constants(t *testing.T) {
	if ModeQuick != "quick" {
		t.Errorf("expected ModeQuick 'quick', got %s", ModeQuick)
	}
	if ModeFull != "full" {
		t.Errorf("expected ModeFull 'full', got %s", ModeFull)
	}
}
