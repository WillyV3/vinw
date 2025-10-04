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
	titleStyle lipgloss.Style

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

// Model
type model struct {
	viewport    viewport.Model
	currentFile string
	content     string
	ready       bool
	width       int
	height      int
}

func (m model) Init() tea.Cmd {
	// Start checking for file changes
	return tea.Batch(
		checkFile(),
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
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			// Manual refresh
			return m, checkFile()
		}

	case fileCheckMsg:
		// Check for new file selection
		return m, tea.Batch(
			checkFile(),
			pollFile(), // Continue polling
		)

	case fileContentMsg:
		// Update content if file changed
		if msg.path != m.currentFile || msg.content != m.content {
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
	return fmt.Sprintf("%s\n%s\n%s", m.headerView(), m.viewport.View(), m.footerView())
}

func (m model) headerView() string {
	title := "vinw viewer"
	if m.currentFile != "" {
		title = fmt.Sprintf("vinw viewer • %s", filepath.Base(m.currentFile))
	}
	return titleStyle.Width(m.width).Render(title)
}

func (m model) footerView() string {
	scrollPercent := fmt.Sprintf("%3.f%%", m.viewport.ScrollPercent()*100)

	info := fmt.Sprintf("Line %d/%d • %s • q: quit • r: refresh",
		m.viewport.YOffset+1,
		m.viewport.TotalLineCount(),
		scrollPercent)

	return infoStyle.Width(m.width).Render(info)
}

// Commands

func pollFile() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return fileCheckMsg{}
	})
}

func checkFile() tea.Cmd {
	return func() tea.Msg {
		// Update theme from Skate
		updateTheme()

		filePath := getSelectedFile()
		if filePath == "" {
			return fileContentMsg{
				path:    "",
				content: "No file selected.\n\nPress Enter in vinw to select a file to view.",
			}
		}

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

	// Default if no theme set
	if bg == "" {
		bg = "62"
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

// Helper functions

func getSelectedFile() string {
	cmd := exec.Command("skate", "get", "vinw-current-file")
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
	fmt.Println("Starting vinw viewer...")
	fmt.Println("Waiting for file selection from vinw...")
	fmt.Println()

	// Initialize theme on startup
	updateTheme()

	p := tea.NewProgram(
		model{},
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}