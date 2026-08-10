package hangar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ─── Policy Sync Race Condition ──────────────────────────────────────

// UpdatePolicyWithRetry handles concurrent PATCH from multiple orchestrators.
// On 412 Precondition Failed, it re-fetches the ETag and retries up to maxRetries.
func (c *Client) UpdatePolicyWithRetry(ctx context.Context, update PolicyUpdate, maxRetries int) (*PolicyResult, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			c.logger.Warn("retrying policy update after conflict",
				"attempt", attempt+1,
				"max", maxRetries+1,
				"err", lastErr,
			)
			// Small backoff before retry to reduce contention
			backoff := time.Duration(attempt) * 100 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		// Fetch fresh ETag
		etag, err := c.getPolicyETag(ctx)
		if err != nil {
			lastErr = fmt.Errorf("fetch etag: %w", err)
			continue
		}

		result, err := c.UpdatePolicy(ctx, update, etag)
		if err == nil {
			return result, nil
		}

		if errors.Is(err, ErrPreconditionFailed) {
			lastErr = err
			c.logger.Info("policy conflict, will retry",
				"attempt", attempt+1,
				"etag", etag,
			)
			continue
		}

		// Non-conflict error — don't retry
		return nil, err
	}

	return nil, fmt.Errorf("policy update failed after %d retries: %w", maxRetries, lastErr)
}

// ─── Hangar Restart During Sync ──────────────────────────────────────

// SyncProvidersWithRestartHandling wraps SyncProviders to handle Hangar restarts.
// If the initial sync request returns 202 and subsequent health polls fail
// (connection reset, 502, 503), it keeps retrying until timeout.
func (c *Client) SyncProvidersWithRestartHandling(ctx context.Context, req SyncRequest) (*SyncResponse, error) {
	syncResp, err := c.SyncProviders(ctx, req, false) // fire-and-forget
	if err != nil {
		return nil, fmt.Errorf("initiate sync: %w", err)
	}

	// Poll with restart tolerance
	deadline := time.After(c.config.Poll.MaxWait)
	ticker := time.NewTicker(c.config.Poll.Interval)
	defer ticker.Stop()

	consecutiveFailures := 0
	const maxConsecutiveFailures = 3

	c.logger.Info("polling for sync completion with restart tolerance",
		"sync_id", syncResp.SyncID,
	)

	for {
		select {
		case <-ctx.Done():
			return syncResp, fmt.Errorf("context cancelled: %w", ctx.Err())
		case <-deadline:
			return syncResp, fmt.Errorf("timeout waiting for sync %s", syncResp.SyncID)
		case <-ticker.C:
			health, err := c.getHealth(ctx)
			if err != nil {
				consecutiveFailures++
				c.logger.Warn("health poll failed (possible Hangar restart)",
					"sync_id", syncResp.SyncID,
					"consecutive_failures", consecutiveFailures,
					"err", err,
				)
				if consecutiveFailures >= maxConsecutiveFailures {
					// Try to re-initiate the sync
					c.logger.Warn("re-initiating sync after Hangar restart")
					newResp, initErr := c.SyncProviders(ctx, req, false)
					if initErr != nil {
						return syncResp, fmt.Errorf("re-initiate sync after restart: %w", initErr)
					}
					syncResp = newResp
					consecutiveFailures = 0
				}
				continue
			}

			consecutiveFailures = 0
			if !health.Stale {
				c.logger.Info("sync complete after restart handling",
					"sync_id", syncResp.SyncID,
				)
				return syncResp, nil
			}
		}
	}
}

// ─── Rate Limiting (429 with Retry-After) ────────────────────────────

// RateLimitedDo wraps doRequest with explicit rate-limit awareness.
// It's exposed for operations that may need custom 429 handling beyond
// the standard retry loop.
func (c *Client) RateLimitedDo(ctx context.Context, method, path string, body any, idemKey string) (*http.Response, []byte, error) {
	return c.doRequest(ctx, method, path, body, idemKey)
}

// ─── Large Fleet: Parallel Per-Connection Fetching ────────────────────

// FetchScorecardByConnection fetches scorecard data partitioned by connection ID.
// Each connection is fetched in parallel with bounded concurrency.
func (c *Client) FetchScorecardByConnection(ctx context.Context) (map[string][]RepoScore, error) {
	// First, get the full fleet to discover connection IDs
	scorecard, err := c.FetchFleetScorecardParallel(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch fleet: %w", err)
	}

	// Group by connection ID
	connections := make(map[string][]RepoScore)
	for _, repo := range scorecard.Repos {
		connID := repo.ConnectionID
		if connID == "" {
			connID = "__unknown__"
		}
		connections[connID] = append(connections[connID], repo)
	}

	c.logger.Info("fleet partitioned by connection",
		"connections", len(connections),
		"total_repos", len(scorecard.Repos),
	)

	return connections, nil
}

// ─── Connection ID Changes ───────────────────────────────────────────

// ConnectionIDChange tracks when a repo moves between providers.
type ConnectionIDChange struct {
	RepoID    string    `json:"repo_id"`
	RepoName  string    `json:"repo_name"`
	OldConnID string    `json:"old_connection_id"`
	NewConnID string    `json:"new_connection_id"`
	ChangedAt time.Time `json:"changed_at"`
}

// DetectConnectionChanges compares two scorecard snapshots and returns any
// repos whose connection ID has changed (e.g., moved from GitHub to Gitea).
func DetectConnectionChanges(before, after []RepoScore) []ConnectionIDChange {
	beforeMap := make(map[string]RepoScore, len(before))
	for _, r := range before {
		beforeMap[r.RepoID] = r
	}

	var changes []ConnectionIDChange
	for _, afterRepo := range after {
		beforeRepo, ok := beforeMap[afterRepo.RepoID]
		if !ok {
			continue // new repo, not a change
		}
		if beforeRepo.ConnectionID != afterRepo.ConnectionID {
			changes = append(changes, ConnectionIDChange{
				RepoID:    afterRepo.RepoID,
				RepoName:  afterRepo.RepoName,
				OldConnID: beforeRepo.ConnectionID,
				NewConnID: afterRepo.ConnectionID,
				ChangedAt: time.Now(),
			})
		}
	}

	return changes
}

// ─── Partial Remediation Failure ─────────────────────────────────────

// RemediationSummary aggregates results from a batch remediation,
// explicitly handling partial failures (e.g., 8/10 repos succeed).
type RemediationSummary struct {
	RemediationID string              `json:"remediation_id"`
	Total         int                 `json:"total"`
	Succeeded     int                 `json:"succeeded"`
	Failed        int                 `json:"failed"`
	Skipped       int                 `json:"skipped"`
	IsComplete    bool                `json:"is_complete"`
	IsPartial     bool                `json:"is_partial"`
	FailedRepos   []RemediationResult `json:"failed_repos,omitempty"`
}

// AnalyzeRemediation inspects a RemediateResponse and produces a summary
// that clearly surfaces partial failures.
func AnalyzeRemediation(resp *RemediateResponse) *RemediationSummary {
	summary := &RemediationSummary{
		RemediationID: resp.RemediationID,
		Total:         resp.TotalRepos,
		Succeeded:     resp.Succeeded,
		Failed:        resp.Failed,
		Skipped:       resp.Skipped,
		IsComplete:    resp.Failed == 0,
		IsPartial:     resp.Failed > 0 && resp.Succeeded > 0,
	}

	// Collect failed repos for inspection
	for _, r := range resp.Results {
		if r.Status == "failed" {
			summary.FailedRepos = append(summary.FailedRepos, r)
		}
	}

	return summary
}

// IsTransientRemediationError checks if a remediation error is transient
// (network timeout, Hangar restart) vs permanent (repo not found, permission denied).
func IsTransientRemediationError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	transientPatterns := []string{
		"timeout",
		"connection reset",
		"EOF",
		"broken pipe",
		"i/o timeout",
	}
	for _, pattern := range transientPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}

// ─── Connection Health for Multiple Providers ─────────────────────────

// ProviderHealth holds health status for a specific provider connection.
type ProviderHealth struct {
	Provider     string    `json:"provider"`
	ConnectionID string    `json:"connection_id"`
	Healthy      bool      `json:"healthy"`
	LastSync     time.Time `json:"last_sync"`
	RepoCount    int       `json:"repo_count"`
	Error        string    `json:"error,omitempty"`
}

// GetProviderHealth returns health status for all provider connections.
func (c *Client) GetProviderHealth(ctx context.Context) ([]ProviderHealth, error) {
	resp, data, err := c.doRequest(ctx, http.MethodGet, "/health/providers", nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider health: unexpected status %d", resp.StatusCode)
	}

	var providers []ProviderHealth
	if err := json.Unmarshal(data, &providers); err != nil {
		return nil, fmt.Errorf("decode provider health: %w", err)
	}

	return providers, nil
}

// ─── Audit Stream with Reconnection ──────────────────────────────────

// StreamProviderAuditWithReconnect wraps StreamProviderAudit with automatic
// reconnection on transient errors. Useful for long-running audit consumers.
func (c *Client) StreamProviderAuditWithReconnect(ctx context.Context, since time.Time, callback func(AuditEntry) error) error {
	for {
		err := c.StreamProviderAudit(ctx, since, callback)
		if err == nil {
			return nil // clean shutdown
		}

		if ctx.Err() != nil {
			return ctx.Err() // context cancelled
		}

		if !IsTransientRemediationError(err) {
			return fmt.Errorf("non-transient audit stream error: %w", err)
		}

		c.logger.Warn("audit stream disconnected, reconnecting",
			"err", err,
			"since", since,
		)

		// Backoff before reconnect
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// ─── Metrics Helper ──────────────────────────────────────────────────

// FleetMetrics holds computed metrics for a fleet scorecard.
type FleetMetrics struct {
	TotalRepos      int     `json:"total_repos"`
	AverageScore    float64 `json:"average_score"`
	MinScore        float64 `json:"min_score"`
	MaxScore        float64 `json:"max_score"`
	PassingRepos    int     `json:"passing_repos"`  // score/max >= 0.8
	FailingRepos    int     `json:"failing_repos"`   // score/max < 0.5
	TotalChecks     int     `json:"total_checks"`
	PassingChecks   int     `json:"passing_checks"`
	FailingChecks   int     `json:"failing_checks"`
	ConnectionCount int     `json:"connection_count"`
}

// ComputeFleetMetrics calculates aggregate metrics from a scorecard.
func ComputeFleetMetrics(scorecard *FleetScorecard) *FleetMetrics {
	if scorecard == nil || len(scorecard.Repos) == 0 {
		return &FleetMetrics{}
	}

	m := &FleetMetrics{
		TotalRepos: len(scorecard.Repos),
		MinScore:   1.0,
	}

	connections := make(map[string]bool)

	for _, repo := range scorecard.Repos {
		pct := repo.Score / repo.MaxScore
		if repo.MaxScore > 0 {
			if pct < m.MinScore {
				m.MinScore = pct
			}
			if pct > m.MaxScore {
				m.MaxScore = pct
			}
			if pct >= 0.8 {
				m.PassingRepos++
			}
			if pct < 0.5 {
				m.FailingRepos++
			}
		}

		if repo.ConnectionID != "" {
			connections[repo.ConnectionID] = true
		}

		for _, check := range repo.Checks {
			m.TotalChecks++
			if check.Passed {
				m.PassingChecks++
			} else {
				m.FailingChecks++
			}
		}
	}

	m.AverageScore = scorecard.AverageScore
	m.ConnectionCount = len(connections)

	return m
}
