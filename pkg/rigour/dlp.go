package rigour

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strings"
)

// DLPFilter implements a Data Loss Prevention pre-filter for the rigour supervisor.
// It runs before agents process input, catching secrets, PII, and high-entropy data.
type DLPFilter struct {
	logger       *slog.Logger
	entropyThresh float64 // Shannon entropy threshold (bits per character)
	secretPatterns []*regexp.Regexp
	piiPatterns    []*regexp.Regexp
	falsePositives []string // paths/patterns to exempt (e.g., test fixtures)
}

// DLPResult captures what the filter found.
type DLPResult struct {
	Blocked      bool
	Reasons      []string
	EntropyHits  []EntropyHit
	SecretHits   []SecretHit
	PIIHits      []PIIHit
	FalsePositiveExemptions int
}

// EntropyHit records a high-entropy substring.
type EntropyHit struct {
	Position  int
	Length    int
	Entropy   float64
	Snippet   string // redacted snippet for logging
}

// SecretHit records a detected secret pattern.
type SecretHit struct {
	Pattern string
	Line    int
	Offset  int
	Match   string // redacted match
}

// PIIHit records detected personally identifiable information.
type PIIHit struct {
	Type string // "email", "phone", "ssn", "credit_card", etc.
	Line int
	Match string // redacted
}

// NewDLPFilter creates a DLP filter with standard detection patterns.
func NewDLPFilter(logger *slog.Logger) *DLPFilter {
	f := &DLPFilter{
		logger:        logger.With("component", "dlp_filter"),
		entropyThresh: 4.5, // bits per character threshold
	}

	// Secret detection patterns
	f.secretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:api[_-]?key|apikey)\s*[:=]\s*['"]?([a-zA-Z0-9_\-]{16,})`),
		regexp.MustCompile(`(?i)(?:secret|token)\s*[:=]\s*['"]?([a-zA-Z0-9_\-\.]{16,})`),
		regexp.MustCompile(`(?i)(?:password|passwd|pwd)\s*[:=]\s*['"]?([^\s'"]{8,})`),
		regexp.MustCompile(`(?i)Bearer\s+[a-zA-Z0-9_\-\.]{20,}`),
		regexp.MustCompile(`(?i)(?:aws_secret_access_key|aws_secret)\s*[:=]\s*['"]?([a-zA-Z0-9/+]{40})`),
		regexp.MustCompile(`-----BEGIN\s+(?:RSA\s+)?PRIVATE\s+KEY-----`),
		regexp.MustCompile(`(?i)(?:client_secret|client_id)\s*[:=]\s*['"]?([a-zA-Z0-9_\-]{20,})`),
	}

	// PII detection patterns
	f.piiPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`),
		regexp.MustCompile(`\b(?:\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`),
		regexp.MustCompile(`\b\d{3}[-.\s]?\d{2}[-.\s]?\d{4}\b`),
		regexp.MustCompile(`\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13})\b`),
	}

	return f
}

// AddFalsePositive adds a path or pattern to the false-positive exemption list.
// Common uses: test fixtures, mock data, example configs.
func (f *DLPFilter) AddFalsePositive(pattern string) {
	f.falsePositives = append(f.falsePositives, pattern)
}

// SetEntropyThreshold adjusts the Shannon entropy detection threshold.
func (f *DLPFilter) SetEntropyThreshold(bits float64) {
	f.entropyThresh = bits
}

// Check runs the full DLP pre-filter on the given text.
// This is the rigour_hooks_check entry point.
func (f *DLPFilter) Check(ctx context.Context, text string, filePath string) *DLPResult {
	result := &DLPResult{}

	// Check false-positive exemptions first
	if f.isExempt(filePath) {
		result.FalsePositiveExemptions++
		f.logger.Debug("DLP exempt path", "path", filePath)
		return result
	}

	// Shannon entropy detection
	result.EntropyHits = f.detectEntropy(text)

	// Secret pattern detection
	result.SecretHits = f.detectSecrets(text)

	// PII detection
	result.PIIHits = f.detectPII(text)

	// Build reasons and determine if blocked
	if len(result.SecretHits) > 0 {
		for _, hit := range result.SecretHits {
			result.Reasons = append(result.Reasons, fmt.Sprintf("secret pattern detected: %s", hit.Pattern))
		}
		result.Blocked = true
	}

	if len(result.PIIHits) > 0 {
		for _, hit := range result.PIIHits {
			result.Reasons = append(result.Reasons, fmt.Sprintf("PII detected: %s", hit.Type))
		}
		result.Blocked = true
	}

	if len(result.EntropyHits) > 0 && len(result.SecretHits) == 0 {
		// High entropy alone is a warning, not a block, unless combined with secrets
		for _, hit := range result.EntropyHits {
			result.Reasons = append(result.Reasons, fmt.Sprintf("high entropy substring (bits=%.2f)", hit.Entropy))
		}
		// Only block on entropy if it's extremely high
		for _, hit := range result.EntropyHits {
			if hit.Entropy > 5.5 {
				result.Blocked = true
				break
			}
		}
	}

	if result.Blocked {
		f.logger.Warn("DLP check blocked",
			"path", filePath,
			"reasons", result.Reasons,
			"secret_hits", len(result.SecretHits),
			"pii_hits", len(result.PIIHits),
			"entropy_hits", len(result.EntropyHits),
		)
	} else if len(result.Reasons) > 0 {
		f.logger.Info("DLP check warnings",
			"path", filePath,
			"reasons", result.Reasons,
		)
	}

	return result
}

// isExempt checks if a file path matches any false-positive exemption pattern.
func (f *DLPFilter) isExempt(filePath string) bool {
	for _, pattern := range f.falsePositives {
		if strings.Contains(filePath, pattern) {
			return true
		}
		if matchGlobSimple(pattern, filePath) {
			return true
		}
	}
	return false
}

// matchGlobSimple does basic glob matching (* matches anything).
func matchGlobSimple(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(name, strings.TrimSuffix(pattern, "*"))
	}
	if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(name, strings.TrimPrefix(pattern, "*"))
	}
	return pattern == name
}

// detectEntropy uses a sliding window to find high-entropy substrings.
func (f *DLPFilter) detectEntropy(text string) []EntropyHit {
	var hits []EntropyHit
	windowSize := 20

	runes := []rune(text)
	if len(runes) < windowSize {
		return hits
	}

	for i := 0; i <= len(runes)-windowSize; i++ {
		window := string(runes[i : i+windowSize])
		e := shannonEntropy(window)
		if e > f.entropyThresh {
			hits = append(hits, EntropyHit{
				Position: i,
				Length:   windowSize,
				Entropy:  e,
				Snippet:  redact(window),
			})
			// Skip ahead to avoid overlapping windows
			i += windowSize - 1
		}
	}

	return hits
}

// shannonEntropy calculates the Shannon entropy of a string in bits per character.
func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}

	freq := make(map[rune]int)
	for _, c := range s {
		freq[c]++
	}

	length := float64(len([]rune(s)))
	entropy := 0.0
	for _, count := range freq {
		p := float64(count) / length
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}
	return entropy
}

// redact masks sensitive content for safe logging.
func redact(s string) string {
	if len(s) <= 6 {
		return "***"
	}
	return s[:3] + strings.Repeat("*", len(s)-6) + s[len(s)-3:]
}

// detectSecrets scans for known secret patterns.
func (f *DLPFilter) detectSecrets(text string) []SecretHit {
	var hits []SecretHit
	lines := strings.Split(text, "\n")

	for lineNum, line := range lines {
		for _, pattern := range f.secretPatterns {
			if loc := pattern.FindStringIndex(line); loc != nil {
				hits = append(hits, SecretHit{
					Pattern: pattern.String(),
					Line:    lineNum + 1,
					Offset:  loc[0],
					Match:   redact(line[loc[0]:loc[1]]),
				})
			}
		}
	}

	return hits
}

// detectPII scans for personally identifiable information.
func (f *DLPFilter) detectPII(text string) []PIIHit {
	var hits []PIIHit
	lines := strings.Split(text, "\n")

	type piiDef struct {
		pattern *regexp.Regexp
		typeName string
	}

	defs := []piiDef{
		{f.piiPatterns[0], "email"},
		{f.piiPatterns[1], "phone"},
		{f.piiPatterns[2], "ssn"},
		{f.piiPatterns[3], "credit_card"},
	}

	for lineNum, line := range lines {
		for _, def := range defs {
			if loc := def.pattern.FindStringIndex(line); loc != nil {
				hits = append(hits, PIIHit{
					Type:  def.typeName,
					Line:  lineNum + 1,
					Match: redact(line[loc[0]:loc[1]]),
				})
			}
		}
	}

	return hits
}
