package httpproxy

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// RewriteRule defines a header transformation.
type RewriteRule struct {
	Match   string `json:"match"`
	Replace string `json:"replace"`
	Action  string `json:"action"`
	NewName string `json:"new_name"`
}

// HeaderRewriter applies rules to HTTP headers.
type HeaderRewriter struct {
	rules    []RewriteRule
	compiled []*regexp.Regexp
}

// NewHeaderRewriter creates a rewriter with the given rules.
func NewHeaderRewriter(rules []RewriteRule) (*HeaderRewriter, error) {
	hr := &HeaderRewriter{rules: rules}

	for _, rule := range rules {
		if rule.Match == "" {
			continue
		}
		re, err := regexp.Compile(rule.Match)
		if err != nil {
			return nil, fmt.Errorf("compile regex %q: %w", rule.Match, err)
		}
		hr.compiled = append(hr.compiled, re)
	}

	return hr, nil
}

// Rewrite applies all rules to the given headers.
func (hr *HeaderRewriter) Rewrite(in http.Header) http.Header {
	out := in.Clone()

	for i, rule := range hr.rules {
		var re *regexp.Regexp
		if i < len(hr.compiled) {
			re = hr.compiled[i]
		}

		switch rule.Action {
		case "remove":
			for name := range out {
				if re != nil && re.MatchString(name) {
					delete(out, name)
				} else if strings.EqualFold(name, rule.Match) {
					delete(out, name)
				}
			}

		case "rename":
			for name, values := range out {
				matched := false
				if re != nil {
					matched = re.MatchString(name)
				} else {
					matched = strings.EqualFold(name, rule.Match)
				}
				if matched {
					delete(out, name)
					out[rule.NewName] = values
				}
			}

		case "append":
			for name, values := range out {
				matched := false
				if re != nil {
					matched = re.MatchString(name)
				} else {
					matched = strings.EqualFold(name, rule.Match)
				}
				if matched {
					out[name] = append(values, rule.Replace)
				}
			}

		case "set", "":
			found := false
			for name := range out {
				matched := false
				if re != nil {
					matched = re.MatchString(name)
				} else {
					matched = strings.EqualFold(name, rule.Match)
				}
				if matched {
					out[name] = []string{rule.Replace}
					found = true
				}
			}
			if !found && re == nil {
				out[rule.Match] = []string{rule.Replace}
			}
		}
	}

	return out
}
