package search

import (
	"context"
	"strings"

	"github.com/brockenlesnair/governor-combined/pkg/callgraph"
)

type TypeFilter struct {
	Params     []string
	Return     string
	Receiver   string
	ExactMatch bool
}

func MatchType(ref *callgraph.Node, filter *TypeFilter) bool {
	if filter == nil {
		return true
	}

	sig := ref.Signature
	if sig == "" {
		return false
	}

	if filter.Receiver != "" && ref.Kind == callgraph.NodeKindMethod {
		if !matchTypePattern(sig, filter.Receiver, filter.ExactMatch) {
			return false
		}
	}

	if len(filter.Params) > 0 {
		params := extractParamTypes(sig)
		if !paramsMatch(params, filter.Params, filter.ExactMatch) {
			return false
		}
	}

	if filter.Return != "" {
		returns := extractReturnTypes(sig)
		if !returnMatches(returns, filter.Return, filter.ExactMatch) {
			return false
		}
	}

	return true
}

func extractParamTypes(sig string) []string {
	start := strings.Index(sig, "(")
	if start == -1 {
		return nil
	}
	end := strings.Index(sig[start:], ")")
	if end == -1 {
		return nil
	}
	paramStr := sig[start+1 : start+end]

	var params []string
	for _, p := range strings.Split(paramStr, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		parts := strings.Fields(p)
		if len(parts) >= 2 {
			params = append(params, parts[len(parts)-1])
		} else {
			params = append(params, p)
		}
	}
	return params
}

func extractReturnTypes(sig string) []string {
	// Find the parameter list's closing ')' by counting nesting
	start := strings.Index(sig, "(")
	if start == -1 {
		return nil
	}
	depth := 0
	end := -1
	for i := start; i < len(sig); i++ {
		if sig[i] == '(' {
			depth++
		} else if sig[i] == ')' {
			depth--
			if depth == 0 {
				end = i
				break
			}
		}
	}
	if end == -1 || end+1 >= len(sig) {
		return nil
	}

	rest := strings.TrimSpace(sig[end+1:])
	if rest == "" || rest == "{" {
		return nil
	}

	// Handle parenthesized multiple returns: "(string, error)"
	rest = strings.TrimPrefix(rest, "(")
	rest = strings.TrimSuffix(rest, ")")
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil
	}

	var returns []string
	for _, r := range strings.Split(rest, ",") {
		r = strings.TrimSpace(r)
		if r != "" {
			returns = append(returns, r)
		}
	}
	return returns
}

func paramsMatch(actual []string, filter []string, exact bool) bool {
	if len(filter) > len(actual) {
		return false
	}
	for _, f := range filter {
		found := false
		for _, a := range actual {
			if matchTypePattern(a, f, exact) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func returnMatches(actual []string, filter string, exact bool) bool {
	for _, a := range actual {
		if matchTypePattern(a, filter, exact) {
			return true
		}
	}
	return false
}

func matchTypePattern(actual, pattern string, exact bool) bool {
	if exact {
		return actual == pattern
	}
	return strings.Contains(actual, pattern)
}

func findBySignature(ctx context.Context, nodes map[string]*callgraph.Node, filter *TypeFilter) []*callgraph.Node {
	var results []*callgraph.Node
	for _, ref := range nodes {
		select {
		case <-ctx.Done():
			return results
		default:
		}
		if MatchType(ref, filter) {
			results = append(results, ref)
		}
	}
	return results
}
