package safety

import (
	"strconv"
	"strings"
)

// ParseDiff parses unified diff output into structured file changes.
func ParseDiff(diff string) ([]FileInput, error) {
	var files []FileInput
	var current *FileInput

	lines := strings.Split(diff, "\n")
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git"):
			if current != nil {
				files = append(files, *current)
			}
			parts := strings.Fields(line)
			path := parts[len(parts)-1]
			current = &FileInput{
				Path:     trimDiffPrefix(path),
				Language: detectLanguage(path),
			}

		case strings.HasPrefix(line, "--- /dev/null"):
			if current != nil {
				current.OldContent = ""
			}

		case strings.HasPrefix(line, "+++ /dev/null"):
			// Deleted file
			if current != nil {
				current.Content = ""
			}

		case strings.HasPrefix(line, "--- a/"):
			if current != nil {
				current.OldContent = ""
			}

		case strings.HasPrefix(line, "+++ b/"):
			// New content starts after this header

		case strings.HasPrefix(line, "@@"):
			if current != nil {
				_, _, _, _ = parseHunkHeader(line)
			}

		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			if current != nil {
				current.Content += strings.TrimPrefix(line, "+") + "\n"
			}

		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			if current != nil {
				current.OldContent += strings.TrimPrefix(line, "-") + "\n"
			}

		case strings.HasPrefix(line, " "):
			if current != nil {
				current.Content += strings.TrimPrefix(line, " ") + "\n"
				current.OldContent += strings.TrimPrefix(line, " ") + "\n"
			}
		}
	}

	if current != nil {
		files = append(files, *current)
	}

	return files, nil
}

// parseHunkHeader extracts line numbers from @@ -a,b +c,d @@.
func parseHunkHeader(line string) (oldStart, oldCount, newStart, newCount int) {
	line = strings.TrimPrefix(line, "@@")
	line = strings.TrimSuffix(line, "@@")
	line = strings.TrimSpace(line)

	parts := strings.SplitN(line, " ", 2)
	if len(parts) < 2 {
		return
	}

	oldPart := strings.TrimPrefix(parts[0], "-")
	oldNums := strings.Split(oldPart, ",")
	if len(oldNums) >= 1 {
		oldStart, _ = strconv.Atoi(oldNums[0])
	}
	if len(oldNums) >= 2 {
		oldCount, _ = strconv.Atoi(oldNums[1])
	}

	newPart := strings.TrimPrefix(parts[1], "+")
	newNums := strings.Split(newPart, ",")
	if len(newNums) >= 1 {
		newStart, _ = strconv.Atoi(newNums[0])
	}
	if len(newNums) >= 2 {
		newCount, _ = strconv.Atoi(newNums[1])
	}

	return
}

// trimDiffPrefix removes a/ or b/ prefix from a diff path.
func trimDiffPrefix(path string) string {
	if strings.HasPrefix(path, "a/") || strings.HasPrefix(path, "b/") {
		return path[2:]
	}
	return path
}