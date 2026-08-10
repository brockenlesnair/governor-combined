package rigour

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Strategy determines how to apply a fix packet based on the issue type.
type Strategy int

const (
	StrategyASTGre      Strategy = iota // Use ast-grep for known patterns
	StrategyCreateFile                  // Create missing files/directories
	StrategyDelegate                    // Delegate back to agent with fix packet
	StrategySkip                        // Skip (no action needed)
)

// String returns the strategy label.
func (s Strategy) String() string {
	switch s {
	case StrategyASTGre:
		return "ast-grep"
	case StrategyCreateFile:
		return "create_file"
	case StrategyDelegate:
		return "delegate"
	case StrategySkip:
		return "skip"
	default:
		return "unknown"
	}
}

// FixResult captures the outcome of applying a fix.
type FixResult struct {
	Strategy    Strategy
	FilesChanged []string
	Success     bool
	Error       string
	AstGrepOutput string
}

// FixApplier applies fix packets using the appropriate strategy.
type FixApplier struct {
	logger *slog.Logger

	// Known patterns that can be solved with ast-grep
	astGrepPatterns map[string]string // gate name -> ast-grep pattern

	// Path matcher for constraint enforcement
	protectedPaths  []string
}

// NewFixApplier creates a FixApplier with standard ast-grep patterns.
func NewFixApplier(logger *slog.Logger) *FixApplier {
	fa := &FixApplier{
		logger:         logger.With("component", "fix_applier"),
		astGrepPatterns: make(map[string]string),
	}

	// Register known patterns for ast-grep transformation
	fa.registerKnownPatterns()

	return fa
}

// registerKnownPatterns maps gate names to ast-grep patterns.
func (fa *FixApplier) registerKnownPatterns() {
	// Complexity gates
	fa.astGrepPatterns["cyclomatic_complexity"] = `
rule:
  pattern: $F($$$)
  predicate: complexity_check
transform:
  complexity: reduce
`

	fa.astGrepPatterns["nested_callbacks"] = `
rule:
  pattern: |
    $F($$$, function($$$) {
      $$$BODY
    })
fix:
  extract: $BODY
  compose: $F($$$)
`

	// Duplication gates
	fa.astGrepPatterns["code_duplication"] = `
rule:
  pattern: $BODY
  multiple: true
fix:
  action: extract_function
  name: extracted_func
`

	fa.astGrepPatterns["duplicated_block"] = `
rule:
  pattern: |
    { $$$BLOCK }
  count: >1
fix:
  action: extract_function
`

	// Naming gates
	fa.astGrepPatterns["naming_convention"] = `
rule:
  pattern: $NAME
  kind: identifier
fix:
  action: rename
  convention: camelCase
`

	// Documentation gates - check for required documentation files
	fa.astGrepPatterns["missing_readme"] = `
rule:
  pattern: README.md
  kind: file
  not_exists: true
fix:
  action: create_file
  template: "# Project Title\n\nDescription\n\n## Installation\n\n## Usage\n\n## Contributing\n\n## License\n"
`

	fa.astGrepPatterns["missing_contributing"] = `
rule:
  pattern: CONTRIBUTING.md
  kind: file
  not_exists: true
fix:
  action: create_file
  template: "# Contributing Guidelines\n\n## How to Contribute\n\n## Code Style\n\n## Pull Request Process\n\n## Code of Conduct\n"
`

	fa.astGrepPatterns["missing_license"] = `
rule:
  pattern: LICENSE
  kind: file
  not_exists: true
fix:
  action: create_file
  template: "MIT License\n\nCopyright (c) $(date +%Y) Contributors\n\nPermission is hereby granted..."
`

	fa.astGrepPatterns["missing_changelog"] = `
rule:
  pattern: CHANGELOG.md
  kind: file
  not_exists: true
fix:
  action: create_file
  template: "# Changelog\n\nAll notable changes to this project will be documented in this file.\n\n## [Unreleased]\n\n### Added\n### Changed\n### Fixed\n### Removed\n"
`

	fa.astGrepPatterns["missing_docs_dir"] = `
rule:
  pattern: docs/
  kind: directory
  not_exists: true
fix:
  action: create_directory
  structure:
    - architecture.md
    - api.md
    - contributing.md
    - deployment.md
`

}

// DetermineStrategy decides which strategy to use for a fix packet.
func (fa *FixApplier) DetermineStrategy(fp *FixPacket) Strategy {
	// Documentation gates - file creation
	docGates := map[string]bool{
		"missing_readme":        true,
		"missing_contributing":  true,
		"missing_license":       true,
		"missing_changelog":     true,
		"missing_docs_dir":      true,
	}
	if docGates[fp.GateName] {
		return StrategyCreateFile
	}

	// Check if gate matches a known ast-grep pattern
	if _, ok := fa.astGrepPatterns[fp.GateName]; ok {
		return StrategyASTGre
	}

	// Check if any instruction references a known pattern
	for _, inst := range fp.Instructions {
		lower := strings.ToLower(inst)
		for gateName := range fa.astGrepPatterns {
			if strings.Contains(lower, strings.ReplaceAll(gateName, "_", " ")) ||
				strings.Contains(lower, gateName) {
				return StrategyASTGre
			}
		}
	}

	// Default: delegate to agent
	if len(fp.Instructions) > 0 {
		return StrategyDelegate
	}

	return StrategySkip
}

// ApplyFix applies a fix packet using the determined strategy.
func (fa *FixApplier) ApplyFix(ctx context.Context, fp *FixPacket, agentID string) (*FixResult, error) {
	if fp == nil || fp.IsEmpty() {
		return &FixResult{Strategy: StrategySkip, Success: true}, nil
	}

	// Enforce constraints before applying
	if err := fa.enforceConstraints(fp); err != nil {
		return nil, fmt.Errorf("constraint violation: %w", err)
	}

	strategy := fa.DetermineStrategy(fp)

	switch strategy {
	case StrategyASTGre:
		return fa.applyASTGre(ctx, fp)
	case StrategyCreateFile:
		return fa.applyCreateFile(ctx, fp)
	case StrategyDelegate:
		return fa.applyDelegate(fp, agentID)
	case StrategySkip:
		return &FixResult{Strategy: StrategySkip, Success: true}, nil
	default:
		return nil, fmt.Errorf("unknown strategy: %d", strategy)
	}
}

// enforceConstraints checks that the fix packet doesn't violate constraints.
func (fa *FixApplier) enforceConstraints(fp *FixPacket) error {
	// Check do_not_touch paths
	for _, protected := range fp.Constraints.DoNotTouch {
		for _, file := range fp.Files {
			if strings.HasPrefix(file.Path, protected) {
				return fmt.Errorf("file %s is in protected path %s", file.Path, protected)
			}
		}
	}

	// Check max files
	if fp.Constraints.MaxFiles > 0 && len(fp.Files) > fp.Constraints.MaxFiles {
		return fmt.Errorf("too many files: %d exceeds max_files %d",
			len(fp.Files), fp.Constraints.MaxFiles)
	}

	return nil
}

// applyASTGre runs ast-grep for known patterns.
func (fa *FixApplier) applyASTGre(ctx context.Context, fp *FixPacket) (*FixResult, error) {
	result := &FixResult{
		Strategy: StrategyASTGre,
	}

	pattern, ok := fa.astGrepPatterns[fp.GateName]
	if !ok {
		// Try to find pattern from instructions
		for _, inst := range fp.Instructions {
			lower := strings.ToLower(inst)
			for name, p := range fa.astGrepPatterns {
				if strings.Contains(lower, name) {
					pattern = p
					ok = true
					break
				}
			}
			if ok {
				break
			}
		}
	}

	if !ok {
		return nil, fmt.Errorf("no ast-grep pattern found for gate: %s", fp.GateName)
	}

	fa.logger.Info("applying ast-grep fix",
		"gate", fp.GateName,
		"files", len(fp.Files),
	)

	// Build ast-grep command
	for _, file := range fp.Files {
		cmdArgs := []string{
			"run",
			"--pattern", pattern,
			"--lang", guessLanguage(file.Path),
			file.Path,
		}

		cmd := exec.CommandContext(ctx, "ast-grep", cmdArgs...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			fa.logger.Error("ast-grep failed",
				"file", file.Path,
				"err", err,
				"output", string(output),
			)
			result.Error = fmt.Sprintf("ast-grep error on %s: %v", file.Path, err)
			return result, nil
		}

		result.FilesChanged = append(result.FilesChanged, file.Path)
		result.AstGrepOutput += string(output) + "\n"
	}

	result.Success = len(result.FilesChanged) > 0
	return result, nil
}


// applyCreateFile creates missing documentation files/directories.
func (fa *FixApplier) applyCreateFile(ctx context.Context, fp *FixPacket) (*FixResult, error) {
	result := &FixResult{
		Strategy: StrategyCreateFile,
	}

	fa.logger.Info("creating missing documentation",
		"gate", fp.GateName,
		"files", len(fp.Files),
	)

	// Parse the fix packet instructions for template content
	template := fa.getTemplateForGate(fp.GateName)

	for _, file := range fp.Files {
		// For documentation gates, the file path indicates what to create
		path := file.Path

		// Check if it's a directory creation
		isDir := strings.HasSuffix(path, "/")

		if isDir {
			if err := os.MkdirAll(path, 0755); err != nil {
				fa.logger.Error("failed to create directory", "path", path, "err", err)
				result.Error = fmt.Sprintf("failed to create directory %s: %v", path, err)
				return result, nil
			}
			fa.logger.Info("created directory", "path", path)
		} else {
			// Create parent directories
			dir := filepath.Dir(path)
			if err := os.MkdirAll(dir, 0755); err != nil {
				fa.logger.Error("failed to create parent dirs", "path", dir, "err", err)
				result.Error = fmt.Sprintf("failed to create parent dirs for %s: %v", path, err)
				return result, nil
			}

			// Check if file already exists
			if _, err := os.Stat(path); err == nil {
				fa.logger.Info("file already exists, skipping", "path", path)
				continue
			}

			// Write template content
			content := template
			if content == "" {
				content = fa.getDefaultTemplate(fp.GateName)
			}
			// Substitute variables
			content = strings.ReplaceAll(content, "$(date +%Y)", time.Now().Format("2006"))

			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				fa.logger.Error("failed to write file", "path", path, "err", err)
				result.Error = fmt.Sprintf("failed to write %s: %v", path, err)
				return result, nil
			}
			fa.logger.Info("created file", "path", path)
		}

		result.FilesChanged = append(result.FilesChanged, path)
	}

	result.Success = len(result.FilesChanged) > 0
	return result, nil
}

// getTemplateForGate extracts template from fix packet instructions or uses default.
func (fa *FixApplier) getTemplateForGate(gateName string) string {
	// In a real implementation, this would parse the fix packet's RawText for template
	// For now, return empty to use defaults
	return ""
}

// getDefaultTemplate returns default template for a documentation gate.
func (fa *FixApplier) getDefaultTemplate(gateName string) string {
	switch gateName {
	case "missing_readme":
		return "# Project Title\n\nDescription\n\n## Installation\n\n## Usage\n\n## Contributing\n\n## License\n"
	case "missing_contributing":
		return "# Contributing Guidelines\n\n## How to Contribute\n\n## Code Style\n\n## Pull Request Process\n\n## Code of Conduct\n"
	case "missing_license":
		return "MIT License\n\nCopyright (c) " + time.Now().Format("2006") + " Contributors\n\nPermission is hereby granted..."
	case "missing_changelog":
		return "# Changelog\n\nAll notable changes to this project will be documented in this file.\n\n## [Unreleased]\n\n### Added\n### Changed\n### Fixed\n### Removed\n"
	case "missing_docs_dir":
		return "" // Directory creation, no file content
	default:
		return ""
	}
}
func (fa *FixApplier) applyDelegate(fp *FixPacket, agentID string) (*FixResult, error) {
	fa.logger.Info("delegating fix to agent",
		"agent", agentID,
		"gate", fp.GateName,
		"instructions", len(fp.Instructions),
		"files", len(fp.Files),
	)

	// Build delegation context
	var contextBuilder strings.Builder
	contextBuilder.WriteString(fmt.Sprintf("Fix Packet for gate: %s\n", fp.GateName))
	contextBuilder.WriteString(fmt.Sprintf("Severity: %s\n", fp.Severity))

	if len(fp.Files) > 0 {
		contextBuilder.WriteString("\nTarget Files:\n")
		for _, f := range fp.Files {
			if f.HasRange() {
				contextBuilder.WriteString(fmt.Sprintf("  %s (lines %d-%d)\n", f.Path, f.StartLine, f.EndLine))
			} else {
				contextBuilder.WriteString(fmt.Sprintf("  %s\n", f.Path))
			}
		}
	}

	if len(fp.Instructions) > 0 {
		contextBuilder.WriteString("\nInstructions:\n")
		for i, inst := range fp.Instructions {
			contextBuilder.WriteString(fmt.Sprintf("  %d. %s\n", i+1, inst))
		}
	}

	if len(fp.Constraints.DoNotTouch) > 0 {
		contextBuilder.WriteString("\nDo Not Touch:\n")
		for _, p := range fp.Constraints.DoNotTouch {
			contextBuilder.WriteString(fmt.Sprintf("  - %s\n", p))
		}
	}

	if fp.Constraints.MaxFiles > 0 {
		contextBuilder.WriteString(fmt.Sprintf("\nMax Files: %d\n", fp.Constraints.MaxFiles))
	}

	if fp.Constraints.Paradigm != "" {
		contextBuilder.WriteString(fmt.Sprintf("Paradigm: %s\n", fp.Constraints.Paradigm))
	}

	return &FixResult{
		Strategy:     StrategyDelegate,
		Success:      true,
		FilesChanged: nil, // agent will handle
	}, nil
}

// guessLanguage returns the ast-grep language identifier from a file path.
func guessLanguage(path string) string {
	if strings.HasSuffix(path, ".go") {
		return "go"
	}
	if strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx") {
		return "typescript"
	}
	if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".jsx") {
		return "javascript"
	}
	if strings.HasSuffix(path, ".py") {
		return "python"
	}
	if strings.HasSuffix(path, ".rs") {
		return "rust"
	}
	if strings.HasSuffix(path, ".java") {
		return "java"
	}
	return "unknown"
}
