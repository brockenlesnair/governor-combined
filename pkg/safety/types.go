// Package safety performs security and safety analysis on codebases.
//
// It detects hardcoded secrets, unsafe patterns, and compliance violations,
// producing findings with severity levels (low, medium, high, critical) and
// confidence scores for prioritized remediation.
package safety

import (
	"time"
)

// Severity classifies the risk level of a finding.
type Severity int

const (
	SeverityLow      Severity = 1
	SeverityMedium   Severity = 2
	SeverityHigh     Severity = 3
	SeverityCritical Severity = 4
)

func (s Severity) String() string {
	switch s {
	case SeverityLow:
		return "low"
	case SeverityMedium:
		return "medium"
	case SeverityHigh:
		return "high"
	case SeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// ActionType represents the action to take on a finding.
type ActionType string

const (
	ActionAllow ActionType = "allow"
	ActionWarn  ActionType = "warn"
	ActionBlock ActionType = "block"
)

// Finding is a single policy violation or risk detection.
type Finding struct {
	ID          string    `json:"id"`
	RuleID      string    `json:"rule_id"`
	Severity    Severity  `json:"severity"`
	Action      ActionType `json:"action"`
	File        string    `json:"file"`
	Line        int       `json:"line"`
	Column      int       `json:"column"`
	Message     string    `json:"message"`
	Snippet     string    `json:"snippet"`
	RiskScore   float64   `json:"risk_score"`
	Suggestion  string    `json:"suggestion,omitempty"`
	Suppressed  bool      `json:"suppressed"` // via // governor:allow
	Context     string    `json:"context,omitempty"`
}

// ValidationRequest is the input to the validator.
type ValidationRequest struct {
	Diff        string       `json:"diff"`         // unified diff or raw file content
	Files       []FileInput  `json:"files"`        // alternative: explicit file list
	Policy      string       `json:"policy"`       // "default", "strict", "permissive", or custom name
	Action      string       `json:"action"`       // "check", "simulate", "apply"
	Context     *ChangeContext `json:"context,omitempty"`
}

// FileInput represents a single file change.
type FileInput struct {
	Path       string `json:"path"`
	Content    string `json:"content"`     // new content
	OldContent string `json:"old_content"` // previous content (for diff)
	Language   string `json:"language"`    // "go", "python", etc.
}

// ChangeContext provides additional context for risk scoring.
type ChangeContext struct {
	IsTest       bool   `json:"is_test"`
	IsDeployment bool   `json:"is_deployment"`
	IsProduction bool   `json:"is_production"`
	PRNumber     int    `json:"pr_number,omitempty"`
	Author       string `json:"author,omitempty"`
}

// ValidationResult is the output of validation.
type ValidationResult struct {
	Passed       bool      `json:"passed"`
	RiskScore    float64   `json:"risk_score"`    // aggregate 0.0-10.0
	RiskLevel    string    `json:"risk_level"`    // "low", "medium", "high", "critical"
	Findings     []Finding `json:"findings"`
	Blocked      bool      `json:"blocked"`       // true if action should be blocked
	Reason       string    `json:"reason"`
	CheckedFiles int       `json:"checked_files"`
	Duration     string    `json:"duration"`
}

// Rule is a validation rule.
type Rule struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Severity    Severity `json:"severity"`
	Action      ActionType `json:"action"`
	Pattern     string   `json:"pattern,omitempty"`      // Semgrep-style pattern
	Languages   []string `json:"languages,omitempty"`    // applicable languages
	Tags        []string `json:"tags,omitempty"`         // rule tags
	Enabled     bool     `json:"enabled"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Policy is a collection of rules with a name.
type Policy struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Rules       []*Rule `json:"rules"`
	DefaultAction ActionType `json:"default_action"`
}

// AuditEntry represents an audit log entry.
type AuditEntry struct {
	ID        string                 `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	Request   *ValidationRequest     `json:"request"`
	Result    *ValidationResult      `json:"result"`
	Hash      string                 `json:"hash"`       // SHA256 of previous entry
	PrevHash  string                 `json:"prev_hash"`  // chain hash
}

// AuditFilter filters audit log entries.
type AuditFilter struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Policy    string    `json:"policy"`
	Blocked   *bool     `json:"blocked,omitempty"`
	Limit     int       `json:"limit"`
	Offset    int       `json:"offset"`
}

// Config controls safety validator behavior.
type Config struct {
	// PolicyDir is the directory containing policy files.
	PolicyDir string `json:"policy_dir" yaml:"policy_dir"`

	// DefaultPolicy is the policy to use when none specified.
	DefaultPolicy string `json:"default_policy" yaml:"default_policy"`

	// EnableAuditLog enables tamper-evident audit logging.
	EnableAuditLog bool `json:"enable_audit_log" yaml:"enable_audit_log"`

	// AuditLogPath is the path to the audit log file.
	AuditLogPath string `json:"audit_log_path" yaml:"audit_log_path"`

	// MaxFileSize is the maximum file size to analyze (bytes).
	MaxFileSize int64 `json:"max_file_size" yaml:"max_file_size"`

	// Timeout is the maximum time for validation.
	Timeout time.Duration `json:"timeout" yaml:"timeout"`

	// EnableObfuscationDetection enables obfuscation detection.
	EnableObfuscationDetection bool `json:"enable_obfuscation_detection" yaml:"enable_obfuscation_detection"`

	// CustomRules are additional rules loaded at startup.
	CustomRules []*Rule `json:"custom_rules" yaml:"custom_rules"`
}

// DefaultConfig returns a default configuration.
func DefaultConfig() *Config {
	return &Config{
		PolicyDir:                  "./policies",
		DefaultPolicy:              "default",
		EnableAuditLog:             true,
		AuditLogPath:               "./data/safety_audit.log",
		MaxFileSize:                1024 * 1024, // 1MB
		Timeout:                    30 * time.Second,
		EnableObfuscationDetection: true,
	}
}

// MCPToolDef defines the MCP tool for the safety validator.
type MCPToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}