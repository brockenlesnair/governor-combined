package rigour

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// FixPacket represents parsed rigour output in TEXT format.
// Rigour returns human-readable text, NOT JSON.
type FixPacket struct {
	GateName      string
	Severity      Severity
	Files         []FileTarget
	Instructions  []string
	Constraints   Constraints
	Verification  []string
	RawText       string
}

// Severity represents the urgency of a fix.
type Severity int

const (
	SeverityInfo     Severity = iota
	SeverityWarning
	SeverityError
	SeverityCritical
)

// String returns the severity label.
func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	case SeverityCritical:
		return "critical"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// ParseSeverity converts a string to Severity.
func ParseSeverity(s string) Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical", "crit":
		return SeverityCritical
	case "error", "err":
		return SeverityError
	case "warning", "warn":
		return SeverityWarning
	case "info", "information":
		return SeverityInfo
	default:
		return SeverityWarning
	}
}

// FileTarget identifies a file and optional line range within it.
type FileTarget struct {
	Path     string
	StartLine int
	EndLine   int
}

// HasRange returns true if a specific line range is specified.
func (ft FileTarget) HasRange() bool {
	return ft.StartLine > 0 || ft.EndLine > 0
}

// Constraints parsed from rigour output.
type Constraints struct {
	DoNotTouch   []string // protected paths
	MaxFiles     int      // maximum files to change (0 = unlimited)
	Paradigm     string   // required paradigm (e.g., "functional", "oop")
}

// Parser handles TEXT format parsing for rigour fix packets.
type Parser struct {
	// Compiled regexes for rigour TEXT format sections.
	reGateName   *regexp.Regexp
	reSeverity   *regexp.Regexp
	reFiles      *regexp.Regexp
	reLineRange  *regexp.Regexp
	reNumber     *regexp.Regexp
	reDoNotTouch *regexp.Regexp
	reMaxFiles   *regexp.Regexp
	reParadigm   *regexp.Regexp
	reVerify     *regexp.Regexp
}

// NewParser creates a Parser with pre-compiled regex patterns
// matching rigour's TEXT output format.
func NewParser() *Parser {
	return &Parser{
		reGateName:   regexp.MustCompile(`(?i)gate[:\s]+([a-zA-Z0-9_\-]+)`),
		reSeverity:   regexp.MustCompile(`(?i)severity[:\s]+(critical|error|warning|info|warn|err|crit|information)`),
		reFiles:      regexp.MustCompile(`(?i)files?[:\s]+(.+)`),
		reLineRange:  regexp.MustCompile(`[:/\\](\d+(?:\s*[-:]\s*\d+)?)`),
		reNumber:     regexp.MustCompile(`^\s*(\d+)[.)\s]\s+(.+)`),
		reDoNotTouch: regexp.MustCompile(`(?i)do_not_touch[:\s]+(.+)`),
		reMaxFiles:   regexp.MustCompile(`(?i)max_files[:\s]+(\d+)`),
		reParadigm:   regexp.MustCompile(`(?i)paradigm[:\s]+(\S+)`),
		reVerify:     regexp.MustCompile(`(?i)verif(?:y|ication)[:\s]+(.+)`),
	}
}

// Parse extracts a FixPacket from rigour's TEXT format output.
func (p *Parser) Parse(text string) (*FixPacket, error) {
	if text == "" {
		return nil, fmt.Errorf("empty fix packet text")
	}

	fp := &FixPacket{RawText: text}

	// Extract gate name
	if m := p.reGateName.FindStringSubmatch(text); m != nil {
		fp.GateName = strings.TrimSpace(m[1])
	}

	// Extract severity
	if m := p.reSeverity.FindStringSubmatch(text); m != nil {
		fp.Severity = ParseSeverity(m[1])
	} else {
		fp.Severity = SeverityWarning // default
	}

	// Extract files with optional line ranges
	fp.Files = p.parseFiles(text)

	// Extract numbered instructions
	fp.Instructions = p.parseInstructions(text)

	// Extract constraints
	fp.Constraints = p.parseConstraints(text)

	// Extract verification commands
	fp.Verification = p.parseVerification(text)

	if fp.GateName == "" && len(fp.Instructions) == 0 {
		return nil, fmt.Errorf("no recognizable rigour gate name or instructions found")
	}

	return fp, nil
}

// parseFiles extracts file targets with optional line ranges from text.
// Supports formats like:
//   - files: src/foo.go:10-20, src/bar.go
//   - file: config.yaml
func (p *Parser) parseFiles(text string) []FileTarget {
	var files []FileTarget

	// Find all file-related lines
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if !p.reFiles.MatchString(line) {
			continue
		}

		m := p.reFiles.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		// Split on comma for multiple files
		parts := strings.Split(m[1], ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}

			ft := FileTarget{}

			// Check for line range: path:10-20 or path:10
			rangeMatch := p.reLineRange.FindStringSubmatchIndex(part)
			if rangeMatch != nil {
				endIdx := rangeMatch[2] // start of match in part
				ft.Path = strings.TrimSpace(part[:endIdx])
				ft.StartLine, _ = strconv.Atoi(part[rangeMatch[2]:rangeMatch[3]])
				if rangeMatch[4] >= 0 && rangeMatch[5] >= 0 {
					ft.EndLine, _ = strconv.Atoi(part[rangeMatch[4]:rangeMatch[5]])
				}
				if ft.EndLine == 0 {
					ft.EndLine = ft.StartLine
				}
			} else {
				ft.Path = strings.TrimSpace(part)
			}

			if ft.Path != "" {
				files = append(files, ft)
			}
		}
	}

	return files
}

// parseInstructions extracts numbered instruction lines.
func (p *Parser) parseInstructions(text string) []string {
	var instructions []string
	lines := strings.Split(text, "\n")

	for _, line := range lines {
		if m := p.reNumber.FindStringSubmatch(line); m != nil {
			instructions = append(instructions, strings.TrimSpace(m[2]))
		}
	}

	return instructions
}

// parseConstraints extracts do_not_touch, max_files, and paradigm constraints.
func (p *Parser) parseConstraints(text string) Constraints {
	c := Constraints{
		MaxFiles: 0, // 0 means unlimited
	}

	if m := p.reDoNotTouch.FindStringSubmatch(text); m != nil {
		paths := strings.Split(m[1], ",")
		for i := range paths {
			paths[i] = strings.TrimSpace(paths[i])
		}
		c.DoNotTouch = paths
	}

	if m := p.reMaxFiles.FindStringSubmatch(text); m != nil {
		v, err := strconv.Atoi(m[1])
		if err == nil {
			c.MaxFiles = v
		}
	}

	if m := p.reParadigm.FindStringSubmatch(text); m != nil {
		c.Paradigm = strings.TrimSpace(m[1])
	}

	return c
}

// parseVerification extracts verification commands.
func (p *Parser) parseVerification(text string) []string {
	var cmds []string
	lines := strings.Split(text, "\n")

	for _, line := range lines {
		if m := p.reVerify.FindStringSubmatch(line); m != nil {
			cmds = append(cmds, strings.TrimSpace(m[1]))
		}
	}

	return cmds
}

// IsEmpty returns true if the packet has no actionable content.
func (fp *FixPacket) IsEmpty() bool {
	return fp.GateName == "" && len(fp.Instructions) == 0 && len(fp.Files) == 0
}
