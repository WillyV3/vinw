package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/tree"
	"vinw/internal"
)

// Styles
var (
	changedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true)

	normalStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	headerStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("62")).
		Foreground(lipgloss.Color("230")).
		Bold(true).
		Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("243")).
		Padding(0, 1)
)

// Messages
type tickMsg time.Time

// Model
type model struct {
	rootPath       string
	tree           *tree.Tree
	treeString     string            // Cached tree string
	treeLines      []string          // Cached tree lines
	maxLine        int               // Cached max line number
	viewport       viewport.Model
	ready          bool
	width          int
	height         int
	diffCache      map[string]int    // Cache for git diff results
	lastContent    string            // Track last content to avoid unnecessary updates
	gitignore      *internal.GitIgnore        // GitIgnore patterns
	respectIgnore  bool              // Whether to respect .gitignore
	selectedLine   int               // Currently selected line in viewport
	fileMap        map[int]string    // Map of line number to file path
	showHelp       bool              // Whether to show help
	showViewer     bool              // Whether to show viewer command popup
	showStartup    bool              // Whether to show startup message
	theme          *internal.ThemeManager     // Theme manager
	sessionID      string            // Unique session ID for this instance
}

// updateTreeCache updates the cached tree string and related values
func (m *model) updateTreeCache() {
	m.treeString = m.tree.String()
	m.treeLines = strings.Split(m.treeString, "\n")
	m.maxLine = len(m.treeLines) - 1
	if m.maxLine < 0 {
		m.maxLine = 0
	}
}

func (m model) Init() tea.Cmd {
	return tick()
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
			// Rebuild tree with initial settings
			m.tree, m.fileMap = buildTreeWithMap(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore)
			m.updateTreeCache()
			content := renderTreeWithSelection(m.treeString, m.selectedLine)
			m.viewport.SetContent(content)
			m.lastContent = content
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMargins
		}

	case tea.KeyMsg:
		// If startup message is showing, handle special keys
		if m.showStartup {
			switch msg.String() {
			case "c":
				// Copy viewer command to clipboard
				viewerCmd := fmt.Sprintf("vinw-viewer %s", m.sessionID)
				copyCmd := exec.Command("pbcopy")
				copyCmd.Stdin = strings.NewReader(viewerCmd)
				copyCmd.Run() // Ignore errors, not all systems have pbcopy
				m.showStartup = false
				return m, nil
			case "q", "ctrl+c":
				return m, tea.Quit
			default:
				// Dismiss startup on any other key
				m.showStartup = false
				return m, nil
			}
		}

		// If help is showing, any key dismisses it
		if m.showHelp {
			switch msg.String() {
			case "?":
				m.showHelp = false
				return m, nil
			case "q", "ctrl+c":
				return m, tea.Quit
			default:
				// Dismiss help on any other key
				m.showHelp = false
			}
		}

		// If viewer popup is showing, handle special keys
		if m.showViewer {
			switch msg.String() {
			case "c":
				// Copy viewer command to clipboard
				viewerCmd := fmt.Sprintf("vinw-viewer %s", m.sessionID)
				copyCmd := exec.Command("pbcopy")
				copyCmd.Stdin = strings.NewReader(viewerCmd)
				copyCmd.Run() // Ignore errors, not all systems have pbcopy
				m.showViewer = false
				return m, nil
			case "v", "escape":
				m.showViewer = false
				return m, nil
			case "q", "ctrl+c":
				return m, tea.Quit
			default:
				// Dismiss viewer popup on any other key
				m.showViewer = false
			}
		}

		switch msg.String() {
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "v":
			m.showViewer = !m.showViewer
			return m, nil
		case "q", "ctrl+c":
			return m, tea.Quit
		case "t":
			// Next theme
			m.theme.NextTheme()
			return m, nil
		case "T":
			// Previous theme
			m.theme.PreviousTheme()
			return m, nil
		case "i":
			// Toggle gitignore respect
			m.respectIgnore = !m.respectIgnore

			// Remember the currently selected file if one exists
			var currentFile string
			if f, ok := m.fileMap[m.selectedLine]; ok {
				currentFile = f
			}

			// Rebuild tree with new ignore setting
			m.tree, m.fileMap = buildTreeWithMap(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore)
			m.updateTreeCache()

			// Try to find the same file in the new map
			newSelectedLine := 0
			if currentFile != "" {
				for line, file := range m.fileMap {
					if file == currentFile {
						newSelectedLine = line
						break
					}
				}
			}

			// Ensure selected line is within bounds
			if newSelectedLine > m.maxLine {
				newSelectedLine = m.maxLine
			}
			if newSelectedLine < 0 {
				newSelectedLine = 0
			}
			m.selectedLine = newSelectedLine

			// Update viewport with new selection
			newContent := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
			m.viewport.SetContent(newContent)
			m.lastContent = newContent
			return m, nil
		case "j", "down":
			// Move selection down using cached values
			if m.selectedLine < m.maxLine {
				m.selectedLine++
				// Update viewport with highlighted line
				content := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
				m.viewport.SetContent(content)
				// Auto-scroll if needed
				if m.selectedLine >= m.viewport.YOffset+m.viewport.Height-1 {
					m.viewport.LineDown(1)
				}
			}
			return m, nil
		case "k", "up":
			// Move selection up using cached values
			if m.selectedLine > 0 {
				m.selectedLine--
				// Update viewport with highlighted line
				content := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
				m.viewport.SetContent(content)
				// Auto-scroll if needed
				if m.selectedLine < m.viewport.YOffset {
					m.viewport.LineUp(1)
				}
			}
			return m, nil
		case "enter":
			// Get the file at the selected line (only files are in the map, not directories)
			if filePath, ok := m.fileMap[m.selectedLine]; ok {
				fullPath := filepath.Join(m.rootPath, filePath)

				// Make sure it's actually a file, not a directory
				if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
					// Write to Skate for viewer to pick up, silently ignore errors
					key := fmt.Sprintf("vinw-current-file@%s", m.sessionID)
					cmd := exec.Command("skate", "set", key, fullPath)
					cmd.Run() // Ignore errors silently
				}
			}
			// If it's a directory or not in map, do nothing (directories aren't selectable)
			return m, nil
		}

	case tickMsg:
		// Update git diff cache efficiently with one call
		m.diffCache = internal.GetAllGitDiffs()

		// Remember the currently selected file if one exists
		var currentFile string
		if f, ok := m.fileMap[m.selectedLine]; ok {
			currentFile = f
		}

		// Rebuild tree with cached diff data and gitignore settings
		m.tree, m.fileMap = buildTreeWithMap(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore)
		m.updateTreeCache()

		// Try to maintain selection on the same file
		if currentFile != "" {
			for line, file := range m.fileMap {
				if file == currentFile {
					m.selectedLine = line
					break
				}
			}
		}

		// Ensure selected line is within bounds
		if m.selectedLine > m.maxLine {
			m.selectedLine = m.maxLine
		}
		if m.selectedLine < 0 {
			m.selectedLine = 0
		}

		// Only update viewport if content has changed
		newContent := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
		if newContent != m.lastContent {
			m.viewport.SetContent(newContent)
			m.lastContent = newContent
		}

		return m, tick()
	}

	// Update viewport (handles scrolling)
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}

	// Show startup message with viewer command
	if m.showStartup {
		startupText := fmt.Sprintf(`╭─────────────────────────────────────╮
│         Welcome to vinw!            │
╰─────────────────────────────────────╯

Session ID: %s

To open the viewer, run in another terminal:

  vinw-viewer %s

Press 'c' to copy command to clipboard
Press any other key to continue...`, m.sessionID, m.sessionID)

		startupStyle := lipgloss.NewStyle().
			Padding(2, 4).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62"))

		return lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			startupStyle.Render(startupText),
		)
	}

	// Show viewer popup
	if m.showViewer {
		viewerText := fmt.Sprintf(`╭─────────────────────────────────────╮
│       Open Paired Viewer            │
╰─────────────────────────────────────╯

Run this command in another terminal:

  vinw-viewer %s

Session ID: %s

Press 'c' to copy command to clipboard
Press any other key to dismiss...`, m.sessionID, m.sessionID)

		viewerStyle := lipgloss.NewStyle().
			Padding(2, 4).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("42"))

		return lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			viewerStyle.Render(viewerText),
		)
	}

	if m.showHelp {
		helpText := `╭─────────────────────────────────────╮
│          vinw Help Guide            │
╰─────────────────────────────────────╯

Setup
─────
  Terminal 1    vinw
  Terminal 2    vinw-viewer

Navigation
──────────
  j, ↓          Move down
  k, ↑          Move up
  Enter         Select file to view
  i             Toggle gitignore
  v             Show viewer command
  ?             Toggle this help
  q             Quit

Git Features
────────────
  • Shows uncommitted changes (+N)
  • Works without remote repos
  • Auto-creates GitHub repos

Press any key to dismiss...`

		helpStyle := lipgloss.NewStyle().
			Padding(2, 4).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62"))

		return lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			helpStyle.Render(helpText),
		)
	}

	return fmt.Sprintf("%s\n%s\n%s", m.headerView(), m.viewport.View(), m.footerView())
}

func shortenPath(path string) string {
	home := os.Getenv("HOME")
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func (m model) headerView() string {
	shortPath := shortenPath(m.rootPath)
	title := fmt.Sprintf("Vinw - %s", shortPath)
	// Use theme colors for header
	themedHeaderStyle := m.theme.CreateHeaderStyle()
	return themedHeaderStyle.Width(m.width).Render(title)
}

func (m model) footerView() string {
	ignoreStatus := "OFF"
	if m.respectIgnore {
		ignoreStatus = "ON"
	}
	// Two lines for skinny layout
	line1 := fmt.Sprintf("j/k: nav | i: git [%s] | t/T: theme [%s]", ignoreStatus, m.theme.Current.Name)
	line2 := "enter: select | ?: help | q: quit"
	info := line1 + "\n" + line2
	return footerStyle.Width(m.width).Render(info)
}

func tick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// buildTree recursively builds a file tree with git diff tracking
func buildTree(rootPath string) *tree.Tree {
	return buildTreeRecursive(rootPath, "", nil, nil, false)
}

// buildTreeWithCache builds a file tree using cached git diff data
func buildTreeWithCache(rootPath string, diffCache map[string]int) *tree.Tree {
	return buildTreeRecursive(rootPath, "", diffCache, nil, false)
}

// buildTreeWithOptions builds a file tree with all options
func buildTreeWithOptions(rootPath string, diffCache map[string]int, gitignore *internal.GitIgnore, respectIgnore bool) *tree.Tree {
	return buildTreeRecursive(rootPath, "", diffCache, gitignore, respectIgnore)
}

// buildTreeWithMap builds tree and returns a map of line numbers to file paths
func buildTreeWithMap(rootPath string, diffCache map[string]int, gitignore *internal.GitIgnore, respectIgnore bool) (*tree.Tree, map[int]string) {
	fileMap := make(map[int]string)
	lineNum := 1 // Start at 1 because the root directory takes line 0
	t := buildTreeRecursiveWithMap(rootPath, "", diffCache, gitignore, respectIgnore, &lineNum, fileMap)
	return t, fileMap
}

// renderTreeWithSelection renders tree with highlighted selected line
func renderTreeWithSelection(content string, selectedLine int) string {
	lines := strings.Split(content, "\n")
	if selectedLine >= 0 && selectedLine < len(lines) {
		// Highlight selected line with inverse colors
		highlightStyle := lipgloss.NewStyle().Reverse(true)
		lines[selectedLine] = highlightStyle.Render(lines[selectedLine])
	}
	return strings.Join(lines, "\n")
}

// renderTreeWithSelectionOptimized works with cached lines for better performance
func renderTreeWithSelectionOptimized(lines []string, selectedLine int) string {
	if len(lines) == 0 {
		return ""
	}

	if selectedLine < 0 || selectedLine >= len(lines) {
		return strings.Join(lines, "\n")
	}

	// Make a copy to avoid modifying the cached lines
	result := make([]string, len(lines))
	copy(result, lines)

	// Highlight selected line
	highlightStyle := lipgloss.NewStyle().Reverse(true)
	result[selectedLine] = highlightStyle.Render(lines[selectedLine])

	return strings.Join(result, "\n")
}


func buildTreeRecursiveWithMap(path string, relativePath string, diffCache map[string]int, gitignore *internal.GitIgnore, respectIgnore bool, lineNum *int, fileMap map[int]string) *tree.Tree {
	dirName := filepath.Base(path)
	t := tree.Root(dirName)

	entries, err := os.ReadDir(path)
	if err != nil {
		return t
	}

	for _, entry := range entries {
		fullPath := filepath.Join(path, entry.Name())
		relPath := filepath.Join(relativePath, entry.Name())

		// Always skip .git directory
		if entry.Name() == ".git" {
			continue
		}

		// Skip hidden files (except .gitignore)
		if strings.HasPrefix(entry.Name(), ".") && entry.Name() != ".gitignore" {
			continue
		}

		// Check gitignore if enabled
		if respectIgnore && gitignore != nil && gitignore.IsIgnored(fullPath) {
			continue
		}

		if entry.IsDir() {
			// Count the directory line
			*lineNum++
			// Recursively build subtree
			subTree := buildTreeRecursiveWithMap(fullPath, relPath, diffCache, gitignore, respectIgnore, lineNum, fileMap)
			t.Child(subTree)
		} else {
			// Track file in map at current line number
			fileMap[*lineNum] = relPath
			*lineNum++

			// Get git diff lines from cache
			var diffLines int
			if diffCache != nil {
				diffLines = diffCache[relPath]
			}

			// Normal style for filename
			fileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
			name := fileStyle.Render(entry.Name())

			// Add diff indicator if file has changes
			if diffLines > 0 {
				diffStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
				name = name + diffStyle.Render(fmt.Sprintf(" (+%d)", diffLines))
			}

			t.Child(name)
		}
	}

	return t
}

func buildTreeRecursive(path string, relativePath string, diffCache map[string]int, gitignore *internal.GitIgnore, respectIgnore bool) *tree.Tree {
	dirName := filepath.Base(path)
	t := tree.Root(dirName)

	entries, err := os.ReadDir(path)
	if err != nil {
		return t
	}

	for _, entry := range entries {
		fullPath := filepath.Join(path, entry.Name())
		relPath := filepath.Join(relativePath, entry.Name())

		// Always skip .git directory
		if entry.Name() == ".git" {
			continue
		}

		// Skip hidden files (except .gitignore)
		if strings.HasPrefix(entry.Name(), ".") && entry.Name() != ".gitignore" {
			continue
		}

		// Check gitignore if enabled
		if respectIgnore && gitignore != nil && gitignore.IsIgnored(fullPath) {
			continue
		}

		if entry.IsDir() {
			// Recursively build subtree
			subTree := buildTreeRecursive(fullPath, relPath, diffCache, gitignore, respectIgnore)
			t.Child(subTree)
		} else {
			// Get git diff lines from cache
			var diffLines int
			if diffCache != nil {
				diffLines = diffCache[relPath]
			}

			// Normal style for filename
			fileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
			name := fileStyle.Render(entry.Name())

			// Add diff indicator if file has changes
			if diffLines > 0 {
				diffStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
				name = name + diffStyle.Render(fmt.Sprintf(" (+%d)", diffLines))
			}

			t.Child(name)
		}
	}

	return t
}

// generateSessionID creates a unique session ID based on the current directory
func generateSessionID(path string) string {
	// Use absolute path to ensure consistency
	absPath, _ := filepath.Abs(path)
	// Create a short hash of the path
	cmd := exec.Command("sh", "-c", fmt.Sprintf("echo \"%s\" | shasum -a 256 | cut -c1-8", absPath))
	output, err := cmd.Output()
	if err != nil {
		// Fallback to a simpler method if shasum fails
		// Use last 8 chars of path as a simple ID
		if len(absPath) > 8 {
			return strings.ReplaceAll(absPath[len(absPath)-8:], "/", "_")
		}
		return "default"
	}
	return strings.TrimSpace(string(output))
}

func main() {
	// Get watch path from args or use current directory
	watchPath := "."
	if len(os.Args) > 1 {
		watchPath = os.Args[1]
	}

	// Get absolute path for everything
	absPath, _ := filepath.Abs(watchPath)
	watchPath = absPath  // Use absolute path everywhere

	// Generate unique session ID for this directory
	sessionID := generateSessionID(absPath)

	// Build the viewer command
	viewerCmd := fmt.Sprintf("vinw-viewer %s", sessionID)

	// Print session info to terminal (copyable)
	fmt.Printf("vinw session started\n")
	fmt.Printf("Directory: %s\n", absPath)
	fmt.Printf("Session ID: %s\n", sessionID)
	fmt.Printf("\nTo open viewer, run this command in another terminal:\n")
	fmt.Printf("%s\n", viewerCmd)

	// Try to copy to clipboard
	copyCmd := exec.Command("pbcopy")
	copyCmd.Stdin = strings.NewReader(viewerCmd)
	if err := copyCmd.Run(); err == nil {
		fmt.Printf("\n✓ Command copied to clipboard! Just paste in a new terminal.\n")
	}
	fmt.Printf("\nStarting vinw...\n\n")

	// Initialize theme manager with session ID FIRST
	themeManager := internal.NewThemeManagerWithSession(sessionID)
	themeManager.BroadcastTheme() // Broadcast initial theme to viewer

	// Initialize GitHub repo if needed (only on first run for this directory)
	if err := internal.InitGitHub(absPath); err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	// Load gitignore
	gitignore := internal.NewGitIgnore(watchPath)

	// Get initial git diff cache
	initialDiffCache := internal.GetAllGitDiffs()

	// Build initial tree with gitignore support (default: ON)
	respectIgnore := true
	tree, fileMap := buildTreeWithMap(watchPath, initialDiffCache, gitignore, respectIgnore)

	// Initialize model
	m := model{
		rootPath:      watchPath,
		tree:          tree,
		diffCache:     initialDiffCache,
		gitignore:     gitignore,
		respectIgnore: respectIgnore,
		selectedLine:  0,
		fileMap:       fileMap,
		theme:         themeManager,
		sessionID:     sessionID,
		showStartup:   true,  // Show startup screen until user presses a key
	}

	// Initialize the cache
	m.updateTreeCache()
	initialContent := renderTreeWithSelectionOptimized(m.treeLines, 0)
	m.lastContent = initialContent

	// Run with fullscreen and mouse support
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
