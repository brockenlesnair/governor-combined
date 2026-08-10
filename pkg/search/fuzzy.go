package search

import (
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

type FuzzyMatcher struct {
	trigrams map[string]map[string]bool
}

type FuzzyMatch struct {
	Name  string
	Score float64
	Kind  string
}

func NewFuzzyMatcher(names []string) *FuzzyMatcher {
	fm := &FuzzyMatcher{
		trigrams: make(map[string]map[string]bool),
	}
	for i, name := range names {
		for _, tri := range extractTrigrams(name) {
			if fm.trigrams[tri] == nil {
				fm.trigrams[tri] = make(map[string]bool)
			}
			fm.trigrams[tri][names[i]] = true
		}
	}
	return fm
}

func (fm *FuzzyMatcher) Match(query string, threshold float64) []FuzzyMatch {
	queryTrigrams := extractTrigrams(strings.ToLower(query))

	candidateSet := make(map[string]int)
	for _, tri := range queryTrigrams {
		for name := range fm.trigrams[tri] {
			candidateSet[name]++
		}
	}

	var matches []FuzzyMatch
	for name, overlap := range candidateSet {
		sim := trigramSimilarity(query, name, overlap)
		lev := levenshteinDistance(strings.ToLower(query), strings.ToLower(name))
		maxLen := math.Max(float64(utf8.RuneCountInString(query)), float64(utf8.RuneCountInString(name)))
		if maxLen == 0 {
			continue
		}
		score := 1.0 - float64(lev)/maxLen
		score = score*0.7 + sim*0.3

		if score >= threshold {
			matches = append(matches, FuzzyMatch{
				Name:  name,
				Score: score,
				Kind:  matchKind(name, query),
			})
		}
	}

	sortMatchesByScore(matches)
	return matches
}

func extractTrigrams(s string) []string {
	s = strings.ToLower(s)
	runes := []rune(s)
	if len(runes) < 3 {
		return []string{s}
	}
	var tris []string
	for i := 0; i <= len(runes)-3; i++ {
		tris = append(tris, string(runes[i:i+3]))
	}
	return tris
}

func trigramSimilarity(query, candidate string, overlap int) float64 {
	qTris := len(extractTrigrams(strings.ToLower(query)))
	cTris := len(extractTrigrams(strings.ToLower(candidate)))
	if qTris == 0 || cTris == 0 {
		return 0
	}
	union := qTris + cTris - overlap
	return float64(overlap) / float64(union)
}

func matchKind(name, query string) string {
	lowerName := strings.ToLower(name)
	lowerQuery := strings.ToLower(query)
	if lowerName == lowerQuery {
		return "exact"
	}
	if strings.HasPrefix(lowerName, lowerQuery) {
		return "prefix"
	}
	if strings.Contains(lowerName, lowerQuery) {
		return "substring"
	}
	return "fuzzy"
}

func levenshteinDistance(a, b string) int {
	runesA := []rune(a)
	runesB := []rune(b)
	lenA := len(runesA)
	lenB := len(runesB)

	if lenA == 0 {
		return lenB
	}
	if lenB == 0 {
		return lenA
	}

	if lenA < lenB {
		runesA, runesB = runesB, runesA
		lenA, lenB = lenB, lenA
	}

	prev := make([]int, lenB+1)
	curr := make([]int, lenB+1)

	for j := 0; j <= lenB; j++ {
		prev[j] = j
	}

	for i := 1; i <= lenA; i++ {
		curr[0] = i
		for j := 1; j <= lenB; j++ {
			cost := 1
			if runesA[i-1] == runesB[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}

	return prev[lenB]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

func sortMatchesByScore(matches []FuzzyMatch) {
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})
}
