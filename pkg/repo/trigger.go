package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ─── Phase Dispatch ───────────────────────────────────────────────────

// RefreshPhase identifies the scope of a refresh operation.
type RefreshPhase string

const (
	// PhaseQuick refreshes only staleness metadata and open PRs.
	PhaseQuick RefreshPhase = "quick"
	// PhaseFull performs a complete repository audit.
	PhaseFull RefreshPhase = "full"
	// PhaseRefresh re-fetches all cached data without a full audit.
	PhaseRefresh RefreshPhase = "refresh"
)

// RefreshRequest is the arguments for trigger_refresh.
type RefreshRequest struct {
	// Phase determines the scope of the refresh.
	Phase RefreshPhase `json:"phase"`

	// RepoPath is the repository to refresh (defaults to current).
	RepoPath string `json:"repo_path,omitempty"`

	// Force bypasses any cooldown or debounce logic.
	Force bool `json:"force,omitempty"`

	// CallbackURL is an optional webhook to notify when refresh completes.
	CallbackURL string `json:"callback_url,omitempty"`
}

// RefreshResponse is the result of trigger_refresh.
type RefreshResponse struct {
	RefreshID   string       `json:"refresh_id"`
	Phase       RefreshPhase `json:"phase"`
	Status      string       `json:"status"` // "dispatched", "completed", "failed"
	StartedAt   time.Time    `json:"started_at"`
	CompletedAt *time.Time   `json:"completed_at,omitempty"`
	Duration    *Duration    `json:"duration,omitempty"`
	Error       string       `json:"error,omitempty"`
}

// Duration is a JSON-friendly duration that marshals as milliseconds.
type Duration struct {
	time.Duration
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Milliseconds())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var ms int64
	if err := json.Unmarshal(b, &ms); err != nil {
		return err
	}
	d.Duration = time.Duration(ms) * time.Millisecond
	return nil
}

// ─── Trigger Refresh ──────────────────────────────────────────────────

// TriggerRefresh dispatches a refresh operation with the specified phase.
// It handles timeout for workflow dispatch and returns the refresh ID for polling.
func (c *Client) TriggerRefresh(ctx context.Context, req RefreshRequest) (*RefreshResponse, error) {
	if req.Phase == "" {
		req.Phase = PhaseQuick
	}

	// Validate phase
	switch req.Phase {
	case PhaseQuick, PhaseFull, PhaseRefresh:
		// valid
	default:
		return nil, fmt.Errorf("invalid phase %q: must be one of quick, full, refresh", req.Phase)
	}

	c.logger.Info("triggering refresh",
		"phase", req.Phase,
		"repo", req.RepoPath,
		"force", req.Force,
	)

	// Set a longer timeout for the refresh dispatch (workflow triggers can be slow)
	dispatchTimeout := 30 * time.Second
	if req.Phase == PhaseFull {
		dispatchTimeout = 60 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, dispatchTimeout)
	defer cancel()

	args := map[string]any{
		"phase": string(req.Phase),
	}
	if req.RepoPath != "" {
		args["repo_path"] = req.RepoPath
	}
	if req.Force {
		args["force"] = true
	}
	if req.CallbackURL != "" {
		args["callback_url"] = req.CallbackURL
	}

	rawResult, err := c.CallToolRaw(ctx, ToolTriggerRefresh, args)
	if err != nil {
		return nil, fmt.Errorf("trigger refresh: %w", err)
	}

	var resp RefreshResponse
	if err := json.Unmarshal(rawResult, &resp); err != nil {
		return nil, fmt.Errorf("parse refresh response: %w", err)
	}

	c.logger.Info("refresh dispatched",
		"refresh_id", resp.RefreshID,
		"phase", resp.Phase,
		"status", resp.Status,
	)

	return &resp, nil
}

// ─── Refresh Polling ──────────────────────────────────────────────────

// PollRefreshOptions controls the polling behavior for async refresh operations.
type PollRefreshOptions struct {
	// Interval is the time between poll attempts.
	Interval time.Duration

	// MaxWait is the maximum time to wait for completion.
	MaxWait time.Duration
}

// DefaultPollOptions returns sensible defaults for refresh polling.
func DefaultPollOptions() PollRefreshOptions {
	return PollRefreshOptions{
		Interval: 5 * time.Second,
		MaxWait:  5 * time.Minute,
	}
}

// PollRefresh waits for a specific refresh operation to complete by polling get_staleness
// with the refreshID. It verifies that the particular refresh (not just any low data age)
// has completed.
func (c *Client) PollRefresh(ctx context.Context, refreshID string, opts PollRefreshOptions) (*RefreshResponse, error) {
	if opts.Interval == 0 {
		opts.Interval = DefaultPollOptions().Interval
	}
	if opts.MaxWait == 0 {
		opts.MaxWait = DefaultPollOptions().MaxWait
	}

	c.logger.Info("polling refresh status",
		"refresh_id", refreshID,
		"interval", opts.Interval,
		"max_wait", opts.MaxWait,
	)

	deadline := time.After(opts.MaxWait)
	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled while polling refresh %s: %w", refreshID, ctx.Err())
		case <-deadline:
			return nil, fmt.Errorf("timeout waiting for refresh %s after %v", refreshID, opts.MaxWait)
		case <-ticker.C:
			status, err := c.getStalenessForRefresh(ctx, refreshID)
			if err != nil {
				c.logger.Warn("poll failed, will retry",
					"refresh_id", refreshID,
					"err", err,
				)
				continue
			}

			if status.DataAgeHours < 0.1 {
				completedAt := time.Now()
				return &RefreshResponse{
					RefreshID:   refreshID,
					Status:      "completed",
					CompletedAt: &completedAt,
				}, nil
			}
		}
	}
}

// getStalenessForRefresh calls get_staleness with the refreshID so the server
// can verify that specific refresh has completed.
func (c *Client) getStalenessForRefresh(ctx context.Context, refreshID string) (*StalenessEnvelope, error) {
	args := map[string]any{
		"refresh_id": refreshID,
	}
	rawResult, err := c.CallToolRaw(ctx, ToolGetStaleness, args)
	if err != nil {
		return nil, fmt.Errorf("get staleness for refresh %s: %w", refreshID, err)
	}

	var envelope StalenessEnvelope
	if err := json.Unmarshal(rawResult, &envelope); err != nil {
		return nil, fmt.Errorf("parse staleness for refresh %s: %w", refreshID, err)
	}

	return &envelope, nil
}

// ─── Get Staleness ────────────────────────────────────────────────────

// GetStaleness returns the current staleness envelope for the repository.
func (c *Client) GetStaleness(ctx context.Context) (*StalenessEnvelope, error) {
	rawResult, err := c.CallToolRaw(ctx, ToolGetStaleness, nil)
	if err != nil {
		return nil, fmt.Errorf("get staleness: %w", err)
	}

	var envelope StalenessEnvelope
	if err := json.Unmarshal(rawResult, &envelope); err != nil {
		return nil, fmt.Errorf("parse staleness: %w", err)
	}

	return &envelope, nil
}

// ─── Check Conflicts ──────────────────────────────────────────────────

// ConflictResult represents a detected merge conflict.
type ConflictResult struct {
	Files    []ConflictFile `json:"files"`
	Count    int            `json:"count"`
	Severity string         `json:"severity"` // "none", "low", "medium", "high"
}

type ConflictFile struct {
	Path      string `json:"path"`
	Status    string `json:"status"` // "conflicted", "resolved", "auto-resolved"
	Conflicts int    `json:"conflicts"`
}

// CheckConflicts detects merge conflicts in the repository.
func (c *Client) CheckConflicts(ctx context.Context, repoPath string) (*ConflictResult, error) {
	args := map[string]any{}
	if repoPath != "" {
		args["repo_path"] = repoPath
	}

	rawResult, err := c.CallToolRaw(ctx, ToolCheckConflicts, args)
	if err != nil {
		return nil, fmt.Errorf("check conflicts: %w", err)
	}

	var result ConflictResult
	if err := json.Unmarshal(rawResult, &result); err != nil {
		return nil, fmt.Errorf("parse conflict result: %w", err)
	}

	c.logger.Info("conflicts checked",
		"count", result.Count,
		"severity", result.Severity,
	)

	return &result, nil
}

// ─── List Dependencies ────────────────────────────────────────────────

// DependencyList holds dependency information for a repository.
type DependencyList struct {
	Dependencies []Dependency `json:"dependencies"`
	Total        int          `json:"total"`
	Outdated     int          `json:"outdated"`
	Vulnerable   int          `json:"vulnerable"`
}

type Dependency struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Latest    string `json:"latest"`
	Manager   string `json:"manager"` // "npm", "go", "pip", etc.
	Vulnerable bool  `json:"vulnerable"`
}

// ListDependencies returns the dependency tree for the repository.
func (c *Client) ListDependencies(ctx context.Context, repoPath string) (*DependencyList, error) {
	args := map[string]any{}
	if repoPath != "" {
		args["repo_path"] = repoPath
	}

	rawResult, err := c.CallToolRaw(ctx, ToolListDependencies, args)
	if err != nil {
		return nil, fmt.Errorf("list dependencies: %w", err)
	}

	var deps DependencyList
	if err := json.Unmarshal(rawResult, &deps); err != nil {
		return nil, fmt.Errorf("parse dependencies: %w", err)
	}

	return &deps, nil
}

// ─── Get Repo Metadata ────────────────────────────────────────────────

// RepoMetadata holds repository metadata.
type RepoMetadata struct {
	Name        string            `json:"name"`
	Path        string            `json:"path"`
	DefaultBranch string          `json:"default_branch"`
	HeadSHA     string            `json:"head_sha"`
	CommitsAhead int              `json:"commits_ahead"`
	CommitsBehind int             `json:"commits_behind"`
	LastCommit  time.Time         `json:"last_commit"`
	Tags        []string          `json:"tags,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// GetRepoMetadata returns metadata for the repository.
func (c *Client) GetRepoMetadata(ctx context.Context, repoPath string) (*RepoMetadata, error) {
	args := map[string]any{}
	if repoPath != "" {
		args["repo_path"] = repoPath
	}

	rawResult, err := c.CallToolRaw(ctx, ToolGetRepoMetadata, args)
	if err != nil {
		return nil, fmt.Errorf("get repo metadata: %w", err)
	}

	var meta RepoMetadata
	if err := json.Unmarshal(rawResult, &meta); err != nil {
		return nil, fmt.Errorf("parse repo metadata: %w", err)
	}

	return &meta, nil
}

// ─── List PRs ─────────────────────────────────────────────────────────

// PRList holds pull request information.
type PRList struct {
	PullRequests []PullRequest `json:"pull_requests"`
	Total        int           `json:"total"`
	Open         int           `json:"open"`
	Merged       int           `json:"merged"`
}

type PullRequest struct {
	Number   int       `json:"number"`
	Title    string    `json:"title"`
	Author   string    `json:"author"`
	State    string    `json:"state"` // "open", "closed", "merged"
	Base     string    `json:"base"`
	Head     string    `json:"head"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
}

// ListPRs returns open pull requests for the repository.
func (c *Client) ListPRs(ctx context.Context, repoPath string) (*PRList, error) {
	args := map[string]any{}
	if repoPath != "" {
		args["repo_path"] = repoPath
	}

	rawResult, err := c.CallToolRaw(ctx, ToolListPRs, args)
	if err != nil {
		return nil, fmt.Errorf("list prs: %w", err)
	}

	var prs PRList
	if err := json.Unmarshal(rawResult, &prs); err != nil {
		return nil, fmt.Errorf("parse pr list: %w", err)
	}

	return &prs, nil
}

// ─── Get Codeowners ───────────────────────────────────────────────────

// Codeowners holds CODEOWNERS file information.
type Codeowners struct {
	Entries []CodeownerEntry `json:"entries"`
	Exists  bool             `json:"exists"`
	Raw     string           `json:"raw,omitempty"`
}

type CodeownerEntry struct {
	Pattern string   `json:"pattern"`
	Owners  []string `json:"owners"`
}

// GetCodeowners returns the CODEOWNERS configuration for the repository.
func (c *Client) GetCodeowners(ctx context.Context, repoPath string) (*Codeowners, error) {
	args := map[string]any{}
	if repoPath != "" {
		args["repo_path"] = repoPath
	}

	rawResult, err := c.CallToolRaw(ctx, ToolGetCodeowners, args)
	if err != nil {
		return nil, fmt.Errorf("get codeowners: %w", err)
	}

	var codeowners Codeowners
	if err := json.Unmarshal(rawResult, &codeowners); err != nil {
		return nil, fmt.Errorf("parse codeowners: %w", err)
	}

	return &codeowners, nil
}

// ─── Check Branch Protection ──────────────────────────────────────────

// BranchProtection holds branch protection configuration.
type BranchProtection struct {
	Branch            string   `json:"branch"`
	Protected         bool     `json:"protected"`
	RequiredReviews   int      `json:"required_reviews"`
	RequireStatusChecks bool   `json:"require_status_checks"`
	RequireUpToDate   bool     `json:"require_up_to_date"`
	RestrictPushes    bool     `json:"restrict_pushes"`
	AllowForcePushes  bool     `json:"allow_force_pushes"`
	AllowDeletions    bool     `json:"allow_deletions"`
	StatusChecks      []string `json:"status_checks,omitempty"`
}

// CheckBranchProtection returns the branch protection rules for the repository.
func (c *Client) CheckBranchProtection(ctx context.Context, repoPath, branch string) (*BranchProtection, error) {
	args := map[string]any{}
	if repoPath != "" {
		args["repo_path"] = repoPath
	}
	if branch != "" {
		args["branch"] = branch
	}

	rawResult, err := c.CallToolRaw(ctx, ToolCheckBranchProtection, args)
	if err != nil {
		return nil, fmt.Errorf("check branch protection: %w", err)
	}

	var protection BranchProtection
	if err := json.Unmarshal(rawResult, &protection); err != nil {
		return nil, fmt.Errorf("parse branch protection: %w", err)
	}

	return &protection, nil
}

// ─── Audit Compliance ─────────────────────────────────────────────────

// ComplianceAudit holds the result of a compliance audit.
type ComplianceAudit struct {
	Passed      bool                `json:"passed"`
	Score       float64             `json:"score"` // 0.0 - 1.0
	Checks      []ComplianceCheck   `json:"checks"`
	TotalChecks int                 `json:"total_checks"`
	PassedChecks int                `json:"passed_checks"`
	FailedChecks int                `json:"failed_checks"`
	AuditedAt   time.Time           `json:"audited_at"`
}

type ComplianceCheck struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
	Severity string `json:"severity"` // "error", "warning", "info"
}

// AuditCompliance runs a compliance audit on the repository.
func (c *Client) AuditCompliance(ctx context.Context, repoPath string) (*ComplianceAudit, error) {
	args := map[string]any{}
	if repoPath != "" {
		args["repo_path"] = repoPath
	}

	rawResult, err := c.CallToolRaw(ctx, ToolAuditCompliance, args)
	if err != nil {
		return nil, fmt.Errorf("audit compliance: %w", err)
	}

	var audit ComplianceAudit
	if err := json.Unmarshal(rawResult, &audit); err != nil {
		return nil, fmt.Errorf("parse compliance audit: %w", err)
	}

	c.logger.Info("compliance audit complete",
		"passed", audit.Passed,
		"score", audit.Score,
		"total", audit.TotalChecks,
		"failed", audit.FailedChecks,
	)

	return &audit, nil
}
