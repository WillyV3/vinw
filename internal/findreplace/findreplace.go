package findreplace

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
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
	Path      string
	LineNum   int
	Line      string
	NewLine   string
	Included  bool
	Error     string // For replacement errors
	Completed bool   // For replacement tracking
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
	return 4 // Path line + old line + new line + blank line
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

	// Truncate long lines to fit screen width
	maxLineWidth := m.Width() - 10 // Leave some margin
	if maxLineWidth < 20 {
		maxLineWidth = 20
	}

	oldLineText := result.Line
	if len(oldLineText) > maxLineWidth {
		oldLineText = oldLineText[:maxLineWidth] + "..."
	}

	newLineText := result.NewLine
	if len(newLineText) > maxLineWidth {
		newLineText = newLineText[:maxLineWidth] + "..."
	}

	// Base style with background for all items
	var baseStyle lipgloss.Style
	if isSelected {
		baseStyle = lipgloss.NewStyle().Background(d.theme.BrightBlue)
	} else {
		baseStyle = lipgloss.NewStyle().Background(d.theme.Black)
	}

	// Line 1: Path with checkbox
	pathStyle := baseStyle.Copy().Foreground(d.theme.Foreground)
	if isSelected {
		pathStyle = pathStyle.Bold(true)
	}
	pathLine := pathStyle.Render(fmt.Sprintf("%s %s:%d", checkbox, result.Path, result.LineNum))

	// Line 2: Old line (red)
	oldStyle := baseStyle.Copy().Foreground(d.theme.BrightRed)
	oldLine := oldStyle.Render(fmt.Sprintf("  - %s", oldLineText))

	// Line 3: New line (green)
	newStyle := baseStyle.Copy().Foreground(d.theme.BrightGreen)
	newLine := newStyle.Render(fmt.Sprintf("  + %s", newLineText))

	// Blank line with background
	blankLine := baseStyle.Render("")

	// Write all lines
	fmt.Fprintf(w, "%s\n%s\n%s\n%s\n", pathLine, oldLine, newLine, blankLine)
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
	regexEnabled  bool
	caseSensitive bool
	focusedField  int // 0=search, 1=replace

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
	rootPath string
	width    int
	height   int
	theme    lipglossthemes.Theme
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
func New(rootPath string, theme lipglossthemes.Theme) Model {
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
		focusedField: 0,
		rootPath:     rootPath,
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

		// Update list dimensions if in results state
		if m.State == StateSearchResults {
			// Recalculate based on actual rendered header/footer
			header := m.renderSearchHeader()
			footer := m.renderSearchFooter()
			headerHeight := lipgloss.Height(header)
			footerHeight := lipgloss.Height(footer)

			// Calculate vertical margins
			// Add 2 for the newlines we insert between header/list/footer
			// Add extra buffer for list internal spacing
			verticalMargins := headerHeight + footerHeight + 2
			listHeight := m.height - verticalMargins - 10  // Extra buffer
			if listHeight < 1 {
				listHeight = 1
			}

			m.resultsList.SetSize(m.width, listHeight)
		}

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
			if m.focusedField == 0 {
				m.searchInput, cmd = m.searchInput.Update(msg)
			} else {
				m.replaceInput, cmd = m.replaceInput.Update(msg)
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

			// Set items in list
			m.resultsList.SetItems(items)
			m.State = StateSearchResults

			// Resize list for results view
			header := m.renderSearchHeader()
			footer := m.renderSearchFooter()
			headerHeight := lipgloss.Height(header)
			footerHeight := lipgloss.Height(footer)
			// Calculate vertical margins
			// Add 2 for the newlines we insert between header/list/footer
			// Add extra buffer for list internal spacing
			verticalMargins := headerHeight + footerHeight + 2
			listHeight := m.height - verticalMargins - 10  // Extra buffer
			if listHeight < 1 {
				listHeight = 1
			}
			m.resultsList.SetSize(m.width, listHeight)
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
	case StateReplacementComplete:
		return m.handleResultsKeys(msg)
	}
	return m, nil
}

func (m Model) handleInputFieldsKeys(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "tab", "shift+tab":
		// Toggle focus between search and replace
		if m.focusedField == 0 {
			m.focusedField = 1
			m.searchInput.Blur()
			m.replaceInput.Focus()
		} else {
			m.focusedField = 0
			m.replaceInput.Blur()
			m.searchInput.Focus()
		}
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

	case "enter":
		// Perform replacement on included items
		m.State = StatePerformingReplacement
		m.progressPct = 0
		m.progressMsg = "Starting replacement..."
		return m, m.performReplacement()

	default:
		// Let the list handle j/k/g/G and other navigation keys
		return m, nil
	}

	return m, nil
}

func (m Model) handleResultsKeys(msg tea.KeyMsg) (Model, tea.Cmd) {
	// Any key exits results screen
	return m, nil
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
				Path:     relPath,
				LineNum:  lineNum,
				Line:     line,
				NewLine:  newLine,
				Included: true, // Include by default
			})
		}
	}

	return results, scanner.Err()
}

func caseInsensitiveReplace(s, old, new string) string {
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

	searchLine := searchLabel + m.searchInput.View()
	replaceLine := replaceLabel + m.replaceInput.View()

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

	// Render the list
	listView := m.resultsList.View()

	// Calculate heights
	headerHeight := lipgloss.Height(header)
	listHeight := lipgloss.Height(listView)
	footerHeight := lipgloss.Height(footer)

	// Calculate padding to push footer to bottom
	usedHeight := headerHeight + listHeight + footerHeight + 2 // +2 for newlines
	availableLines := m.height - usedHeight
	if availableLines < 0 {
		availableLines = 0
	}
	padding := strings.Repeat("\n", availableLines)

	// Assemble with padding to push footer to bottom
	return header + "\n" + listView + padding + "\n" + footer
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

	return helpStyle.Render("j/k: navigate | space: toggle | a: toggle all | enter: replace | esc: cancel")
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
