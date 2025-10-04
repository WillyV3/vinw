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
	viewport       viewport.Model
	ready          bool
	width          int
	height         int
	diffCache      map[string]int    // Cache for git diff results
	lastContent    string            // Track last content to avoid unnecessary updates
	gitignore      *GitIgnore        // GitIgnore patterns
	respectIgnore  bool              // Whether to respect .gitignore
	selectedLine   int               // Currently selected line in viewport
	fileMap        map[int]string    // Map of line number to file path
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
			content := renderTreeWithSelection(m.tree.String(), m.selectedLine)
			m.viewport.SetContent(content)
			m.lastContent = content
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMargins
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "i":
			// Toggle gitignore respect
			m.respectIgnore = !m.respectIgnore
			// Rebuild tree with new ignore setting - force update
			m.tree, m.fileMap = buildTreeWithMap(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore)
			newContent := renderTreeWithSelection(m.tree.String(), m.selectedLine)
			m.viewport.SetContent(newContent)
			m.lastContent = newContent
			// Maintain scroll position if possible
			return m, nil
		case "j", "down":
			// Move selection down
			if m.selectedLine < len(m.fileMap)-1 {
				m.selectedLine++
				// Update viewport with highlighted line
				content := renderTreeWithSelection(m.tree.String(), m.selectedLine)
				m.viewport.SetContent(content)
				// Auto-scroll if needed
				if m.selectedLine >= m.viewport.YOffset+m.viewport.Height-1 {
					m.viewport.LineDown(1)
				}
			}
			return m, nil
		case "k", "up":
			// Move selection up
			if m.selectedLine > 0 {
				m.selectedLine--
				// Update viewport with highlighted line
				content := renderTreeWithSelection(m.tree.String(), m.selectedLine)
				m.viewport.SetContent(content)
				// Auto-scroll if needed
				if m.selectedLine < m.viewport.YOffset {
					m.viewport.LineUp(1)
				}
			}
			return m, nil
		case "enter":
			// Get the file at the selected line
			if filePath, ok := m.fileMap[m.selectedLine]; ok {
				fullPath := filepath.Join(m.rootPath, filePath)
				// Write to Skate for server to pick up
				cmd := exec.Command("skate", "set", "vinw-current-file", fullPath)
				if err := cmd.Run(); err != nil {
					// Debug: print error if any
					fmt.Printf("Error writing to skate: %v\n", err)
				}
			}
			return m, nil
		}

	case tickMsg:
		// Update git diff cache efficiently with one call
		m.diffCache = getAllGitDiffs()

		// Rebuild tree with cached diff data and gitignore settings
		m.tree, m.fileMap = buildTreeWithMap(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore)

		// Only update viewport if content has changed
		newContent := renderTreeWithSelection(m.tree.String(), m.selectedLine)
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
	return fmt.Sprintf("%s\n%s\n%s", m.headerView(), m.viewport.View(), m.footerView())
}

func (m model) headerView() string {
	title := fmt.Sprintf("Vinw - %s", m.rootPath)
	return headerStyle.Width(m.width).Render(title)
}

func (m model) footerView() string {
	ignoreStatus := "OFF"
	if m.respectIgnore {
		ignoreStatus = "ON"
	}
	info := fmt.Sprintf("↑/↓: scroll | i: gitignore [%s] | q: quit", ignoreStatus)
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
func buildTreeWithOptions(rootPath string, diffCache map[string]int, gitignore *GitIgnore, respectIgnore bool) *tree.Tree {
	return buildTreeRecursive(rootPath, "", diffCache, gitignore, respectIgnore)
}

// buildTreeWithMap builds tree and returns a map of line numbers to file paths
func buildTreeWithMap(rootPath string, diffCache map[string]int, gitignore *GitIgnore, respectIgnore bool) (*tree.Tree, map[int]string) {
	fileMap := make(map[int]string)
	lineNum := 0
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


func buildTreeRecursiveWithMap(path string, relativePath string, diffCache map[string]int, gitignore *GitIgnore, respectIgnore bool, lineNum *int, fileMap map[int]string) *tree.Tree {
	dirName := filepath.Base(path)
	t := tree.Root(dirName)

	// Don't count the root directory line
	if relativePath != "" {
		*lineNum++ // Count the directory line itself
	}

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
			subTree := buildTreeRecursiveWithMap(fullPath, relPath, diffCache, gitignore, respectIgnore, lineNum, fileMap)
			t.Child(subTree)
		} else {
			// Track file in map BEFORE incrementing line number
			// The current line (*lineNum) is where this file appears
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

func buildTreeRecursive(path string, relativePath string, diffCache map[string]int, gitignore *GitIgnore, respectIgnore bool) *tree.Tree {
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
			// Get git diff lines from cache or fall back to individual call
			var diffLines int
			if diffCache != nil {
				diffLines = diffCache[relPath]
			} else {
				// Fallback for initial load or when cache isn't available
				diffLines = getGitDiffLines(fullPath)
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

func main() {
	// Get watch path from args or use current directory
	watchPath := "."
	if len(os.Args) > 1 {
		watchPath = os.Args[1]
	}

	// Get absolute path for everything
	absPath, _ := filepath.Abs(watchPath)
	watchPath = absPath  // Use absolute path everywhere

	// Initialize GitHub repo if needed (only on first run for this directory)
	if err := initGitHub(absPath); err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	// Load gitignore
	gitignore := NewGitIgnore(watchPath)

	// Get initial git diff cache
	initialDiffCache := getAllGitDiffs()

	// Build initial tree with gitignore support (default: ON)
	respectIgnore := true
	tree, fileMap := buildTreeWithMap(watchPath, initialDiffCache, gitignore, respectIgnore)
	initialContent := renderTreeWithSelection(tree.String(), 0)

	// Initialize model
	m := model{
		rootPath:      watchPath,
		tree:          tree,
		diffCache:     initialDiffCache,
		lastContent:   initialContent,
		gitignore:     gitignore,
		respectIgnore: respectIgnore,
		selectedLine:  0,
		fileMap:       fileMap,
	}

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
