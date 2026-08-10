package hangar

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCircuitBreaker(t *testing.T) {
	cb := newCircuitBreaker(CircuitBreakerConfig{
		Threshold:    3,
		HalfOpenMax:  2,
		RecoveryTime: 1 * time.Millisecond,
	})

	// Initially closed — should allow
	if !cb.allow() {
		t.Error("closed breaker should allow")
	}
	if cb.stateName() != "closed" {
		t.Errorf("expected closed state, got %s", cb.stateName())
	}

	// Record 3 failures to open
	cb.recordFailure()
	cb.recordFailure()
	cb.recordFailure()

	if cb.stateName() != "open" {
		t.Errorf("expected open state after 3 failures, got %s", cb.stateName())
	}

	// Open breaker should not allow
	if cb.allow() {
		t.Error("open breaker should not allow")
	}

	// Wait for recovery time
	time.Sleep(2 * time.Millisecond)

	// Should transition to half-open on allow
	if !cb.allow() {
		t.Error("half-open breaker should allow probe")
	}
	if cb.stateName() != "half-open" {
		t.Errorf("expected half-open state, got %s", cb.stateName())
	}

	// Record success to close
	cb.recordSuccess()
	cb.recordSuccess()

	if cb.stateName() != "closed" {
		t.Errorf("expected closed state after recovery, got %s", cb.stateName())
	}
}

func TestCircuitBreaker_HalfOpen_FailureGoesOpen(t *testing.T) {
	cb := newCircuitBreaker(CircuitBreakerConfig{
		Threshold:    2,
		HalfOpenMax:  2,
		RecoveryTime: 1 * time.Millisecond,
	})

	cb.recordFailure()
	cb.recordFailure()
	if cb.stateName() != "open" {
		t.Fatalf("expected open, got %s", cb.stateName())
	}

	time.Sleep(2 * time.Millisecond)
	cb.allow() // transition to half-open

	// Failure in half-open goes back to open
	cb.recordFailure()
	if cb.stateName() != "open" {
		t.Errorf("half-open failure should go to open, got %s", cb.stateName())
	}
}

func TestGetScorecard(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/fleet/scorecard" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		page := ScorecardPage{
			Repos: []RepoScore{
				{RepoID: "r1", RepoName: "repo1", Score: 8.0, MaxScore: 10.0, ConnectionID: "conn-1"},
				{RepoID: "r2", RepoName: "repo2", Score: 6.0, MaxScore: 10.0, ConnectionID: "conn-2"},
			},
			Page:       1,
			PageSize:   100,
			TotalCount: 2,
			HasMore:    false,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(page)
	}))
	defer ts.Close()

	client, err := NewClient(Config{
		BaseURL: ts.URL,
		APIKey:  "test-key",
	}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	scorecard, err := client.GetFleetScorecard(t.Context(), 1, 100)
	if err != nil {
		t.Fatal(err)
	}

	if len(scorecard.Repos) != 2 {
		t.Errorf("expected 2 repos, got %d", len(scorecard.Repos))
	}
	if scorecard.TotalCount != 2 {
		t.Errorf("expected total count 2, got %d", scorecard.TotalCount)
	}
	if scorecard.AverageScore != 0.7 {
		t.Errorf("expected average score 0.7, got %f", scorecard.AverageScore)
	}
}

func TestGetScorecard_Pagination(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		hasMore := callCount < 2
		page := ScorecardPage{
			Repos: []RepoScore{
				{RepoID: fmt.Sprintf("r%d", callCount), Score: 5.0, MaxScore: 10.0},
			},
			Page:       callCount,
			PageSize:   1,
			TotalCount: 2,
			HasMore:    hasMore,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(page)
	}))
	defer ts.Close()

	client, err := NewClient(Config{
		BaseURL: ts.URL,
		APIKey:  "test-key",
		Fleet:   FleetConfig{PageSize: 1},
	}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	scorecard, err := client.GetFleetScorecard(t.Context(), 0, 0) // auto-paginate
	if err != nil {
		t.Fatal(err)
	}

	if len(scorecard.Repos) != 2 {
		t.Errorf("expected 2 repos from pagination, got %d", len(scorecard.Repos))
	}
}

func TestComputeFleetMetrics(t *testing.T) {
	scorecard := &FleetScorecard{
		Repos: []RepoScore{
			{
				RepoID: "r1", Score: 9.0, MaxScore: 10.0, ConnectionID: "github-1",
				Checks: []CheckResult{
					{CheckID: "c1", Passed: true},
					{CheckID: "c2", Passed: false},
				},
			},
			{
				RepoID: "r2", Score: 4.0, MaxScore: 10.0, ConnectionID: "github-2",
				Checks: []CheckResult{
					{CheckID: "c1", Passed: true},
				},
			},
		},
		AverageScore: 0.65,
	}

	metrics := ComputeFleetMetrics(scorecard)

	if metrics.TotalRepos != 2 {
		t.Errorf("expected 2 repos, got %d", metrics.TotalRepos)
	}
	if metrics.AverageScore != 0.65 {
		t.Errorf("expected average 0.65, got %f", metrics.AverageScore)
	}
	if metrics.PassingRepos != 1 {
		t.Errorf("expected 1 passing repo, got %d", metrics.PassingRepos)
	}
	if metrics.FailingRepos != 1 {
		t.Errorf("expected 1 failing repo, got %d", metrics.FailingRepos)
	}
	if metrics.TotalChecks != 3 {
		t.Errorf("expected 3 total checks, got %d", metrics.TotalChecks)
	}
	if metrics.PassingChecks != 2 {
		t.Errorf("expected 2 passing checks, got %d", metrics.PassingChecks)
	}
	if metrics.FailingChecks != 1 {
		t.Errorf("expected 1 failing check, got %d", metrics.FailingChecks)
	}
	if metrics.ConnectionCount != 2 {
		t.Errorf("expected 2 connections, got %d", metrics.ConnectionCount)
	}
}

func TestComputeFleetMetrics_Empty(t *testing.T) {
	metrics := ComputeFleetMetrics(nil)
	if metrics.TotalRepos != 0 {
		t.Errorf("expected 0 repos, got %d", metrics.TotalRepos)
	}

	metrics = ComputeFleetMetrics(&FleetScorecard{})
	if metrics.TotalRepos != 0 {
		t.Errorf("expected 0 repos for empty scorecard, got %d", metrics.TotalRepos)
	}
}

func TestDetectConnectionChanges(t *testing.T) {
	before := []RepoScore{
		{RepoID: "r1", RepoName: "repo1", ConnectionID: "github-1"},
		{RepoID: "r2", RepoName: "repo2", ConnectionID: "github-2"},
		{RepoID: "r3", RepoName: "repo3", ConnectionID: "gitea-1"},
	}

	after := []RepoScore{
		{RepoID: "r1", RepoName: "repo1", ConnectionID: "github-1"},
		{RepoID: "r2", RepoName: "repo2", ConnectionID: "gitea-2"},
		{RepoID: "r3", RepoName: "repo3", ConnectionID: "gitea-1"},
		{RepoID: "r4", RepoName: "repo4", ConnectionID: "github-3"},
	}

	changes := DetectConnectionChanges(before, after)

	if len(changes) != 1 {
		t.Fatalf("expected 1 connection change, got %d", len(changes))
	}
	if changes[0].RepoID != "r2" {
		t.Errorf("expected repo r2, got %s", changes[0].RepoID)
	}
	if changes[0].OldConnID != "github-2" {
		t.Errorf("expected old conn github-2, got %s", changes[0].OldConnID)
	}
	if changes[0].NewConnID != "gitea-2" {
		t.Errorf("expected new conn gitea-2, got %s", changes[0].NewConnID)
	}
}

func TestDetectConnectionChanges_NoChanges(t *testing.T) {
	repos := []RepoScore{
		{RepoID: "r1", ConnectionID: "github-1"},
		{RepoID: "r2", ConnectionID: "github-2"},
	}

	changes := DetectConnectionChanges(repos, repos)
	if len(changes) != 0 {
		t.Errorf("expected no changes, got %d", len(changes))
	}
}

func TestAnalyzeRemediation(t *testing.T) {
	resp := &RemediateResponse{
		RemediationID: "rem-1",
		TotalRepos:    10,
		Succeeded:     7,
		Failed:        2,
		Skipped:       1,
		Results: []RemediationResult{
			{RepoID: "r1", Status: "success"},
			{RepoID: "r2", Status: "failed", Message: "permission denied"},
			{RepoID: "r3", Status: "success"},
			{RepoID: "r4", Status: "failed", Message: "repo not found"},
		},
	}

	summary := AnalyzeRemediation(resp)

	if summary.Total != 10 {
		t.Errorf("expected total 10, got %d", summary.Total)
	}
	if summary.Succeeded != 7 {
		t.Errorf("expected succeeded 7, got %d", summary.Succeeded)
	}
	if summary.Failed != 2 {
		t.Errorf("expected failed 2, got %d", summary.Failed)
	}
	if summary.Skipped != 1 {
		t.Errorf("expected skipped 1, got %d", summary.Skipped)
	}
	if !summary.IsPartial {
		t.Error("expected partial remediation")
	}
	if summary.IsComplete {
		t.Error("should not be complete with failures")
	}
	if len(summary.FailedRepos) != 2 {
		t.Errorf("expected 2 failed repos, got %d", len(summary.FailedRepos))
	}
}

func TestAnalyzeRemediation_CompleteSuccess(t *testing.T) {
	resp := &RemediateResponse{
		RemediationID: "rem-2",
		TotalRepos:    5,
		Succeeded:     5,
		Failed:        0,
	}

	summary := AnalyzeRemediation(resp)

	if !summary.IsComplete {
		t.Error("should be complete with no failures")
	}
	if summary.IsPartial {
		t.Error("should not be partial with no failures")
	}
}

func TestIsTransientRemediationError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil", nil, false},
		{"timeout", fmt.Errorf("connection timeout"), true},
		{"reset", fmt.Errorf("connection reset by peer"), true},
		{"eof", fmt.Errorf("unexpected EOF"), true},
		{"broken pipe", fmt.Errorf("broken pipe"), true},
		{"io timeout", fmt.Errorf("i/o timeout"), true},
		{"permanent", fmt.Errorf("repo not found"), false},
		{"permission", fmt.Errorf("permission denied"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTransientRemediationError(tt.err); got != tt.expected {
				t.Errorf("IsTransientRemediationError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestAverageScore(t *testing.T) {
	repos := []RepoScore{
		{Score: 8.0, MaxScore: 10.0},
		{Score: 6.0, MaxScore: 10.0},
	}

	avg := averageScore(repos)
	expected := (0.8 + 0.6) / 2.0
	if avg != expected {
		t.Errorf("expected average %f, got %f", expected, avg)
	}
}

func TestAverageScore_Empty(t *testing.T) {
	avg := averageScore(nil)
	if avg != 0 {
		t.Errorf("expected 0 for empty repos, got %f", avg)
	}
}

func TestParseTotalCount(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{
			"X-Total-Count": []string{"42"},
		},
	}
	if n := parseTotalCount(resp); n != 42 {
		t.Errorf("expected 42, got %d", n)
	}

	resp2 := &http.Response{Header: http.Header{}}
	if n := parseTotalCount(resp2); n != 0 {
		t.Errorf("expected 0 for missing header, got %d", n)
	}
}

func testLogger() *slog.Logger {
	return slog.Default()
}
