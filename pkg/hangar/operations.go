package hangar

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ─── Fleet Scorecard ─────────────────────────────────────────────────

// RepoScore represents a single repository's compliance score.
type RepoScore struct {
	RepoID      string            `json:"repo_id"`
	RepoName    string            `json:"repo_name"`
	ConnectionID string           `json:"connection_id"`
	Score       float64           `json:"score"`
	MaxScore    float64           `json:"max_score"`
	Checks      []CheckResult     `json:"checks"`
	LastScan    time.Time         `json:"last_scan"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// CheckResult is the outcome of a single compliance check for a repo.
type CheckResult struct {
	CheckID  string `json:"check_id"`
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Message  string `json:"message,omitempty"`
	Category string `json:"category,omitempty"`
}

// ScorecardPage is one page of fleet scorecard results.
type ScorecardPage struct {
	Repos      []RepoScore `json:"repos"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalCount int         `json:"total_count"`
	HasMore    bool        `json:"has_more"`
}

// FleetScorecard holds the complete fleet scorecard with all repos.
type FleetScorecard struct {
	Repos       []RepoScore `json:"repos"`
	TotalCount  int         `json:"total_count"`
	FetchedAt   time.Time   `json:"fetched_at"`
	AverageScore float64    `json:"average_score"`
}

// GetFleetScorecard retrieves the fleet scorecard with automatic pagination.
// If page is 0 and pageSize is 0, uses config defaults and fetches all pages.
func (c *Client) GetFleetScorecard(ctx context.Context, page, pageSize int) (*FleetScorecard, error) {
	if page <= 0 {
		return c.fetchAllScorecardPages(ctx)
	}
	result, err := c.fetchScorecardPage(ctx, page, pageSize)
	if err != nil {
		return nil, err
	}
	return &FleetScorecard{
		Repos:       result.Repos,
		TotalCount:  result.TotalCount,
		FetchedAt:   time.Now(),
		AverageScore: averageScore(result.Repos),
	}, nil
}

func (c *Client) fetchAllScorecardPages(ctx context.Context) (*FleetScorecard, error) {
	var allRepos []RepoScore
	page := 1
	pageSize := c.config.Fleet.PageSize
	var totalCount int

	for {
		result, err := c.fetchScorecardPage(ctx, page, pageSize)
		if err != nil {
			return nil, fmt.Errorf("fetch scorecard page %d: %w", page, err)
		}

		allRepos = append(allRepos, result.Repos...)
		totalCount = result.TotalCount

		if !result.HasMore {
			break
		}
		page++
	}

	scorecard := &FleetScorecard{
		Repos:      allRepos,
		TotalCount: totalCount,
		FetchedAt:  time.Now(),
	}

	if len(allRepos) > 0 {
		var total float64
		for _, r := range allRepos {
			if r.MaxScore > 0 {
				total += r.Score / r.MaxScore
			}
		}
		scorecard.AverageScore = total / float64(len(allRepos))
	}

	c.logger.Info("fetched fleet scorecard",
		"total_repos", len(allRepos),
		"avg_score", scorecard.AverageScore,
	)
	return scorecard, nil
}

func (c *Client) fetchScorecardPage(ctx context.Context, page, pageSize int) (*ScorecardPage, error) {
	path := fmt.Sprintf("/api/v1/fleet/scorecard?page=%d&page_size=%d", page, pageSize)

	resp, data, err := c.doRequest(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scorecard: unexpected status %d: %s", resp.StatusCode, string(data))
	}

	var pageResult ScorecardPage
	if err := json.Unmarshal(data, &pageResult); err != nil {
		return nil, fmt.Errorf("decode scorecard: %w", err)
	}

	return &pageResult, nil
}

// ─── Provider Sync + Poll ────────────────────────────────────────────

// SyncRequest is the payload for POST /api/v1/providers/sync.
type SyncRequest struct {
	ProviderType string   `json:"provider_type"` // "github" or "gitea"
	ConnectionIDs []string `json:"connection_ids,omitempty"`
	Force         bool     `json:"force,omitempty"`
}

// SyncResponse is returned by POST /api/v1/providers/sync.
type SyncResponse struct {
	SyncID     string `json:"sync_id"`
	Status     string `json:"status"`     // "accepted", "already_syncing"
	Message    string `json:"message,omitempty"`
	StartedAt  time.Time `json:"started_at"`
}

// HealthStatus is the response from GET /health.
type HealthStatus struct {
	Status    string `json:"status"`     // "healthy", "syncing", "unhealthy"
	Stale     bool   `json:"stale"`     // true if data is stale
	SyncID    string `json:"sync_id"`
	Progress  *SyncProgress `json:"progress,omitempty"`
}

// SyncProgress tracks in-flight sync progress.
type SyncProgress struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

// SyncProviders triggers a provider sync and optionally polls until data is fresh.
// If wait is true, it polls /health until stale=false or timeout.
func (c *Client) SyncProviders(ctx context.Context, req SyncRequest, wait bool) (*SyncResponse, error) {
	idemKey := idempotencyKey()

	resp, data, err := c.doRequest(ctx, http.MethodPost, "/api/v1/providers/sync", req, idemKey)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 202 Accepted means async sync started
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sync providers: unexpected status %d: %s", resp.StatusCode, string(data))
	}

	var syncResp SyncResponse
	if err := json.Unmarshal(data, &syncResp); err != nil {
		return nil, fmt.Errorf("decode sync response: %w", err)
	}

	c.logger.Info("provider sync initiated",
		"sync_id", syncResp.SyncID,
		"provider", req.ProviderType,
		"idempotency_key", idemKey,
	)

	if wait {
		if err := c.PollHealthUntilFresh(ctx, syncResp.SyncID); err != nil {
			return &syncResp, fmt.Errorf("sync poll: %w", err)
		}
		syncResp.Status = "completed"
	}

	return &syncResp, nil
}

// PollHealthUntilFresh polls GET /health until stale=false or timeout.
func (c *Client) PollHealthUntilFresh(ctx context.Context, syncID string) error {
	deadline := time.After(c.config.Poll.MaxWait)
	ticker := time.NewTicker(c.config.Poll.Interval)
	defer ticker.Stop()

	c.logger.Info("polling health for freshness", "sync_id", syncID)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled waiting for sync %s: %w", syncID, ctx.Err())
		case <-deadline:
			return fmt.Errorf("timeout waiting for sync %s to complete after %v", syncID, c.config.Poll.MaxWait)
		case <-ticker.C:
			health, err := c.getHealth(ctx)
			if err != nil {
				// Hangar may have restarted — log and continue polling
				c.logger.Warn("health check failed during poll, will retry",
					"sync_id", syncID,
					"err", err,
				)
				continue
			}

			if !health.Stale {
				c.logger.Info("sync complete, data is fresh",
					"sync_id", syncID,
					"progress", health.Progress,
				)
				return nil
			}

			if health.Progress != nil {
				c.logger.Debug("sync in progress",
					"sync_id", syncID,
					"completed", health.Progress.Completed,
					"total", health.Progress.Total,
					"failed", health.Progress.Failed,
				)
			}
		}
	}
}

func (c *Client) getHealth(ctx context.Context) (*HealthStatus, error) {
	resp, data, err := c.doRequest(ctx, http.MethodGet, "/health", nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health: unexpected status %d", resp.StatusCode)
	}

	var health HealthStatus
	if err := json.Unmarshal(data, &health); err != nil {
		return nil, fmt.Errorf("decode health: %w", err)
	}

	return &health, nil
}

// ─── Policy Update with Optimistic Locking ───────────────────────────

// PolicyUpdate is the payload for PATCH /api/v1/policy.
type PolicyUpdate struct {
	Rules     []PolicyRule       `json:"rules"`
	Variables map[string]string  `json:"variables,omitempty"`
	Metadata  map[string]string  `json:"metadata,omitempty"`
}

// PolicyRule defines a single governance rule.
type PolicyRule struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Severity string `json:"severity"` // "error", "warning", "info"
	Target   string `json:"target"`   // glob pattern for repos
	Config   map[string]any `json:"config,omitempty"`
}

// PolicyResult is the response from PATCH /api/v1/policy.
type PolicyResult struct {
	Version  string    `json:"version"`
	ETag     string    `json:"etag"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UpdatePolicy updates governance policy with optimistic locking via ETag.
// If etag is empty, the current ETag is fetched first.
// Returns ErrPreconditionFailed if the policy was modified concurrently.
func (c *Client) UpdatePolicy(ctx context.Context, update PolicyUpdate, etag string) (*PolicyResult, error) {
	idemKey := idempotencyKey()

	// Fetch current ETag if not provided
	if etag == "" {
		current, err := c.getPolicyETag(ctx)
		if err != nil {
			return nil, fmt.Errorf("fetch current policy: %w", err)
		}
		etag = current
	}

	resp, data, err := c.doRequestWithETag(ctx, http.MethodPatch, "/api/v1/policy", update, idemKey, etag)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPreconditionFailed {
		return nil, ErrPreconditionFailed
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update policy: unexpected status %d: %s", resp.StatusCode, string(data))
	}

	var result PolicyResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode policy result: %w", err)
	}

	// Use response ETag for subsequent calls
	if newETag := resp.Header.Get("ETag"); newETag != "" {
		result.ETag = newETag
	}

	c.logger.Info("policy updated",
		"version", result.Version,
		"etag", result.ETag,
		"idempotency_key", idemKey,
	)

	return &result, nil
}

func (c *Client) getPolicyETag(ctx context.Context) (string, error) {
	resp, _, err := c.doRequest(ctx, http.MethodGet, "/api/v1/policy", nil, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	etag := resp.Header.Get("ETag")
	if etag == "" {
		return "", fmt.Errorf("no ETag returned from policy endpoint")
	}
	return etag, nil
}

// ─── Remediate Batch ─────────────────────────────────────────────────

// RemediateRequest is the payload for POST /api/v1/checks/{check_id}/remediate-batch.
type RemediateRequest struct {
	RepoIDs    []string         `json:"repo_ids"`
	DryRun     bool             `json:"dry_run,omitempty"`
	Options    map[string]any   `json:"options,omitempty"`
}

// RemediateResponse tracks the outcome of a batch remediation.
type RemediateResponse struct {
	RemediationID string              `json:"remediation_id"`
	Status        string              `json:"status"` // "completed", "partial", "failed"
	Results       []RemediationResult `json:"results"`
	TotalRepos    int                 `json:"total_repos"`
	Succeeded     int                 `json:"succeeded"`
	Failed        int                 `json:"failed"`
	Skipped       int                 `json:"skipped"`
}

// RemediationResult is the per-repo outcome of remediation.
type RemediationResult struct {
	RepoID       string `json:"repo_id"`
	RepoName     string `json:"repo_name"`
	Status       string `json:"status"` // "success", "failed", "skipped"
	Message      string `json:"message,omitempty"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	CommitSHA    string `json:"commit_sha,omitempty"`
}

// RemediateBatch triggers remediation of a check across multiple repos.
func (c *Client) RemediateBatch(ctx context.Context, checkID string, req RemediateRequest) (*RemediateResponse, error) {
	idemKey := idempotencyKey()
	path := fmt.Sprintf("/api/v1/checks/%s/remediate-batch", checkID)

	resp, data, err := c.doRequest(ctx, http.MethodPost, path, req, idemKey)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("remediate batch: unexpected status %d: %s", resp.StatusCode, string(data))
	}

	var result RemediateResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode remediate response: %w", err)
	}

	c.logger.Info("remediation batch complete",
		"check_id", checkID,
		"remediation_id", result.RemediationID,
		"total", result.TotalRepos,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
		"idempotency_key", idemKey,
	)

	return &result, nil
}

// ─── Provider Audit (Streaming) ──────────────────────────────────────

// AuditEntry represents a single audit event from the streaming endpoint.
type AuditEntry struct {
	Timestamp   time.Time         `json:"timestamp"`
	EventType   string            `json:"event_type"` // "sync.start", "sync.complete", "check.run", etc.
	Provider    string            `json:"provider"`
	ConnectionID string           `json:"connection_id"`
	RepoID      string            `json:"repo_id,omitempty"`
	Details     map[string]any    `json:"details,omitempty"`
}

// StreamProviderAudit opens a streaming connection to GET /api/v1/providers/audit.
// It calls the callback for each entry received. The stream is cancelled when ctx
// is done or the server closes the connection.
func (c *Client) StreamProviderAudit(ctx context.Context, since time.Time, callback func(AuditEntry) error) error {
	params := url.Values{}
	if !since.IsZero() {
		params.Set("since", since.Format(time.RFC3339))
	}
	path := "/api/v1/providers/audit"
	if encoded := params.Encode(); encoded != "" {
		path += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.config.BaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create audit request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	req.Header.Set("Accept", "application/x-ndjson")
	req.Header.Set("User-Agent", "governor-hangar-client/1.0")

	c.logger.Debug("starting audit stream", "since", since)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("audit stream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("audit stream: unexpected status %d: %s", resp.StatusCode, string(data))
	}

	c.logger.Info("audit stream connected", "status", resp.StatusCode)

	scanner := bufio.NewScanner(resp.Body)
	// Increase buffer for large audit entries
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry AuditEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			c.logger.Warn("failed to parse audit entry", "err", err, "line", string(line))
			continue
		}

		if err := callback(entry); err != nil {
			return fmt.Errorf("audit callback error: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("audit stream read: %w", err)
	}

	c.logger.Info("audit stream ended")
	return nil
}

// ─── Scorecard by Page (explicit pagination helper) ──────────────────

// GetScorecardPage retrieves a single page of scorecard results.
func (c *Client) GetScorecardPage(ctx context.Context, page, pageSize int) (*ScorecardPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = c.config.Fleet.PageSize
	}
	return c.fetchScorecardPage(ctx, page, pageSize)
}

// ─── Health check (public) ───────────────────────────────────────────

// GetHealth returns the current health status from Hangar.
func (c *Client) GetHealth(ctx context.Context) (*HealthStatus, error) {
	return c.getHealth(ctx)
}

// ─── Fleet Count Helper ──────────────────────────────────────────────

// GetFleetSize returns the total number of repos in the fleet.
func (c *Client) GetFleetSize(ctx context.Context) (int, error) {
	resp, data, err := c.doRequest(ctx, http.MethodGet, "/api/v1/fleet/scorecard?page=1&page_size=1", nil, "")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fleet size: unexpected status %d", resp.StatusCode)
	}

	var page ScorecardPage
	if err := json.Unmarshal(data, &page); err != nil {
		return 0, fmt.Errorf("decode fleet size: %w", err)
	}
	return page.TotalCount, nil
}

// ─── Parallel Fleet Fetching ─────────────────────────────────────────

// FetchFleetScorecardParallel fetches all scorecard pages in parallel.
// Useful for large fleets (1000+ repos) where sequential fetching is too slow.
func (c *Client) FetchFleetScorecardParallel(ctx context.Context) (*FleetScorecard, error) {
	// First, get total count with a single-page request
	firstPage, err := c.fetchScorecardPage(ctx, 1, c.config.Fleet.PageSize)
	if err != nil {
		return nil, fmt.Errorf("initial page fetch: %w", err)
	}

	if firstPage.TotalCount <= c.config.Fleet.PageSize {
		// Single page, no parallelism needed
		return &FleetScorecard{
			Repos:       firstPage.Repos,
			TotalCount:  firstPage.TotalCount,
			FetchedAt:   time.Now(),
			AverageScore: averageScore(firstPage.Repos),
		}, nil
	}

	// Calculate total pages
	totalPages := (firstPage.TotalCount + c.config.Fleet.PageSize - 1) / c.config.Fleet.PageSize

	// Collect all repos from page 1
	results := make([]RepoScore, 0, firstPage.TotalCount)
	results = append(results, firstPage.Repos...)

	// Fetch remaining pages in parallel with bounded concurrency
	type pageResult struct {
		page  int
		repos []RepoScore
		err   error
	}

	sem := make(chan struct{}, c.config.Fleet.MaxConcurrency)
	pageCh := make(chan pageResult, totalPages-1)

	for p := 2; p <= totalPages; p++ {
		page := p
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			pg, err := c.fetchScorecardPage(ctx, page, c.config.Fleet.PageSize)
			if err != nil {
				pageCh <- pageResult{page: page, err: err}
				return
			}
			pageCh <- pageResult{page: page, repos: pg.Repos}
		}()
	}

	// Collect results
	var fetchErr error
	for i := 0; i < totalPages-1; i++ {
		pr := <-pageCh
		if pr.err != nil {
			c.logger.Error("parallel fetch failed", "page", pr.page, "err", pr.err)
			if fetchErr == nil {
				fetchErr = fmt.Errorf("page %d: %w", pr.page, pr.err)
			}
			continue
		}
		results = append(results, pr.repos...)
	}

	if fetchErr != nil {
		return nil, fmt.Errorf("parallel fleet fetch: %w", fetchErr)
	}

	scorecard := &FleetScorecard{
		Repos:       results,
		TotalCount:  firstPage.TotalCount,
		FetchedAt:   time.Now(),
		AverageScore: averageScore(results),
	}

	c.logger.Info("parallel fleet fetch complete",
		"total_repos", len(results),
		"pages", totalPages,
		"avg_score", scorecard.AverageScore,
	)

	return scorecard, nil
}

func averageScore(repos []RepoScore) float64 {
	if len(repos) == 0 {
		return 0
	}
	var total float64
	for _, r := range repos {
		if r.MaxScore > 0 {
			total += r.Score / r.MaxScore
		}
	}
	return total / float64(len(repos))
}

// ─── Errors ──────────────────────────────────────────────────────────

// ErrPreconditionFailed is returned when a PATCH fails the ETag check.
// The caller should re-fetch the current state and retry.
var ErrPreconditionFailed = fmt.Errorf("hangar: precondition failed (policy modified by another orchestrator)")

// ─── Scorecard Count Helper ──────────────────────────────────────────

func parseTotalCount(resp *http.Response) int {
	if v := resp.Header.Get("X-Total-Count"); v != "" {
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}
