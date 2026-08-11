package docgov

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DocumentValidator is the interface for validating documents of a specific type.
type DocumentValidator interface {
	Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult
	Name() string
}

// ValidatorRegistry holds all document validators.
type ValidatorRegistry struct {
	validators map[string]DocumentValidator
}

// NewValidatorRegistry creates a new validator registry with built-in validators.
func NewValidatorRegistry() *ValidatorRegistry {
	vr := &ValidatorRegistry{
		validators: make(map[string]DocumentValidator),
	}
	vr.registerBuiltins()
	return vr
}

func (vr *ValidatorRegistry) registerBuiltins() {
	validators := []DocumentValidator{
		NewADRValidator(),
		NewOpenAPIValidator(),
		NewC4ContextValidator(),
		NewC4ContainerValidator(),
		NewC4ComponentValidator(),
		NewRunbookValidator(),
		NewRunbookOperationalValidator(),
		NewDisasterRecoveryValidator(),
		NewIncidentPlaybookValidator(),
		NewSTRIDEValidator(),
		NewDataDictionaryValidator(),
		NewSLOValidator(),
		NewPostmortemValidator(),
		NewChangelogValidator(),
		NewAPIChangelogValidator(),
		NewMigrationGuideValidator(),
		NewReleaseNotesValidator(),
		NewCodeGuideValidator(),
	}

	for _, v := range validators {
		vr.validators[v.Name()] = v
	}
}

// Get returns a validator by name.
func (vr *ValidatorRegistry) Get(name string) (DocumentValidator, bool) {
	v, ok := vr.validators[name]
	return v, ok
}

// Validate validates document content using the appropriate validator.
func (vr *ValidatorRegistry) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	validator, ok := vr.Get(config.Validator)
	if !ok {
		return ValidationResult{
			Valid:     false,
			Errors:    []string{fmt.Sprintf("unknown validator: %s", config.Validator)},
			CheckedAt: time.Now(),
		}
	}
	result := validator.Validate(ctx, content, config)
	result.Validator = config.Validator
	result.CheckedAt = time.Now()
	return result
}

// Frontmatter parses YAML frontmatter from markdown content.
func ParseFrontmatter(content []byte) (map[string]any, []byte, error) {
	str := strings.TrimPrefix(string(content), "\ufeff")
	lines := strings.Split(str, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		start = i
		break
	}

	if start == -1 || strings.TrimSpace(lines[start]) != "---" {
		return nil, content, fmt.Errorf("no frontmatter found")
	}

	end := -1
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return nil, content, fmt.Errorf("invalid frontmatter format")
	}

	frontmatter := strings.Join(lines[start+1:end], "\n")
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
		return nil, content, fmt.Errorf("parse frontmatter: %w", err)
	}

	body := []byte(strings.TrimSpace(strings.Join(lines[end+1:], "\n")))
	return fm, body, nil
}

// getString gets a string value from frontmatter.
func getString(fm map[string]any, key string) (string, bool) {
	if v, ok := fm[key]; ok {
		if s, ok := v.(string); ok {
			return s, true
		}
	}
	return "", false
}

// getStringSlice gets a string slice from frontmatter.
func getStringSlice(fm map[string]any, key string) ([]string, bool) {
	if v, ok := fm[key]; ok {
		if slice, ok := v.([]any); ok {
			result := make([]string, 0, len(slice))
			for _, item := range slice {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result, true
		}
	}
	return nil, false
}

// hasRequiredFields checks if all required fields are present in frontmatter.
func hasRequiredFields(fm map[string]any, required []string) ([]string, []string) {
	var missing []string
	var present []string
	for _, field := range required {
		if _, ok := fm[field]; ok {
			present = append(present, field)
		} else {
			missing = append(missing, field)
		}
	}
	return present, missing
}

// BaseValidator provides common validation logic.
type BaseValidator struct {
	name string
}

func (bv *BaseValidator) Name() string {
	return bv.name
}

func (bv *BaseValidator) checkFrontmatter(content []byte, requiredFields []string) (map[string]any, []string, []string, error) {
	fm, _, err := ParseFrontmatter(content)
	if err != nil {
		return nil, nil, []string{err.Error()}, err
	}
	present, missing := hasRequiredFields(fm, requiredFields)
	return fm, present, missing, nil
}

// ADRValidator validates Architecture Decision Records (MADR format).
type ADRValidator struct {
	BaseValidator
}

func NewADRValidator() *ADRValidator {
	return &ADRValidator{BaseValidator{name: "madr"}}
}

func (v *ADRValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	fm, _, missing, err := v.checkFrontmatter(content, config.RequiredFields)
	if err != nil {
		errors = append(errors, fmt.Sprintf("frontmatter parse error: %v", err))
		score -= 0.3
	}

	if len(missing) > 0 {
		errors = append(errors, fmt.Sprintf("missing required fields: %v", missing))
		score -= 0.2 * float64(len(missing))
	}

	// Check status value
	if status, ok := getString(fm, "status"); ok {
		validStatuses := []string{"proposed", "accepted", "rejected", "deprecated", "superseded"}
		valid := false
		for _, vs := range validStatuses {
			if strings.EqualFold(status, vs) {
				valid = true
				break
			}
		}
		if !valid {
			warnings = append(warnings, fmt.Sprintf("unusual status value: %s (expected: proposed/accepted/rejected/deprecated/superseded)", status))
			score -= 0.1
		}
	}

	// Check for context and decision sections in body
	body := string(content)
	if !strings.Contains(strings.ToLower(body), "context") {
		warnings = append(warnings, "missing 'Context' section in body")
		score -= 0.1
	}
	if !strings.Contains(strings.ToLower(body), "decision") {
		warnings = append(warnings, "missing 'Decision' section in body")
		score -= 0.1
	}
	if !strings.Contains(strings.ToLower(body), "consequence") {
		warnings = append(warnings, "missing 'Consequences' section in body")
		score -= 0.1
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// OpenAPIValidator validates OpenAPI specifications.
type OpenAPIValidator struct {
	BaseValidator
}

func NewOpenAPIValidator() *OpenAPIValidator {
	return &OpenAPIValidator{BaseValidator{name: "openapi"}}
}

func (v *OpenAPIValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	// Try to parse as YAML/JSON
	var spec map[string]any
	if err := yaml.Unmarshal(content, &spec); err != nil {
		errors = append(errors, fmt.Sprintf("invalid YAML/JSON: %v", err))
		score = 0
		return ValidationResult{
			Valid:    false,
			Errors:   errors,
			Warnings: warnings,
			Score:    score,
		}
	}

	// Check required top-level fields
	required := []string{"openapi", "info", "paths"}
	for _, field := range required {
		if _, ok := spec[field]; !ok {
			errors = append(errors, fmt.Sprintf("missing required field: %s", field))
			score -= 0.2
		}
	}

	// Check openapi version
	if version, ok := spec["openapi"].(string); ok {
		if !strings.HasPrefix(version, "3.") {
			warnings = append(warnings, fmt.Sprintf("OpenAPI version %s is not 3.x", version))
			score -= 0.1
		}
	}

	// Check info object
	if info, ok := spec["info"].(map[string]any); ok {
		if _, ok := info["title"]; !ok {
			warnings = append(warnings, "info.title is missing")
			score -= 0.1
		}
		if _, ok := info["version"]; !ok {
			warnings = append(warnings, "info.version is missing")
			score -= 0.1
		}
	}

	// Check paths
	if paths, ok := spec["paths"].(map[string]any); ok {
		if len(paths) == 0 {
			warnings = append(warnings, "no API paths defined")
			score -= 0.1
		}
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// C4ContextValidator validates C4 Context diagrams.
type C4ContextValidator struct {
	BaseValidator
}

func NewC4ContextValidator() *C4ContextValidator {
	return &C4ContextValidator{BaseValidator{name: "c4_context"}}
}

func (v *C4ContextValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	_, _, missing, err := v.checkFrontmatter(content, config.RequiredFields)
	if err != nil {
		errors = append(errors, fmt.Sprintf("frontmatter parse error: %v", err))
		score -= 0.3
	}

	if len(missing) > 0 {
		errors = append(errors, fmt.Sprintf("missing required fields: %v", missing))
		score -= 0.2 * float64(len(missing))
	}

	body := strings.ToLower(string(content))
	// Check for key C4 context elements
	contextKeywords := []string{"system", "external", "user", "person", "actor"}
	found := 0
	for _, kw := range contextKeywords {
		if strings.Contains(body, kw) {
			found++
		}
	}
	if found < 2 {
		warnings = append(warnings, "C4 Context should describe system boundaries and external actors")
		score -= 0.15
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// C4ContainerValidator validates C4 Container diagrams.
type C4ContainerValidator struct {
	BaseValidator
}

func NewC4ContainerValidator() *C4ContainerValidator {
	return &C4ContainerValidator{BaseValidator{name: "c4_container"}}
}

func (v *C4ContainerValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	_, _, missing, err := v.checkFrontmatter(content, config.RequiredFields)
	if err != nil {
		errors = append(errors, fmt.Sprintf("frontmatter parse error: %v", err))
		score -= 0.3
	}

	if len(missing) > 0 {
		errors = append(errors, fmt.Sprintf("missing required fields: %v", missing))
		score -= 0.2 * float64(len(missing))
	}

	body := strings.ToLower(string(content))
	// Check for container types
	containerKeywords := []string{"container", "service", "database", "queue", "api", "frontend", "backend"}
	found := 0
	for _, kw := range containerKeywords {
		if strings.Contains(body, kw) {
			found++
		}
	}
	if found < 2 {
		warnings = append(warnings, "C4 Container should describe services, databases, and other containers")
		score -= 0.15
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// C4ComponentValidator validates C4 Component diagrams.
type C4ComponentValidator struct {
	BaseValidator
}

func NewC4ComponentValidator() *C4ComponentValidator {
	return &C4ComponentValidator{BaseValidator{name: "c4_component"}}
}

func (v *C4ComponentValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	_, _, missing, err := v.checkFrontmatter(content, config.RequiredFields)
	if err != nil {
		errors = append(errors, fmt.Sprintf("frontmatter parse error: %v", err))
		score -= 0.3
	}

	if len(missing) > 0 {
		errors = append(errors, fmt.Sprintf("missing required fields: %v", missing))
		score -= 0.2 * float64(len(missing))
	}

	body := strings.ToLower(string(content))
	componentKeywords := []string{"component", "responsibility", "interface", "dependency", "module", "service"}
	found := 0
	for _, kw := range componentKeywords {
		if strings.Contains(body, kw) {
			found++
		}
	}
	if found < 2 {
		warnings = append(warnings, "C4 Component should describe component responsibilities, interfaces, and dependencies")
		score -= 0.15
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// RunbookValidator validates operational runbooks.
type RunbookValidator struct {
	BaseValidator
}

func NewRunbookValidator() *RunbookValidator {
	return &RunbookValidator{BaseValidator{name: "runbook"}}
}

func (v *RunbookValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	fm, _, missing, err := v.checkFrontmatter(content, config.RequiredFields)
	if err != nil {
		errors = append(errors, fmt.Sprintf("frontmatter parse error: %v", err))
		score -= 0.3
	}

	if len(missing) > 0 {
		errors = append(errors, fmt.Sprintf("missing required fields: %v", missing))
		score -= 0.2 * float64(len(missing))
	}

	body := strings.ToLower(string(content))
	// Check for runbook sections
	sections := []string{"prerequisite", "step", "verification", "rollback", "reference"}
	found := 0
	for _, s := range sections {
		if strings.Contains(body, s) {
			found++
		}
	}
	if found < 3 {
		warnings = append(warnings, "runbook should include prerequisites, steps, verification, and rollback sections")
		score -= 0.15
	}

	// Check severity
	if severity, ok := getString(fm, "severity"); ok {
		valid := []string{"sev-0", "sev-1", "sev-2", "sev-3", "0", "1", "2", "3"}
		validSeverity := false
		for _, v := range valid {
			if strings.EqualFold(severity, v) {
				validSeverity = true
				break
			}
		}
		if !validSeverity {
			warnings = append(warnings, fmt.Sprintf("unusual severity: %s (expected SEV-0 through SEV-3)", severity))
			score -= 0.1
		}
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// RunbookOperationalValidator validates operational runbooks.
type RunbookOperationalValidator struct {
	BaseValidator
}

func NewRunbookOperationalValidator() *RunbookOperationalValidator {
	return &RunbookOperationalValidator{BaseValidator{name: "runbook_operational"}}
}

func (v *RunbookOperationalValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	fm, _, missing, err := v.checkFrontmatter(content, config.RequiredFields)
	if err != nil {
		errors = append(errors, fmt.Sprintf("frontmatter parse error: %v", err))
		score -= 0.3
	}

	if len(missing) > 0 {
		errors = append(errors, fmt.Sprintf("missing required fields: %v", missing))
		score -= 0.2 * float64(len(missing))
	}

	body := strings.ToLower(string(content))
	sections := []string{"steps", "monitoring", "checks", "alerts", "rollback", "verification"}
	found := 0
	for _, s := range sections {
		if strings.Contains(body, s) {
			found++
		}
	}
	if found < 3 {
		warnings = append(warnings, "operational runbook should include steps, monitoring, checks, verification, and rollback")
		score -= 0.15
	}

	if severity, ok := getString(fm, "severity"); ok {
		valid := []string{"sev-0", "sev-1", "sev-2", "sev-3", "0", "1", "2", "3"}
		validSeverity := false
		for _, v := range valid {
			if strings.EqualFold(severity, v) {
				validSeverity = true
				break
			}
		}
		if !validSeverity {
			warnings = append(warnings, fmt.Sprintf("unusual severity: %s (expected SEV-0 through SEV-3)", severity))
			score -= 0.1
		}
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// DisasterRecoveryValidator validates disaster recovery plans.
type DisasterRecoveryValidator struct {
	BaseValidator
}

func NewDisasterRecoveryValidator() *DisasterRecoveryValidator {
	return &DisasterRecoveryValidator{BaseValidator{name: "disaster_recovery"}}
}

func (v *DisasterRecoveryValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	_, _, missing, err := v.checkFrontmatter(content, config.RequiredFields)
	if err != nil {
		errors = append(errors, fmt.Sprintf("frontmatter parse error: %v", err))
		score -= 0.3
	}

	if len(missing) > 0 {
		errors = append(errors, fmt.Sprintf("missing required fields: %v", missing))
		score -= 0.2 * float64(len(missing))
	}

	body := strings.ToLower(string(content))
	keywords := []string{"recovery", "restore", "backup", "failover", "replication", "rto", "rpo"}
	found := 0
	for _, kw := range keywords {
		if strings.Contains(body, kw) {
			found++
		}
	}
	if found < 3 {
		warnings = append(warnings, "disaster recovery plan should cover recovery, restore, backup, failover, RTO, and RPO")
		score -= 0.15
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// IncidentPlaybookValidator validates incident response playbooks.
type IncidentPlaybookValidator struct {
	BaseValidator
}

func NewIncidentPlaybookValidator() *IncidentPlaybookValidator {
	return &IncidentPlaybookValidator{BaseValidator{name: "incident_playbook"}}
}

func (v *IncidentPlaybookValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	_, _, missing, err := v.checkFrontmatter(content, config.RequiredFields)
	if err != nil {
		errors = append(errors, fmt.Sprintf("frontmatter parse error: %v", err))
		score -= 0.3
	}

	if len(missing) > 0 {
		errors = append(errors, fmt.Sprintf("missing required fields: %v", missing))
		score -= 0.2 * float64(len(missing))
	}

	body := strings.ToLower(string(content))
	sections := []string{"triage", "role", "escalation", "communication", "diagnosis", "mitigation"}
	found := 0
	for _, s := range sections {
		if strings.Contains(body, s) {
			found++
		}
	}
	if found < 4 {
		warnings = append(warnings, "incident playbook should cover triage, roles, escalation, communication, diagnosis, mitigation")
		score -= 0.15
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// STRIDEValidator validates STRIDE threat models.
type STRIDEValidator struct {
	BaseValidator
}

func NewSTRIDEValidator() *STRIDEValidator {
	return &STRIDEValidator{BaseValidator{name: "stride"}}
}

func (v *STRIDEValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	_, _, missing, err := v.checkFrontmatter(content, config.RequiredFields)
	if err != nil {
		errors = append(errors, fmt.Sprintf("frontmatter parse error: %v", err))
		score -= 0.3
	}

	if len(missing) > 0 {
		errors = append(errors, fmt.Sprintf("missing required fields: %v", missing))
		score -= 0.2 * float64(len(missing))
	}

	body := strings.ToLower(string(content))
	// Check for STRIDE categories
	stride := []string{"spoofing", "tampering", "repudiation", "information disclosure", "denial of service", "elevation of privilege"}
	found := 0
	for _, s := range stride {
		if strings.Contains(body, s) {
			found++
		}
	}
	if found < 4 {
		warnings = append(warnings, "threat model should cover all 6 STRIDE categories")
		score -= 0.15
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// DataDictionaryValidator validates data dictionaries.
type DataDictionaryValidator struct {
	BaseValidator
}

func NewDataDictionaryValidator() *DataDictionaryValidator {
	return &DataDictionaryValidator{BaseValidator{name: "data_dictionary"}}
}

func (v *DataDictionaryValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	_, _, missing, err := v.checkFrontmatter(content, config.RequiredFields)
	if err != nil {
		errors = append(errors, fmt.Sprintf("frontmatter parse error: %v", err))
		score -= 0.3
	}

	if len(missing) > 0 {
		errors = append(errors, fmt.Sprintf("missing required fields: %v", missing))
		score -= 0.2 * float64(len(missing))
	}

	body := strings.ToLower(string(content))
	if !strings.Contains(body, "table") && !strings.Contains(body, "field") && !strings.Contains(body, "column") {
		warnings = append(warnings, "data dictionary should document tables/fields/columns")
		score -= 0.15
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// SLOValidator validates SLO documents.
type SLOValidator struct {
	BaseValidator
}

func NewSLOValidator() *SLOValidator {
	return &SLOValidator{BaseValidator{name: "slo"}}
}

func (v *SLOValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	_, _, missing, err := v.checkFrontmatter(content, config.RequiredFields)
	if err != nil {
		errors = append(errors, fmt.Sprintf("frontmatter parse error: %v", err))
		score -= 0.3
	}

	if len(missing) > 0 {
		errors = append(errors, fmt.Sprintf("missing required fields: %v", missing))
		score -= 0.2 * float64(len(missing))
	}

	body := strings.ToLower(string(content))
	requiredSections := []string{"sli", "slo", "error budget", "burn rate"}
	found := 0
	for _, s := range requiredSections {
		if strings.Contains(body, s) {
			found++
		}
	}
	if found < 3 {
		warnings = append(warnings, "SLO document should define SLIs, SLO targets, error budgets, and burn rates")
		score -= 0.15
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// PostmortemValidator validates postmortems.
type PostmortemValidator struct {
	BaseValidator
}

func NewPostmortemValidator() *PostmortemValidator {
	return &PostmortemValidator{BaseValidator{name: "postmortem"}}
}

func (v *PostmortemValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	_, _, missing, err := v.checkFrontmatter(content, config.RequiredFields)
	if err != nil {
		errors = append(errors, fmt.Sprintf("frontmatter parse error: %v", err))
		score -= 0.3
	}

	if len(missing) > 0 {
		errors = append(errors, fmt.Sprintf("missing required fields: %v", missing))
		score -= 0.2 * float64(len(missing))
	}

	body := strings.ToLower(string(content))
	sections := []string{"summary", "timeline", "root cause", "action item", "lesson"}
	found := 0
	for _, s := range sections {
		if strings.Contains(body, s) {
			found++
		}
	}
	if found < 4 {
		warnings = append(warnings, "postmortem should include summary, timeline, root cause, action items, lessons learned")
		score -= 0.15
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// ChangelogValidator validates changelogs.
type ChangelogValidator struct {
	BaseValidator
}

func NewChangelogValidator() *ChangelogValidator {
	return &ChangelogValidator{BaseValidator{name: "changelog"}}
}

func (v *ChangelogValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	body := strings.ToLower(string(content))
	// Check for Keep a Changelog sections
	sections := []string{"added", "changed", "deprecated", "removed", "fixed", "security"}
	found := 0
	for _, s := range sections {
		if strings.Contains(body, s) {
			found++
		}
	}
	if found == 0 {
		warnings = append(warnings, "changelog should follow Keep a Changelog format (Added, Changed, Deprecated, Removed, Fixed, Security)")
		score -= 0.2
	}

	// Check for version headers
	if !strings.Contains(body, "## [") && !strings.Contains(body, "### [") {
		warnings = append(warnings, "changelog should have version headers (e.g., ## [1.0.0] - 2024-01-01)")
		score -= 0.15
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// APIChangelogValidator validates API changelogs.
type APIChangelogValidator struct {
	BaseValidator
}

func NewAPIChangelogValidator() *APIChangelogValidator {
	return &APIChangelogValidator{BaseValidator{name: "api_changelog"}}
}

func (v *APIChangelogValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	_, _, missing, err := v.checkFrontmatter(content, config.RequiredFields)
	if err != nil {
		errors = append(errors, fmt.Sprintf("frontmatter parse error: %v", err))
		score -= 0.3
	}

	if len(missing) > 0 {
		errors = append(errors, fmt.Sprintf("missing required fields: %v", missing))
		score -= 0.2 * float64(len(missing))
	}

	body := strings.ToLower(string(content))
	sections := []string{"added", "changed", "deprecated", "removed", "fixed", "breaking"}
	found := 0
	for _, s := range sections {
		if strings.Contains(body, s) {
			found++
		}
	}
	if found < 2 {
		warnings = append(warnings, "API changelog should describe added, changed, deprecated, removed, fixed, and breaking changes")
		score -= 0.15
	}

	if !strings.Contains(body, "## [") && !strings.Contains(body, "### [") {
		warnings = append(warnings, "API changelog should include version headers")
		score -= 0.1
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// MigrationGuideValidator validates migration guides.
type MigrationGuideValidator struct {
	BaseValidator
}

func NewMigrationGuideValidator() *MigrationGuideValidator {
	return &MigrationGuideValidator{BaseValidator{name: "migration_guide"}}
}

func (v *MigrationGuideValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	_, _, missing, err := v.checkFrontmatter(content, config.RequiredFields)
	if err != nil {
		errors = append(errors, fmt.Sprintf("frontmatter parse error: %v", err))
		score -= 0.3
	}

	if len(missing) > 0 {
		errors = append(errors, fmt.Sprintf("missing required fields: %v", missing))
		score -= 0.2 * float64(len(missing))
	}

	body := strings.ToLower(string(content))
	sections := []string{"prerequisite", "upgrade", "migration", "rollback", "compatibility", "verification", "steps"}
	found := 0
	for _, s := range sections {
		if strings.Contains(body, s) {
			found++
		}
	}
	if found < 3 {
		warnings = append(warnings, "migration guide should include prerequisites, upgrade steps, verification, rollback, and compatibility notes")
		score -= 0.15
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// ReleaseNotesValidator validates release notes.
type ReleaseNotesValidator struct {
	BaseValidator
}

func NewReleaseNotesValidator() *ReleaseNotesValidator {
	return &ReleaseNotesValidator{BaseValidator{name: "release_notes"}}
}

func (v *ReleaseNotesValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	body := strings.ToLower(string(content))
	sections := []string{"highlight", "breaking change", "migration", "deprecat", "bug fix", "new feature"}
	found := 0
	for _, s := range sections {
		if strings.Contains(body, s) {
			found++
		}
	}
	if found < 3 {
		warnings = append(warnings, "release notes should include highlights, breaking changes, migration guide, deprecations, bug fixes")
		score -= 0.15
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}

// CodeGuideValidator validates code documentation guides.
type CodeGuideValidator struct {
	BaseValidator
}

func NewCodeGuideValidator() *CodeGuideValidator {
	return &CodeGuideValidator{BaseValidator{name: "code_guide"}}
}

func (v *CodeGuideValidator) Validate(ctx context.Context, content []byte, config DocumentTypeConfig) ValidationResult {
	var errors, warnings []string
	score := 1.0

	body := strings.ToLower(string(content))
	sections := []string{"godoc", "jsdoc", "rustdoc", "docstring", "comment", "example", "naming", "format"}
	found := 0
	for _, s := range sections {
		if strings.Contains(body, s) {
			found++
		}
	}
	if found < 3 {
		warnings = append(warnings, "code guide should cover documentation standards (GoDoc/JSDoc/RustDoc), naming, formatting, examples")
		score -= 0.15
	}

	if score < 0 {
		score = 0
	}

	return ValidationResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
		Score:    score,
	}
}
