package untested

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// CoverageData holds the parsed output of a Go coverage profile.
type CoverageData struct {
	// FuncCoverage maps qualified function names to their line coverage %.
	FuncCoverage map[string]float64
	// FileCoverage maps file paths to their statement coverage %.
	FileCoverage map[string]float64
	// TotalLines is the total number of instrumentable statements.
	TotalLines int
	// CoveredLines is the number of statements executed at least once.
	CoveredLines int
}

// ParseCoverProfile parses a Go cover profile written by `go test -coverprofile`.
//
// Format:
//
//	mode: set
//	file.go:10.2,10.6 1 0
//	file.go:12.3,12.7 1 1
//
// Each coverage line has: file:startCol,endCol numStatements count
func ParseCoverProfile(path string) (*CoverageData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, NewErrorWrap(ErrCodeUntestedFailed, "open cover profile", err)
	}
	defer f.Close()

	data := &CoverageData{
		FuncCoverage: make(map[string]float64),
		FileCoverage: make(map[string]float64),
	}

	// Per-file accumulators: file -> {total, covered}
	type fileStats struct {
		total   int
		covered int
	}
	stats := make(map[string]*fileStats)

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lineNo++
		if line == "" {
			continue
		}

		// First line must be "mode: ..."
		if lineNo == 1 {
			if !strings.HasPrefix(line, "mode:") {
				return nil, NewError(ErrCodeUntestedFailed, "invalid cover profile: missing mode header")
			}
			continue
		}

		// Parse: file:startCol,endCol numStatements count
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 3 {
			continue
		}
		rangePart := parts[0] // "file.go:10.2,10.6"
		stmtPart := parts[1]  // "1"
		countPart := parts[2] // "0" or "1"

		colonIdx := strings.LastIndex(rangePart, ":")
		if colonIdx < 0 {
			continue
		}
		file := rangePart[:colonIdx]
		// We don't need the exact columns for coverage aggregation.

		numStmt, err := strconv.Atoi(stmtPart)
		if err != nil {
			continue
		}
		count, err := strconv.Atoi(countPart)
		if err != nil {
			continue
		}

		fs, ok := stats[file]
		if !ok {
			fs = &fileStats{}
			stats[file] = fs
		}
		fs.total += numStmt
		data.TotalLines += numStmt
		if count > 0 {
			fs.covered += numStmt
			data.CoveredLines += numStmt
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, NewErrorWrap(ErrCodeUntestedFailed, "read cover profile", err)
	}

	// Compute per-file coverage percentages.
	for file, fs := range stats {
		if fs.total > 0 {
			data.FileCoverage[file] = float64(fs.covered) / float64(fs.total) * 100.0
		} else {
			data.FileCoverage[file] = 0
		}
	}

	return data, nil
}

// RunGoTestCover runs `go test -coverprofile` for the given package path
// and returns the path to the generated profile file.
func RunGoTestCover(ctx context.Context, pkgPath string, buildTags []string) (string, error) {
	tmpFile, err := os.CreateTemp("", "gov-cover-*.out")
	if err != nil {
		return "", NewErrorWrap(ErrCodeUntestedFailed, "create temp file", err)
	}
	coverPath := tmpFile.Name()
	tmpFile.Close()

	goBin := "/usr/local/go/bin/go"
	args := []string{"test", "-coverprofile", coverPath, "-count=1"}
	for _, tag := range buildTags {
		args = append(args, "-tags", tag)
	}
	args = append(args, pkgPath)

	cmd := exec.CommandContext(ctx, goBin, args...)
	cmd.Dir = filepath.Dir(pkgPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(coverPath)
		return "", NewErrorWrap(ErrCodeUntestedFailed,
			fmt.Sprintf("go test failed: %s", string(output)), err)
	}

	return coverPath, nil
}
