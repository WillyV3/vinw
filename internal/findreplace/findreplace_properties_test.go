package findreplace

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	lipglossthemes "github.com/willyv3/gogh-themes/lipgloss"
	"pgregory.net/rapid"
)

// TestSearchFindsAllMatches tests that search finds all occurrences of the pattern
func TestSearchFindsAllMatches(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate test data
		pattern := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "pattern")
		content := generateContentWithPattern(rt, pattern, 1, 10)

		// Create test environment
		tmpDir := t.TempDir()

		testFile := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		// Create model and search
		theme, _ := lipglossthemes.Get("Dracula")
		m := New(tmpDir, theme)
		m.caseSensitive = true

		results, _, err := m.search(pattern, "replacement")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		// Count actual occurrences in content
		actualCount := strings.Count(content, pattern)
		foundCount := len(results)

		// Property: All occurrences should be found
		if foundCount != actualCount {
			t.Fatalf("expected %d matches, found %d", actualCount, foundCount)
		}
	})
}

// TestReplacePreservesNonMatchingLines tests that replacement doesn't affect non-matching lines
func TestReplacePreservesNonMatchingLines(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate test data
		pattern := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "pattern")
		replacement := rapid.StringMatching(`[A-Z]{3,8}`).Draw(rt, "replacement")

		// Generate content with some lines having pattern, some not
		linesWithPattern := rapid.IntRange(1, 5).Draw(rt, "linesWithPattern")
		linesWithoutPattern := rapid.IntRange(1, 5).Draw(rt, "linesWithoutPattern")

		var lines []string
		nonMatchingLines := make(map[int]string)

		// Add lines without pattern
		for i := 0; i < linesWithoutPattern; i++ {
			line := rapid.StringMatching(`[0-9]{5,10}`).Draw(rt, "nonMatchingLine")
			lines = append(lines, line)
			nonMatchingLines[len(lines)-1] = line
		}

		// Add lines with pattern
		for i := 0; i < linesWithPattern; i++ {
			line := rapid.StringMatching(`[a-z]{3,8}` + pattern + `[a-z]{3,8}`).Draw(rt, "matchingLine")
			lines = append(lines, line)
		}

		content := strings.Join(lines, "\n")

		// Create test environment
		tmpDir := t.TempDir()

		testFile := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		// Search and replace
		theme, _ := lipglossthemes.Get("Dracula")
		m := New(tmpDir, theme)
		m.caseSensitive = true

		results, _, err := m.search(pattern, replacement)
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		// Mark all as included
		for i := range results {
			results[i].Included = true
		}

		// Perform replacement
		if len(results) > 0 {
			byFile := make(map[string][]SearchResult)
			for _, r := range results {
				byFile[r.Path] = append(byFile[r.Path], r)
			}

			for path, fileResults := range byFile {
				fullPath := filepath.Join(tmpDir, path)
				if err := replaceInFile(fullPath, fileResults); err != nil {
					t.Fatalf("replace failed: %v", err)
				}
			}
		}

		// Read modified file
		modifiedContent, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatalf("failed to read modified file: %v", err)
		}

		modifiedLines := strings.Split(string(modifiedContent), "\n")

		// Property: Non-matching lines should be unchanged
		for lineNum, originalLine := range nonMatchingLines {
			if lineNum < len(modifiedLines) {
				if modifiedLines[lineNum] != originalLine {
					t.Fatalf("non-matching line %d changed: expected %q, got %q",
						lineNum, originalLine, modifiedLines[lineNum])
				}
			}
		}
	})
}

// TestReplaceIsCorrect tests that replacement produces expected output
func TestReplaceIsCorrect(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate test data
		pattern := rapid.StringMatching(`[a-z]{3,5}`).Draw(rt, "pattern")
		replacement := rapid.StringMatching(`[A-Z]{3,5}`).Draw(rt, "replacement")
		prefix := rapid.StringMatching(`[0-9]{2,4}`).Draw(rt, "prefix")
		suffix := rapid.StringMatching(`[0-9]{2,4}`).Draw(rt, "suffix")

		// Create line with pattern
		line := prefix + pattern + suffix
		expectedLine := prefix + replacement + suffix

		// Create test environment
		tmpDir := t.TempDir()

		testFile := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(testFile, []byte(line), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		// Search and replace
		theme, _ := lipglossthemes.Get("Dracula")
		m := New(tmpDir, theme)
		m.caseSensitive = true

		results, _, err := m.search(pattern, replacement)
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		if len(results) == 0 {
			t.Fatalf("expected to find match")
		}

		// Property: NewLine should be correct
		if results[0].NewLine != expectedLine {
			t.Fatalf("expected %q, got %q", expectedLine, results[0].NewLine)
		}
	})
}

// TestCaseSensitivityRespected tests case sensitivity flag behavior
func TestCaseSensitivityRespected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate lowercase pattern
		pattern := rapid.StringMatching(`[a-z]{4,6}`).Draw(rt, "pattern")
		upperPattern := strings.ToUpper(pattern)

		// Create content with uppercase version
		content := "prefix " + upperPattern + " suffix"

		// Create test environment
		tmpDir := t.TempDir()

		testFile := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		theme, _ := lipglossthemes.Get("Dracula")

		// Test case-sensitive (should NOT match)
		m1 := New(tmpDir, theme)
		m1.caseSensitive = true
		results1, _, err := m1.search(pattern, "replacement")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		// Property: Case-sensitive search should not match different case
		if len(results1) != 0 {
			t.Fatalf("case-sensitive search should not match different case, found %d matches", len(results1))
		}

		// Test case-insensitive (should match)
		m2 := New(tmpDir, theme)
		m2.caseSensitive = false
		results2, _, err := m2.search(pattern, "replacement")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		// Property: Case-insensitive search should match different case
		if len(results2) == 0 {
			t.Fatalf("case-insensitive search should match different case")
		}
	})
}

// TestRegexMatchesCorrectly tests regex pattern matching
func TestRegexMatchesCorrectly(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate simple regex patterns (avoid complex ones that might be invalid)
		patterns := []string{
			`\d+`,           // digits
			`[a-z]+`,        // lowercase letters
			`\w+@\w+\.\w+`,  // simple email pattern
			`test\d+`,       // literal with digit
		}

		patternIdx := rapid.IntRange(0, len(patterns)-1).Draw(rt, "patternIdx")
		pattern := patterns[patternIdx]

		// Generate matching content
		var content string
		switch patternIdx {
		case 0:
			content = "line with 123 numbers"
		case 1:
			content = "line with abc letters"
		case 2:
			content = "email user@domain.com here"
		case 3:
			content = "line with test42 value"
		}

		// Create test environment
		tmpDir := t.TempDir()

		testFile := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		// Search with regex enabled
		theme, _ := lipglossthemes.Get("Dracula")
		m := New(tmpDir, theme)
		m.regexEnabled = true
		m.caseSensitive = true

		results, _, err := m.search(pattern, "REPLACEMENT")
		if err != nil {
			t.Fatalf("regex search failed: %v", err)
		}

		// Property: Should find at least one result (search returns per-line, not per-match)
		re := regexp.MustCompile(pattern)
		hasMatches := re.MatchString(content)

		if hasMatches && len(results) == 0 {
			t.Fatalf("regex pattern %q should match content, but found no results", pattern)
		}
	})
}

// TestLineCountPreserved tests that replacement preserves line count
func TestLineCountPreserved(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate multiple lines
		numLines := rapid.IntRange(5, 20).Draw(rt, "numLines")
		pattern := rapid.StringMatching(`[a-z]{3,5}`).Draw(rt, "pattern")
		replacement := rapid.StringMatching(`[A-Z]{3,5}`).Draw(rt, "replacement")

		var lines []string
		for i := 0; i < numLines; i++ {
			// Some lines have pattern, some don't
			if rapid.Bool().Draw(rt, "hasPattern") {
				lines = append(lines, "prefix "+pattern+" suffix")
			} else {
				lines = append(lines, "line without pattern "+rapid.StringMatching(`[0-9]+`).Draw(rt, "number"))
			}
		}

		content := strings.Join(lines, "\n")

		// Create test environment
		tmpDir := t.TempDir()

		testFile := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		originalLineCount := len(lines)

		// Search and replace
		theme, _ := lipglossthemes.Get("Dracula")
		m := New(tmpDir, theme)
		m.caseSensitive = true

		results, _, err := m.search(pattern, replacement)
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		if len(results) > 0 {
			for i := range results {
				results[i].Included = true
			}

			byFile := make(map[string][]SearchResult)
			for _, r := range results {
				byFile[r.Path] = append(byFile[r.Path], r)
			}

			for path, fileResults := range byFile {
				fullPath := filepath.Join(tmpDir, path)
				if err := replaceInFile(fullPath, fileResults); err != nil {
					t.Fatalf("replace failed: %v", err)
				}
			}
		}

		// Read modified file
		modifiedContent, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatalf("failed to read modified file: %v", err)
		}

		modifiedLineCount := len(strings.Split(string(modifiedContent), "\n"))

		// Property: Line count should be preserved
		if modifiedLineCount != originalLineCount {
			t.Fatalf("line count changed: expected %d, got %d", originalLineCount, modifiedLineCount)
		}
	})
}

// TestEmptyPatternHandling tests behavior with empty patterns
func TestEmptyPatternHandling(t *testing.T) {
	// Property: caseInsensitiveReplace should handle empty patterns without hanging
	// This is a regression test for the infinite loop bug

	// Test the fix directly
	result := caseInsensitiveReplace("test string", "", "replacement")

	// Property: Should return original string unchanged
	if result != "test string" {
		t.Fatalf("expected original string %q, got %q", "test string", result)
	}

	// Also test that it doesn't hang with various inputs
	testCases := []struct {
		input string
		old   string
		new   string
	}{
		{"hello", "", "X"},
		{"", "", "X"},
		{"abc def", "", ""},
	}

	for _, tc := range testCases {
		result := caseInsensitiveReplace(tc.input, tc.old, tc.new)
		// Should not hang and should return original when old is empty
		if tc.old == "" && result != tc.input {
			t.Fatalf("empty pattern should return original: expected %q, got %q", tc.input, result)
		}
	}
}

// TestSpecialCharactersHandled tests handling of special regex characters in plain text mode
func TestSpecialCharactersHandled(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Special regex characters
		specialChars := []string{".", "*", "+", "?", "[", "]", "(", ")", "{", "}", "|", "^", "$", "\\"}
		char := specialChars[rapid.IntRange(0, len(specialChars)-1).Draw(rt, "charIdx")]

		// Create content with the special character
		content := "prefix " + char + " suffix"

		// Create test environment
		tmpDir := t.TempDir()

		testFile := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		// Search in plain text mode (NOT regex)
		theme, _ := lipglossthemes.Get("Dracula")
		m := New(tmpDir, theme)
		m.regexEnabled = false
		m.caseSensitive = true

		results, _, err := m.search(char, "REPLACEMENT")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		// Property: Should find the literal character in plain text mode
		if len(results) == 0 {
			t.Fatalf("should find literal special character %q in plain text mode", char)
		}
	})
}

// TestMultipleOccurrencesInSameLine tests multiple matches on same line
func TestMultipleOccurrencesInSameLine(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		pattern := rapid.StringMatching(`[a-z]{3,5}`).Draw(rt, "pattern")
		replacement := rapid.StringMatching(`[A-Z]{3,5}`).Draw(rt, "replacement")
		occurrences := rapid.IntRange(2, 5).Draw(rt, "occurrences")

		// Create line with multiple occurrences
		parts := make([]string, occurrences)
		for i := 0; i < occurrences; i++ {
			parts[i] = pattern
		}
		line := strings.Join(parts, " ")

		// Create test environment
		tmpDir := t.TempDir()

		testFile := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(testFile, []byte(line), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		// Search
		theme, _ := lipglossthemes.Get("Dracula")
		m := New(tmpDir, theme)
		m.caseSensitive = true

		results, _, err := m.search(pattern, replacement)
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		// Property: Should find line once (reports line, not individual matches)
		if len(results) != 1 {
			t.Fatalf("expected 1 result (line with matches), got %d", len(results))
		}

		// Property: NewLine should have all occurrences replaced
		expectedReplacements := strings.Count(results[0].NewLine, replacement)
		if expectedReplacements != occurrences {
			t.Fatalf("expected %d replacements in line, found %d", occurrences, expectedReplacements)
		}
	})
}

// TestIncludePatterns tests that include patterns work correctly
func TestIncludePatterns(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Create test environment with different file types
		tmpDir := t.TempDir()

		// Create test files with different extensions
		goFile := filepath.Join(tmpDir, "test.go")
		jsFile := filepath.Join(tmpDir, "test.js")
		txtFile := filepath.Join(tmpDir, "test.txt")

		content := "searchpattern"
		os.WriteFile(goFile, []byte(content), 0644)
		os.WriteFile(jsFile, []byte(content), 0644)
		os.WriteFile(txtFile, []byte(content), 0644)

		theme, _ := lipglossthemes.Get("Dracula")
		m := New(tmpDir, theme)
		m.caseSensitive = true

		// Set include pattern to only *.go files
		m.includeInput.SetValue("*.go")

		results, _, err := m.search("searchpattern", "replacement")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		// Property: Should only find matches in .go files
		for _, result := range results {
			if !strings.HasSuffix(result.Path, ".go") {
				t.Fatalf("found result in non-.go file: %s", result.Path)
			}
		}

		// Property: Should find at least one match (in test.go)
		if len(results) == 0 {
			t.Fatalf("expected to find matches in .go file")
		}
	})
}

// TestExcludePatterns tests that exclude patterns work correctly
func TestExcludePatterns(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Create test environment
		tmpDir := t.TempDir()

		// Create test files
		regularFile := filepath.Join(tmpDir, "test.go")
		testFile := filepath.Join(tmpDir, "test_test.go")

		content := "searchpattern"
		os.WriteFile(regularFile, []byte(content), 0644)
		os.WriteFile(testFile, []byte(content), 0644)

		theme, _ := lipglossthemes.Get("Dracula")
		m := New(tmpDir, theme)
		m.caseSensitive = true

		// Set exclude pattern to skip test files
		m.excludeInput.SetValue("*_test.go")

		results, _, err := m.search("searchpattern", "replacement")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		// Property: Should not find matches in *_test.go files
		for _, result := range results {
			if strings.HasSuffix(result.Path, "_test.go") {
				t.Fatalf("found result in excluded test file: %s", result.Path)
			}
		}

		// Property: Should find matches in non-test files
		foundNonTest := false
		for _, result := range results {
			if !strings.HasSuffix(result.Path, "_test.go") {
				foundNonTest = true
				break
			}
		}
		if !foundNonTest {
			t.Fatalf("expected to find matches in non-test files")
		}
	})
}

// TestIncludeAndExcludePatterns tests combined include and exclude logic
func TestIncludeAndExcludePatterns(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Create test environment
		tmpDir := t.TempDir()

		// Create various test files
		goFile := filepath.Join(tmpDir, "main.go")
		goTestFile := filepath.Join(tmpDir, "main_test.go")
		jsFile := filepath.Join(tmpDir, "app.js")

		content := "pattern"
		os.WriteFile(goFile, []byte(content), 0644)
		os.WriteFile(goTestFile, []byte(content), 0644)
		os.WriteFile(jsFile, []byte(content), 0644)

		theme, _ := lipglossthemes.Get("Dracula")
		m := New(tmpDir, theme)
		m.caseSensitive = true

		// Include only .go files, but exclude test files
		m.includeInput.SetValue("*.go")
		m.excludeInput.SetValue("*_test.go")

		results, _, err := m.search("pattern", "replacement")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		// Property: Results should only be .go files that are NOT test files
		for _, result := range results {
			if !strings.HasSuffix(result.Path, ".go") {
				t.Fatalf("found non-.go file: %s", result.Path)
			}
			if strings.HasSuffix(result.Path, "_test.go") {
				t.Fatalf("found excluded test file: %s", result.Path)
			}
		}

		// Property: Should find main.go only
		if len(results) == 0 {
			t.Fatalf("expected to find matches in main.go")
		}
	})
}

// TestMultipleIncludePatterns tests comma-separated include patterns
func TestMultipleIncludePatterns(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Create test environment
		tmpDir := t.TempDir()

		// Create files with different extensions
		goFile := filepath.Join(tmpDir, "test.go")
		jsFile := filepath.Join(tmpDir, "test.js")
		pyFile := filepath.Join(tmpDir, "test.py")
		txtFile := filepath.Join(tmpDir, "test.txt")

		content := "match"
		os.WriteFile(goFile, []byte(content), 0644)
		os.WriteFile(jsFile, []byte(content), 0644)
		os.WriteFile(pyFile, []byte(content), 0644)
		os.WriteFile(txtFile, []byte(content), 0644)

		theme, _ := lipglossthemes.Get("Dracula")
		m := New(tmpDir, theme)
		m.caseSensitive = true

		// Include multiple patterns
		m.includeInput.SetValue("*.go, *.js")

		results, _, err := m.search("match", "replacement")
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		// Property: Results should only be .go or .js files
		for _, result := range results {
			ext := filepath.Ext(result.Path)
			if ext != ".go" && ext != ".js" {
				t.Fatalf("found file with unexpected extension: %s", result.Path)
			}
		}

		// Property: Should find at least 2 files (test.go and test.js)
		if len(results) < 2 {
			t.Fatalf("expected to find matches in both .go and .js files")
		}
	})
}

// TestParsePatterns tests pattern parsing logic
func TestParsePatterns(t *testing.T) {
	testCases := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"*.go", []string{"*.go"}},
		{"*.go,*.js", []string{"*.go", "*.js"}},
		{"*.go, *.js, *.ts", []string{"*.go", "*.js", "*.ts"}},
		{" *.go , *.js ", []string{"*.go", "*.js"}},
		{",,,*.go,,,", []string{"*.go"}},
	}

	for _, tc := range testCases {
		result := parsePatterns(tc.input)
		if len(result) != len(tc.expected) {
			t.Fatalf("for input %q: expected %d patterns, got %d", tc.input, len(tc.expected), len(result))
		}
		for i, p := range result {
			if p != tc.expected[i] {
				t.Fatalf("for input %q: expected pattern %d to be %q, got %q", tc.input, i, tc.expected[i], p)
			}
		}
	}
}

// Helper functions

func generateContentWithPattern(rt *rapid.T, pattern string, minOccurrences, maxOccurrences int) string {
	occurrences := rapid.IntRange(minOccurrences, maxOccurrences).Draw(rt, "occurrences")

	var lines []string
	for i := 0; i < occurrences; i++ {
		prefix := rapid.StringMatching(`[0-9]{2,5}`).Draw(rt, "prefix")
		suffix := rapid.StringMatching(`[0-9]{2,5}`).Draw(rt, "suffix")
		lines = append(lines, prefix+" "+pattern+" "+suffix)
	}

	return strings.Join(lines, "\n")
}
