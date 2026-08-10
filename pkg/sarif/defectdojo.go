package sarif

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// ─── DefectDojo API v2 Client ─────────────────────────────────────────

// DefectDojoConfig holds configuration for the DefectDojo API client.
type DefectDojoConfig struct {
	// BaseURL is the DefectDojo API base URL.
	BaseURL string `yaml:"base_url"`

	// APIKey is the bearer token for authentication.
	// Loaded from DEFECTDOJO_API_KEY env var if empty.
	APIKey string `yaml:"api_key"`

	// RequestTimeout is the timeout for individual API calls.
	RequestTimeout time.Duration `yaml:"request_timeout"`

	// JiraSync enables Jira synchronization for findings.
	JiraSync bool `yaml:"jira_sync"`

	// SLATracking enables SLA tracking for findings.
	SLATracking bool `yaml:"sla_tracking"`
}

func (c *DefectDojoConfig) applyDefaults() {
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 30 * time.Second
	}
}

// Validate checks the config for required fields.
func (c *DefectDojoConfig) Validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("defectdojo: base_url is required")
	}
	if c.APIKey == "" {
		c.APIKey = os.Getenv("DEFECTDOJO_API_KEY")
	}
	if c.APIKey == "" {
		return fmt.Errorf("defectdojo: api_key is required (set in config or DEFECTDOJO_API_KEY env)")
	}
	c.applyDefaults()
	return nil
}

// ─── API v2 Hierarchy ────────────────────────────────────────────────

// Product represents a DefectDojo product.
type Product struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ProdType    int    `json:"prod_type,omitempty"`
}

// Engagement represents a DefectDojo engagement.
type Engagement struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	ProductID int       `json:"product"`
	TargetStart time.Time `json:"target_start"`
	TargetEnd   time.Time `json:"target_end"`
	Status    string    `json:"status"` // "In Progress", "Completed", etc.
}

// Test represents a DefectDojo test (a scan import).
type Test struct {
	ID            int       `json:"id"`
	Title         string    `json:"title,omitempty"`
	EngagementID  int       `json:"engagement"`
	TestType      int       `json:"test_type"`
	ScanType      string    `json:"scan_type,omitempty"`
	Completed     bool      `json:"completed"`
	NumberOfPats  int       `json:"number_of_pats,omitempty"`
}

type DDFinding struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Severity      string `json:"severity"` // "Critical", "High", "Medium", "Low", "Info"
	Confidence    string `json:"confidence,omitempty"`
	Description   string `json:"description,omitempty"`
	FilePath      string `json:"file_path,omitempty"`
	Line          int    `json:"line,omitempty"`
	ComponentName string `json:"component_name,omitempty"`
	TestID        int    `json:"test"`
	Active        bool   `json:"active"`
	Verified      bool   `json:"verified"`
	Mitigated     bool   `json:"mitigated"`
	FalsePositive bool   `json:"false_p_positive,omitempty"`
}

// ─── Client ───────────────────────────────────────────────────────────

// DefectDojoClient is a REST client for the DefectDojo API v2.
type DefectDojoClient struct {
	config     DefectDojoConfig
	httpClient *http.Client
	logger     *slog.Logger
}

// NewDefectDojoClient creates a new DefectDojo API client.
func NewDefectDojoClient(config DefectDojoConfig, logger *slog.Logger) (*DefectDojoClient, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &DefectDojoClient{
		config: config,
		httpClient: &http.Client{
			Timeout: config.RequestTimeout,
		},
		logger: logger.With("component", "defectdojo-client"),
	}, nil
}

// ─── SARIF Import ─────────────────────────────────────────────────────

// ImportResult holds the result of a SARIF import.
type ImportResult struct {
	TestID     int    `json:"test_id"`
	Imported   int    `json:"imported"`
	Skipped    int    `json:"skipped"`
	Errors     int    `json:"errors"`
	ImportTime time.Duration `json:"import_time"`
}

// ImportSARIF uploads a SARIF report to DefectDojo via the import-scan endpoint.
func (c *DefectDojoClient) ImportSARIF(ctx context.Context, engagementID int, sarifData []byte, scanType string) (*ImportResult, error) {
	if scanType == "" {
		scanType = "SARIF"
	}

	c.logger.Info("importing SARIF to DefectDojo",
		"engagement_id", engagementID,
		"scan_type", scanType,
		"size", len(sarifData),
	)

	start := time.Now()

	// Build multipart form
	boundary := fmt.Sprintf("----FormBoundary%d", time.Now().UnixNano())
	var body bytes.Buffer

	// Write engagement ID
	body.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	body.WriteString("Content-Disposition: form-data; name=\"engagement\"\r\n\r\n")
	body.WriteString(fmt.Sprintf("%d\r\n", engagementID))

	// Write scan type
	body.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	body.WriteString("Content-Disposition: form-data; name=\"scan_type\"\r\n\r\n")
	body.WriteString(scanType + "\r\n")

	// Write file
	body.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	body.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"report.sarif\"\r\n")
	body.WriteString("Content-Type: application/json\r\n\r\n")
	body.Write(sarifData)
	body.WriteString("\r\n")

	// Write close boundary
	body.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	// Create request
	url := strings.TrimRight(c.config.BaseURL, "/") + "/api/v2/import-scan/"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, fmt.Errorf("create import request: %w", err)
	}

	req.Header.Set("Authorization", "Token "+c.config.APIKey)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	req.Header.Set("User-Agent", "governor-sarif-client/1.0")

	c.logger.Debug("sending import request", "url", url)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("import sarif request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read import response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("import sarif failed: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var importResp struct {
		TestID int `json:"test,omitempty"`
	}
	if err := json.Unmarshal(respBody, &importResp); err != nil {
		c.logger.Warn("failed to parse import response", "err", err)
	}

	elapsed := time.Since(start)

	result := &ImportResult{
		TestID:     importResp.TestID,
		ImportTime: elapsed,
	}

	c.logger.Info("SARIF import complete",
		"test_id", result.TestID,
		"duration", elapsed,
	)

	return result, nil
}

// ─── Jira Sync Configuration ─────────────────────────────────────────

// JiraSyncConfig holds the Jira synchronization configuration.
type JiraSyncConfig struct {
	Enabled     bool   `json:"enabled"`
	ProjectKey  string `json:"project_key"`
	HostURL     string `json:"host_url"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
 EpicName    string `json:"epic_name,omitempty"`
}

// ConfigureJiraSync sets up Jira synchronization for findings.
func (c *DefectDojoClient) ConfigureJiraSync(ctx context.Context, config JiraSyncConfig) error {
	c.logger.Info("configuring Jira sync",
		"enabled", config.Enabled,
		"project_key", config.ProjectKey,
	)

	url := strings.TrimRight(c.config.BaseURL, "/") + "/api/v2/jira-product-configuration/"

	reqBody := map[string]any{
		"enabled":     config.Enabled,
		"project_key": config.ProjectKey,
		"host_url":    config.HostURL,
	}
	if config.Username != "" {
		reqBody["username"] = config.Username
	}
	if config.Password != "" {
		reqBody["password"] = config.Password
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create jira config request: %w", err)
	}

	req.Header.Set("Authorization", "Token "+c.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("jira config request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("jira config failed: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	c.logger.Info("Jira sync configured")
	return nil
}

// ─── SLA Tracking ─────────────────────────────────────────────────────

// SLAMetrics holds SLA tracking metrics for findings.
type SLAMetrics struct {
	TotalFindings    int           `json:"total_findings"`
	OpenFindings     int           `json:"open_findings"`
	ClosedFindings   int           `json:"closed_findings"`
	AvgMTTR          time.Duration `json:"avg_mttr"`          // Mean Time to Resolve
	AvgTimeToFirstFix time.Duration `json:"avg_time_to_first_fix"`
	RegressionRate   float64       `json:"regression_rate"`  // 0.0 - 1.0
	SLAViolations    int           `json:"sla_violations"`
	SLAMet           int           `json:"sla_met"`
}

// SLADefinition defines the SLA thresholds for a severity level.
type SLADefinition struct {
	Severity  string        `json:"severity"`
	Threshold time.Duration `json:"threshold"` // max time to resolve
}

// DefaultSLADefinitions returns standard SLA definitions.
func DefaultSLADefinitions() []SLADefinition {
	return []SLADefinition{
		{Severity: "Critical", Threshold: 24 * time.Hour},
		{Severity: "High", Threshold: 72 * time.Hour},
		{Severity: "Medium", Threshold: 7 * 24 * time.Hour},
		{Severity: "Low", Threshold: 30 * 24 * time.Hour},
		{Severity: "Info", Threshold: 90 * 24 * time.Hour},
	}
}

// TrackSLA computes SLA metrics for findings.
func TrackSLA(findings []Finding, slaDefs []SLADefinition) SLAMetrics {
	if len(slaDefs) == 0 {
		slaDefs = DefaultSLADefinitions()
	}

	metrics := SLAMetrics{
		TotalFindings: len(findings),
	}

	// Build SLA lookup
	slaLookup := make(map[string]time.Duration)
	for _, d := range slaDefs {
		slaLookup[d.Severity] = d.Threshold
	}

	for _, f := range findings {
		if f.Severity == SeverityError || f.Severity == SeverityWarning {
			metrics.OpenFindings++
		} else {
			metrics.ClosedFindings++
		}
	}

	// Compute regression rate (simplified: findings that reappear)
	// In production, this would compare against historical data
	if metrics.TotalFindings > 0 {
		metrics.RegressionRate = float64(metrics.ClosedFindings) / float64(metrics.TotalFindings)
	}

	return metrics
}

// ─── Product/Engagement/Test/Finding Hierarchy ────────────────────────

// EnsureProduct creates or gets a product in DefectDojo.
func (c *DefectDojoClient) EnsureProduct(ctx context.Context, name string) (*Product, error) {
	// Try to find existing product
	products, err := c.listProducts(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}

	if len(products) > 0 {
		return &products[0], nil
	}

	// Create new product
	url := strings.TrimRight(c.config.BaseURL, "/") + "/api/v2/products/"
	reqBody := map[string]any{
		"name": name,
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create product request: %w", err)
	}

	req.Header.Set("Authorization", "Token "+c.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create product request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create product failed: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var product Product
	if err := json.NewDecoder(resp.Body).Decode(&product); err != nil {
		return nil, fmt.Errorf("decode product: %w", err)
	}

	c.logger.Info("product created", "id", product.ID, "name", product.Name)
	return &product, nil
}

func (c *DefectDojoClient) listProducts(ctx context.Context, name string) ([]Product, error) {
	url := strings.TrimRight(c.config.BaseURL, "/") + "/api/v2/products/?name=" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create list products request: %w", err)
	}

	req.Header.Set("Authorization", "Token "+c.config.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list products request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Results []Product `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode products: %w", err)
	}

	return result.Results, nil
}

// EnsureEngagement creates or gets an engagement in DefectDojo.
func (c *DefectDojoClient) EnsureEngagement(ctx context.Context, productID int, name string) (*Engagement, error) {
	url := strings.TrimRight(c.config.BaseURL, "/") + "/api/v2/engagements/"
	reqBody := map[string]any{
		"name":      name,
		"product":   productID,
		"target_start": time.Now().Format(time.RFC3339),
		"target_end":   time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339),
		"status":    "In Progress",
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create engagement request: %w", err)
	}

	req.Header.Set("Authorization", "Token "+c.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create engagement request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create engagement failed: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var engagement Engagement
	if err := json.NewDecoder(resp.Body).Decode(&engagement); err != nil {
		return nil, fmt.Errorf("decode engagement: %w", err)
	}

	c.logger.Info("engagement created", "id", engagement.ID, "name", engagement.Name)
	return &engagement, nil
}

// ─── Finding Sync ─────────────────────────────────────────────────────

// SyncFindings synchronizes findings between SARIF and DefectDojo.
func (c *DefectDojoClient) SyncFindings(ctx context.Context, testID int, findings []Finding) (int, int, error) {
	c.logger.Info("syncing findings",
		"test_id", testID,
		"findings", len(findings),
	)

	created := 0
	skipped := 0

	for _, f := range findings {
		// Convert SARIF severity to DefectDojo severity
		ddSeverity := mapSeverityToDefectDojo(f.Severity)

		finding := DDFinding{
			Title:       f.Message,
			Severity:    ddSeverity,
			Description: fmt.Sprintf("Rule: %s\nTool: %s\nSource: %s", f.RuleID, f.Tool, f.Source),
			FilePath:    f.File,
			Line:        f.Line,
			TestID:      testID,
			Active:      true,
		}

		_, err := c.createFinding(ctx, finding)
		if err != nil {
			c.logger.Warn("failed to create finding",
				"rule_id", f.RuleID,
				"err", err,
			)
			skipped++
			continue
		}
		created++
	}

	c.logger.Info("finding sync complete",
		"created", created,
		"skipped", skipped,
	)

	return created, skipped, nil
}

func (c *DefectDojoClient) createFinding(ctx context.Context, finding DDFinding) (*DDFinding, error) {
	url := strings.TrimRight(c.config.BaseURL, "/") + "/api/v2/findings/"

	bodyBytes, _ := json.Marshal(finding)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create finding request: %w", err)
	}

	req.Header.Set("Authorization", "Token "+c.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create finding request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create finding failed: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var created DDFinding
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("decode finding: %w", err)
	}

	return &created, nil
}

func mapSeverityToDefectDojo(s Severity) string {
	switch s {
	case SeverityError:
		return "High"
	case SeverityWarning:
		return "Medium"
	case SeverityNote:
		return "Low"
	default:
		return "Info"
	}
}
