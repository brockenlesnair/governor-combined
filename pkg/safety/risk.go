package safety

import (
	"math"
)

// RiskScorer computes CVSS-like risk scores.
type RiskScorer struct{}

// NewRiskScorer creates a new risk scorer.
func NewRiskScorer() *RiskScorer {
	return &RiskScorer{}
}

// ComputeRuleRisk computes the risk score for a single rule finding.
func (r *RiskScorer) ComputeRuleRisk(rule *Rule, ctx *ChangeContext) float64 {
	// Base score from severity
	baseScore := severityToBaseScore(rule.Severity)

	// Adjust based on context
	multiplier := 1.0
	if ctx != nil {
		// Production changes are riskier
		if ctx.IsProduction {
			multiplier *= 1.5
		}
		// Deployment changes are riskier
		if ctx.IsDeployment {
			multiplier *= 1.3
		}
		// Test changes are less risky
		if ctx.IsTest {
			multiplier *= 0.5
		}
	}

	// Check for security tags (extra risk)
	hasSecurityTag := false
	for _, tag := range rule.Tags {
		if tag == "security" {
			hasSecurityTag = true
			break
		}
	}
	if hasSecurityTag {
		multiplier *= 1.2
	}

	score := baseScore * multiplier

	// Clamp to [0, 10]
	if score > 10 {
		score = 10
	}
	if score < 0 {
		score = 0
	}

	return score
}

// ComputeAggregateRisk computes the aggregate risk score for multiple findings.
func (r *RiskScorer) ComputeAggregateRisk(findings []Finding, ctx *ChangeContext) float64 {
	if len(findings) == 0 {
		return 0
	}

	// Filter out suppressed findings
	var activeFindings []Finding
	for _, f := range findings {
		if !f.Suppressed {
			activeFindings = append(activeFindings, f)
		}
	}

	if len(activeFindings) == 0 {
		return 0
	}

	// Use the maximum risk score as base
	maxScore := 0.0
	for _, f := range activeFindings {
		if f.RiskScore > maxScore {
			maxScore = f.RiskScore
		}
	}

	// Add compound risk for multiple findings
	compoundRisk := 0.0
	for i, f := range activeFindings {
		// Each additional finding adds diminishing risk
		compoundRisk += f.RiskScore * (1.0 / float64(i+1)) * 0.1
	}

	// Apply context multiplier
	multiplier := 1.0
	if ctx != nil {
		if ctx.IsProduction {
			multiplier *= 1.2
		}
		if ctx.IsDeployment {
			multiplier *= 1.1
		}
	}

	aggregateScore := (maxScore + compoundRisk) * multiplier

	// Clamp to [0, 10]
	if aggregateScore > 10 {
		aggregateScore = 10
	}
	if aggregateScore < 0 {
		aggregateScore = 0
	}

	return math.Round(aggregateScore*100) / 100
}

// GetRiskLevel converts a numeric risk score to a categorical level.
func (r *RiskScorer) GetRiskLevel(score float64) string {
	switch {
	case score >= 8.0:
		return "critical"
	case score >= 5.0:
		return "high"
	case score >= 2.5:
		return "medium"
	default:
		return "low"
	}
}

// severityToBaseScore converts a severity to a base risk score.
func severityToBaseScore(severity Severity) float64 {
	switch severity {
	case SeverityCritical:
		return 9.0
	case SeverityHigh:
		return 6.0
	case SeverityMedium:
		return 3.5
	case SeverityLow:
		return 1.0
	default:
		return 0
	}
}

// CVSSVector represents a CVSS 3.1 vector string.
type CVSSVector struct {
	AttackVector          string
	AttackComplexity      string
	PrivilegesRequired    string
	UserInteraction       string
	Scope                 string
	ConfidentialityImpact string
	IntegrityImpact       string
	AvailabilityImpact    string
}

// ToString converts a CVSSVector to a CVSS vector string.
func (cv *CVSSVector) ToString() string {
	return "CVSS:3.1/" +
		"AV:" + cv.AttackVector +
		"/AC:" + cv.AttackComplexity +
		"/PR:" + cv.PrivilegesRequired +
		"/UI:" + cv.UserInteraction +
		"/S:" + cv.Scope +
		"/C:" + cv.ConfidentialityImpact +
		"/I:" + cv.IntegrityImpact +
		"/A:" + cv.AvailabilityImpact
}

// ComputeCVSS computes a CVSS-like score.
func (r *RiskScorer) ComputeCVSS(cv *CVSSVector) float64 {
	// Simplified CVSS calculation
	score := 0.0

	// Base score components
	switch cv.AttackVector {
	case "N":
		score += 0.85
	case "A":
		score += 0.62
	case "L":
		score += 0.55
	case "P":
		score += 0.2
	}

	switch cv.AttackComplexity {
	case "L":
		score += 0.77
	case "H":
		score += 0.44
	}

	switch cv.PrivilegesRequired {
	case "N":
		score += 0.85
	case "L":
		score += 0.62
	case "H":
		score += 0.27
	}

	switch cv.UserInteraction {
	case "N":
		score += 0.85
	case "R":
		score += 0.62
	}

	// Impact
	impact := 1.0
	switch cv.ConfidentialityImpact {
	case "H":
		impact *= 0.56
	case "L":
		impact *= 0.22
	}
	switch cv.IntegrityImpact {
	case "H":
		impact *= 0.56
	case "L":
		impact *= 0.22
	}
	switch cv.AvailabilityImpact {
	case "H":
		impact *= 0.56
	case "L":
		impact *= 0.22
	}

	score *= impact

	// Clamp to [0, 10]
	if score > 10 {
		score = 10
	}

	return math.Round(score*100) / 100
}