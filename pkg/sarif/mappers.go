// Package sarif provides SARIF 2.1.0 parsing, deduplication, and DefectDojo integration
// for the governance orchestration layer.
package sarif

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// ─── SARIF 2.1.0 Structure ───────────────────────────────────────────

// Report is the top-level SARIF 2.1.0 report structure.
type Report struct {
	Schema  string `json:"$schema"`
	Version string `json:"version"`
	Runs    []Run  `json:"runs"`
}

// Run is a single analysis run within a SARIF report.
type Run struct {
	Tool       Tool       `json:"tool"`
	Results    []Result   `json:"results"`
	Artifacts  []Artifact `json:"artifacts,omitempty"`
	Timestamps *Timestamps `json:"timestamps,omitempty"`
}

// Tool describes the tool that produced the results.
type Tool struct {
	Driver ToolDriver `json:"driver"`
}

type ToolDriver struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	InformationURI string `json:"informationUri,omitempty"`
	Rules          []Rule `json:"rules,omitempty"`
}

// Rule defines a rule that can produce results.
type Rule struct {
	ID                  string       `json:"id"`
	Name                string       `json:"name"`
	Description         *Message     `json:"description,omitempty"`
	HelpURI             string       `json:"helpUri,omitempty"`
	Properties          *RuleProperties `json:"properties,omitempty"`
	DefaultConfiguration *RuleConfig `json:"defaultConfiguration,omitempty"`
}

type Message struct {
	Text string `json:"text"`
}

type RuleProperties struct {
	Tags      []string `json:"tags,omitempty"`
	Severity  string   `json:"severity,omitempty"`
	Precision string   `json:"precision,omitempty"`
}

type RuleConfig struct {
	Level string `json:"level"`
}

// Result is a single finding from the analysis.
type Result struct {
	RuleID       string            `json:"ruleId"`
	RuleIndex    int               `json:"ruleIndex,omitempty"`
	Level        string            `json:"level"` // "error", "warning", "note"
	Message      Message           `json:"message"`
	Locations    []Location        `json:"locations,omitempty"`
	Fingerprints map[string]string `json:"fingerprints,omitempty"`
	Properties   *ResultProperties `json:"properties,omitempty"`
}

// Location identifies where a result occurs.
type Location struct {
	PhysicalLocation *PhysicalLocation `json:"physicalLocation,omitempty"`
	LogicalLocation  *LogicalLocation  `json:"logicalLocation,omitempty"`
}

// PhysicalLocation identifies a file and region.
type PhysicalLocation struct {
	ArtifactLocation ArtifactLocation `json:"artifactLocation"`
	Region           Region           `json:"region"`
}

type ArtifactLocation struct {
	URI       string   `json:"uri"`
	URIBaseId string   `json:"uriBaseId,omitempty"`
	Message   *Message `json:"message,omitempty"`
}

// Region identifies a range within a file.
type Region struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
	EndLine     int `json:"endLine,omitempty"`
	EndColumn   int `json:"endColumn,omitempty"`
}

type LogicalLocation struct {
	FullyQualifiedName string `json:"fullyQualifiedName,omitempty"`
	Kind               string `json:"kind,omitempty"`
	Name               string `json:"name,omitempty"`
}

type ResultProperties struct{}

type Artifact struct {
	Location ArtifactLocation `json:"location"`
}

type Timestamps struct {
	StartTime time.Time `json:"startTime,omitempty"`
	EndTime   time.Time `json:"endTime,omitempty"`
}

// ─── Rule ID Normalization ────────────────────────────────────────────

// RulePrefix identifies the governance tool that produced a rule.
type RulePrefix string

const (
	PrefixDependabot    RulePrefix = "hangar.dependabot"
	PrefixASTComplexity RulePrefix = "rigour.ast.complexity"
	PrefixADRConflict   RulePrefix = "adr.conflict"
	PrefixStandardsGap  RulePrefix = "rb.standards-gap"
	PrefixSARIF         RulePrefix = "sarif"
)

// NormalizedRuleID holds the decomposed rule ID components.
type NormalizedRuleID struct {
	Prefix   RulePrefix `json:"prefix"`
	Category string     `json:"category"`
	Rule     string     `json:"rule"`
	Full     string     `json:"full"`
}

// NormalizeRuleID decomposes a rule ID into prefix, category, and rule components.
//
// Examples:
//
//	"hangar.dependabot.vulnerable-dependency" → prefix=hangar.dependabot, rule=vulnerable-dependency
//	"rigour.ast.complexity.high-cognitive-load" → prefix=rigour.ast.complexity, rule=high-cognitive-load
//	"adr.conflict.duplicate" → prefix=adr.conflict, rule=duplicate
//	"rb.standards-gap.missing-codeowners" → prefix=rb.standards-gap, rule=missing-codeowners
func NormalizeRuleID(ruleID string) NormalizedRuleID {
	norm := NormalizedRuleID{Full: ruleID}

	prefixes := []RulePrefix{
		PrefixDependabot,
		PrefixASTComplexity,
		PrefixADRConflict,
		PrefixStandardsGap,
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(ruleID, string(prefix)+".") {
			norm.Prefix = prefix
			remainder := ruleID[len(string(prefix))+1:]
			parts := strings.SplitN(remainder, ".", 2)
			if len(parts) == 2 {
				norm.Category = parts[0]
				norm.Rule = parts[1]
			} else {
				norm.Rule = parts[0]
			}
			return norm
		}
	}

	// Unknown prefix: treat the whole thing as the rule
	norm.Prefix = PrefixSARIF
	norm.Rule = ruleID
	return norm
}

// ─── Severity Mapping ─────────────────────────────────────────────────

// Severity represents the normalized severity of a finding.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityNote    Severity = "note"
	SeverityNone    Severity = "none"
)

// MapSeverity converts a SARIF level to a normalized severity.
func MapSeverity(level string) Severity {
	switch strings.ToLower(level) {
	case "error":
		return SeverityError
	case "warning":
		return SeverityWarning
	case "note":
		return SeverityNote
	default:
		return SeverityNone
	}
}

// SeverityWeight returns a numeric weight for sorting (higher = more severe).
func SeverityWeight(s Severity) int {
	switch s {
	case SeverityError:
		return 4
	case SeverityWarning:
		return 3
	case SeverityNote:
		return 2
	default:
		return 0
	}
}

// ─── Mappers ──────────────────────────────────────────────────────────

// Finding is a normalized finding extracted from SARIF.
type Finding struct {
	ID          string           `json:"id"`
	RuleID      string           `json:"rule_id"`
	Normalized  NormalizedRuleID `json:"normalized_rule"`
	Level       string           `json:"level"`
	Severity    Severity         `json:"severity"`
	Message     string           `json:"message"`
	File        string           `json:"file"`
	Line        int              `json:"line"`
	Column      int              `json:"column"`
	EndLine     int              `json:"end_line,omitempty"`
	EndColumn   int              `json:"end_column,omitempty"`
	Tool        string           `json:"tool"`
	ToolVersion string           `json:"tool_version"`
	Fingerprint string           `json:"fingerprint"`
	Properties  map[string]string `json:"properties,omitempty"`
	Source      string           `json:"source"` // the SARIF report source
	Timestamp   time.Time        `json:"timestamp"` // when the finding was reported
}

// ParseReport parses a SARIF 2.1.0 JSON report.
func ParseReport(data []byte) (*Report, error) {
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parse sarif report: %w", err)
	}

	if report.Version != "2.1.0" {
		return nil, fmt.Errorf("unsupported sarif version: %s (expected 2.1.0)", report.Version)
	}

	return &report, nil
}

// ParseReportFile reads and parses a SARIF report from a file path.
func ParseReportFile(path string) (*Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read sarif file %s: %w", path, err)
	}
	return ParseReport(data)
}

// MapResults extracts all findings from a SARIF report.
func MapResults(report *Report, source string) []Finding {
	var findings []Finding

	for _, run := range report.Runs {
		toolName := run.Tool.Driver.Name
		toolVersion := run.Tool.Driver.Version

		// Use run start time, or end time if start is not available
		runTime := time.Time{}
		if run.Timestamps != nil {
			if !run.Timestamps.StartTime.IsZero() {
				runTime = run.Timestamps.StartTime
			} else if !run.Timestamps.EndTime.IsZero() {
				runTime = run.Timestamps.EndTime
			}
		}

		for _, result := range run.Results {
			finding := mapResult(result, toolName, toolVersion, source, runTime)
			findings = append(findings, finding)
		}
	}

	return findings
}

// mapResult converts a single SARIF result to a normalized Finding.
func mapResult(result Result, toolName, toolVersion, source string, runTime time.Time) Finding {
	finding := Finding{
		RuleID:      result.RuleID,
		Normalized:  NormalizeRuleID(result.RuleID),
		Level:       result.Level,
		Severity:    MapSeverity(result.Level),
		Message:     result.Message.Text,
		Tool:        toolName,
		ToolVersion: toolVersion,
		Source:      source,
		Timestamp:   runTime,
	}

	// Extract file location
	if len(result.Locations) > 0 {
		loc := result.Locations[0]
		if loc.PhysicalLocation != nil {
			finding.File = loc.PhysicalLocation.ArtifactLocation.URI
			finding.Line = loc.PhysicalLocation.Region.StartLine
			finding.Column = loc.PhysicalLocation.Region.StartColumn
			finding.EndLine = loc.PhysicalLocation.Region.EndLine
			finding.EndColumn = loc.PhysicalLocation.Region.EndColumn
		}
	}

	// Extract fingerprints
	if len(result.Fingerprints) > 0 {
		for k, v := range result.Fingerprints {
			if strings.Contains(k, "sha256") || strings.Contains(k, "vulnerability") {
				finding.Fingerprint = v
				break
			}
		}
	}

	// Generate fingerprint if not provided by SARIF
	if finding.Fingerprint == "" {
		finding.Fingerprint = GenerateFingerprint(finding.RuleID, finding.File, finding.Line)
	}

	return finding
}

// GenerateFingerprint creates a SHA256 fingerprint for deduplication.
func GenerateFingerprint(ruleID, filePath string, line int) string {
	h := sha256.New()
	h.Write([]byte(ruleID))
	h.Write([]byte{0})
	h.Write([]byte(filePath))
	h.Write([]byte{0})
	h.Write([]byte(fmt.Sprintf("%d", line)))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ─── Summary Statistics ───────────────────────────────────────────────

// ReportSummary holds aggregate statistics for a SARIF report.
type ReportSummary struct {
	TotalFindings int                `json:"total_findings"`
	BySeverity    map[Severity]int   `json:"by_severity"`
	ByTool        map[string]int     `json:"by_tool"`
	ByPrefix      map[RulePrefix]int `json:"by_prefix"`
	FilesAffected int                `json:"files_affected"`
	TopRules      []RuleCount        `json:"top_rules"`
}

type RuleCount struct {
	RuleID string `json:"rule_id"`
	Count  int    `json:"count"`
}

// SummarizeReport computes aggregate statistics for a set of findings.
func SummarizeReport(findings []Finding) ReportSummary {
	summary := ReportSummary{
		TotalFindings: len(findings),
		BySeverity:    make(map[Severity]int),
		ByTool:        make(map[string]int),
		ByPrefix:      make(map[RulePrefix]int),
	}

	files := make(map[string]bool)
	ruleCounts := make(map[string]int)

	for _, f := range findings {
		summary.BySeverity[f.Severity]++
		summary.ByTool[f.Tool]++
		summary.ByPrefix[f.Normalized.Prefix]++

		if f.File != "" {
			files[f.File] = true
		}

		ruleCounts[f.RuleID]++
	}

	summary.FilesAffected = len(files)

	// Find top rules by count (simple sort)
	type kv struct {
		key   string
		value int
	}
	var sorted []kv
	for k, v := range ruleCounts {
		sorted = append(sorted, kv{k, v})
	}
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].value > sorted[i].value {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	limit := 10
	if len(sorted) < limit {
		limit = len(sorted)
	}
	for i := 0; i < limit; i++ {
		summary.TopRules = append(summary.TopRules, RuleCount{
			RuleID: sorted[i].key,
			Count:  sorted[i].value,
		})
	}

	return summary
}
