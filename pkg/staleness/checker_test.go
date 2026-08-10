package staleness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// createTempFiles creates temp files with specific modification times.
func createTempFiles(t *testing.T, dir string, files map[string]time.Duration) {
	t.Helper()
	for name, age := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("failed to create dir for %s: %v", path, err)
		}
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("failed to create file %s: %v", path, err)
		}
		f.Close()
		modTime := time.Now().Add(-age)
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatalf("failed to set mod time for %s: %v", path, err)
		}
	}
}

func TestNewChecker(t *testing.T) {
	cfg := Config{
		MaxAge:          24 * time.Hour,
		WarnAge:         12 * time.Hour,
		ScanPaths:       []string{"/tmp/test"},
		IncludePatterns: []string{"*.md"},
		ExcludePatterns: []string{"vendor/*"},
	}
	checker := NewChecker(cfg)
	if checker == nil {
		t.Fatal("NewChecker returned nil")
	}
	if checker.config.MaxAge != 24*time.Hour {
		t.Errorf("MaxAge = %v, want %v", checker.config.MaxAge, 24*time.Hour)
	}
	if checker.config.WarnAge != 12*time.Hour {
		t.Errorf("WarnAge = %v, want %v", checker.config.WarnAge, 12*time.Hour)
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name     string
		age      time.Duration
		cfg      Config
		expected StalenessLevel
	}{
		{
			name:     "fresh - within warn threshold",
			age:      1 * time.Hour,
			cfg:      Config{MaxAge: 24 * time.Hour, WarnAge: 12 * time.Hour},
			expected: StalenessFresh,
		},
		{
			name:     "warning - between warn and max",
			age:      18 * time.Hour,
			cfg:      Config{MaxAge: 24 * time.Hour, WarnAge: 12 * time.Hour},
			expected: StalenessWarning,
		},
		{
			name:     "stale - beyond max",
			age:      48 * time.Hour,
			cfg:      Config{MaxAge: 24 * time.Hour, WarnAge: 12 * time.Hour},
			expected: StalenessStale,
		},
		{
			name:     "unknown - no thresholds",
			age:      1 * time.Hour,
			cfg:      Config{},
			expected: StalenessUnknown,
		},
		{
			name:     "warning - at exact warn boundary",
			age:      12 * time.Hour,
			cfg:      Config{MaxAge: 24 * time.Hour, WarnAge: 12 * time.Hour},
			expected: StalenessWarning,
		},
		{
			name:     "stale - at exact max boundary",
			age:      24 * time.Hour,
			cfg:      Config{MaxAge: 24 * time.Hour, WarnAge: 12 * time.Hour},
			expected: StalenessStale,
		},
		{
			name:     "unknown - only max set, file fresh",
			age:      1 * time.Hour,
			cfg:      Config{MaxAge: 24 * time.Hour},
			expected: StalenessFresh,
		},
		{
			name:     "stale - only max set, file old",
			age:      48 * time.Hour,
			cfg:      Config{MaxAge: 24 * time.Hour},
			expected: StalenessStale,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modTime := time.Now().Add(-tt.age)
			result := classify(modTime, tt.cfg)
			if result != tt.expected {
				t.Errorf("classify() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMatchPatterns(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		patterns []string
		expected bool
	}{
		{
			name:     "match md extension",
			fileName: "readme.md",
			patterns: []string{"*.md"},
			expected: true,
		},
		{
			name:     "no match",
			fileName: "readme.txt",
			patterns: []string{"*.md"},
			expected: false,
		},
		{
			name:     "match go extension",
			fileName: "main.go",
			patterns: []string{"*.go", "*.md"},
			expected: true,
		},
		{
			name:     "multiple patterns no match",
			fileName: "data.csv",
			patterns: []string{"*.go", "*.md"},
			expected: false,
		},
		{
			name:     "empty patterns",
			fileName: "anything.txt",
			patterns: []string{},
			expected: false,
		},
		{
			name:     "exact match",
			fileName: "Makefile",
			patterns: []string{"Makefile"},
			expected: true,
		},
		{
			name:     "wildcard all",
			fileName: "anything",
			patterns: []string{"*"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchPatterns(tt.fileName, tt.patterns)
			if result != tt.expected {
				t.Errorf("matchPatterns(%s, %v) = %v, want %v", tt.fileName, tt.patterns, result, tt.expected)
			}
		})
	}
}

func TestCheckFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")
	f, err := os.Create(filePath)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	// Set mod time to 2 hours ago
	modTime := time.Now().Add(-2 * time.Hour)
	os.Chtimes(filePath, modTime, modTime)

	cfg := Config{MaxAge: 24 * time.Hour, WarnAge: 12 * time.Hour}
	checker := NewChecker(cfg)
	ctx := context.Background()

	t.Run("existing file", func(t *testing.T) {
		fs, err := checker.CheckFile(ctx, filePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fs == nil {
			t.Fatal("expected FileStatus, got nil")
		}
		if fs.Status != StalenessFresh {
			t.Errorf("status = %v, want fresh", fs.Status)
		}
		if fs.Path != filePath {
			t.Errorf("path = %v, want %v", fs.Path, filePath)
		}
		if fs.Extension != ".txt" {
			t.Errorf("extension = %v, want .txt", fs.Extension)
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := checker.CheckFile(ctx, filepath.Join(tmpDir, "nonexistent.txt"))
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
		var se *StalenessError
		if !errors.As(err, &se) {
			t.Errorf("expected StalenessError, got %T", err)
		}
		if se.Code != ErrCodePathNotFound {
			t.Errorf("error code = %v, want %v", se.Code, ErrCodePathNotFound)
		}
	})
}

func TestIsStale(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a stale file (old)
	staleFile := filepath.Join(tmpDir, "stale.txt")
	f, _ := os.Create(staleFile)
	f.Close()
	staleTime := time.Now().Add(-48 * time.Hour)
	os.Chtimes(staleFile, staleTime, staleTime)

	// Create a fresh file
	freshFile := filepath.Join(tmpDir, "fresh.txt")
	f2, _ := os.Create(freshFile)
	f2.Close()

	cfg := Config{MaxAge: 24 * time.Hour, WarnAge: 12 * time.Hour}
	checker := NewChecker(cfg)
	ctx := context.Background()

	t.Run("stale file", func(t *testing.T) {
		stale, err := checker.IsStale(ctx, staleFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !stale {
			t.Error("expected stale = true for old file")
		}
	})

	t.Run("fresh file", func(t *testing.T) {
		stale, err := checker.IsStale(ctx, freshFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if stale {
			t.Error("expected stale = false for fresh file")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := checker.IsStale(ctx, filepath.Join(tmpDir, "nope.txt"))
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})
}

func TestCheckDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files with different ages
	files := map[string]time.Duration{
		"fresh.txt":    1 * time.Hour,
		"warning.txt":  18 * time.Hour,
		"stale.txt":    48 * time.Hour,
		"sub/nested.md": 2 * time.Hour,
	}
	createTempFiles(t, tmpDir, files)

	cfg := Config{MaxAge: 24 * time.Hour, WarnAge: 12 * time.Hour}
	checker := NewChecker(cfg)
	ctx := context.Background()

	t.Run("scans all files", func(t *testing.T) {
		report, err := checker.CheckDirectory(ctx, tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.TotalFiles != 4 {
			t.Errorf("total files = %d, want 4", report.TotalFiles)
		}
		if report.Fresh != 2 {
			t.Errorf("fresh = %d, want 2 (fresh.txt + nested.md)", report.Fresh)
		}
		if report.Warning != 1 {
			t.Errorf("warning = %d, want 1", report.Warning)
		}
		if report.Stale != 1 {
			t.Errorf("stale = %d, want 1", report.Stale)
		}
		if report.DurationMs < 0 {
			t.Errorf("duration_ms = %d, want >= 0", report.DurationMs)
		}
	})

	t.Run("with include patterns", func(t *testing.T) {
		cfg := Config{
			MaxAge:          24 * time.Hour,
			WarnAge:         12 * time.Hour,
			IncludePatterns: []string{"*.md"},
		}
		checker := NewChecker(cfg)
		report, err := checker.CheckDirectory(ctx, tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.TotalFiles != 1 {
			t.Errorf("total files = %d, want 1 (only .md)", report.TotalFiles)
		}
	})

	t.Run("with exclude patterns", func(t *testing.T) {
		cfg := Config{
			MaxAge:          24 * time.Hour,
			WarnAge:         12 * time.Hour,
			ExcludePatterns: []string{"*.txt"},
		}
		checker := NewChecker(cfg)
		report, err := checker.CheckDirectory(ctx, tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.TotalFiles != 1 {
			t.Errorf("total files = %d, want 1 (only .md after excluding .txt)", report.TotalFiles)
		}
	})

	t.Run("exclude all", func(t *testing.T) {
		cfg := Config{
			MaxAge:          24 * time.Hour,
			WarnAge:         12 * time.Hour,
			ExcludePatterns: []string{"*"},
		}
		checker := NewChecker(cfg)
		report, err := checker.CheckDirectory(ctx, tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.TotalFiles != 0 {
			t.Errorf("total files = %d, want 0 (all excluded)", report.TotalFiles)
		}
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		_, err := checker.CheckDirectory(ctx, filepath.Join(tmpDir, "nonexistent"))
		if err == nil {
			t.Fatal("expected error for nonexistent directory")
		}
		var se *StalenessError
		if !errors.As(err, &se) {
			t.Errorf("expected StalenessError, got %T", err)
		}
		if se.Code != ErrCodePathNotFound {
			t.Errorf("error code = %v, want %v", se.Code, ErrCodePathNotFound)
		}
	})

	t.Run("path is file not directory", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "fresh.txt")
		_, err := checker.CheckDirectory(ctx, filePath)
		if err == nil {
			t.Fatal("expected error when path is a file")
		}
		var se *StalenessError
		if !errors.As(err, &se) {
			t.Errorf("expected StalenessError, got %T", err)
		}
		if se.Code != ErrCodeConfigInvalid {
			t.Errorf("error code = %v, want %v", se.Code, ErrCodeConfigInvalid)
		}
	})
}

func TestCheck(t *testing.T) {
	tmpDir := t.TempDir()

	// Create subdirectories with files
	subDir1 := filepath.Join(tmpDir, "dir1")
	subDir2 := filepath.Join(tmpDir, "dir2")
	os.MkdirAll(subDir1, 0o755)
	os.MkdirAll(subDir2, 0o755)

	createTempFiles(t, subDir1, map[string]time.Duration{
		"a.txt": 1 * time.Hour,  // fresh
		"b.txt": 48 * time.Hour, // stale
	})
	createTempFiles(t, subDir2, map[string]time.Duration{
		"c.md": 18 * time.Hour, // warning
	})

	t.Run("multiple scan paths", func(t *testing.T) {
		cfg := Config{
			MaxAge:  24 * time.Hour,
			WarnAge: 12 * time.Hour,
			ScanPaths: []string{subDir1, subDir2},
		}
		checker := NewChecker(cfg)
		ctx := context.Background()

		report, err := checker.Check(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.TotalFiles != 3 {
			t.Errorf("total files = %d, want 3", report.TotalFiles)
		}
		if report.Fresh != 1 {
			t.Errorf("fresh = %d, want 1", report.Fresh)
		}
		if report.Warning != 1 {
			t.Errorf("warning = %d, want 1", report.Warning)
		}
		if report.Stale != 1 {
			t.Errorf("stale = %d, want 1", report.Stale)
		}
	})

	t.Run("empty scan paths", func(t *testing.T) {
		cfg := Config{
			MaxAge:    24 * time.Hour,
			WarnAge:   12 * time.Hour,
			ScanPaths: []string{},
		}
		checker := NewChecker(cfg)
		ctx := context.Background()

		report, err := checker.Check(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.TotalFiles != 0 {
			t.Errorf("total files = %d, want 0", report.TotalFiles)
		}
	})

	t.Run("with exclude filtering all", func(t *testing.T) {
		cfg := Config{
			MaxAge:          24 * time.Hour,
			WarnAge:         12 * time.Hour,
			ScanPaths:       []string{subDir1},
			ExcludePatterns: []string{"*"},
		}
		checker := NewChecker(cfg)
		ctx := context.Background()

		report, err := checker.Check(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.TotalFiles != 0 {
			t.Errorf("total files = %d, want 0", report.TotalFiles)
		}
	})

	t.Run("nonexistent scan path is skipped", func(t *testing.T) {
		cfg := Config{
			MaxAge:    24 * time.Hour,
			WarnAge:   12 * time.Hour,
			ScanPaths: []string{"/nonexistent/path/abc123"},
		}
		checker := NewChecker(cfg)
		ctx := context.Background()

		report, err := checker.Check(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.TotalFiles != 0 {
			t.Errorf("total files = %d, want 0 (nonexistent path skipped)", report.TotalFiles)
		}
	})
}

func TestCheckWithDirExclude(t *testing.T) {
	tmpDir := t.TempDir()

	// Create vendor directory (should be excluded)
	vendorDir := filepath.Join(tmpDir, "vendor")
	os.MkdirAll(vendorDir, 0o755)
	createTempFiles(t, vendorDir, map[string]time.Duration{
		"dep.go": 1 * time.Hour,
	})

	// Create main directory
	createTempFiles(t, tmpDir, map[string]time.Duration{
		"main.go": 1 * time.Hour,
	})

	cfg := Config{
		MaxAge:          24 * time.Hour,
		WarnAge:         12 * time.Hour,
		ScanPaths:       []string{tmpDir},
		ExcludePatterns: []string{"vendor"},
	}
	checker := NewChecker(cfg)
	ctx := context.Background()

	report, err := checker.Check(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// vendor/ should be excluded, only main.go remains
	if report.TotalFiles != 1 {
		t.Errorf("total files = %d, want 1 (vendor excluded)", report.TotalFiles)
	}
}

func TestErrorFormatting(t *testing.T) {
	t.Run("error with path", func(t *testing.T) {
		err := NewScanError("scan failed", "/tmp/test", nil)
		expected := "[SCAN_FAILED] scan failed (path: /tmp/test)"
		if err.Error() != expected {
			t.Errorf("error = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("error without path", func(t *testing.T) {
		err := NewConfigError("bad config", nil)
		expected := "[CONFIG_INVALID] bad config"
		if err.Error() != expected {
			t.Errorf("error = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("unwrap", func(t *testing.T) {
		inner := os.ErrNotExist
		err := NewPathError("/tmp", inner)
		if err.Unwrap() != inner {
			t.Error("Unwrap() did not return inner error")
		}
	})
}
