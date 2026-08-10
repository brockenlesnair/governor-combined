package search

import (
	"sort"

	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
)

type Ranker struct {
	graphSearch *GraphSearch
}

func NewRanker(gs *GraphSearch) *Ranker {
	return &Ranker{graphSearch: gs}
}

func (r *Ranker) ScoreResult(match FuzzyMatch, ref *callgraph.Node) float64 {
	baseScore := match.Score

	callerCount := r.graphSearch.CallerCount(ref.ID)
	popScore := float64(callerCount) / 100.0
	if popScore > 1.0 {
		popScore = 1.0
	}

	exportBonus := 0.0
	if ref.Kind == callgraph.NodeKindFunction || ref.Kind == callgraph.NodeKindMethod {
		if len(ref.Name) > 0 && ref.Name[0] >= 'A' && ref.Name[0] <= 'Z' {
			exportBonus = 1.0
		}
	}

	kindBonus := 0.5
	switch ref.Kind {
	case callgraph.NodeKindFunction:
		kindBonus = 1.0
	case callgraph.NodeKindMethod:
		kindBonus = 0.8
	case callgraph.NodeKindType:
		kindBonus = 0.6
	case callgraph.NodeKindPackage:
		kindBonus = 0.4
	}

	score := baseScore*0.4 +
		popScore*0.25 +
		exportBonus*0.15 +
		kindBonus*0.1

	switch match.Kind {
	case "exact":
		score += 0.1
	case "prefix":
		score += 0.05
	}

	return clampScore(score)
}

func (r *Ranker) RankAndSort(matches []FuzzyMatch, nodes map[string]*callgraph.Node) []Result {
	var results []Result
	for _, m := range matches {
		var ref *callgraph.Node
		for _, n := range nodes {
			if n.Name == m.Name || n.Short() == m.Name {
				ref = n
				break
			}
		}
		if ref == nil {
			continue
		}

		score := r.ScoreResult(m, ref)
		results = append(results, Result{
			Entity:    *ref,
			Score:     score,
			MatchType: m.Kind,
			Callers:   r.graphSearch.CallerCount(ref.ID),
			Callees:   r.graphSearch.CalleeCount(ref.ID),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

func clampScore(s float64) float64 {
	if s < 0 {
		return 0
	}
	if s > 1.0 {
		return 1.0
	}
	return s
}
