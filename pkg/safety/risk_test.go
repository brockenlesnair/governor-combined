package safety

import (
	"testing"
)

func TestSeverityString(t *testing.T) {
	tests := []struct {
		s    Severity
		want string
	}{
		{SeverityLow, "low"},
		{SeverityMedium, "medium"},
		{SeverityHigh, "high"},
		{SeverityCritical, "critical"},
		{Severity(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestSeverityToBaseScore(t *testing.T) {
	tests := []struct {
		severity Severity
		want     float64
	}{
		{SeverityCritical, 9.0},
		{SeverityHigh, 6.0},
		{SeverityMedium, 3.5},
		{SeverityLow, 1.0},
		{Severity(99), 0},
	}
	for _, tt := range tests {
		got := severityToBaseScore(tt.severity)
		if got != tt.want {
			t.Errorf("severityToBaseScore(%d) = %v, want %v", tt.severity, got, tt.want)
		}
	}
}

func TestComputeRuleRisk_NoContext(t *testing.T) {
	rs := NewRiskScorer()
	rule := &Rule{Severity: SeverityCritical, Tags: []string{"security"}}
	score := rs.ComputeRuleRisk(rule, nil)
	if score < 0 || score > 10 {
		t.Errorf("score out of range: %v", score)
	}
}

func TestComputeRuleRisk_ProductionContext(t *testing.T) {
	rs := NewRiskScorer()
	rule := &Rule{Severity: SeverityMedium, Tags: []string{"security"}}
	ctx := &ChangeContext{IsProduction: true}
	score := rs.ComputeRuleRisk(rule, ctx)
	baseRule := &Rule{Severity: SeverityMedium, Tags: []string{"security"}}
	base := rs.ComputeRuleRisk(baseRule, nil)
	if score <= base {
		t.Errorf("production context should increase risk: %v <= %v", score, base)
	}
}

func TestComputeRuleRisk_TestContext(t *testing.T) {
	rs := NewRiskScorer()
	rule := &Rule{Severity: SeverityHigh, Tags: []string{}}
	ctx := &ChangeContext{IsTest: true}
	score := rs.ComputeRuleRisk(rule, ctx)
	baseRule := &Rule{Severity: SeverityHigh, Tags: []string{}}
	base := rs.ComputeRuleRisk(baseRule, nil)
	if score >= base {
		t.Errorf("test context should decrease risk: %v >= %v", score, base)
	}
}

func TestComputeRuleRisk_Clamped(t *testing.T) {
	rs := NewRiskScorer()
	rule := &Rule{Severity: SeverityCritical, Tags: []string{"security"}}
	ctx := &ChangeContext{IsProduction: true, IsDeployment: true}
	score := rs.ComputeRuleRisk(rule, ctx)
	if score > 10 {
		t.Errorf("score should be clamped to 10, got %v", score)
	}
}

func TestComputeAggregateRisk_Empty(t *testing.T) {
	rs := NewRiskScorer()
	if got := rs.ComputeAggregateRisk(nil, nil); got != 0 {
		t.Errorf("empty findings should return 0, got %v", got)
	}
}

func TestComputeAggregateRisk_AllSuppressed(t *testing.T) {
	rs := NewRiskScorer()
	findings := []Finding{
		{RiskScore: 8.0, Suppressed: true},
		{RiskScore: 9.0, Suppressed: true},
	}
	if got := rs.ComputeAggregateRisk(findings, nil); got != 0 {
		t.Errorf("all suppressed should return 0, got %v", got)
	}
}

func TestComputeAggregateRisk_MultipleFindings(t *testing.T) {
	rs := NewRiskScorer()
	findings := []Finding{
		{RiskScore: 5.0, Suppressed: false},
		{RiskScore: 3.0, Suppressed: false},
	}
	score := rs.ComputeAggregateRisk(findings, nil)
	if score < 0 || score > 10 {
		t.Errorf("score out of range: %v", score)
	}
	if score < 5.0 {
		t.Errorf("aggregate should be at least the max finding score, got %v", score)
	}
}

func TestGetRiskLevel(t *testing.T) {
	rs := NewRiskScorer()
	tests := []struct {
		score float64
		want  string
	}{
		{0.0, "low"},
		{2.0, "low"},
		{2.5, "medium"},
		{4.0, "medium"},
		{5.0, "high"},
		{7.0, "high"},
		{8.0, "critical"},
		{10.0, "critical"},
	}
	for _, tt := range tests {
		if got := rs.GetRiskLevel(tt.score); got != tt.want {
			t.Errorf("GetRiskLevel(%v) = %q, want %q", tt.score, got, tt.want)
		}
	}
}

func TestCVSSVector_ToString(t *testing.T) {
	cv := &CVSSVector{
		AttackVector:          "N",
		AttackComplexity:      "L",
		PrivilegesRequired:    "N",
		UserInteraction:       "N",
		Scope:                 "U",
		ConfidentialityImpact: "H",
		IntegrityImpact:       "H",
		AvailabilityImpact:    "H",
	}
	got := cv.ToString()
	if got != "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H" {
		t.Errorf("CVSS string: got %q", got)
	}
}

func TestComputeCVSS(t *testing.T) {
	rs := NewRiskScorer()
	cv := &CVSSVector{
		AttackVector:          "N",
		AttackComplexity:      "L",
		PrivilegesRequired:    "N",
		UserInteraction:       "N",
		Scope:                 "U",
		ConfidentialityImpact: "H",
		IntegrityImpact:       "H",
		AvailabilityImpact:    "H",
	}
	score := rs.ComputeCVSS(cv)
	if score < 0 || score > 10 {
		t.Errorf("CVSS score out of range: %v", score)
	}
}
