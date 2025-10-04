package internal

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// GitIgnore handles .gitignore pattern matching
type GitIgnore struct {
	patterns []string
	rootPath string
}

// NewGitIgnore loads and parses .gitignore file
func NewGitIgnore(rootPath string) *GitIgnore {
	gi := &GitIgnore{
		patterns: []string{},
		rootPath: rootPath,
	}

	// Load .gitignore file if it exists
	gitignorePath := filepath.Join(rootPath, ".gitignore")
	file, err := os.Open(gitignorePath)
	if err != nil {
		// No .gitignore file
		return gi
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		gi.patterns = append(gi.patterns, line)
	}

	return gi
}

// IsIgnored checks if a path should be ignored
func (gi *GitIgnore) IsIgnored(path string) bool {
	// Get relative path from root
	relPath, err := filepath.Rel(gi.rootPath, path)
	if err != nil {
		return false
	}

	// Check each pattern
	for _, pattern := range gi.patterns {
		if gi.matchPattern(relPath, pattern) {
			return true
		}
	}
	return false
}

// matchPattern checks if a path matches a gitignore pattern
func (gi *GitIgnore) matchPattern(path, pattern string) bool {
	// Simple pattern matching (not full gitignore spec, but covers common cases)

	// Remove leading slash if present
	pattern = strings.TrimPrefix(pattern, "/")

	// Directory patterns (ending with /)
	if strings.HasSuffix(pattern, "/") {
		pattern = strings.TrimSuffix(pattern, "/")
		// Check if any part of the path matches the directory pattern
		parts := strings.Split(path, string(filepath.Separator))
		for _, part := range parts {
			if matched, _ := filepath.Match(pattern, part); matched {
				return true
			}
		}
	}

	// File or directory patterns
	base := filepath.Base(path)

	// Direct match on basename
	if matched, _ := filepath.Match(pattern, base); matched {
		return true
	}

	// Match against full relative path
	if matched, _ := filepath.Match(pattern, path); matched {
		return true
	}

	// Handle ** patterns (match any depth)
	if strings.Contains(pattern, "**") {
		// Convert ** to * for simple matching
		simplePattern := strings.ReplaceAll(pattern, "**", "*")
		if matched, _ := filepath.Match(simplePattern, path); matched {
			return true
		}
	}

	// Handle patterns that should match anywhere in the tree
	if !strings.Contains(pattern, "/") {
		// Pattern like "*.log" should match in any directory
		parts := strings.Split(path, string(filepath.Separator))
		for _, part := range parts {
			if matched, _ := filepath.Match(pattern, part); matched {
				return true
			}
		}
	}

	return false
}