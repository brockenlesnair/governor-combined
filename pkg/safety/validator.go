package safety

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"sync"
	"time"
)

// SafetyValidator is the main validation engine.
type SafetyValidator struct {
	config    *Config
	policies  map[string]*Policy
	mu        sync.RWMutex
	auditLog  *AuditLog
	obfuscDetector *ObfuscationDetector
	riskScorer *RiskScorer
}

// NewValidator creates a new safety validator.
func NewValidator(cfg *Config) (*SafetyValidator, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	v := &SafetyValidator{
		config:        cfg,
		policies:      make(map[string]*Policy),
		obfuscDetector: NewObfuscationDetector(),
		riskScorer:    NewRiskScorer(),
	}

	// Load built-in policies
	for _, policy := range builtinPolicies {
		v.policies[policy.Name] = policy
	}

	// Load custom policies from directory
	if err := v.loadPoliciesFromDir(cfg.PolicyDir); err != nil {
		return nil, fmt.Errorf("load policies: %w", err)
	}

	// Add custom rules
	for _, rule := range cfg.CustomRules {
		v.AddRule(rule)
	}

	// Initialize audit log
	if cfg.EnableAuditLog {
		auditLog, err := NewAuditLog(cfg.AuditLogPath)
		if err != nil {
			return nil, fmt.Errorf("init audit log: %w", err)
		}
		v.auditLog = auditLog
	}

	return v, nil
}

// Validate validates a request.
func (v *SafetyValidator) Validate(ctx context.Context, req *ValidationRequest) (*ValidationResult, error) {
	start := time.Now()

	if req.Policy == "" {
		req.Policy = v.config.DefaultPolicy
	}

	if req.Action == "" {
		req.Action = "check"
	}

	// Apply timeout
	ctx, cancel := context.WithTimeout(ctx, v.config.Timeout)
	defer cancel()

	// Get policy
	policy, err := v.LoadPolicy(req.Policy)
	if err != nil {
		return nil, err
	}

	result := &ValidationResult{
		Findings: make([]Finding, 0),
	}

	// Parse files from request
	files := req.Files
	if len(files) == 0 && req.Diff != "" {
		// Parse from diff
		parsedFiles, err := ParseDiff(req.Diff)
		if err != nil {
			return nil, NewDiffParseFailedError(err)
		}
		files = parsedFiles
	}

	// Validate each file
	for _, file := range files {
		select {
		case <-ctx.Done():
			return nil, &SafetyError{Code: ErrCodeTimeout, Message: "validation timeout"}
		default:
		}

		if int64(len(file.Content)) > v.config.MaxFileSize {
			continue
		}

		findings := v.validateFile(ctx, file, policy, req.Context)
		result.Findings = append(result.Findings, findings...)
		result.CheckedFiles++
	}

	// Compute aggregate risk score
	result.RiskScore = v.riskScorer.ComputeAggregateRisk(result.Findings, req.Context)
	result.RiskLevel = v.riskScorer.GetRiskLevel(result.RiskScore)

	// Determine if validation passed
	result.Passed = !v.shouldBlock(result.Findings, policy)
	result.Blocked = !result.Passed
	if result.Blocked {
		result.Reason = v.getBlockReason(result.Findings, policy)
	}

	result.Duration = time.Since(start).String()

	// Write to audit log
	if v.auditLog != nil {
		if err := v.auditLog.Log(req, result); err != nil {
			// Don't fail validation on audit log error
			if v.config.CustomRules != nil {
				fmt.Printf("audit log write failed: %v\n", err)
			}
		}
	}

	return result, nil
}

// ValidateDiff validates a unified diff.
func (v *SafetyValidator) ValidateDiff(ctx context.Context, diff string, policyName string) (*ValidationResult, error) {
	return v.Validate(ctx, &ValidationRequest{
		Diff:   diff,
		Policy: policyName,
		Action: "check",
	})
}

// ValidateFile validates a single file.
func (v *SafetyValidator) ValidateFile(ctx context.Context, file FileInput, policyName string) (*ValidationResult, error) {
	return v.Validate(ctx, &ValidationRequest{
		Files:  []FileInput{file},
		Policy: policyName,
		Action: "check",
	})
}

// validateFile validates a single file against the policy.
func (v *SafetyValidator) validateFile(ctx context.Context, file FileInput, policy *Policy, changeCtx *ChangeContext) []Finding {
	var findings []Finding

	// Parse based on language
	if file.Language == "" {
		file.Language = detectLanguage(file.Path)
	}

	if file.Language != "go" {
		// Only Go is supported in v1
		return findings
	}

	// Parse Go AST
	fset := token.NewFileSet()
	fileAst, err := parser.ParseFile(fset, file.Path, file.Content, parser.ParseComments)
	if err != nil {
		// Log error but continue
		return findings
	}

	// Apply each rule
	for _, rule := range policy.Rules {
		if !rule.Enabled {
			continue
		}

		// Check language match
		if len(rule.Languages) > 0 && !contains(rule.Languages, file.Language) {
			continue
		}

		ruleFindings := v.applyRule(ctx, fileAst, fset, file, rule, changeCtx)
		findings = append(findings, ruleFindings...)
	}

	// Obfuscation detection
	if v.config.EnableObfuscationDetection {
		obfuscFindings := v.obfuscDetector.Detect(fileAst, fset, file)
		findings = append(findings, obfuscFindings...)
	}

	return findings
}

// applyRule applies a single rule to a file.
func (v *SafetyValidator) applyRule(ctx context.Context, fileAst *ast.File, fset *token.FileSet, file FileInput, rule *Rule, changeCtx *ChangeContext) []Finding {
	var findings []Finding

	// Walk AST and check for pattern matches
	pattern, err := CompilePattern(rule.Pattern)
	if err != nil {
		return findings
	}

	matches := MatchPattern(pattern, fileAst, fset)
	for _, match := range matches {
		// Check for suppression comment
		suggestion := ""
		if rule.Metadata != nil {
			if s, ok := rule.Metadata["suggestion"].(string); ok {
				suggestion = s
			}
		}

		if v.isSuppressed(file.Content, match.Line, rule.ID) {
			finding := Finding{
				ID:         fmt.Sprintf("finding-%s-%d", rule.ID, match.Line),
				RuleID:     rule.ID,
				Severity:   rule.Severity,
				Action:     rule.Action,
				File:       file.Path,
				Line:       match.Line,
				Column:     match.Column,
				Message:    rule.Description,
				Snippet:    match.Snippet,
				RiskScore:  v.riskScorer.ComputeRuleRisk(rule, changeCtx),
				Suggestion: suggestion,
				Suppressed: true,
			}
			findings = append(findings, finding)
			continue
		}

		finding := Finding{
			ID:         fmt.Sprintf("finding-%s-%d", rule.ID, match.Line),
			RuleID:     rule.ID,
			Severity:   rule.Severity,
			Action:     rule.Action,
			File:       file.Path,
			Line:       match.Line,
			Column:     match.Column,
			Message:    rule.Description,
			Snippet:    match.Snippet,
			RiskScore:  v.riskScorer.ComputeRuleRisk(rule, changeCtx),
			Suggestion: suggestion,
		}
		findings = append(findings, finding)
	}

	return findings
}

// isSuppressed checks if a rule is suppressed by a comment.
func (v *SafetyValidator) isSuppressed(content string, line int, ruleID string) bool {
	lines := splitLines(content)
	if line <= 0 || line > len(lines) {
		return false
	}

	// Check current line and previous lines
	for i := line - 1; i >= max(0, line-5); i-- {
		if i >= len(lines) {
			continue
		}
		lineContent := lines[i]
		if strings.Contains(lineContent, "governor:allow") {
			if ruleID == "" || strings.Contains(lineContent, ruleID) {
				return true
			}
		}
	}

	return false
}

// shouldBlock determines if the validation should block based on findings and policy.
func (v *SafetyValidator) shouldBlock(findings []Finding, policy *Policy) bool {
	for _, finding := range findings {
		if finding.Suppressed {
			continue
		}
		if finding.Action == ActionBlock {
			return true
		}
		if policy.DefaultAction == ActionBlock && finding.Action != ActionAllow {
			return true
		}
	}
	return false
}

// getBlockReason returns a human-readable reason for blocking.
func (v *SafetyValidator) getBlockReason(findings []Finding, policy *Policy) string {
	var reasons []string
	for _, finding := range findings {
		if finding.Suppressed {
			continue
		}
		if finding.Action == ActionBlock {
			reasons = append(reasons, fmt.Sprintf("%s: %s", finding.RuleID, finding.Message))
		}
	}
	if len(reasons) == 0 {
		return "policy violation"
	}
	return joinStrings(reasons, "; ")
}

// LoadPolicy loads a policy by name.
func (v *SafetyValidator) LoadPolicy(name string) (*Policy, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	policy, ok := v.policies[name]
	if !ok {
		return nil, NewPolicyNotFoundError(name)
	}

	// Return a copy to prevent mutation
	policyCopy := *policy
	policyCopy.Rules = make([]*Rule, len(policy.Rules))
	for i, rule := range policy.Rules {
		ruleCopy := *rule
		policyCopy.Rules[i] = &ruleCopy
	}
	return &policyCopy, nil
}

// ListPolicies returns all available policy names.
func (v *SafetyValidator) ListPolicies() []string {
	v.mu.RLock()
	defer v.mu.RUnlock()

	names := make([]string, 0, len(v.policies))
	for name := range v.policies {
		names = append(names, name)
	}
	return names
}

// AddRule adds a custom rule to the default policy.
func (v *SafetyValidator) AddRule(rule *Rule) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	policy, ok := v.policies[v.config.DefaultPolicy]
	if !ok {
		return NewPolicyNotFoundError(v.config.DefaultPolicy)
	}

	policy.Rules = append(policy.Rules, rule)
	return nil
}

// SuppressRule adds a suppression for a rule.
func (v *SafetyValidator) SuppressRule(ruleID, reason string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	policy, ok := v.policies[v.config.DefaultPolicy]
	if !ok {
		return NewPolicyNotFoundError(v.config.DefaultPolicy)
	}

	for _, rule := range policy.Rules {
		if rule.ID == ruleID {
			rule.Enabled = false
			return nil
		}
	}

	return NewRuleNotFoundError(ruleID)
}

// GetAuditLog retrieves audit log entries.
func (v *SafetyValidator) GetAuditLog(ctx context.Context, filter AuditFilter) ([]AuditEntry, error) {
	if v.auditLog == nil {
		return nil, nil
	}
	return v.auditLog.GetEntries(filter)
}

// loadPoliciesFromDir loads policies from the configured directory.
func (v *SafetyValidator) loadPoliciesFromDir(dir string) error {
	// In a full implementation, this would scan the directory for YAML/JSON policy files
	// For now, just skip if directory doesn't exist
	return nil
}

// Close closes the validator and releases resources.
func (v *SafetyValidator) Close() error {
	if v.auditLog != nil {
		return v.auditLog.Close()
	}
	return nil
}

// Helper functions

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += sep + parts[i]
	}
	return result
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func detectLanguage(path string) string {
	if len(path) < 3 {
		return "unknown"
	}
	ext := path[len(path)-3:]
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	}
	return "unknown"
}