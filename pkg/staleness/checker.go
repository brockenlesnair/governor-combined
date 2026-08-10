// Package staleness detects outdated documentation and code artifacts.
//
// It checks file modification times, git history, and dependency freshness
// to identify stale content that needs review or removal.
package staleness

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Checker performs staleness checks on files.
type Checker struct {
	config Config
}

// NewChecker creates a new Checker with the given config.
func NewChecker(cfg Config) *Checker {
	return &Checker{config: cfg}
}

// Check scans all configured ScanPaths, applies include/exclude patterns,
// and classifies each file by staleness level.
func (c *Checker) Check(ctx context.Context) (*StalenessReport, error) {
	if len(c.config.ScanPaths) == 0 {
		return &StalenessReport{
			ScannedAt:  time.Now(),
			TotalFiles: 0,
			Files:      []FileStatus{},
		}, nil
	}

	start := time.Now()
	report := &StalenessReport{
		ScannedAt: start,
		Files:     []FileStatus{},
	}

	for _, scanPath := range c.config.ScanPaths {
		select {
		case <-ctx.Done():
			return report, ctx.Err()
		default:
		}

		subReport, err := c.CheckDirectory(ctx, scanPath)
		if err != nil {
			// Graceful degradation: wrap and continue if it's a path error
		var stalenessErr *StalenessError
		if errors.As(err, &stalenessErr) {
			if stalenessErr.Code == ErrCodePathNotFound {
				continue
			}
		}
			return report, err
		}

		report.Files = append(report.Files, subReport.Files...)
		report.Fresh += subReport.Fresh
		report.Warning += subReport.Warning
		report.Stale += subReport.Stale
		report.Unknown += subReport.Unknown
	}

	report.TotalFiles = len(report.Files)
	report.DurationMs = time.Since(start).Milliseconds()
	return report, nil
}

// CheckFile checks a single file and returns its staleness status.
func (c *Checker) CheckFile(ctx context.Context, path string) (*FileStatus, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, NewPathError(path, err)
		}
		return nil, NewScanError("failed to stat file", path, err)
	}

	status := classify(info.ModTime(), c.config)
	age := time.Since(info.ModTime())

	return &FileStatus{
		Path:         path,
		LastModified: info.ModTime(),
		Age:          age,
		Status:       status,
		Size:         info.Size(),
		Extension:    filepath.Ext(path),
	}, nil
}

// CheckDirectory scans a single directory and returns a staleness report.
func (c *Checker) CheckDirectory(ctx context.Context, dir string) (*StalenessReport, error) {
	start := time.Now()

	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, NewPathError(dir, err)
		}
		return nil, NewScanError("failed to stat directory", dir, err)
	}

	if !info.IsDir() {
		return nil, NewConfigError("path is not a directory: "+dir, nil)
	}

	report := &StalenessReport{
		ScannedAt: start,
		Files:     []FileStatus{},
	}

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Permission denied or other error — skip, mark as unknown
			report.Unknown++
			report.Files = append(report.Files, FileStatus{
				Path:   path,
				Status: StalenessUnknown,
			})
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip directories
		if d.IsDir() {
			// Check if directory should be excluded
			if matchPatterns(d.Name(), c.config.ExcludePatterns) {
				return filepath.SkipDir
			}
			return nil
		}

		name := d.Name()

		// Check exclude patterns
		if matchPatterns(name, c.config.ExcludePatterns) {
			return nil
		}

		// Check include patterns (if any specified, file must match)
		if len(c.config.IncludePatterns) > 0 && !matchPatterns(name, c.config.IncludePatterns) {
			return nil
		}

		// Get file info for mod time and size
		info, statErr := os.Stat(path)
		if statErr != nil {
			// Can't stat — mark as unknown
			report.Unknown++
			report.Files = append(report.Files, FileStatus{
				Path:   path,
				Status: StalenessUnknown,
			})
			return nil
		}

		status := classify(info.ModTime(), c.config)
		age := time.Since(info.ModTime())

		fileStatus := FileStatus{
			Path:         path,
			LastModified: info.ModTime(),
			Age:          age,
			Status:       status,
			Size:         info.Size(),
			Extension:    filepath.Ext(name),
		}

		report.Files = append(report.Files, fileStatus)

		switch status {
		case StalenessFresh:
			report.Fresh++
		case StalenessWarning:
			report.Warning++
		case StalenessStale:
			report.Stale++
		default:
			report.Unknown++
		}

		return nil
	})

	if err != nil {
		return report, NewScanError("walk failed", dir, err)
	}

	report.TotalFiles = len(report.Files)
	report.DurationMs = time.Since(start).Milliseconds()
	return report, nil
}

// IsStale returns true if the file at the given path is stale.
func (c *Checker) IsStale(ctx context.Context, path string) (bool, error) {
	fs, err := c.CheckFile(ctx, path)
	if err != nil {
		return false, err
	}
	return fs.Status == StalenessStale, nil
}

// classify determines the staleness level based on file modification time.
func classify(modTime time.Time, cfg Config) StalenessLevel {
	age := time.Since(modTime)

	// If MaxAge is set and file is older, it's stale
	if cfg.MaxAge > 0 && age > cfg.MaxAge {
		return StalenessStale
	}

	// If WarnAge is set and file is older, it's warning
	if cfg.WarnAge > 0 && age > cfg.WarnAge {
		return StalenessWarning
	}

	// If we have thresholds and file is within them, it's fresh
	if cfg.MaxAge > 0 || cfg.WarnAge > 0 {
		return StalenessFresh
	}

	// No thresholds configured — can't determine
	return StalenessUnknown
}

// matchPatterns checks if a filename matches any of the given glob patterns.
func matchPatterns(name string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, name)
		if err != nil {
			continue
		}
		if matched {
			return true
		}
	}
	return false
}


