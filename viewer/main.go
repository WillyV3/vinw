package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	lipglossthemes "github.com/willyv3/gogh-themes/lipgloss"
)

// Messages
type fileCheckMsg struct{}
type themeUpdateMsg struct {
	theme lipglossthemes.Theme
	name  string
}
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
	sessionID       string                 // Session ID for Skate isolation
	mouseEnabled    bool                   // Toggle for mouse mode
	showEditorPicker bool                  // Whether to show editor selection UI
	availableEditors []string              // List of available editors
	editorCursor     int                    // Selected editor in picker
	currentTheme    lipglossthemes.Theme   // Current theme from gogh-themes
	themeName       string                 // Current theme name
	chromaStyle     *chroma.Style          // Pre-built chroma style for syntax highlighting
}

func (m model) Init() tea.Cmd {
	// Start checking for file and theme changes
	return tea.Batch(
		m.checkTheme(),
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

	case themeUpdateMsg:
		// Theme changed - rebuild chroma style and re-process content
		m.currentTheme = msg.theme
		m.themeName = msg.name
		m.chromaStyle = buildChromaStyle(msg.theme, msg.name)

		// Re-process current file content with new theme colors
		if m.currentFile != "" && m.content != "" {
			processedContent := processFileContent(m.currentFile, m.content, m.width, m.currentTheme, m.chromaStyle)
			m.viewport.SetContent(processedContent)
		}

		return m, nil

	case fileCheckMsg:
		// Check for new file selection and theme updates
		return m, tea.Batch(
			m.checkTheme(),
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

			// Process content based on file type using pre-built chroma style
			processedContent := processFileContent(msg.path, msg.content, m.width, m.currentTheme, m.chromaStyle)

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

	// Use Foreground on Background for guaranteed contrast
	// These two colors are designed to work together in every theme
	titleStyle := lipgloss.NewStyle().
		Background(m.currentTheme.Background).
		Foreground(m.currentTheme.Foreground).
		Bold(true).
		Padding(0, 1)

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

	// Use theme's Foreground for good contrast with terminal background
	infoStyle := lipgloss.NewStyle().
		Foreground(m.currentTheme.Foreground)

	return infoStyle.Width(m.width).Render(info)
}

// Commands

func pollFile() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return fileCheckMsg{}
	})
}

func (m model) checkTheme() tea.Cmd {
	return func() tea.Msg {
		// Read theme name from Skate
		cmd := exec.Command("skate", "get", fmt.Sprintf("vinw-theme-name@%s", m.sessionID))
		nameBytes, _ := cmd.Output()
		themeName := strings.TrimSpace(string(nameBytes))

		// Only send update if theme actually changed
		if themeName != "" && themeName != m.themeName {
			if theme, ok := lipglossthemes.Get(themeName); ok {
				return themeUpdateMsg{
					theme: theme,
					name:  themeName,
				}
			}
		}

		// No change - return nil (no-op)
		return nil
	}
}

func (m model) checkFile() tea.Cmd {
	return func() tea.Msg {
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

// buildChromaStyle creates a dynamic chroma style from the theme's 16 ANSI colors
func buildChromaStyle(theme lipglossthemes.Theme, themeName string) *chroma.Style {
	// Convert lipgloss.Color to hex strings for chroma
	bg := string(theme.Background)
	fg := string(theme.Foreground)

	// Use theme name in style name to avoid chroma caching the same style
	styleName := "vinw-" + themeName

	// Build chroma style entries mapping token types to theme colors
	return chroma.MustNewStyle(styleName, chroma.StyleEntries{
		chroma.Background:      bg,
		chroma.Text:            fg,
		chroma.Error:           string(theme.BrightRed),
		chroma.Comment:         string(theme.BrightBlack),
		chroma.CommentPreproc:  string(theme.Cyan),
		chroma.Keyword:         string(theme.Magenta),
		chroma.KeywordType:     string(theme.Blue),
		chroma.Operator:        string(theme.Magenta),
		chroma.Punctuation:     fg,
		chroma.Name:            fg,
		chroma.NameBuiltin:     string(theme.Yellow),
		chroma.NameFunction:    string(theme.Yellow),
		chroma.NameClass:       string(theme.Blue),
		chroma.NameNamespace:   string(theme.Cyan),
		chroma.NameException:   string(theme.Red),
		chroma.NameVariable:    string(theme.Cyan),
		chroma.NameConstant:    string(theme.BrightYellow),
		chroma.NameAttribute:   string(theme.Cyan),
		chroma.NameTag:         string(theme.Magenta),
		chroma.LiteralString:   string(theme.Green),
		chroma.LiteralNumber:   string(theme.Cyan),
		chroma.Literal:         string(theme.Green),
		chroma.LiteralDate:     string(theme.Green),
		chroma.Generic:         fg,
		chroma.GenericDeleted:  string(theme.Red),
		chroma.GenericEmph:     fg + " italic",
		chroma.GenericInserted: string(theme.Green),
		chroma.GenericStrong:   fg + " bold",
		chroma.GenericHeading:  string(theme.BrightCyan) + " bold",
	})
}

func processFileContent(path string, content string, width int, theme lipglossthemes.Theme, chromaStyle *chroma.Style) string {
	if isCodeFile(path) || isMarkdown(path) {
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
			return addLineNumbers(content, theme)
		}

		// Use our dynamically built style with actual theme colors
		style := chromaStyle

		// Get formatter - use terminal16m for true color support
		formatter := formatters.Get("terminal16m")

		// Tokenize the content
		tokens, err := lexer.Tokenise(nil, content)
		if err != nil {
			return addLineNumbers(content, theme)
		}

		// Format the tokens
		var buf bytes.Buffer
		err = formatter.Format(&buf, style, tokens)
		if err != nil {
			return addLineNumbers(content, theme)
		}

		// Add line numbers to the highlighted content
		highlighted := buf.String()
		if highlighted == "" || highlighted == content {
			// If no actual highlighting happened, just add line numbers
			return addLineNumbers(content, theme)
		}
		return addLineNumbers(highlighted, theme)
	}

	// For other files, just return as-is
	return content
}

func addLineNumbers(content string, theme lipglossthemes.Theme) string {
	lines := strings.Split(content, "\n")
	maxLineNum := len(lines)
	width := len(fmt.Sprintf("%d", maxLineNum))

	// Create line number style from theme
	lineNumberStyle := lipgloss.NewStyle().
		Foreground(theme.BrightBlack).
		MarginRight(1)

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

	// Initialize theme on startup - try to load from Skate, fallback to Dracula
	var initialTheme lipglossthemes.Theme
	var initialThemeName string

	cmd := exec.Command("skate", "get", fmt.Sprintf("vinw-theme-name@%s", sessionID))
	nameBytes, _ := cmd.Output()
	themeName := strings.TrimSpace(string(nameBytes))

	if themeName != "" {
		if theme, ok := lipglossthemes.Get(themeName); ok {
			initialTheme = theme
			initialThemeName = themeName
		}
	}

	// Fallback to Dracula if no theme found
	if initialThemeName == "" {
		if theme, ok := lipglossthemes.Get("Dracula"); ok {
			initialTheme = theme
			initialThemeName = "Dracula"
		}
	}

	// Build initial chroma style
	initialChromaStyle := buildChromaStyle(initialTheme, initialThemeName)

	p := tea.NewProgram(
		model{
			sessionID:    sessionID,
			mouseEnabled: true, // Start with mouse enabled for scrolling
			currentTheme: initialTheme,
			themeName:    initialThemeName,
			chromaStyle:  initialChromaStyle,
		},
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
