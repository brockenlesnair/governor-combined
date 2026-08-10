package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ─── Health Tier ──────────────────────────────────────────────────────

// HealthTier represents the overall health of a repository.
type HealthTier string

const (
	TierHealthy  HealthTier = "healthy"  // score >= 0.8
	TierDegraded HealthTier = "degraded" // 0.5 <= score < 0.8
	TierCritical HealthTier = "critical" // score < 0.5
	TierUnknown  HealthTier = "unknown"
)

// HealthTierResult holds the health tier assessment.
type HealthTierResult struct {
	Tier        HealthTier       `json:"tier"`
	Score       float64          `json:"score"` // 0.0 - 1.0
	Components  []HealthComponent `json:"components"`
	AssessedAt  time.Time        `json:"assessed_at"`
}

type HealthComponent struct {
	Name    string  `json:"name"`
	Score   float64 `json:"score"`
	MaxScore float64 `json:"max_score"`
	Status  string  `json:"status"` // "pass", "warn", "fail"
	Message string  `json:"message,omitempty"`
}

// GetHealthTier returns the health tier for a repository.
func (c *Client) GetHealthTier(ctx context.Context, repoPath string) (*HealthTierResult, error) {
	args := map[string]any{}
	if repoPath != "" {
		args["repo_path"] = repoPath
	}

	rawResult, err := c.CallToolRaw(ctx, ToolGetHealthTier, args)
	if err != nil {
		return nil, fmt.Errorf("get health tier: %w", err)
	}

	var result HealthTierResult
	if err := json.Unmarshal(rawResult, &result); err != nil {
		return nil, fmt.Errorf("parse health tier: %w", err)
	}

	c.logger.Info("health tier assessed",
		"tier", result.Tier,
		"score", result.Score,
		"components", len(result.Components),
	)

	return &result, nil
}

// ─── Campaign Status ──────────────────────────────────────────────────

// CampaignStatus represents the status of a governance campaign.
type CampaignStatus struct {
	CampaignID   string            `json:"campaign_id"`
	Name         string            `json:"name"`
	Status       string            `json:"status"` // "active", "completed", "paused", "failed"
	Progress     float64           `json:"progress"` // 0.0 - 1.0
	TotalRepos   int               `json:"total_repos"`
	CompletedRepos int             `json:"completed_repos"`
	FailedRepos  int               `json:"failed_repos"`
	SkippedRepos int               `json:"skipped_repos"`
	StartedAt    time.Time         `json:"started_at"`
	CompletedAt  *time.Time        `json:"completed_at,omitempty"`
	LastUpdated  time.Time         `json:"last_updated"`
	Results      []CampaignResult  `json:"results,omitempty"`
}

type CampaignResult struct {
	RepoPath string `json:"repo_path"`
	Status   string `json:"status"` // "passed", "failed", "skipped"
	Score    float64 `json:"score,omitempty"`
	Error    string `json:"error,omitempty"`
}

// GetCampaignStatus returns the status of a governance campaign.
func (c *Client) GetCampaignStatus(ctx context.Context, campaignID string) (*CampaignStatus, error) {
	args := map[string]any{
		"campaign_id": campaignID,
	}

	rawResult, err := c.CallToolRaw(ctx, ToolGetCampaignStatus, args)
	if err != nil {
		return nil, fmt.Errorf("get campaign status: %w", err)
	}

	var status CampaignStatus
	if err := json.Unmarshal(rawResult, &status); err != nil {
		return nil, fmt.Errorf("parse campaign status: %w", err)
	}

	c.logger.Info("campaign status retrieved",
		"campaign_id", status.CampaignID,
		"status", status.Status,
		"progress", status.Progress,
		"completed", status.CompletedRepos,
		"total", status.TotalRepos,
	)

	return &status, nil
}

// ─── Query Portfolio ──────────────────────────────────────────────────

// PortfolioQuery is the arguments for query_portfolio.
type PortfolioQuery struct {
	// Filter by health tier.
	HealthTier HealthTier `json:"health_tier,omitempty"`

	// Filter by campaign ID.
	CampaignID string `json:"campaign_id,omitempty"`

	// Filter by labels.
	Labels map[string]string `json:"labels,omitempty"`

	// Sort by field.
	SortBy string `json:"sort_by,omitempty"` // "score", "name", "updated"

	// Sort order.
	SortOrder string `json:"sort_order,omitempty"` // "asc", "desc"

	// Limit results.
	Limit int `json:"limit,omitempty"`

	// Offset for pagination.
	Offset int `json:"offset,omitempty"`
}

// PortfolioResult holds the portfolio query result.
type PortfolioResult struct {
	Repos      []PortfolioRepo `json:"repos"`
	Total      int             `json:"total"`
	HasMore    bool            `json:"has_more"`
	Summary    PortfolioSummary `json:"summary"`
}

type PortfolioRepo struct {
	Path        string     `json:"path"`
	Name        string     `json:"name"`
	HealthTier  HealthTier `json:"health_tier"`
	Score       float64    `json:"score"`
	LastUpdated time.Time  `json:"last_updated"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type PortfolioSummary struct {
	TotalRepos     int            `json:"total_repos"`
	HealthyRepos   int            `json:"healthy_repos"`
	DegradedRepos  int            `json:"degraded_repos"`
	CriticalRepos  int            `json:"critical_repos"`
	AverageScore   float64        `json:"average_score"`
	TierDistribution map[HealthTier]int `json:"tier_distribution"`
}

// QueryPortfolio queries the portfolio with filtering and aggregation.
func (c *Client) QueryPortfolio(ctx context.Context, query PortfolioQuery) (*PortfolioResult, error) {
	args := map[string]any{}
	if query.HealthTier != "" {
		args["health_tier"] = string(query.HealthTier)
	}
	if query.CampaignID != "" {
		args["campaign_id"] = query.CampaignID
	}
	if len(query.Labels) > 0 {
		args["labels"] = query.Labels
	}
	if query.SortBy != "" {
		args["sort_by"] = query.SortBy
	}
	if query.SortOrder != "" {
		args["sort_order"] = query.SortOrder
	}
	if query.Limit > 0 {
		args["limit"] = query.Limit
	}
	if query.Offset > 0 {
		args["offset"] = query.Offset
	}

	rawResult, err := c.CallToolRaw(ctx, ToolQueryPortfolio, args)
	if err != nil {
		return nil, fmt.Errorf("query portfolio: %w", err)
	}

	var result PortfolioResult
	if err := json.Unmarshal(rawResult, &result); err != nil {
		return nil, fmt.Errorf("parse portfolio result: %w", err)
	}

	c.logger.Info("portfolio queried",
		"total", result.Total,
		"has_more", result.HasMore,
		"average_score", result.Summary.AverageScore,
		"healthy", result.Summary.HealthyRepos,
		"degraded", result.Summary.DegradedRepos,
		"critical", result.Summary.CriticalRepos,
	)

	return &result, nil
}

// ─── Portfolio Aggregation ────────────────────────────────────────────

// AggregatePortfolioHealth computes aggregate health metrics across all repos.
func AggregatePortfolioHealth(repos []PortfolioRepo) PortfolioSummary {
	summary := PortfolioSummary{
		TotalRepos:       len(repos),
		TierDistribution: make(map[HealthTier]int),
	}

	if len(repos) == 0 {
		return summary
	}

	totalScore := 0.0
	for _, repo := range repos {
		totalScore += repo.Score
		summary.TierDistribution[repo.HealthTier]++

		switch repo.HealthTier {
		case TierHealthy:
			summary.HealthyRepos++
		case TierDegraded:
			summary.DegradedRepos++
		case TierCritical:
			summary.CriticalRepos++
		}
	}

	summary.AverageScore = totalScore / float64(len(repos))

	return summary
}

// FilterByTier returns repos matching the given health tier.
func FilterByTier(repos []PortfolioRepo, tier HealthTier) []PortfolioRepo {
	var filtered []PortfolioRepo
	for _, repo := range repos {
		if repo.HealthTier == tier {
			filtered = append(filtered, repo)
		}
	}
	return filtered
}

// SortByScore sorts repos by score in descending order.
func SortByScore(repos []PortfolioRepo) {
	// Simple insertion sort for small lists
	for i := 1; i < len(repos); i++ {
		key := repos[i]
		j := i - 1
		for j >= 0 && repos[j].Score < key.Score {
			repos[j+1] = repos[j]
			j--
		}
		repos[j+1] = key
	}
}

// ─── Cross-Repo Health Aggregation ────────────────────────────────────

// CrossRepoHealth aggregates health data across multiple repositories.
type CrossRepoHealth struct {
	Repos       []HealthTierResult `json:"repos"`
	OverallTier HealthTier         `json:"overall_tier"`
	OverallScore float64           `json:"overall_score"`
	WorstRepos  []string           `json:"worst_repos"` // bottom 5
	BestRepos   []string           `json:"best_repos"`  // top 5
	AggregatedAt time.Time         `json:"aggregated_at"`
}

// AggregateCrossRepoHealth combines health data from multiple repos.
func AggregateCrossRepoHealth(results []HealthTierResult) CrossRepoHealth {
	agg := CrossRepoHealth{
		Repos:        results,
		AggregatedAt: time.Now(),
	}

	if len(results) == 0 {
		return agg
	}

	totalScore := 0.0
	type repoScore struct {
		path  string
		score float64
	}
	var scores []repoScore

	for _, r := range results {
		totalScore += r.Score
		name := ""
		if len(r.Components) > 0 {
			name = r.Components[0].Name
		}
		scores = append(scores, repoScore{name, r.Score})
	}
	agg.OverallScore = totalScore / float64(len(results))

	// Determine overall tier
	switch {
	case agg.OverallScore >= 0.8:
		agg.OverallTier = TierHealthy
	case agg.OverallScore >= 0.5:
		agg.OverallTier = TierDegraded
	default:
		agg.OverallTier = TierCritical
	}

	for i := 0; i < len(scores); i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[j].score > scores[i].score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	// Top 5 and bottom 5
	n := len(scores)
	if n > 5 {
		n = 5
	}
	for i := 0; i < n; i++ {
		agg.BestRepos = append(agg.BestRepos, scores[i].path)
		agg.WorstRepos = append(agg.WorstRepos, scores[len(scores)-1-i].path)
	}

	return agg
}
