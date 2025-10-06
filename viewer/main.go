package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// Styles
var (
	// titleStyle will be dynamically created based on theme
	titleStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("30")). // Default to Teal theme
			Foreground(lipgloss.Color("230")).
			Bold(true).
			Padding(0, 1)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	lineNumberStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("239")).
			MarginRight(1)
)

// Messages
type fileCheckMsg struct{}
type fileContentMsg struct {
	path    string
	content string
}
type editorFinishedMsg struct{ err error }

// Model
type model struct {
	viewport        viewport.Model
	currentFile     string
	content         string
	ready           bool
	width           int
	height          int
	sessionID       string   // Session ID for Skate isolation
	mouseEnabled    bool     // Toggle for mouse mode
	showEditorPicker bool    // Whether to show editor selection UI
	availableEditors []string // List of available editors
	editorCursor     int      // Selected editor in picker
}

func (m model) Init() tea.Cmd {
	// Start checking for file changes
	return tea.Batch(
		m.checkFile(),
		pollFile(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		headerHeight := lipgloss.Height(m.headerView())
		footerHeight := lipgloss.Height(m.footerView())
		verticalMargins := headerHeight + footerHeight

		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMargins)
			m.viewport.YPosition = headerHeight
			m.viewport.SetContent(m.content)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMargins
		}

	case tea.KeyMsg:
		// Handle editor picker navigation
		if m.showEditorPicker {
			switch msg.String() {
			case "q", "ctrl+c", "esc":
				m.showEditorPicker = false
				return m, nil
			case "j", "down":
				if m.editorCursor < len(m.availableEditors)-1 {
					m.editorCursor++
				}
				return m, nil
			case "k", "up":
				if m.editorCursor > 0 {
					m.editorCursor--
				}
				return m, nil
			case "enter":
				// Save preference and open editor
				if m.editorCursor < len(m.availableEditors) {
					selectedEditor := m.availableEditors[m.editorCursor]
					setEditorPreference(m.sessionID, selectedEditor)
					m.showEditorPicker = false
					return m, openEditor(selectedEditor, m.currentFile)
				}
				return m, nil
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			// Manual refresh
			return m, m.checkFile()
		case "m":
			// Toggle mouse mode
			m.mouseEnabled = !m.mouseEnabled
			if m.mouseEnabled {
				return m, tea.EnableMouseCellMotion
			}
			return m, tea.DisableMouse
		case "e":
			// Edit current file
			if m.currentFile == "" {
				return m, nil // No file to edit
			}

			// Check for saved editor preference
			preferredEditor := getEditorPreference(m.sessionID)
			if preferredEditor != "" {
				// Use saved preference
				return m, openEditor(preferredEditor, m.currentFile)
			}

			// No preference - detect and show picker
			m.availableEditors = detectAvailableEditors()
			if len(m.availableEditors) == 0 {
				// No editors found
				return m, nil
			} else if len(m.availableEditors) == 1 {
				// Only one editor - use it directly
				setEditorPreference(m.sessionID, m.availableEditors[0])
				return m, openEditor(m.availableEditors[0], m.currentFile)
			}

			// Multiple editors - show picker
			m.showEditorPicker = true
			m.editorCursor = 0
			return m, nil
		}

	case fileCheckMsg:
		// Check for new file selection
		return m, tea.Batch(
			m.checkFile(),
			pollFile(), // Continue polling
		)

	case editorFinishedMsg:
		// Editor closed - refresh the file content
		return m, m.checkFile()

	case fileContentMsg:
		// Only update if something actually changed
		if msg.path == "" && msg.content == "" && m.currentFile != "" {
			// This was an empty read but we have content - keep current state
			return m, nil
		}

		// Check if this is the initial "no file" message
		if msg.path == "" && m.currentFile == "" {
			// First time, show the message
			m.viewport.SetContent("No file selected.\n\nPress Enter in vinw to select a file to view.")
			return m, nil
		}

		// Update content if file actually changed
		if msg.path != m.currentFile || (msg.path != "" && msg.content != m.content) {
			m.currentFile = msg.path
			m.content = msg.content

			// Process content based on file type
			processedContent := processFileContent(msg.path, msg.content, m.width)

			m.viewport.SetContent(processedContent)
			m.viewport.GotoTop()
		}
		return m, nil
	}

	// Update viewport (handles scrolling)
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if !m.ready {
		return "\n  Initializing viewer..."
	}

	// Show editor picker overlay
	if m.showEditorPicker {
		// Build content using plain strings (no styling in loop)
		s := strings.Builder{}
		s.WriteString("Choose Your Editor\n\n")

		for i, editor := range m.availableEditors {
			if i == m.editorCursor {
				s.WriteString("(•) ")
			} else {
				s.WriteString("( ) ")
			}
			s.WriteString(editor)
			s.WriteString("\n")
		}

		s.WriteString("\n")
		s.WriteString("j/k: navigate • enter: select • esc: cancel")

		// Apply styling AFTER building the plain string
		pickerStyle := lipgloss.NewStyle().
			Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62"))

		return lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			pickerStyle.Render(s.String()),
		)
	}

	return fmt.Sprintf("%s\n%s\n%s", m.headerView(), m.viewport.View(), m.footerView())
}

func (m model) headerView() string {
	title := "ⓋⒾⓃⓌ ⓋⒾⒺⓌⒺⓇ"
	if m.currentFile != "" {
		title = fmt.Sprintf("ⓋⒾⓃⓌ ⓋⒾⒺⓌⒺⓇ • %s", filepath.Base(m.currentFile))
	}
	return titleStyle.Width(m.width).Render(title)
}

func (m model) footerView() string {
	scrollPercent := fmt.Sprintf("%3.f%%", m.viewport.ScrollPercent()*100)

	mouseStatus := "scroll"
	if !m.mouseEnabled {
		mouseStatus = "select/copy"
	}

	// Two lines for skinny layout
	line1 := fmt.Sprintf("Line %d/%d • %s",
		m.viewport.YOffset+1,
		m.viewport.TotalLineCount(),
		scrollPercent)
	line2 := fmt.Sprintf("e: edit • m: mouse [%s] • r: refresh • q: quit", mouseStatus)
	info := line1 + "\n" + line2

	return infoStyle.Width(m.width).Render(info)
}

// Commands

func pollFile() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return fileCheckMsg{}
	})
}

func (m model) checkFile() tea.Cmd {
	return func() tea.Msg {
		// Update theme from Skate (doesn't affect file content)
		updateThemeWithSession(m.sessionID)

		// Get current file from Skate
		filePath := getSelectedFileWithSession(m.sessionID)
		if filePath == "" {
			// Don't immediately clear - might be a temporary Skate read issue
			// The Update method will handle this appropriately
			return fileContentMsg{
				path:    "",
				content: "",
			}
		}

		// File exists, read it
		content := readFileContent(filePath)
		return fileContentMsg{
			path:    filePath,
			content: content,
		}
	}
}

// updateTheme updates the title style based on current theme
func updateTheme() {
	// Get theme colors from Skate
	cmd := exec.Command("skate", "get", "vinw-theme-bg")
	bgBytes, _ := cmd.Output()
	bg := strings.TrimSpace(string(bgBytes))

	cmd = exec.Command("skate", "get", "vinw-theme-fg")
	fgBytes, _ := cmd.Output()
	fg := strings.TrimSpace(string(fgBytes))

	// Default to first theme (Teal) if no theme set
	if bg == "" {
		bg = "30" // Teal from theme.go
	}
	if fg == "" {
		fg = "230"
	}

	// Update title style with theme colors
	titleStyle = lipgloss.NewStyle().
		Background(lipgloss.Color(bg)).
		Foreground(lipgloss.Color(fg)).
		Bold(true).
		Padding(0, 1)
}

// Track current theme to avoid unnecessary updates
var (
	currentBg = ""
	currentFg = ""
)

// updateThemeWithSession updates the title style based on current theme with session
func updateThemeWithSession(sessionID string) {
	// Read theme colors in parallel for faster updates
	var wg sync.WaitGroup
	var bg, fg string
	wg.Add(2)

	go func() {
		defer wg.Done()
		cmd := exec.Command("skate", "get", fmt.Sprintf("vinw-theme-bg@%s", sessionID))
		bgBytes, _ := cmd.Output()
		bg = strings.TrimSpace(string(bgBytes))
	}()

	go func() {
		defer wg.Done()
		cmd := exec.Command("skate", "get", fmt.Sprintf("vinw-theme-fg@%s", sessionID))
		fgBytes, _ := cmd.Output()
		fg = strings.TrimSpace(string(fgBytes))
	}()

	wg.Wait()

	// Default to first theme (Teal) if no theme set
	if bg == "" {
		bg = "30" // Teal from theme.go
	}
	if fg == "" {
		fg = "230"
	}

	// Only update if theme actually changed
	if bg != currentBg || fg != currentFg {
		currentBg = bg
		currentFg = fg

		// Update title style with theme colors
		titleStyle = lipgloss.NewStyle().
			Background(lipgloss.Color(bg)).
			Foreground(lipgloss.Color(fg)).
			Bold(true).
			Padding(0, 1)
	}
}

// Editor helper functions

// detectAvailableEditors finds all installed terminal editors
func detectAvailableEditors() []string {
	editors := []string{"nvim", "vim", "nano", "emacs", "vi"}
	available := []string{}

	for _, editor := range editors {
		if _, err := exec.LookPath(editor); err == nil {
			available = append(available, editor)
		}
	}

	return available
}

// getEditorPreference gets the saved editor preference for this session
func getEditorPreference(sessionID string) string {
	cmd := exec.Command("skate", "get", fmt.Sprintf("vinw-editor@%s", sessionID))
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// setEditorPreference saves the editor preference for this session
func setEditorPreference(sessionID, editor string) {
	cmd := exec.Command("skate", "set", fmt.Sprintf("vinw-editor@%s", sessionID), editor)
	cmd.Run()
}

// openEditor suspends the TUI and opens the file in the specified editor
func openEditor(editor, filePath string) tea.Cmd {
	c := exec.Command(editor, filePath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err}
	})
}

// Helper functions

func getSelectedFile() string {
	cmd := exec.Command("skate", "get", "vinw-current-file")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func getSelectedFileWithSession(sessionID string) string {
	cmd := exec.Command("skate", "get", fmt.Sprintf("vinw-current-file@%s", sessionID))
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func readFileContent(path string) string {
	if path == "" {
		return "No file selected."
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("Error opening file: %v", err)
	}
	defer file.Close()

	// Read up to 1MB to prevent huge files from breaking the viewer
	limited := io.LimitReader(file, 1024*1024)
	content, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Sprintf("Error reading file: %v", err)
	}

	return string(content)
}

func isCodeFile(path string) bool {
	// Simple check for code files based on extension
	ext := strings.ToLower(filepath.Ext(path))
	codeExts := []string{".go", ".js", ".ts", ".py", ".rb", ".java", ".c", ".cpp", ".h", ".rs", ".sh", ".yml", ".yaml", ".json", ".xml", ".html", ".css", ".scss", ".sql", ".swift", ".kt", ".scala", ".r", ".m", ".mm"}

	for _, codeExt := range codeExts {
		if ext == codeExt {
			return true
		}
	}
	return false
}

func isMarkdown(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown" || ext == ".mdown"
}

func processFileContent(path string, content string, width int) string {
	if isMarkdown(path) {
		// Render markdown with glamour using dracula theme
		renderer, err := glamour.NewTermRenderer(
			glamour.WithStylePath("dracula"),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			// Fall back to auto style if dracula not available
			renderer, err = glamour.NewTermRenderer(
				glamour.WithAutoStyle(),
				glamour.WithWordWrap(width),
			)
			if err != nil {
				return content
			}
		}

		rendered, err := renderer.Render(content)
		if err != nil {
			return content
		}
		return rendered
	} else if isCodeFile(path) {
		// Syntax highlight code files
		// Get lexer for the file type
		lexer := lexers.Match(path)
		if lexer == nil {
			// Try to get lexer by extension
			ext := strings.TrimPrefix(filepath.Ext(path), ".")
			lexer = lexers.Get(ext)
		}
		if lexer == nil {
			// If no lexer found, just add line numbers
			return addLineNumbers(content)
		}

		// Get style - try Dracula first, then Monokai
		style := styles.Get("dracula")
		if style == nil {
			style = styles.Get("monokai")
		}
		if style == nil {
			style = styles.Get("github-dark")
		}
		if style == nil {
			// Fall back to a default style
			style = styles.Fallback
		}

		// Get formatter
		formatter := formatters.Get("terminal16m")
		if formatter == nil {
			formatter = formatters.Get("terminal256")
		}
		if formatter == nil {
			formatter = formatters.Get("terminal")
		}

		// Tokenize the content
		tokens, err := lexer.Tokenise(nil, content)
		if err != nil {
			return addLineNumbers(content)
		}

		// Format the tokens
		var buf bytes.Buffer
		err = formatter.Format(&buf, style, tokens)
		if err != nil {
			return addLineNumbers(content)
		}

		// Add line numbers to the highlighted content
		highlighted := buf.String()
		if highlighted == "" || highlighted == content {
			// If no actual highlighting happened, just add line numbers
			return addLineNumbers(content)
		}
		return addLineNumbers(highlighted)
	}

	// For other files, just return as-is
	return content
}

func addLineNumbers(content string) string {
	lines := strings.Split(content, "\n")
	maxLineNum := len(lines)
	width := len(fmt.Sprintf("%d", maxLineNum))

	var result strings.Builder
	for i, line := range lines {
		lineNum := fmt.Sprintf("%*d", width, i+1)
		result.WriteString(lineNumberStyle.Render(lineNum))
		result.WriteString(line)
		if i < len(lines)-1 {
			result.WriteString("\n")
		}
	}

	return result.String()
}

func main() {
	// Get session ID from command line argument
	var sessionID string
	if len(os.Args) > 1 {
		sessionID = os.Args[1]
		fmt.Printf("Starting vinw viewer with session: %s\n", sessionID)
	} else {
		fmt.Println("Usage: vinw-viewer <session-id>")
		fmt.Println("\nGet the session ID from the vinw instance you want to connect to.")
		os.Exit(1)
	}

	fmt.Println("Waiting for file selection from vinw...")
	fmt.Println()

	// Initialize theme on startup with session
	updateThemeWithSession(sessionID)

	p := tea.NewProgram(
		model{
			sessionID:    sessionID,
			mouseEnabled: true, // Start with mouse enabled for scrolling
		},
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
