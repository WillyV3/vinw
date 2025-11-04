package findreplace

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	lipglossthemes "github.com/willyv3/gogh-themes/lipgloss"
	"golang.org/x/sync/errgroup"
)

// State represents the current state in the find/replace flow
type State int

const (
	// StateInputFields is the initial state where user enters search/replace text
	StateInputFields State = iota
	// StateSearchResults shows the list of search results
	StateSearchResults
	// StateConfirmReplacement shows confirmation dialog before replacement
	StateConfirmReplacement
	// StatePerformingReplacement shows progress during replacement
	StatePerformingReplacement
	// StateReplacementComplete shows final stats
	StateReplacementComplete
)

const (
	// MaxResults limits the number of results to prevent UI freezing
	MaxResults = 1000
)

// SearchResult represents a single match
type SearchResult struct {
	Path          string
	LineNum       int
	Line          string
	NewLine       string
	SearchTerm    string // The actual search pattern that matched
	CaseSensitive bool   // Whether the search was case-sensitive
	Included      bool
	Error         string // For replacement errors
	Completed     bool   // For replacement tracking
}

// Implement list.Item interface
func (r SearchResult) FilterValue() string {
	return fmt.Sprintf("%s:%d %s", r.Path, r.LineNum, r.Line)
}

// Implement list.DefaultItem interface (optional but recommended)
func (r SearchResult) Title() string {
	checkbox := "[ ]"
	if r.Included {
		checkbox = "[x]"
	}
	return fmt.Sprintf("%s %s:%d", checkbox, r.Path, r.LineNum)
}

func (r SearchResult) Description() string {
	return ""
}

// resultItemDelegate is a custom delegate for rendering SearchResults with 3 lines
type resultItemDelegate struct {
	theme lipglossthemes.Theme
}

func (d resultItemDelegate) Height() int {
	return 6 // Estimate: path + ~2 wrapped old lines + ~2 wrapped new lines + blank
}

func (d resultItemDelegate) Spacing() int {
	return 0 // We handle spacing in our rendering
}

func (d resultItemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d resultItemDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	result, ok := item.(SearchResult)
	if !ok {
		return
	}

	// Determine if this item is selected
	isSelected := index == m.Index()

	// Checkbox
	checkbox := "[ ]"
	if result.Included {
		checkbox = "[x]"
	}

	// Calculate available width for content
	maxLineWidth := m.Width() - 6 // Leave margin for "  - " prefix
	if maxLineWidth < 40 {
		maxLineWidth = 40
	}

	// Base style with subtle background
	var baseStyle lipgloss.Style
	if isSelected {
		// Subtle selection: just slightly lighter than black
		baseStyle = lipgloss.NewStyle().Background(d.theme.BrightBlack)
	} else {
		baseStyle = lipgloss.NewStyle().Background(d.theme.Black)
	}

	// Line 1: Path with checkbox and subtle selection indicator
	pathStyle := baseStyle.Copy().Foreground(d.theme.Foreground)
	var indicator string
	if isSelected {
		// Add a subtle arrow indicator instead of changing the whole background
		indicator = "› "
	} else {
		indicator = "  "
	}
	pathLine := pathStyle.Render(fmt.Sprintf("%s%s %s:%d", indicator, checkbox, result.Path, result.LineNum))

	// Check if there's actually a replacement
	hasReplacement := result.NewLine != result.Line

	// Wrap and render old line with highlighting (highlight all occurrences of search term)
	oldWrapped := wrapText(result.Line, maxLineWidth)
	var oldLines []string
	oldStyle := baseStyle.Copy().Foreground(d.theme.BrightRed)
	for _, line := range oldWrapped {
		rendered := highlightAllOccurrences(line, result.SearchTerm, d.theme.BrightRed, d.theme.BrightBlack, result.CaseSensitive)
		oldLines = append(oldLines, oldStyle.Render("  - ")+rendered)
	}

	// Only render new line if there's actually a replacement
	var newLines []string
	if hasReplacement {
		newWrapped := wrapText(result.NewLine, maxLineWidth)
		newStyle := baseStyle.Copy().Foreground(d.theme.BrightGreen)
		for _, line := range newWrapped {
			// No highlighting needed on new line - it's the result
			newLines = append(newLines, newStyle.Render("  + "+line))
		}
	}

	// Blank line with background
	blankLine := baseStyle.Render("")

	// Write all lines
	fmt.Fprintf(w, "%s\n", pathLine)
	for _, line := range oldLines {
		fmt.Fprintf(w, "%s\n", line)
	}
	for _, line := range newLines {
		fmt.Fprintf(w, "%s\n", line)
	}
	fmt.Fprintf(w, "%s\n", blankLine)
}

// ReplaceStats tracks replacement outcomes
type ReplaceStats struct {
	TotalMatches int
	Included     int
	Excluded     int
	Successes    int
	Errors       int
	ErrorDetails []SearchResult
}

// Model is the Bubble Tea model for find/replace
type Model struct {
	State State // Current state (exported for external access)

	// Input fields
	searchInput   textinput.Model
	replaceInput  textinput.Model
	includeInput  textinput.Model
	excludeInput  textinput.Model
	regexEnabled  bool
	caseSensitive bool
	focusedField  int // 0=search, 1=replace, 2=include, 3=exclude

	// Search state
	resultsList      list.Model      // NEW: bubble list component
	resultsTruncated bool
	searching        bool
	searchPerformed  bool // Track if a search was actually run
	searchErr        error
	spinner          spinner.Model

	// Replacement state
	progressPct float64
	progressMsg string
	stats       ReplaceStats

	// Context
	rootPath  string
	sessionID string
	width     int
	height    int
	theme     lipglossthemes.Theme
}

// Messages
type searchCompleteMsg struct {
	results   []SearchResult
	truncated bool
	err       error
}

type replacementProgressMsg struct {
	completed int
	total     int
}

type replacementCompleteMsg struct {
	stats ReplaceStats
}

// New creates a new find/replace model
func New(rootPath string, sessionID string, theme lipglossthemes.Theme) Model {
	// Search input
	searchInput := textinput.New()
	searchInput.Placeholder = "Search pattern..."
	searchInput.Focus()
	searchInput.CharLimit = 500
	searchInput.Width = 50

	// Replace input
	replaceInput := textinput.New()
	replaceInput.Placeholder = "Replacement text..."
	replaceInput.CharLimit = 500
	replaceInput.Width = 50

	// Include patterns input
	includeInput := textinput.New()
	includeInput.Placeholder = "Include files (e.g., *.go,*.js)..."
	includeInput.CharLimit = 200
	includeInput.Width = 50

	// Exclude patterns input
	excludeInput := textinput.New()
	excludeInput.Placeholder = "Exclude files (e.g., *_test.go,vendor/*)..."
	excludeInput.CharLimit = 200
	excludeInput.Width = 50

	// Spinner for search progress
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(theme.BrightBlue)

	// Create list with custom delegate
	delegate := resultItemDelegate{theme: theme}
	resultsList := list.New([]list.Item{}, delegate, 0, 0)
	resultsList.SetShowStatusBar(false)
	resultsList.SetShowTitle(false)
	resultsList.SetShowHelp(false)
	resultsList.SetFilteringEnabled(false)
	resultsList.DisableQuitKeybindings()

	// Customize list styles to match theme
	resultsList.Styles.NoItems = lipgloss.NewStyle().
		Foreground(theme.BrightBlack).
		Padding(1, 2)

	return Model{
		State:        StateInputFields,
		searchInput:  searchInput,
		replaceInput: replaceInput,
		includeInput: includeInput,
		excludeInput: excludeInput,
		focusedField: 0,
		rootPath:     rootPath,
		sessionID:    sessionID,
		theme:        theme,
		spinner:      s,
		resultsList:  resultsList,
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick)
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeResultsList() // Resize list if showing results

	case tea.KeyMsg:
		// Handle special keys first
		newModel, cmd := m.handleKeyPress(msg)
		if cmd != nil {
			// Special key was handled (tab, enter, etc)
			return newModel, cmd
		}
		m = newModel

		// If in input fields state and key wasn't specially handled,
		// pass to text input
		if m.State == StateInputFields {
			switch m.focusedField {
			case 0:
				m.searchInput, cmd = m.searchInput.Update(msg)
			case 1:
				m.replaceInput, cmd = m.replaceInput.Update(msg)
			case 2:
				m.includeInput, cmd = m.includeInput.Update(msg)
			case 3:
				m.excludeInput, cmd = m.excludeInput.Update(msg)
			}
			cmds = append(cmds, cmd)
		}

	case searchCompleteMsg:
		m.searching = false
		m.searchErr = msg.err
		m.resultsTruncated = msg.truncated
		if msg.err == nil && len(msg.results) > 0 {
			// Convert SearchResults to list.Items
			items := make([]list.Item, len(msg.results))
			for i, r := range msg.results {
				items[i] = r
			}

			// IMPORTANT: Set state FIRST so resizeResultsList() works correctly
			m.State = StateSearchResults
			m.resultsList.SetItems(items)
			m.resizeResultsList() // Size the list to fill available space

			// CRITICAL: Force list to apply the size by sending it a WindowSizeMsg
			// The list component needs WindowSizeMsg specifically to update its viewport
			var cmd tea.Cmd
			m.resultsList, cmd = m.resultsList.Update(tea.WindowSizeMsg{
				Width:  m.width,
				Height: m.height,
			})
			cmds = append(cmds, cmd)
		}
		// If 0 results, stay in InputFields state - status line will show count

	case replacementProgressMsg:
		m.progressPct = float64(msg.completed) / float64(msg.total) * 100
		m.progressMsg = fmt.Sprintf("Replacing %d/%d", msg.completed, msg.total)

	case replacementCompleteMsg:
		m.State = StateReplacementComplete
		m.stats = msg.stats

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Update list in results state
	if m.State == StateSearchResults {
		var cmd tea.Cmd
		m.resultsList, cmd = m.resultsList.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleKeyPress(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch m.State {
	case StateInputFields:
		return m.handleInputFieldsKeys(msg)
	case StateSearchResults:
		return m.handleSearchResultsKeys(msg)
	case StateConfirmReplacement:
		return m.handleConfirmKeys(msg)
	case StateReplacementComplete:
		return m.handleResultsKeys(msg)
	}
	return m, nil
}

func (m Model) handleInputFieldsKeys(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		// Cycle forward through fields: search -> replace -> include -> exclude -> search
		m.blurAllInputs()
		m.focusedField = (m.focusedField + 1) % 4
		m.focusCurrentInput()
		return m, textinput.Blink

	case "shift+tab":
		// Cycle backward through fields
		m.blurAllInputs()
		m.focusedField = (m.focusedField - 1 + 4) % 4
		m.focusCurrentInput()
		return m, textinput.Blink

	case "ctrl+r":
		// Toggle regex
		m.regexEnabled = !m.regexEnabled
		return m, nil

	case "ctrl+c":
		// Toggle case sensitive
		m.caseSensitive = !m.caseSensitive
		return m, nil

	case "enter":
		// Start search
		if m.searchInput.Value() != "" {
			m.searching = true
			m.searchPerformed = true
			return m, m.performSearch()
		}
		return m, nil
	}

	// Let text input handle other keys - return nil to signal fall-through
	return m, nil
}

func (m Model) handleSearchResultsKeys(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Go back to input fields
		m.State = StateInputFields
		m.resultsList.SetItems([]list.Item{}) // Clear list
		m.searchPerformed = false              // Reset search state
		return m, nil

	case " ":
		// Toggle selected result's checkbox
		idx := m.resultsList.Index()
		items := m.resultsList.Items()
		if idx >= 0 && idx < len(items) {
			if result, ok := items[idx].(SearchResult); ok {
				result.Included = !result.Included
				items[idx] = result
				m.resultsList.SetItems(items)
			}
		}

	case "a":
		// Toggle all items
		items := m.resultsList.Items()
		if len(items) == 0 {
			return m, nil
		}

		// Check if all are included
		allIncluded := true
		for _, item := range items {
			if result, ok := item.(SearchResult); ok {
				if !result.Included {
					allIncluded = false
					break
				}
			}
		}

		// Toggle all to opposite state
		newState := !allIncluded
		for i, item := range items {
			if result, ok := item.(SearchResult); ok {
				result.Included = newState
				items[i] = result
			}
		}
		m.resultsList.SetItems(items)

	case "v", "enter":
		// Jump to this result in the viewer
		items := m.resultsList.Items()
		if len(items) > 0 {
			idx := m.resultsList.Index()
			if result, ok := items[idx].(SearchResult); ok {
				// Send jump command to viewer via Skate
				m.sendJumpToViewer(result)
			}
		}
		return m, nil

	case "r":
		// Show confirmation dialog before performing replacement
		m.State = StateConfirmReplacement
		return m, nil

	default:
		// Let the list handle j/k/g/G and other navigation keys
		return m, nil
	}

	return m, nil
}

func (m Model) handleConfirmKeys(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		// Confirm - proceed with replacement
		m.State = StatePerformingReplacement
		m.progressPct = 0
		m.progressMsg = "Starting replacement..."
		return m, m.performReplacement()

	case "n", "esc":
		// Cancel - go back to results
		m.State = StateSearchResults
		return m, nil
	}

	return m, nil
}

func (m Model) handleResultsKeys(msg tea.KeyMsg) (Model, tea.Cmd) {
	// Any key exits results screen
	return m, nil
}

// sendJumpToViewer sends a jump command to the viewer via Skate
func (m *Model) sendJumpToViewer(result SearchResult) {
	// Build full path
	fullPath := filepath.Join(m.rootPath, result.Path)

	// Format: path:lineNum:searchTerm
	value := fmt.Sprintf("%s:%d:%s", fullPath, result.LineNum, result.SearchTerm)

	// Write jump command to Skate with session ID
	jumpKey := fmt.Sprintf("vinw-jump@%s", m.sessionID)
	jumpCmd := exec.Command("skate", "set", jumpKey, value)
	jumpCmd.Run() // Fire and forget

	// ALSO update vinw-current-file to keep file selection in sync
	// This prevents the normal file polling from overriding the jump
	fileKey := fmt.Sprintf("vinw-current-file@%s", m.sessionID)
	fileCmd := exec.Command("skate", "set", fileKey, fullPath)
	fileCmd.Run() // Fire and forget
}

// performSearch searches for the pattern in all files
func (m Model) performSearch() tea.Cmd {
	return func() tea.Msg {
		searchPattern := m.searchInput.Value()
		replacePattern := m.replaceInput.Value()
		results, truncated, err := m.search(searchPattern, replacePattern)
		return searchCompleteMsg{
			results:   results,
			truncated: truncated,
			err:       err,
		}
	}
}

func (m Model) search(pattern string, replacement string) ([]SearchResult, bool, error) {
	var results []SearchResult
	var mu sync.Mutex
	truncated := false

	// Compile regex if needed
	var re *regexp.Regexp
	var err error
	if m.regexEnabled {
		flags := ""
		if !m.caseSensitive {
			flags = "(?i)"
		}
		re, err = regexp.Compile(flags + pattern)
		if err != nil {
			return nil, false, fmt.Errorf("invalid regex: %w", err)
		}
	}

	// Walk directory
	err = filepath.Walk(m.rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Check if we've hit the limit
		mu.Lock()
		if len(results) >= MaxResults {
			truncated = true
			mu.Unlock()
			return filepath.SkipAll // Stop walking
		}
		mu.Unlock()

		// Skip directories
		if info.IsDir() {
			// Skip hidden directories and common ignore patterns
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == ".next" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip binary files (simple heuristic)
		if !isTextFile(path) {
			return nil
		}

		// Check include/exclude patterns
		relPath, _ := filepath.Rel(m.rootPath, path)
		if !m.shouldIncludeFile(relPath) {
			return nil
		}

		// Search in file
		fileResults, err := m.searchInFile(path, pattern, replacement, re)
		if err == nil && len(fileResults) > 0 {
			mu.Lock()
			// Only add results up to the limit
			remainingSpace := MaxResults - len(results)
			if remainingSpace > 0 {
				if len(fileResults) <= remainingSpace {
					results = append(results, fileResults...)
				} else {
					results = append(results, fileResults[:remainingSpace]...)
					truncated = true
				}
			}
			mu.Unlock()
		}

		return nil
	})

	return results, truncated, err
}

func (m Model) searchInFile(path string, pattern string, replacement string, re *regexp.Regexp) ([]SearchResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var results []SearchResult
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		var matched bool
		var newLine string

		if m.regexEnabled {
			if re.MatchString(line) {
				matched = true
				newLine = re.ReplaceAllString(line, replacement)
			}
		} else {
			// Plain text search
			searchStr := pattern
			if !m.caseSensitive {
				if strings.Contains(strings.ToLower(line), strings.ToLower(searchStr)) {
					matched = true
					// Case-insensitive replacement
					newLine = caseInsensitiveReplace(line, searchStr, replacement)
				}
			} else {
				if strings.Contains(line, searchStr) {
					matched = true
					newLine = strings.ReplaceAll(line, searchStr, replacement)
				}
			}
		}

		if matched {
			relPath, _ := filepath.Rel(m.rootPath, path)
			results = append(results, SearchResult{
				Path:          relPath,
				LineNum:       lineNum,
				Line:          line,
				NewLine:       newLine,
				SearchTerm:    pattern,
				CaseSensitive: m.caseSensitive,
				Included:      true, // Include by default
			})
		}
	}

	return results, scanner.Err()
}

func caseInsensitiveReplace(s, old, new string) string {
	// Handle empty pattern - return string unchanged
	if old == "" {
		return s
	}

	// Simple case-insensitive replace
	lowerOld := strings.ToLower(old)

	result := ""
	i := 0
	for i < len(s) {
		if i <= len(s)-len(old) && strings.ToLower(s[i:i+len(old)]) == lowerOld {
			result += new
			i += len(old)
		} else {
			result += string(s[i])
			i++
		}
	}
	return result
}

func isTextFile(path string) bool {
	// Simple heuristic: check extension
	ext := strings.ToLower(filepath.Ext(path))
	textExts := map[string]bool{
		".go": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true,
		".py": true, ".rb": true, ".java": true, ".c": true, ".cpp": true,
		".h": true, ".hpp": true, ".cs": true, ".php": true, ".rs": true,
		".txt": true, ".md": true, ".json": true, ".yaml": true, ".yml": true,
		".xml": true, ".html": true, ".css": true, ".scss": true, ".sql": true,
		".sh": true, ".bash": true, ".zsh": true, ".env": true, ".toml": true,
	}
	return textExts[ext]
}

// parsePatterns parses comma-separated glob patterns
func parsePatterns(input string) []string {
	if input == "" {
		return nil
	}
	patterns := strings.Split(input, ",")
	var result []string
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// matchesAnyPattern checks if path matches any of the glob patterns
func matchesAnyPattern(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}

	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, filepath.Base(path))
		if err == nil && matched {
			return true
		}
		// Also try matching against full relative path
		matched, err = filepath.Match(pattern, path)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// shouldIncludeFile determines if a file should be searched based on include/exclude patterns
func (m Model) shouldIncludeFile(relPath string) bool {
	includePatterns := parsePatterns(m.includeInput.Value())
	excludePatterns := parsePatterns(m.excludeInput.Value())

	// If include patterns specified, file must match at least one
	if len(includePatterns) > 0 {
		if !matchesAnyPattern(relPath, includePatterns) {
			return false
		}
	}

	// If exclude patterns specified, file must not match any
	if len(excludePatterns) > 0 {
		if matchesAnyPattern(relPath, excludePatterns) {
			return false
		}
	}

	return true
}

// performReplacement performs the actual file replacements
func (m Model) performReplacement() tea.Cmd {
	return func() tea.Msg {
		//  results from list
		items := m.resultsList.Items()
		results := make([]SearchResult, len(items))
		for i, item := range items {
			if r, ok := item.(SearchResult); ok {
				results[i] = r
			}
		}

		// Group results by file
		byFile := make(map[string][]SearchResult)
		for _, r := range results {
			if r.Included {
				byFile[r.Path] = append(byFile[r.Path], r)
			}
		}

		stats := ReplaceStats{
			TotalMatches: len(results),
		}

		for _, r := range results {
			if r.Included {
				stats.Included++
			} else {
				stats.Excluded++
			}
		}

		// Process files in parallel
		g, ctx := errgroup.WithContext(context.Background())
		g.SetLimit(8) // Max 8 concurrent

		completed := 0
		var mu sync.Mutex

		for path, results := range byFile {
			path := path
			results := results

			g.Go(func() error {
				fullPath := filepath.Join(m.rootPath, path)
				err := replaceInFile(fullPath, results)

				mu.Lock()
				completed++
				if err != nil {
					stats.Errors++
					for _, r := range results {
						r.Error = err.Error()
						stats.ErrorDetails = append(stats.ErrorDetails, r)
					}
				} else {
					stats.Successes += len(results)
				}
				mu.Unlock()

				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					return nil
				}
			})
		}

		_ = g.Wait()

		return replacementCompleteMsg{stats: stats}
	}
}

func replaceInFile(path string, results []SearchResult) error {
	// Read entire file
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")

	// Apply replacements (line numbers are 1-indexed)
	for _, r := range results {
		if r.LineNum > 0 && r.LineNum <= len(lines) {
			lines[r.LineNum-1] = r.NewLine
		}
	}

	// Write to temp file
	tmpPath := path + ".vinw.tmp"
	newContent := strings.Join(lines, "\n")
	if err := os.WriteFile(tmpPath, []byte(newContent), 0644); err != nil {
		return err
	}

	// Atomic rename
	return os.Rename(tmpPath, path)
}

// View renders the UI
func (m Model) View() string {
	switch m.State {
	case StateInputFields:
		return m.viewInputFields()
	case StateSearchResults:
		return m.viewSearchResults()
	case StateConfirmReplacement:
		return m.viewConfirmReplacement()
	case StatePerformingReplacement:
		return m.viewPerformingReplacement()
	case StateReplacementComplete:
		return m.viewReplacementComplete()
	}
	return ""
}

func (m Model) viewInputFields() string {
	headerStyle := lipgloss.NewStyle().
		Background(m.theme.Background).
		Foreground(m.theme.Foreground).
		Bold(true).
		Padding(0, 1).
		Width(m.width)

	header := headerStyle.Render("FIND & REPLACE IN PROJECT")

	// Input fields
	searchLabel := "Search:  "
	replaceLabel := "Replace: "
	includeLabel := "Include: "
	excludeLabel := "Exclude: "

	searchLine := searchLabel + m.searchInput.View()
	replaceLine := replaceLabel + m.replaceInput.View()
	includeLine := includeLabel + m.includeInput.View()
	excludeLine := excludeLabel + m.excludeInput.View()

	// Options
	regexBox := "[ ]"
	if m.regexEnabled {
		regexBox = "[x]"
	}
	caseBox := "[ ]"
	if m.caseSensitive {
		caseBox = "[x]"
	}

	optionsLine := fmt.Sprintf("%s Regex (Ctrl+R)    %s Case Sensitive (Ctrl+C)", regexBox, caseBox)

	// Status line
	statusLine := ""
	if m.searching {
		statusLine = lipgloss.NewStyle().Foreground(m.theme.BrightBlue).
			Render(fmt.Sprintf("%s Searching...", m.spinner.View()))
	} else if m.searchErr != nil {
		statusLine = lipgloss.NewStyle().Foreground(m.theme.BrightRed).Render("Error: " + m.searchErr.Error())
	} else if len(m.resultsList.Items()) > 0 {
		statusLine = lipgloss.NewStyle().Foreground(m.theme.BrightGreen).
			Render(fmt.Sprintf("Found %d matches in %d files", len(m.resultsList.Items()), m.countFiles()))
	} else if m.searchPerformed && len(m.resultsList.Items()) == 0 {
		// Only show "no matches" if a search was actually performed and returned 0 results
		statusLine = lipgloss.NewStyle().Foreground(m.theme.BrightYellow).
			Render("No matches found")
	}

	// Help (compact style with no padding)
	helpStyle := lipgloss.NewStyle().
		Foreground(m.theme.BrightBlack)

	help := helpStyle.Render("Tab: next field | Enter: search | Esc: cancel")

	// Build content with normal spacing first
	normalContent := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		searchLine,
		replaceLine,
		includeLine,
		excludeLine,
		"",
		optionsLine,
		"",
		statusLine,
	)

	// Measure actual heights
	normalHeight := lipgloss.Height(normalContent)
	helpHeight := lipgloss.Height(help)
	totalNormalHeight := normalHeight + helpHeight + 1 // +1 for separator line

	// Check if we need compact mode
	if totalNormalHeight > m.height {
		// Compact mode: remove blank lines
		compactContent := lipgloss.JoinVertical(
			lipgloss.Left,
			header,
			searchLine,
			replaceLine,
			includeLine,
			excludeLine,
			optionsLine,
			statusLine,
		)

		// Just join with no padding
		return lipgloss.JoinVertical(
			lipgloss.Left,
			compactContent,
			help,
		)
	}

	// Normal mode: calculate padding to push help to bottom
	availableLines := m.height - normalHeight - helpHeight - 1
	if availableLines < 0 {
		availableLines = 0
	}
	padding := strings.Repeat("\n", availableLines)

	return normalContent + padding + "\n" + help
}

func (m Model) viewSearchResults() string {
	// Render header and footer
	header := m.renderSearchHeader()
	footer := m.renderSearchFooter()

	// Calculate exact height the list should be
	listHeight := m.calculateListHeight()

	// Render the list and FORCE it to the correct height with lipgloss
	// Note: The Bubble Tea list component doesn't apply SetSize() immediately,
	// so we use lipgloss Height() to constrain the output to the correct size
	listView := m.resultsList.View()
	listStyle := lipgloss.NewStyle().
		Height(listHeight).
		MaxHeight(listHeight)
	constrainedList := listStyle.Render(listView)

	// Clean layout: header, list, footer (maximize list space)
	return header + "\n" + constrainedList + "\n" + footer
}

func (m Model) renderSearchHeader() string {
	headerStyle := lipgloss.NewStyle().
		Background(m.theme.Background).
		Foreground(m.theme.Foreground).
		Bold(true).
		Padding(0, 1).
		Width(m.width)

	// Count included items from list
	included := 0
	items := m.resultsList.Items()
	for _, item := range items {
		if r, ok := item.(SearchResult); ok && r.Included {
			included++
		}
	}

	headerText := fmt.Sprintf("FIND & REPLACE - %d/%d matches will be replaced", included, len(items))
	if m.resultsTruncated {
		headerText += fmt.Sprintf(" (showing first %d)", MaxResults)
	}

	headerLines := []string{headerStyle.Render(headerText)}

	// Warning if truncated
	if m.resultsTruncated {
		warningStyle := lipgloss.NewStyle().
			Foreground(m.theme.BrightYellow).
			Padding(0, 1)
		warning := warningStyle.Render(fmt.Sprintf("⚠ Results limited to %d matches. Refine your search for better results.", MaxResults))
		headerLines = append(headerLines, "", warning)
	}

	return lipgloss.JoinVertical(lipgloss.Left, headerLines...)
}

func (m Model) renderSearchFooter() string {
	helpStyle := lipgloss.NewStyle().
		Foreground(m.theme.BrightBlack).
		Padding(0, 1).
		Width(m.width)

	return helpStyle.Render("j/k: navigate | space: toggle | a: toggle all | v/enter: jump to viewer | r: replace | esc: cancel")
}

func (m Model) viewConfirmReplacement() string {
	// Count selected instances
	selectedCount := 0
	for _, item := range m.resultsList.Items() {
		if result, ok := item.(SearchResult); ok && result.Included {
			selectedCount++
		}
	}

	// Build confirmation message
	searchTerm := m.searchInput.Value()
	replaceTerm := m.replaceInput.Value()
	if replaceTerm == "" {
		replaceTerm = "(empty)"
	}

	// Confirmation box
	titleStyle := lipgloss.NewStyle().
		Foreground(m.theme.BrightYellow).
		Bold(true).
		Padding(0, 1)

	warningStyle := lipgloss.NewStyle().
		Foreground(m.theme.BrightRed).
		Bold(true).
		Padding(0, 1)

	messageStyle := lipgloss.NewStyle().
		Foreground(m.theme.Foreground).
		Padding(0, 1)

	helpStyle := lipgloss.NewStyle().
		Foreground(m.theme.BrightBlack).
		Padding(0, 1)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.BrightYellow).
		Padding(1, 2)

	title := titleStyle.Render("⚠ CONFIRM REPLACEMENT")
	warning := warningStyle.Render("This action cannot be undone!")
	message1 := messageStyle.Render(fmt.Sprintf("You are about to replace %d instance(s) of:", selectedCount))
	message2 := messageStyle.Render(fmt.Sprintf("  '%s'", searchTerm))
	message3 := messageStyle.Render("with:")
	message4 := messageStyle.Render(fmt.Sprintf("  '%s'", replaceTerm))
	help := helpStyle.Render("\ny/enter: Confirm | n/esc: Cancel")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		warning,
		"",
		message1,
		message2,
		message3,
		message4,
		help,
	)

	box := boxStyle.Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) viewPerformingReplacement() string {
	headerStyle := lipgloss.NewStyle().
		Background(m.theme.Background).
		Foreground(m.theme.Foreground).
		Bold(true).
		Padding(0, 1).
		Width(m.width)

	header := headerStyle.Render("PERFORMING REPLACEMENT...")

	progressBar := m.renderProgressBar()
	progressText := lipgloss.NewStyle().
		Foreground(m.theme.BrightBlue).
		Render(m.progressMsg)

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		"",
		"",
		header,
		"",
		progressBar,
		progressText,
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) renderProgressBar() string {
	barWidth := 50
	filled := int(m.progressPct / 100 * float64(barWidth))
	empty := barWidth - filled

	filledBar := strings.Repeat("█", filled)
	emptyBar := strings.Repeat("░", empty)

	return lipgloss.NewStyle().
		Foreground(m.theme.BrightGreen).
		Render(filledBar + emptyBar + fmt.Sprintf(" %.0f%%", m.progressPct))
}

func (m Model) viewReplacementComplete() string {
	headerStyle := lipgloss.NewStyle().
		Background(m.theme.Background).
		Foreground(m.theme.Foreground).
		Bold(true).
		Padding(0, 1).
		Width(m.width)

	header := headerStyle.Render("REPLACEMENT COMPLETE")

	successStyle := lipgloss.NewStyle().Foreground(m.theme.BrightGreen)
	errorStyle := lipgloss.NewStyle().Foreground(m.theme.BrightRed)

	stats := fmt.Sprintf(`
Total Matches: %d
Included:      %d
Excluded:      %d
Successes:     %s
Errors:        %s
`,
		m.stats.TotalMatches,
		m.stats.Included,
		m.stats.Excluded,
		successStyle.Render(fmt.Sprintf("%d", m.stats.Successes)),
		errorStyle.Render(fmt.Sprintf("%d", m.stats.Errors)),
	)

	helpStyle := lipgloss.NewStyle().
		Foreground(m.theme.BrightBlack).
		Padding(1, 0)

	help := helpStyle.Render("Press any key to continue")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		stats,
		"",
		help,
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) countFiles() int {
	fileSet := make(map[string]bool)
	for _, item := range m.resultsList.Items() {
		if r, ok := item.(SearchResult); ok {
			fileSet[r.Path] = true
		}
	}
	return len(fileSet)
}

// calculateListHeight determines the exact height for the results list
// to fill the space between header and footer
func (m Model) calculateListHeight() int {
	// Render header and footer to get their actual heights
	header := m.renderSearchHeader()
	footer := m.renderSearchFooter()
	headerHeight := lipgloss.Height(header)
	footerHeight := lipgloss.Height(footer)

	// Calculate available space for list
	// Give list maximum space - only account for actual newlines in layout
	listHeight := m.height - headerHeight - footerHeight

	// Ensure minimum height
	if listHeight < 1 {
		listHeight = 1
	}

	return listHeight
}

// resizeResultsList resizes the results list to fill available space
// This is the ONLY place list sizing should happen
func (m *Model) resizeResultsList() {
	if m.State != StateSearchResults {
		return
	}
	listHeight := m.calculateListHeight()
	m.resultsList.SetSize(m.width, listHeight)
}

// blurAllInputs blurs all text input fields
func (m *Model) blurAllInputs() {
	m.searchInput.Blur()
	m.replaceInput.Blur()
	m.includeInput.Blur()
	m.excludeInput.Blur()
}

// focusCurrentInput focuses the currently selected input field
func (m *Model) focusCurrentInput() {
	switch m.focusedField {
	case 0:
		m.searchInput.Focus()
	case 1:
		m.replaceInput.Focus()
	case 2:
		m.includeInput.Focus()
	case 3:
		m.excludeInput.Focus()
	}
}

// wrapText wraps text intelligently at word boundaries
func wrapText(text string, maxWidth int) []string {
	if len(text) <= maxWidth {
		return []string{text}
	}

	var lines []string
	var currentLine strings.Builder
	words := strings.Fields(text)

	for i, word := range words {
		// Check if adding this word would exceed width
		testLine := currentLine.String()
		if testLine != "" {
			testLine += " "
		}
		testLine += word

		if len(testLine) > maxWidth {
			// If current line has content, save it
			if currentLine.Len() > 0 {
				lines = append(lines, currentLine.String())
				currentLine.Reset()
			}

			// If word itself is too long, split it
			if len(word) > maxWidth {
				for len(word) > maxWidth {
					lines = append(lines, word[:maxWidth])
					word = word[maxWidth:]
				}
				if len(word) > 0 {
					currentLine.WriteString(word)
				}
			} else {
				currentLine.WriteString(word)
			}
		} else {
			if currentLine.Len() > 0 {
				currentLine.WriteString(" ")
			}
			currentLine.WriteString(word)
		}

		// Last word - flush
		if i == len(words)-1 && currentLine.Len() > 0 {
			lines = append(lines, currentLine.String())
		}
	}

	if len(lines) == 0 {
		return []string{text}
	}
	return lines
}

// highlightAllOccurrences highlights ALL occurrences of the search term in the line
func highlightAllOccurrences(lineSegment, searchTerm string, textColor, bgColor lipgloss.Color, caseSensitive bool) string {
	if searchTerm == "" {
		return lipgloss.NewStyle().Foreground(textColor).Render(lineSegment)
	}

	normalStyle := lipgloss.NewStyle().Foreground(textColor)
	highlightStyle := lipgloss.NewStyle().Foreground(textColor).Background(bgColor)

	// Handle case-insensitive search
	if !caseSensitive {
		lowerLine := strings.ToLower(lineSegment)
		lowerSearch := strings.ToLower(searchTerm)

		if !strings.Contains(lowerLine, lowerSearch) {
			return normalStyle.Render(lineSegment)
		}

		// Build result by finding each occurrence in the lowercase version
		// but rendering the actual case from the original line
		var result string
		remaining := lineSegment
		lowerRemaining := lowerLine

		for {
			idx := strings.Index(lowerRemaining, lowerSearch)
			if idx == -1 {
				result += normalStyle.Render(remaining)
				break
			}

			// Add part before match
			result += normalStyle.Render(remaining[:idx])
			// Add highlighted match (with original case)
			result += highlightStyle.Render(remaining[idx : idx+len(searchTerm)])
			// Continue with remainder
			remaining = remaining[idx+len(searchTerm):]
			lowerRemaining = lowerRemaining[idx+len(searchTerm):]
		}

		return result
	}

	// Case-sensitive: simple split
	if !strings.Contains(lineSegment, searchTerm) {
		return normalStyle.Render(lineSegment)
	}

	parts := strings.Split(lineSegment, searchTerm)
	var result string
	for i, part := range parts {
		result += normalStyle.Render(part)
		if i < len(parts)-1 {
			result += highlightStyle.Render(searchTerm)
		}
	}

	return result
}

// findChangedPortion finds what actually changed between two lines
// Returns (changedInOld, changedInNew)
func findChangedPortion(oldLine, newLine string) (string, string) {
	// Find common prefix
	minLen := len(oldLine)
	if len(newLine) < minLen {
		minLen = len(newLine)
	}

	prefixLen := 0
	for prefixLen < minLen && oldLine[prefixLen] == newLine[prefixLen] {
		prefixLen++
	}

	// Find common suffix
	suffixLen := 0
	for suffixLen < minLen-prefixLen &&
		oldLine[len(oldLine)-1-suffixLen] == newLine[len(newLine)-1-suffixLen] {
		suffixLen++
	}

	// Extract changed portions
	changedInOld := ""
	if prefixLen < len(oldLine)-suffixLen {
		changedInOld = oldLine[prefixLen : len(oldLine)-suffixLen]
	}

	changedInNew := ""
	if prefixLen < len(newLine)-suffixLen {
		changedInNew = newLine[prefixLen : len(newLine)-suffixLen]
	}

	return changedInOld, changedInNew
}

// highlightChangedInLine highlights ALL occurrences of the changed portion in this line segment
func highlightChangedInLine(lineSegment, changedPortion string, textColor, bgColor lipgloss.Color) string {
	// If no changed portion or it's not in this segment, just return plain
	if changedPortion == "" || !strings.Contains(lineSegment, changedPortion) {
		return lipgloss.NewStyle().Foreground(textColor).Render(lineSegment)
	}

	normalStyle := lipgloss.NewStyle().Foreground(textColor)
	highlightStyle := lipgloss.NewStyle().Foreground(textColor).Background(bgColor)

	// Highlight ALL occurrences by splitting on the changed portion
	parts := strings.Split(lineSegment, changedPortion)
	if len(parts) == 1 {
		// Shouldn't happen since we checked Contains above, but safety check
		return normalStyle.Render(lineSegment)
	}

	// Build result with highlighted occurrences
	var result string
	for i, part := range parts {
		result += normalStyle.Render(part)
		// Add highlighted changed portion between parts (but not after the last part)
		if i < len(parts)-1 {
			result += highlightStyle.Render(changedPortion)
		}
	}

	return result
}

// highlightMatches highlights the changed portion in a line using theme colors
// textColor: main color for the line (BrightRed or BrightGreen)
// highlightBg: background color for highlighted part (Red or Green)
// isOld: true for old line, false for new line
func highlightMatches(line, oldLine, newLine string, textColor, highlightBg lipgloss.Color, isOld bool) string {
	// If lines are identical, no highlighting needed
	if oldLine == newLine {
		return lipgloss.NewStyle().Foreground(textColor).Render(line)
	}

	// Find the common prefix and suffix
	minLen := len(oldLine)
	if len(newLine) < minLen {
		minLen = len(newLine)
	}

	prefixLen := 0
	for prefixLen < minLen && oldLine[prefixLen] == newLine[prefixLen] {
		prefixLen++
	}

	suffixLen := 0
	for suffixLen < minLen-prefixLen &&
		oldLine[len(oldLine)-1-suffixLen] == newLine[len(newLine)-1-suffixLen] {
		suffixLen++
	}

	// Extract the changed part from the line we're highlighting
	var changedPart string

	if isOld {
		// Highlighting the old line - show what will be removed
		if prefixLen < len(oldLine)-suffixLen {
			changedPart = oldLine[prefixLen : len(oldLine)-suffixLen]
		}
	} else {
		// Highlighting the new line - show what will be added
		if prefixLen < len(newLine)-suffixLen {
			changedPart = newLine[prefixLen : len(newLine)-suffixLen]
		}
	}

	// Build the styled output
	if changedPart != "" && strings.Contains(line, changedPart) {
		parts := strings.SplitN(line, changedPart, 2)
		if len(parts) == 2 {
			// Normal style for unchanged parts
			normalStyle := lipgloss.NewStyle().Foreground(textColor)

			// Subtle highlight for changed part - inverse colors
			highlightStyle := lipgloss.NewStyle().
				Foreground(textColor).
				Background(highlightBg)

			return normalStyle.Render(parts[0]) +
				highlightStyle.Render(changedPart) +
				normalStyle.Render(parts[1])
		}
	}

	// Fallback: just color the whole line
	return lipgloss.NewStyle().Foreground(textColor).Render(line)
}
